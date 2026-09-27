package user

import (
	"context"
	"fmt"

	"SOJ/internal/auth"

	"github.com/jackc/pgx/v5/pgtype"
)

// ActorState is what the request middleware needs to decide whether a token
// still describes a usable session: the account's status and its current
// global roles.
type ActorState struct {
	Status string
	Roles  []auth.Role
}

// ActorResolver resolves one account's status and global roles in a single
// read. The middleware runs it on every authenticated request, so it must not
// be split into two round trips: roles are re-read per request precisely so
// grants and revocations take effect immediately, and account status has to
// travel with them for the same reason.
type ActorResolver interface {
	ResolveActor(ctx context.Context, userID int64) (ActorState, error)
}

func (r *PostgresRoleRepository) ResolveActor(ctx context.Context, userID int64) (ActorState, error) {
	if userID <= 0 {
		return ActorState{}, ErrNotFound
	}
	rows, err := r.db.Query(ctx, `
		SELECT u.status, a.role_code
		FROM users AS u
		LEFT JOIN user_role_assignments AS a
			ON a.user_id = u.id AND a.revoked_at IS NULL
		WHERE u.id = $1
		ORDER BY a.role_code
	`, userID)
	if err != nil {
		return ActorState{}, err
	}
	defer rows.Close()

	var state ActorState
	found := false
	for rows.Next() {
		var (
			status   string
			roleCode pgtype.Text
		)
		if err := rows.Scan(&status, &roleCode); err != nil {
			return ActorState{}, err
		}
		found = true
		state.Status = status
		if !roleCode.Valid {
			continue
		}
		role, err := auth.ParseRole(roleCode.String)
		if err != nil {
			return ActorState{}, fmt.Errorf("invalid role assignment: %w", err)
		}
		if !auth.IsGlobalRole(role) {
			return ActorState{}, fmt.Errorf("invalid global role assignment %q", roleCode.String)
		}
		state.Roles = append(state.Roles, role)
	}
	if err := rows.Err(); err != nil {
		return ActorState{}, err
	}
	if !found {
		return ActorState{}, ErrNotFound
	}
	return state, nil
}
