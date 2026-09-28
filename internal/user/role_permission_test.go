package user

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/authz"
)

type fakeRolePermissionStore struct {
	roles          []auth.Role
	byRole         map[auth.Role][]authz.Permission
	replaceCalls   int
	replacedRole   auth.Role
	replacedPerms  []authz.Permission
	replacedReason string
	replacedActor  int64
	changed        bool
	replaceErr     error
}

func (s *fakeRolePermissionStore) ListRoles(context.Context) ([]auth.Role, error) {
	return s.roles, nil
}

func (s *fakeRolePermissionStore) ListPermissions(context.Context) (map[auth.Role][]authz.Permission, error) {
	return s.byRole, nil
}

func (s *fakeRolePermissionStore) Replace(_ context.Context, role auth.Role, permissions []authz.Permission, actorID int64, reason string) (bool, error) {
	s.replaceCalls++
	s.replacedRole = role
	s.replacedPerms = permissions
	s.replacedReason = reason
	s.replacedActor = actorID
	return s.changed, s.replaceErr
}

func newRolePermissionService(store RolePermissionStore) *Service {
	return NewService(&memoryRepo{}, auth.NewJWTManager("secret", time.Minute), WithRolePermissionStore(store))
}

func rolePermissionManager() auth.Actor {
	return auth.Actor{UserID: 1, Permissions: seededPermissions(auth.RoleAdmin)}
}

func TestNormalizeRolePermissions(t *testing.T) {
	permissions, err := normalizeRolePermissions(auth.RoleAuthor, []string{
		"problem.submit_review",
		"problem.create",
		"problem.create",
	})
	if err != nil {
		t.Fatalf("normalizeRolePermissions() error = %v", err)
	}
	want := []authz.Permission{authz.PermissionProblemCreate, authz.PermissionProblemSubmitReview}
	if !equalSlice(permissions, want) {
		t.Fatalf("normalizeRolePermissions() = %v, want %v", permissions, want)
	}

	cleared, err := normalizeRolePermissions(auth.RoleAuthor, []string{})
	if err != nil {
		t.Fatalf("normalizeRolePermissions(empty) error = %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("normalizeRolePermissions(empty) = %v, want empty", cleared)
	}
}

func TestNormalizeRolePermissionsAllowsGlobalContestCapabilities(t *testing.T) {
	// contest.create and contest.manage_all are global capabilities even though
	// they carry the contest prefix; a global role may hold them.
	permissions, err := normalizeRolePermissions(auth.RoleAuthor, []string{"contest.create", "contest.manage_all"})
	if err != nil {
		t.Fatalf("normalizeRolePermissions(contest.create, contest.manage_all) error = %v", err)
	}
	if len(permissions) != 2 {
		t.Fatalf("permissions = %v, want both contest capabilities", permissions)
	}
}

func TestNormalizeRolePermissionsRejectsInvalidRules(t *testing.T) {
	tests := []struct {
		name  string
		role  auth.Role
		codes []string
		code  string
	}{
		{"unknown permission", auth.RoleAuthor, []string{"problem.explode"}, "role.permission_invalid"},
		{"not delegable", auth.RoleAuthor, []string{"audit.read"}, "role.permission_not_delegable"},
		{"global role with contest permission", auth.RoleAuthor, []string{"contest.read"}, "role.permission_scope_mismatch"},
		{"contest role with global permission", auth.RoleContestManager, []string{"problem.create"}, "role.permission_scope_mismatch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeRolePermissions(tc.role, tc.codes)
			if code := codeOfUserError(err); code != tc.code {
				t.Fatalf("error code = %q, want %q (err=%v)", code, tc.code, err)
			}
		})
	}
}

