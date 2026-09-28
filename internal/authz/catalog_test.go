package authz

import (
	"testing"
)

// catalogPermissions is the test's own copy of the directory, keyed by the Go
// constant identifier. It must match Catalog() one for one; a permission added
// to the production directory without being listed here fails the test.
var catalogPermissions = map[string]Permission{
	"PermissionAuditRead":            PermissionAuditRead,
	"PermissionContestCreate":        PermissionContestCreate,
	"PermissionContestJudge":         PermissionContestJudge,
	"PermissionContestManage":        PermissionContestManage,
	"PermissionContestManageAll":     PermissionContestManageAll,
	"PermissionContestRead":          PermissionContestRead,
	"PermissionJudgeInspect":         PermissionJudgeInspect,
	"PermissionProblemCreate":        PermissionProblemCreate,
	"PermissionProblemEditOwn":       PermissionProblemEditOwn,
	"PermissionProblemManageAll":     PermissionProblemManageAll,
	"PermissionProblemPublish":       PermissionProblemPublish,
	"PermissionProblemReview":        PermissionProblemReview,
	"PermissionProblemSubmitReview":  PermissionProblemSubmitReview,
	"PermissionRoleGrant":            PermissionRoleGrant,
	"PermissionRolePermissionManage": PermissionRolePermissionManage,
	"PermissionRoleRevoke":           PermissionRoleRevoke,
	"PermissionSubmissionRejudge":    PermissionSubmissionRejudge,
	"PermissionSystemManage":         PermissionSystemManage,
	"PermissionUserManage":           PermissionUserManage,
}

// nonDelegable is the design's full-access-only set: these may only be held via
// admin/root and the phase-2 matrix must not hand them to a scoped role.
var nonDelegable = map[Permission]struct{}{
	PermissionAuditRead:            {},
	PermissionRoleGrant:            {},
	PermissionRolePermissionManage: {},
	PermissionRoleRevoke:           {},
	PermissionSystemManage:         {},
	PermissionUserManage:           {},
}

func TestCatalogMatchesPermissionDirectory(t *testing.T) {
	catalog := Catalog()
	if len(catalog) != len(catalogPermissions) {
		t.Fatalf("Catalog() has %d entries, want %d", len(catalog), len(catalogPermissions))
	}

	seen := make(map[Permission]string, len(catalog))
	for _, spec := range catalog {
		ident, ok := catalogIdentFor(spec.Code)
		if !ok {
			t.Fatalf("Catalog() contains unknown permission %q", spec.Code)
		}
		if previous, duplicate := seen[spec.Code]; duplicate {
			t.Fatalf("permission %q appears twice (%s and %s)", spec.Code, previous, ident)
		}
		seen[spec.Code] = ident

		if spec.Consumer == "" {
			t.Errorf("%s has an empty Consumer", ident)
		}
		if want := expectedScope(spec.Code); spec.Scope != want {
			t.Errorf("%s scope = %q, want %q", ident, spec.Scope, want)
		}
		if _, restricted := nonDelegable[spec.Code]; spec.Delegable == restricted {
			t.Errorf("%s delegable = %v, want %v", ident, spec.Delegable, !restricted)
		}
	}

	for ident, permission := range catalogPermissions {
		if seen[permission] != ident {
			t.Errorf("%s is missing from Catalog()", ident)
		}
	}
}

func TestCatalogReturnsCopy(t *testing.T) {
	catalog := Catalog()
	if len(catalog) == 0 {
		t.Fatal("Catalog() is empty")
	}
	catalog[0].Consumer = "mutated"
	if Catalog()[0].Consumer == "mutated" {
		t.Fatal("Catalog() returned a shared slice")
	}
}

func TestCombosMatchSharedGateMatrix(t *testing.T) {
	want := map[string][]Permission{
		"problem.authoring.access": {
			PermissionProblemCreate,
			PermissionProblemReview,
			PermissionProblemManageAll,
		},
		"problem.review.queue": {
			PermissionProblemReview,
			PermissionProblemManageAll,
		},
		"problem.review.decide": {
			PermissionProblemReview,
			PermissionProblemPublish,
			PermissionProblemManageAll,
		},
	}

	got := Combos()
	if len(got) != len(want) {
		t.Fatalf("Combos() has %d entries, want %d", len(got), len(want))
	}
	for name, permissions := range want {
		gotPermissions, ok := got[name]
		if !ok {
			t.Errorf("Combos() is missing %q", name)
			continue
		}
		if !equalPermissions(gotPermissions, permissions) {
			t.Errorf("Combos()[%q] = %v, want %v", name, gotPermissions, permissions)
		}
		for _, permission := range permissions {
			if _, ok := catalogIdentFor(permission); !ok {
				t.Errorf("Combos()[%q] references %q, which is not in the directory", name, permission)
			}
		}
	}
}

func TestCombosReturnsCopy(t *testing.T) {
	combos := Combos()
	combos["problem.review.queue"][0] = PermissionUserManage
	if Combos()["problem.review.queue"][0] == PermissionUserManage {
		t.Fatal("Combos() returned a shared slice")
	}
}

func catalogIdentFor(code Permission) (string, bool) {
	for ident, permission := range catalogPermissions {
		if permission == code {
			return ident, true
		}
	}
	return "", false
}

// contestScoped mirrors the production rule: only these permissions are tied
// to a contest role. contest.create and contest.manage_all are global.
var contestScoped = map[Permission]struct{}{
	PermissionContestRead:   {},
	PermissionContestManage: {},
	PermissionContestJudge:  {},
}

func expectedScope(code Permission) PermissionScope {
	if _, ok := contestScoped[code]; ok {
		return ScopeContest
	}
	return ScopeGlobal
}
