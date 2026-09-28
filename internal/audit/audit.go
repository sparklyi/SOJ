// Package audit records administrative actions: who did what to which object,
// when. Writes go through Insert so the action vocabulary and the table shape
// stay in one place; reads back the admin console's audit surface.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Action is the stable code of one administrative action.
type Action string

const (
	ActionUserRoleGranted        Action = "user.role.granted"
	ActionUserRoleRevoked        Action = "user.role.revoked"
	ActionUserDisabled           Action = "user.disabled"
	ActionUserEnabled            Action = "user.enabled"
	ActionUserDeleted            Action = "user.deleted"
	ActionLanguageEnabled        Action = "language.enabled"
	ActionLanguageDisabled       Action = "language.disabled"
	ActionProblemArchived        Action = "problem.archived"
	ActionProblemRestored        Action = "problem.restored"
	ActionContestArchived        Action = "contest.archived"
	ActionRolePermissionsUpdated Action = "role.permissions.updated"
)

// ObjectType names the kind of object an action targets.
type ObjectType string

const (
	ObjectUser     ObjectType = "user"
	ObjectLanguage ObjectType = "language"
	ObjectProblem  ObjectType = "problem"
	ObjectContest  ObjectType = "contest"
	ObjectRole     ObjectType = "role"
)

// Event is one administrative action. ActorUserID identifies the operator;
// zero means the action had no operator (for example the role granted during
// registration) and is stored as NULL. Metadata carries action-specific fields
// (for example the granted role); Reason is the operator-supplied explanation
// when the action requires one.
type Event struct {
	ActorUserID int64
	Action      Action
	ObjectType  ObjectType
	ObjectID    int64
	Reason      string
	Metadata    map[string]string
}

// Execer is the narrow database surface needed to append an event. It is
// satisfied by pgx.Tx, which is how callers make the event atomic with the
// mutation it describes.
type Execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// Insert appends the event. Except for ActorUserID being set, invalid events
// are programming errors and are rejected instead of silently recording a
// half-described action.
func Insert(ctx context.Context, db Execer, event Event) error {
	if err := event.validate(); err != nil {
		return err
	}
	metadata := []byte("{}")
	if len(event.Metadata) > 0 {
		encoded, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("encode audit metadata: %w", err)
		}
		metadata = encoded
	}
	_, err := db.Exec(ctx, `
		INSERT INTO audit_events (actor_user_id, action, object_type, object_id, reason, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, nullableActorID(event.ActorUserID), string(event.Action), string(event.ObjectType), nullableObjectID(event.ObjectID), event.Reason, metadata)
	return err
}

var validActions = map[Action]struct{}{
	ActionUserRoleGranted:        {},
	ActionUserRoleRevoked:        {},
	ActionUserDisabled:           {},
	ActionUserEnabled:            {},
	ActionUserDeleted:            {},
	ActionLanguageEnabled:        {},
	ActionLanguageDisabled:       {},
	ActionProblemArchived:        {},
	ActionProblemRestored:        {},
	ActionContestArchived:        {},
	ActionRolePermissionsUpdated: {},
}

var validObjectTypes = map[ObjectType]struct{}{
	ObjectUser:     {},
	ObjectLanguage: {},
	ObjectProblem:  {},
	ObjectContest:  {},
	ObjectRole:     {},
}

func (a Action) Valid() bool {
	_, ok := validActions[a]
	return ok
}

func (o ObjectType) Valid() bool {
	_, ok := validObjectTypes[o]
	return ok
}

func (e Event) validate() error {
	if e.ActorUserID < 0 {
		return fmt.Errorf("audit actor must not be negative")
	}
	if !e.Action.Valid() {
		return fmt.Errorf("unknown audit action %q", e.Action)
	}
	if !e.ObjectType.Valid() {
		return fmt.Errorf("unknown audit object type %q", e.ObjectType)
	}
	if e.ObjectID < 0 {
		return fmt.Errorf("audit object id must not be negative")
	}
	if e.ObjectType == ObjectRole {
		// A role has no numeric id: the role code lives in metadata. Recording
		// a bogus object_id here would make the event lie about its target.
		if e.ObjectID != 0 {
			return fmt.Errorf("role audit object id must be zero")
		}
		return nil
	}
	if e.ObjectID == 0 {
		return fmt.Errorf("audit object id must be positive")
	}
	return nil
}

func nullableActorID(userID int64) any {
	if userID <= 0 {
		return nil
	}
	return userID
}

func nullableObjectID(objectID int64) any {
	if objectID <= 0 {
		return nil
	}
	return objectID
}