func TestReplaceRolePermissionsValidationChain(t *testing.T) {
	tests := []struct {
		name   string
		role   string
		input  ReplaceRolePermissionsInput
		code   string
		status int
	}{
		{
			name:   "role not found",
			role:   "wizard",
			input:  ReplaceRolePermissionsInput{Permissions: []string{}, Reason: "why"},
			code:   "role.not_found",
			status: http.StatusNotFound,
		},
		{
			name:   "admin locked",
			role:   "admin",
			input:  ReplaceRolePermissionsInput{Permissions: []string{}, Reason: "why"},
			code:   "role.locked",
			status: http.StatusConflict,
		},
		{
			name:   "root locked",
			role:   "root",
			input:  ReplaceRolePermissionsInput{Permissions: []string{"problem.create"}, Reason: "why"},
			code:   "role.locked",
			status: http.StatusConflict,
		},
		{
			name:   "permissions missing",
			role:   "author",
			input:  ReplaceRolePermissionsInput{Reason: "why"},
			code:   "role.permissions_required",
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown permission",
			role:   "author",
			input:  ReplaceRolePermissionsInput{Permissions: []string{"problem.explode"}, Reason: "why"},
			code:   "role.permission_invalid",
			status: http.StatusBadRequest,
		},
		{
			name:   "not delegable",
			role:   "author",
			input:  ReplaceRolePermissionsInput{Permissions: []string{"audit.read"}, Reason: "why"},
			code:   "role.permission_not_delegable",
			status: http.StatusBadRequest,
		},
		{
			name:   "scope mismatch",
			role:   "author",
			input:  ReplaceRolePermissionsInput{Permissions: []string{"contest.read"}, Reason: "why"},
			code:   "role.permission_scope_mismatch",
			status: http.StatusBadRequest,
		},
		{
			name:   "reason empty",
			role:   "author",
			input:  ReplaceRolePermissionsInput{Permissions: []string{"problem.create"}},
			code:   "role.permission_reason_required",
			status: http.StatusBadRequest,
		},
		{
			name:   "reason too long",
			role:   "author",
			input:  ReplaceRolePermissionsInput{Permissions: []string{"problem.create"}, Reason: strings.Repeat("x", 501)},
			code:   "role.permission_reason_required",
			status: http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := newRolePermissionService(&fakeRolePermissionStore{changed: true})
			_, err := service.ReplaceRolePermissions(context.Background(), rolePermissionManager(), tc.role, tc.input)
			appErr, ok := apperror.From(err)
			if !ok || appErr.Code != tc.code || appErr.HTTPStatus != tc.status {
				t.Fatalf("error = %v, want %s/%d", err, tc.code, tc.status)
			}
		})
	}
}

func TestReplaceRolePermissionsNormalizesAndReturnsView(t *testing.T) {
	store := &fakeRolePermissionStore{changed: true}
	service := newRolePermissionService(store)

	view, err := service.ReplaceRolePermissions(context.Background(), rolePermissionManager(), "author", ReplaceRolePermissionsInput{
		Permissions: []string{"problem.submit_review", "problem.create", "problem.create"},
		Reason:      "  seed author  ",
	})
	if err != nil {
		t.Fatalf("ReplaceRolePermissions() error = %v", err)
	}
	if store.replaceCalls != 1 || store.replacedRole != auth.RoleAuthor || store.replacedActor != 1 {
		t.Fatalf("store call = %+v, want one replace for author by actor 1", store)
	}
	if store.replacedReason != "seed author" {
		t.Fatalf("reason = %q, want trimmed", store.replacedReason)
	}
	if !equalSlice(store.replacedPerms, []authz.Permission{authz.PermissionProblemCreate, authz.PermissionProblemSubmitReview}) {
		t.Fatalf("store permissions = %v, want normalized and sorted", store.replacedPerms)
	}
	if view.Code != "author" || view.Scope != "global" || view.Locked {
		t.Fatalf("view = %+v, want unlocked global author", view)
	}
	if !equalStrings(view.Permissions, []string{"problem.create", "problem.submit_review"}) {
		t.Fatalf("view permissions = %v", view.Permissions)
	}
}

func TestReplaceRolePermissionsIdempotentUnchangedSet(t *testing.T) {
	// The store reports "nothing changed" for a no-op write; the service still
	// answers with the current role view, so a repeated PUT is idempotent.
	store := &fakeRolePermissionStore{changed: false}
	service := newRolePermissionService(store)

	view, err := service.ReplaceRolePermissions(context.Background(), rolePermissionManager(), "author", ReplaceRolePermissionsInput{
		Permissions: []string{"problem.create"},
		Reason:      "same again",
	})
	if err != nil {
		t.Fatalf("ReplaceRolePermissions() error = %v", err)
	}
	if store.replaceCalls != 1 {
		t.Fatalf("replace calls = %d, want 1", store.replaceCalls)
	}
	if !equalStrings(view.Permissions, []string{"problem.create"}) {
		t.Fatalf("view permissions = %v, want the unchanged set", view.Permissions)
	}
}

