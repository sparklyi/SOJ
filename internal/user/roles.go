package user

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"SOJ/internal/audit"
	"SOJ/internal/auth"
	"SOJ/internal/authz"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrRoleStoreUnavailable = errors.New("role store is not configured")
	ErrLastRoot             = errors.New("cannot remove the last root role")
	ErrProtectedRole        = errors.New("role is protected")
)

type RoleAssignment struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Role      auth.Role  `json:"role"`
	GrantedBy *int64     `json:"granted_by,omitempty"`
	GrantedAt time.Time  `json:"granted_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Access is one account's authorization state: the global roles it holds and
// the effective permission set resolved from those roles. Permissions already
// have admin/root expanded to the full directory and duplicates removed.
type Access struct {
	Roles       []auth.Role
	Permissions []authz.Permission
}

type RoleStore interface {
	ListAccess(context.Context, int64) (Access, error)
	ListAccessForUsers(context.Context, []int64) (map[int64]Access, error)
	GrantRole(context.Context, int64, auth.Role, *int64, string) (RoleAssignment, error)
	RevokeRole(context.Context, int64, auth.Role, int64, string) error
}

// RoleDB is the small database surface needed by the role repository. The
// transaction requirement keeps assignment and its audit event atomic.
type RoleDB interface {
	db.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRoleRepository struct {
	db RoleDB
}

func NewPostgresRoleRepository(database RoleDB) *PostgresRoleRepository {
	return &PostgresRoleRepository{db: database}
}

// knownPermissions is the directory lookup used to reject an unknown
// permission_code coming out of role_permissions. The directory is static, so
// the set is built once.
var knownPermissions = func() map[authz.Permission]struct{} {
	set := make(map[authz.Permission]struct{})
	for _, permission := range authz.AllPermissions() {
		set[permission] = struct{}{}
	}
	return set
}()

// accessAccumulator folds the (role_code, permission_code) rows of one account
// into an Access value. Rows are ordered by role_code, permission_code, so
// consecutive role codes collapse without a set.
type accessAccumulator struct {
	roles       []auth.Role
	permissions []authz.Permission
}

func (a *accessAccumulator) add(roleCode, permissionCode pgtype.Text) error {
	if roleCode.Valid {
		role, err := auth.ParseRole(roleCode.String)
		if err != nil {
			return fmt.Errorf("invalid role assignment: %w", err)
		}
		if !auth.IsGlobalRole(role) {
			return fmt.Errorf("invalid global role assignment %q", roleCode.String)
		}
		if len(a.roles) == 0 || a.roles[len(a.roles)-1] != role {
			a.roles = append(a.roles, role)
		}
	}
	if permissionCode.Valid {
		permission := authz.Permission(permissionCode.String)
		if _, ok := knownPermissions[permission]; !ok {
			return fmt.Errorf("invalid permission assignment %q", permissionCode.String)
		}
		a.permissions = append(a.permissions, permission)
	}
	return nil
}

func (a accessAccumulator) access() Access {
	return Access{Roles: a.roles, Permissions: expandFullAccess(a.roles, a.permissions)}
}

// expandFullAccess is the single place full-access roles are expanded. Any held
// admin/root role yields the whole directory; the joined set is otherwise
// deduplicated and sorted.
func expandFullAccess(roles []auth.Role, permissions []authz.Permission) []authz.Permission {
	for _, role := range roles {
		if authz.IsFullAccessRole(role) {
			return authz.AllPermissions()
		}
	}
	deduped := make([]authz.Permission, 0, len(permissions))
	seen := make(map[authz.Permission]struct{}, len(permissions))
	for _, permission := range permissions {
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		deduped = append(deduped, permission)
	}
	sort.Slice(deduped, func(i, j int) bool { return deduped[i] < deduped[j] })
	return deduped
}

// ListAccess resolves one account's roles and effective permissions. It shares
// the multi-user path so the join and full-access expansion stay in one place.
// A missing account returns an empty Access, matching the previous role lookup
// (callers fetch the account first).
func (r *PostgresRoleRepository) ListAccess(ctx context.Context, userID int64) (Access, error) {
	if userID <= 0 {
		return Access{}, ErrNotFound
	}
	access, err := r.ListAccessForUsers(ctx, []int64{userID})
	if err != nil {
		return Access{}, err
	}
	return access[userID], nil
}

// ListAccessForUsers resolves the roles and effective permissions of every
// requested user in one query. The admin user list needs them for a whole page,
// and asking per user would turn one page into N+1 queries.
func (r *PostgresRoleRepository) ListAccessForUsers(ctx context.Context, userIDs []int64) (map[int64]Access, error) {
	access := make(map[int64]Access, len(userIDs))
	if len(userIDs) == 0 {
		return access, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT u.id, a.role_code, rp.permission_code
		FROM users AS u
		LEFT JOIN user_role_assignments AS a
			ON a.user_id = u.id AND a.revoked_at IS NULL
		LEFT JOIN role_permissions AS rp
			ON rp.role_code = a.role_code
		WHERE u.id = ANY($1)
		ORDER BY u.id, a.role_code, rp.permission_code
	`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		currentUser int64
		accumulator accessAccumulator
	)
	flush := func() {
		access[currentUser] = accumulator.access()
		accumulator = accessAccumulator{}
	}
	for rows.Next() {
		var (
			userID         int64
			roleCode       pgtype.Text
			permissionCode pgtype.Text
		)
		if err := rows.Scan(&userID, &roleCode, &permissionCode); err != nil {
			return nil, err
		}
		if userID != currentUser {
			if currentUser != 0 {
				flush()
			}
			currentUser = userID
		}
		if err := accumulator.add(roleCode, permissionCode); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if currentUser != 0 {
		flush()
	}
	return access, nil
}

