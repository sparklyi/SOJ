package user

import (
	"context"

	"SOJ/internal/auth"
	"SOJ/internal/authz"

	"github.com/jackc/pgx/v5/pgtype"
)

// ActorState is what the request middleware needs to decide whether a token
// still describes a usable session: the account's status, its current global
// roles, and the permissions those roles currently grant.
type ActorState struct {
	Status      string
	Roles       []auth.Role
	Permissions []authz.Permission
}

// ActorResolver resolves one account's status, global roles, and effective
// permissions in a single read. The middleware runs it on every authenticated
// request, so it must not be split into several round trips: roles and
// permissions are re-read per request precisely so grants, revocations, and
// matrix edits take effect immediately, and account status has to travel with
// them for the same reason.
type ActorResolver interface {
	ResolveActor(ctx context.Context, userID int64) (ActorState, error)
}

func (r *PostgresRoleRepository) ResolveActor(ctx context.Context, userID int64) (ActorState, error) {
	if userID <= 0 {
		return ActorState{}, ErrNotFound
	}
	rows, err := r.db.Query(ctx, `
		SELECT u.status, a.role_code, rp.permission_code
		FROM users AS u
		LEFT JOIN user_role_assignments AS a
			ON a.user_id = u.id AND a.revoked_at IS NULL
		LEFT JOIN role_permissions AS rp
			ON rp.role_code = a.role_code
		WHERE u.id = $1
		ORDER BY a.role_code, rp.permission_code
	`, userID)
	if err != nil {
		return ActorState{}, err
	}
	defer rows.Close()

	var (
		state       ActorState
		accumulator accessAccumulator
		found       bool
	)
	for rows.Next() {
		var (
			status         string
			roleCode       pgtype.Text
			permissionCode pgtype.Text
		)
		if err := rows.Scan(&status, &roleCode, &permissionCode); err != nil {
			return ActorState{}, err
		}
		found = true
		state.Status = status
		if err := accumulator.add(roleCode, permissionCode); err != nil {
			return ActorState{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return ActorState{}, err
	}
	if !found {
		return ActorState{}, ErrNotFound
	}
	access := accumulator.access()
	state.Roles = access.Roles
	state.Permissions = access.Permissions
	return state, nil
}
