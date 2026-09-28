package authz

import (
	"errors"
	"testing"

	"SOJ/internal/auth"
)

func TestFullAccessRolesBehaveAsPermitted(t *testing.T) {
	for _, role := range []Role{RoleAdmin, RoleRoot} {
		if !IsFullAccessRole(role) {
			t.Fatalf("IsFullAccessRole(%s) = false, want true", role)
		}
		// Full access is now an expansion done by the resolver, not a property
		// of the Subject. A Subject carrying the whole directory authorizes
		// every permission.
		subject := NewSubject(auth.Actor{UserID: 5, Roles: []auth.Role{role}, Permissions: AllPermissions()})
		for _, permission := range AllPermissions() {
			if err := Authorize(subject, permission); err != nil {
				t.Fatalf("Authorize(%s, %s) error = %v", role, permission, err)
			}
		}
	}
}

func TestAuthorizeRequiresAuthenticatedSubjectAndPermission(t *testing.T) {
	author := NewSubject(auth.Actor{
		UserID:      7,
		Roles:       []auth.Role{auth.RoleAuthor},
		Permissions: []Permission{PermissionProblemCreate},
	})
	if err := Authorize(author, PermissionProblemCreate); err != nil {
		t.Fatalf("Authorize(author, create) error = %v", err)
	}
	if err := Authorize(author, PermissionProblemPublish); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize(author, publish) error = %v, want %v", err, ErrForbidden)
	}
	if err := Authorize(Subject{Permissions: []Permission{PermissionSystemManage}}, PermissionSystemManage); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize(anonymous, system.manage) error = %v, want %v", err, ErrForbidden)
	}
}

func TestNewSubjectCopiesActorPermissionsWithoutRederiving(t *testing.T) {
	// The Subject must not consult Roles: a role set with no permissions is
	// denied. This is the fail-closed contract the resolver relies on.
	subject := NewSubject(auth.Actor{
		UserID: 7,
		Role:   auth.RoleUser,
		Roles:  []auth.Role{auth.RoleAuthor},
	})
	if subject.Has(PermissionProblemCreate) {
		t.Fatalf("subject without resolved permissions granted %s", PermissionProblemCreate)
	}

	granted := NewSubject(auth.Actor{
		UserID:      7,
		Permissions: []auth.Permission{PermissionProblemCreate},
	})
	if !granted.Has(PermissionProblemCreate) {
		t.Fatalf("subject permissions = %v, want problem.create", granted.Permissions)
	}
}

func equalPermissions(got, want []Permission) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
