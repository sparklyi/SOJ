package authz

import (
	"testing"

	"SOJ/internal/auth"
)

func TestPermissionsForRolesCombinesAndSorts(t *testing.T) {
	got := PermissionsForRoles([]Role{RoleAuthor, RoleUser})
	want := []Permission{
		PermissionContestJoin,
		PermissionProblemCheckOwn,
		PermissionProblemCreate,
		PermissionProblemEditOwn,
		PermissionProblemRead,
		PermissionProblemSubmitReview,
		PermissionProblemTestcaseOwn,
		PermissionSubmissionCreate,
		PermissionSubmissionReadOwn,
	}
	if !equalPermissions(got, want) {
		t.Fatalf("permissions = %v, want %v", got, want)
	}
}

func TestRootHasEveryPermission(t *testing.T) {
	got := PermissionsForRoles([]Role{RoleRoot})
	if !equalPermissions(got, AllPermissions()) {
		t.Fatalf("root permissions = %v, want %v", got, AllPermissions())
	}
}

func TestContestRolesExposeOnlyScopedPermissions(t *testing.T) {
	manager := PermissionsForRoles([]Role{RoleContestManager})
	if !equalPermissions(manager, []Permission{PermissionContestManage, PermissionContestRead}) {
		t.Fatalf("contest manager permissions = %v", manager)
	}
	judge := NewSubject(auth.Actor{UserID: 9, Roles: []auth.Role{auth.RoleContestJudge}})
	if err := Authorize(judge, PermissionContestJudge); err != nil {
		t.Fatalf("contest judge permission error = %v", err)
	}
	if err := Authorize(judge, PermissionContestManage); err != ErrForbidden {
		t.Fatalf("contest judge manage error = %v, want %v", err, ErrForbidden)
	}
}

func TestAuthorizeRequiresAuthenticatedSubjectAndPermission(t *testing.T) {
	author := NewSubject(auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleAuthor}})
	if err := Authorize(author, PermissionProblemCreate); err != nil {
		t.Fatalf("Authorize(author, create) error = %v", err)
	}
	if err := Authorize(author, PermissionProblemPublish); err != ErrForbidden {
		t.Fatalf("Authorize(author, publish) error = %v, want %v", err, ErrForbidden)
	}
	if err := Authorize(Subject{Roles: []Role{RoleRoot}}, PermissionSystemManage); err != ErrForbidden {
		t.Fatalf("Authorize(anonymous, system.manage) error = %v, want %v", err, ErrForbidden)
	}
}

func TestNewSubjectUsesOnlyAssignedRoles(t *testing.T) {
	subject := NewSubject(auth.Actor{
		UserID: 7,
		Role:   auth.RoleUser,
		Roles:  []auth.Role{auth.RoleAuthor},
	})
	if subject.HasRole(RoleUser) || !subject.HasRole(RoleAuthor) {
		t.Fatalf("subject roles = %v, want only assigned author role", subject.Roles)
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