func (r *PostgresRoleRepository) GrantRole(ctx context.Context, userID int64, role auth.Role, grantedBy *int64, reason string) (RoleAssignment, error) {
	if userID <= 0 || !knownRole(role) {
		return RoleAssignment{}, ErrNotFound
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return RoleAssignment{}, errors.New("role grant reason is required")
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return RoleAssignment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	assignment, err := scanRoleAssignment(tx.QueryRow(ctx, `
		INSERT INTO user_role_assignments (user_id, role_code, granted_by)
		SELECT $1, $2, $3
		WHERE EXISTS (SELECT 1 FROM users WHERE id = $1)
		RETURNING id, user_id, role_code, granted_by, granted_at, revoked_at
	`, userID, string(role), nullableID(grantedBy)))
	if err != nil {
		return RoleAssignment{}, mapDBError(err)
	}
	if err := audit.Insert(ctx, tx, audit.Event{
		ActorUserID: actorUserID(grantedBy),
		Action:      audit.ActionUserRoleGranted,
		ObjectType:  audit.ObjectUser,
		ObjectID:    userID,
		Reason:      reason,
		Metadata:    map[string]string{"role": string(role)},
	}); err != nil {
		return RoleAssignment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoleAssignment{}, err
	}
	return assignment, nil
}

func (r *PostgresRoleRepository) RevokeRole(ctx context.Context, userID int64, role auth.Role, revokedBy int64, reason string) error {
	if userID <= 0 || revokedBy <= 0 || !knownRole(role) {
		return ErrNotFound
	}
	if role == auth.RoleUser {
		return ErrProtectedRole
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("role revoke reason is required")
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if role == auth.RoleRoot {
		rows, err := tx.Query(ctx, `
			SELECT id
			FROM user_role_assignments
			WHERE role_code = 'root' AND revoked_at IS NULL
			ORDER BY id
			FOR UPDATE
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	tag, err := tx.Exec(ctx, `
		UPDATE user_role_assignments
		SET revoked_at = now()
		WHERE user_id = $1 AND role_code = $2 AND revoked_at IS NULL
	`, userID, string(role))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if role == auth.RoleRoot {

		var roots int64
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			FROM user_role_assignments
			WHERE role_code = 'root' AND revoked_at IS NULL
		`).Scan(&roots); err != nil {
			return err
		}
		if roots == 0 {
			return ErrLastRoot
		}
	}
	if err := audit.Insert(ctx, tx, audit.Event{
		ActorUserID: revokedBy,
		Action:      audit.ActionUserRoleRevoked,
		ObjectType:  audit.ObjectUser,
		ObjectID:    userID,
		Reason:      reason,
		Metadata:    map[string]string{"role": string(role)},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func scanRoleAssignment(row pgx.Row) (RoleAssignment, error) {
	var (
		assignment RoleAssignment
		roleCode   string
		grantedBy  pgtype.Int8
		revokedAt  pgtype.Timestamptz
	)
	if err := row.Scan(
		&assignment.ID,
		&assignment.UserID,
		&roleCode,
		&grantedBy,
		&assignment.GrantedAt,
		&revokedAt,
	); err != nil {
		return RoleAssignment{}, err
	}
	role, err := auth.ParseRole(roleCode)
	if err != nil {
		return RoleAssignment{}, err
	}
	assignment.Role = role
	if grantedBy.Valid {
		assignment.GrantedBy = &grantedBy.Int64
	}
	if revokedAt.Valid {
		assignment.RevokedAt = &revokedAt.Time
	}
	return assignment, nil
}

func nullableID(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func actorUserID(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func knownRole(role auth.Role) bool {
	return auth.IsGlobalRole(role)
}