func TestRolePermissionMatrixReturnsCatalogAndRoleViews(t *testing.T) {
	store := &fakeRolePermissionStore{
		roles: []auth.Role{auth.RoleAuthor, auth.RoleContestManager, auth.RoleAdmin},
		byRole: map[auth.Role][]authz.Permission{
			auth.RoleAuthor:         {authz.PermissionProblemCreate},
			auth.RoleContestManager: {authz.PermissionContestRead, authz.PermissionContestManage},
		},
	}
	service := newRolePermissionService(store)

	matrix, err := service.RolePermissionMatrix(context.Background(), rolePermissionManager())
	if err != nil {
		t.Fatalf("RolePermissionMatrix() error = %v", err)
	}
	if len(matrix.Permissions) != len(authz.Catalog()) {
		t.Fatalf("permission catalog size = %d, want %d", len(matrix.Permissions), len(authz.Catalog()))
	}
	roles := make(map[string]RolePermissionView, len(matrix.Roles))
	for _, role := range matrix.Roles {
		roles[role.Code] = role
	}
	if roles["author"].Locked || roles["author"].Scope != "global" {
		t.Fatalf("author view = %+v, want unlocked global", roles["author"])
	}
	if roles["contest_manager"].Scope != "contest" || roles["contest_manager"].Locked {
		t.Fatalf("contest_manager view = %+v, want unlocked contest", roles["contest_manager"])
	}
	if roles["admin"].Locked == false || roles["admin"].Scope != "global" {
		t.Fatalf("admin view = %+v, want locked global", roles["admin"])
	}
	if len(roles["admin"].Permissions) != len(authz.AllPermissions()) {
		t.Fatalf("admin permissions = %v, want the whole directory", roles["admin"].Permissions)
	}
}

func TestRolePermissionMatrixRequiresManagePermission(t *testing.T) {
	service := newRolePermissionService(&fakeRolePermissionStore{})
	_, err := service.RolePermissionMatrix(context.Background(), auth.Actor{UserID: 7})
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != http.StatusForbidden || appErr.Code != "forbidden" {
		t.Fatalf("RolePermissionMatrix() error = %v, want 403 forbidden", err)
	}
}

func TestRolePermissionMatrixHandlerStatuses(t *testing.T) {
	tests := []struct {
		name    string
		service fakeService
		status  int
	}{
		{
			name: "unauthorized",
			service: fakeService{rolePermissionMatrix: func(context.Context, auth.Actor) (PermissionMatrix, error) {
				return PermissionMatrix{}, apperror.Unauthorized("unauthorized", "unauthorized")
			}},
			status: http.StatusUnauthorized,
		},
		{
			name: "forbidden",
			service: fakeService{rolePermissionMatrix: func(context.Context, auth.Actor) (PermissionMatrix, error) {
				return PermissionMatrix{}, apperror.Forbidden("forbidden", "missing")
			}},
			status: http.StatusForbidden,
		},
		{
			name: "success",
			service: fakeService{rolePermissionMatrix: func(context.Context, auth.Actor) (PermissionMatrix, error) {
				return PermissionMatrix{Roles: []RolePermissionView{}}, nil
			}},
			status: http.StatusOK,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := testRouter(tc.service)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestReplaceRolePermissionsHandlerStatuses(t *testing.T) {
	tests := []struct {
		name    string
		service fakeService
		body    string
		status  int
	}{
		{
			name:    "bad json",
			service: fakeService{},
			body:    `{`,
			status:  http.StatusBadRequest,
		},
		{
			name: "bad request",
			service: fakeService{replaceRolePermissions: func(context.Context, auth.Actor, string, ReplaceRolePermissionsInput) (RolePermissionView, error) {
				return RolePermissionView{}, apperror.BadRequest("role.permissions_required", "permissions is required")
			}},
			body:   `{}`,
			status: http.StatusBadRequest,
		},
		{
			name: "forbidden",
			service: fakeService{replaceRolePermissions: func(context.Context, auth.Actor, string, ReplaceRolePermissionsInput) (RolePermissionView, error) {
				return RolePermissionView{}, apperror.Forbidden("forbidden", "missing")
			}},
			body:   `{"permissions":[],"reason":"why"}`,
			status: http.StatusForbidden,
		},
		{
			name: "success",
			service: fakeService{replaceRolePermissions: func(_ context.Context, _ auth.Actor, role string, _ ReplaceRolePermissionsInput) (RolePermissionView, error) {
				return RolePermissionView{Code: role, Scope: "global", Permissions: []string{}}, nil
			}},
			body:   `{"permissions":[],"reason":"clear"}`,
			status: http.StatusOK,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := testRouter(tc.service)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/admin/roles/author/permissions", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func equalSlice[T comparable](got, want []T) bool {
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

func equalStrings(got, want []string) bool {
	return equalSlice(got, want)
}
