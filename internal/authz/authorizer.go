package authz

import (
	"errors"

	"SOJ/internal/auth"
)

var ErrForbidden = errors.New("permission denied")

type Subject struct {
	UserID int64
	Roles  []Role
}

func NewSubject(actor auth.Actor) Subject {
	roles := append([]Role(nil), actor.Roles...)
	if actor.Role != "" && !containsRole(roles, actor.Role) {
		roles = append(roles, actor.Role)
	}
	return Subject{UserID: actor.UserID, Roles: roles}
}

func (s Subject) Authenticated() bool {
	return s.UserID > 0
}

func (s Subject) HasRole(role Role) bool {
	return containsRole(s.Roles, role)
}

func (s Subject) Has(permission Permission) bool {
	for _, granted := range PermissionsForRoles(s.Roles) {
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

func containsRole(roles []Role, target Role) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}
