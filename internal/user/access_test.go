package user

import (
	"strings"
	"testing"

	"SOJ/internal/auth"
	"SOJ/internal/authz"

	"github.com/jackc/pgx/v5/pgtype"
)

func validText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func TestAccessAccumulatorUnionsRolesAndPermissions(t *testing.T) {
	var accumulator accessAccumulator
	pairs := []struct{ role, permission string }{
		{"author", "problem.create"},
		{"author", "problem.edit_own"},
		{"reviewer", "problem.review"},
	}
	for _, pair := range pairs {
		if err := accumulator.add(validText(pair.role), validText(pair.permission)); err != nil {
			t.Fatalf("add(%q, %q) error = %v", pair.role, pair.permission, err)
		}
	}

	access := accumulator.access()
	if !equalRoles(access.Roles, []auth.Role{auth.RoleAuthor, auth.RoleReviewer}) {
		t.Fatalf("roles = %v, want [author reviewer]", access.Roles)
	}
	want := []authz.Permission{
		authz.PermissionProblemCreate,
		authz.PermissionProblemEditOwn,
		authz.PermissionProblemReview,
	}
	if !equalPermissionSet(access.Permissions, want) {
		t.Fatalf("permissions = %v, want %v", access.Permissions, want)
	}
}

func TestAccessAccumulatorDeduplicatesOverlappingPermissions(t *testing.T) {
	var accumulator accessAccumulator
	// Two roles can carry the same permission; the effective set must contain
	// it once.
	for _, pair := range []struct{ role, permission string }{
		{"author", "problem.create"},
		{"reviewer", "problem.create"},
		{"reviewer", "problem.review"},
	} {
		if err := accumulator.add(validText(pair.role), validText(pair.permission)); err != nil {
			t.Fatalf("add(%q, %q) error = %v", pair.role, pair.permission, err)
		}
	}
	access := accumulator.access()
	count := 0
	for _, permission := range access.Permissions {
		if permission == authz.PermissionProblemCreate {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("problem.create appears %d times in %v, want once", count, access.Permissions)
	}
}

func TestAccessAccumulatorExpandsFullAccess(t *testing.T) {
	for _, role := range []string{"admin", "root"} {
		var accumulator accessAccumulator
		// admin/root have no role_permissions rows, so the join yields a NULL
		// permission; the full-access expansion happens on the role alone.
		if err := accumulator.add(validText(role), pgtype.Text{}); err != nil {
			t.Fatalf("add(%q) error = %v", role, err)
		}
		access := accumulator.access()
		if !equalPermissionSet(access.Permissions, authz.AllPermissions()) {
			t.Fatalf("%s permissions = %v, want the whole directory", role, access.Permissions)
		}
	}
}

func TestAccessAccumulatorRejectsUnknownCodes(t *testing.T) {
	var accumulator accessAccumulator
	err := accumulator.add(validText("wizard"), pgtype.Text{})
	if err == nil || !strings.Contains(err.Error(), "invalid role assignment") {
		t.Fatalf("unknown role error = %v, want invalid role assignment", err)
	}

	err = accumulator.add(validText("author"), validText("problem.explode"))
	if err == nil || !strings.Contains(err.Error(), "invalid permission assignment") {
		t.Fatalf("unknown permission error = %v, want invalid permission assignment", err)
	}
}

func TestExpandFullAccessKeepsScopedSetSorted(t *testing.T) {
	got := expandFullAccess(
		[]auth.Role{auth.RoleAuthor},
		[]authz.Permission{authz.PermissionProblemSubmitReview, authz.PermissionProblemCreate, authz.PermissionProblemCreate},
	)
	want := []authz.Permission{authz.PermissionProblemCreate, authz.PermissionProblemSubmitReview}
	if !equalPermissionSet(got, want) {
		t.Fatalf("expandFullAccess() = %v, want %v", got, want)
	}
}

func equalPermissionSet(got, want []authz.Permission) bool {
	if len(got) != len(want) {
		return false
	}
	set := make(map[authz.Permission]struct{}, len(got))
	for _, permission := range got {
		set[permission] = struct{}{}
	}
	for _, permission := range want {
		if _, ok := set[permission]; !ok {
			return false
		}
	}
	return true
}
