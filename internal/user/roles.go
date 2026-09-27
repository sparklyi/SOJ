package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SOJ/internal/audit"
	"SOJ/internal/auth"
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

type RoleStore interface {
	ListUserRoles(context.Context, int64) ([]auth.Role, error)
	ListUserRolesForUsers(context.Context, []int64) (map[int64][]auth.Role, error)
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

func (r *PostgresRoleRepository) ListUserRoles(ctx context.Context, userID int64) ([]auth.Role, error) {
	rows, err := r.db.Query(ctx, `
		SELECT role_code
		FROM user_role_assignments
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY role_code
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]auth.Role, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		role, err := auth.ParseRole(value)
		if err != nil {
			return nil, fmt.Errorf("invalid role assignment: %w", err)
		}
		if !auth.IsGlobalRole(role) {
			return nil, fmt.Errorf("invalid global role assignment %q", value)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

// ListUserRolesForUsers returns the active global roles of every requested user
// in one query. The admin user list needs roles for a whole page, and asking
// per user would turn one page into N+1 queries.
func (r *PostgresRoleRepository) ListUserRolesForUsers(ctx context.Context, userIDs []int64) (map[int64][]auth.Role, error) {
	roles := make(map[int64][]auth.Role, len(userIDs))
	if len(userIDs) == 0 {
		return roles, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT user_id, role_code
		FROM user_role_assignments
		WHERE user_id = ANY($1) AND revoked_at IS NULL
		ORDER BY user_id, role_code
	`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			userID   int64
			roleCode string
		)
		if err := rows.Scan(&userID, &roleCode); err != nil {
			return nil, err
		}
		role, err := auth.ParseRole(roleCode)
		if err != nil {
			return nil, fmt.Errorf("invalid role assignment: %w", err)
		}
		if !auth.IsGlobalRole(role) {
			return nil, fmt.Errorf("invalid global role assignment %q", roleCode)
		}
		roles[userID] = append(roles[userID], role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
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
