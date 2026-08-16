package contest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type ContestRoleAssignment struct {
	ID        int64      `json:"id"`
	ContestID int64      `json:"contest_id"`
	UserID    int64      `json:"user_id"`
	Role      auth.Role  `json:"role"`
	GrantedBy *int64     `json:"granted_by,omitempty"`
	GrantedAt time.Time  `json:"granted_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type ContestRoleGrantInput struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
	Reason string `json:"reason"`
}

type ContestRoleRevokeInput struct {
	Reason string `json:"reason"`
}

type ContestRoleStore interface {
	ListContestIDs(context.Context, int64) ([]int64, error)
	ListContestRoles(context.Context, int64, int64) ([]auth.Role, error)
	GrantContestRole(context.Context, int64, int64, auth.Role, int64, string) (ContestRoleAssignment, error)
	RevokeContestRole(context.Context, int64, int64, auth.Role, int64, string) error
}

type contestRoleDB interface {
	db.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresContestRoleStore struct {
	db contestRoleDB
}

func NewPostgresContestRoleStore(database contestRoleDB) *PostgresContestRoleStore {
	return &PostgresContestRoleStore{db: database}
}

func (r *PostgresContestRoleStore) ListContestIDs(ctx context.Context, userID int64) ([]int64, error) {
	if userID <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT contest_id
		FROM contest_role_assignments
		WHERE user_id = $1 AND revoked_at IS NULL
		GROUP BY contest_id
		ORDER BY contest_id
	`, userID)
	if err != nil {
		return nil, mapContestRoleDBError(err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *PostgresContestRoleStore) ListContestRoles(ctx context.Context, contestID, userID int64) ([]auth.Role, error) {
	if contestID <= 0 || userID <= 0 {
		return nil, apperror.NotFound("contest.role_not_found", "contest role assignment not found")
	}
	rows, err := r.db.Query(ctx, `
		SELECT role_code
		FROM contest_role_assignments
		WHERE contest_id = $1 AND user_id = $2 AND revoked_at IS NULL
		ORDER BY role_code
	`, contestID, userID)
	if err != nil {
		return nil, mapContestRoleDBError(err)
	}
	defer rows.Close()

	roles := make([]auth.Role, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		role, err := auth.ParseRole(value)
		if err != nil || !auth.IsContestRole(role) {
			return nil, fmt.Errorf("invalid contest role assignment %q", value)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func (r *PostgresContestRoleStore) GrantContestRole(ctx context.Context, contestID, userID int64, role auth.Role, grantedBy int64, reason string) (ContestRoleAssignment, error) {
	if contestID <= 0 || userID <= 0 || grantedBy <= 0 || !auth.IsContestRole(role) {
		return ContestRoleAssignment{}, apperror.NotFound("contest.role_not_found", "contest role assignment target not found")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ContestRoleAssignment{}, apperror.BadRequest("contest.role_reason_required", "contest role change reason is required")
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return ContestRoleAssignment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	assignment, err := scanContestRoleAssignment(tx.QueryRow(ctx, `
		INSERT INTO contest_role_assignments (contest_id, user_id, role_code, granted_by)
		SELECT $1, $2, $3, $4
		WHERE EXISTS (SELECT 1 FROM contests WHERE id = $1)
		  AND EXISTS (SELECT 1 FROM users WHERE id = $2)
		RETURNING id, contest_id, user_id, role_code, granted_by, granted_at, revoked_at
	`, contestID, userID, string(role), grantedBy))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ContestRoleAssignment{}, apperror.NotFound("contest.role_target_not_found", "contest or user not found")
		}
		return ContestRoleAssignment{}, mapContestRoleDBError(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO contest_role_audit_events (contest_id, user_id, role_code, actor_user_id, action, reason)
		VALUES ($1, $2, $3, $4, 'granted', $5)
	`, contestID, userID, string(role), grantedBy, reason); err != nil {
		return ContestRoleAssignment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ContestRoleAssignment{}, err
	}
	return assignment, nil
}

func (r *PostgresContestRoleStore) RevokeContestRole(ctx context.Context, contestID, userID int64, role auth.Role, revokedBy int64, reason string) error {
	if contestID <= 0 || userID <= 0 || revokedBy <= 0 || !auth.IsContestRole(role) {
		return apperror.NotFound("contest.role_not_found", "contest role assignment not found")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return apperror.BadRequest("contest.role_reason_required", "contest role change reason is required")
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE contest_role_assignments
		SET revoked_at = now()
		WHERE contest_id = $1 AND user_id = $2 AND role_code = $3 AND revoked_at IS NULL
	`, contestID, userID, string(role))
	if err != nil {
		return mapContestRoleDBError(err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NotFound("contest.role_not_found", "contest role assignment not found")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO contest_role_audit_events (contest_id, user_id, role_code, actor_user_id, action, reason)
		VALUES ($1, $2, $3, $4, 'revoked', $5)
	`, contestID, userID, string(role), revokedBy, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanContestRoleAssignment(row pgx.Row) (ContestRoleAssignment, error) {
	var (
		assignment ContestRoleAssignment
		roleCode   string
		grantedBy  pgtype.Int8
		revokedAt  pgtype.Timestamptz
	)
	if err := row.Scan(
		&assignment.ID,
		&assignment.ContestID,
		&assignment.UserID,
		&roleCode,
		&grantedBy,
		&assignment.GrantedAt,
		&revokedAt,
	); err != nil {
		return ContestRoleAssignment{}, err
	}
	role, err := auth.ParseRole(roleCode)
	if err != nil || !auth.IsContestRole(role) {
		return ContestRoleAssignment{}, fmt.Errorf("invalid contest role assignment %q", roleCode)
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

func mapContestRoleDBError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return apperror.Conflict("contest.role_exists", "contest role is already assigned")
		case "23503":
			return apperror.NotFound("contest.role_target_not_found", "contest or user not found")
		}
	}
	return err
}
