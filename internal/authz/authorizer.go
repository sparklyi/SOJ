package authz

import (
	"errors"

	"SOJ/internal/auth"
)

var ErrForbidden = errors.New("permission denied")

// Subject is the authorization view of an actor: the resolved permission set.
// Roles are not consulted here. The resolver expands admin/root and joins the
// database's role_permissions rows before the request reaches this package.
type Subject struct {
	UserID      int64
	Permissions []Permission
}

func NewSubject(actor auth.Actor) Subject {
	return Subject{UserID: actor.UserID, Permissions: append([]Permission(nil), actor.Permissions...)}
}

func (s Subject) Authenticated() bool {
	return s.UserID > 0
}

func (s Subject) Has(permission Permission) bool {
	for _, granted := range s.Permissions {
		if granted == permission {
			return true
		}
	}
	return false
}

func Authorize(subject Subject, permission Permission) error {
	if !subject.Authenticated() || !subject.Has(permission) {
		return ErrForbidden
	}
	return nil
}
