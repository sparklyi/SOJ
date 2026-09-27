package user

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"SOJ/internal/apperror"
	"SOJ/internal/audit"
	"SOJ/internal/auth"
	"SOJ/internal/authz"
	"SOJ/internal/httpapi"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// RolePermissionStore reads and replaces the role→permission matrix. The
// replace path is transactional so the new set and its audit event land
// together; concurrent writers to the same role are last-write-wins.
type RolePermissionStore interface {
	ListRoles(ctx context.Context) ([]auth.Role, error)
	ListPermissions(ctx context.Context) (map[auth.Role][]authz.Permission, error)
	Replace(ctx context.Context, role auth.Role, permissions []authz.Permission, actorID int64, reason string) (bool, error)
}

type PostgresRolePermissionStore struct {
	db RoleDB
}

func NewPostgresRolePermissionStore(database RoleDB) *PostgresRolePermissionStore {
	return &PostgresRolePermissionStore{db: database}
}

func (s *PostgresRolePermissionStore) ListRoles(ctx context.Context) ([]auth.Role, error) {
	rows, err := s.db.Query(ctx, `SELECT code FROM roles ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]auth.Role, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		role, err := auth.ParseRole(code)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func (s *PostgresRolePermissionStore) ListPermissions(ctx context.Context) (map[auth.Role][]authz.Permission, error) {
	rows, err := s.db.Query(ctx, `
		SELECT role_code, permission_code
		FROM role_permissions
		ORDER BY role_code, permission_code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byRole := make(map[auth.Role][]authz.Permission)
	for rows.Next() {
		var roleCode, permissionCode string
		if err := rows.Scan(&roleCode, &permissionCode); err != nil {
			return nil, err
		}
		role, err := auth.ParseRole(roleCode)
		if err != nil {
			return nil, err
		}
		permission := authz.Permission(permissionCode)
		if _, ok := authz.Lookup(permission); !ok {
			return nil, apperror.Internal().WithDetails(map[string]string{"permission": permissionCode})
		}
		byRole[role] = append(byRole[role], permission)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return byRole, nil
}

// Replace swaps one role's whole permission set and records the change. It
// returns false (and writes nothing) when the normalized set already matches
// what is stored, which makes a repeated PUT idempotent.
func (s *PostgresRolePermissionStore) Replace(ctx context.Context, role auth.Role, permissions []authz.Permission, actorID int64, reason string) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := rolePermissionsTx(ctx, tx, role)
	if err != nil {
		return false, err
	}
	if equalPermissions(before, permissions) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_code = $1`, string(role)); err != nil {
		return false, err
	}
	if len(permissions) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_permissions (role_code, permission_code)
			SELECT $1, unnest($2::text[])
		`, string(role), permissionCodes(permissions)); err != nil {
			return false, err
		}
	}
	beforeJSON, err := json.Marshal(permissionCodes(before))
	if err != nil {
		return false, err
	}
	afterJSON, err := json.Marshal(permissionCodes(permissions))
	if err != nil {
		return false, err
	}
	if err := audit.Insert(ctx, tx, audit.Event{
		ActorUserID: actorID,
		Action:      audit.ActionRolePermissionsUpdated,
		ObjectType:  audit.ObjectRole,
		ObjectID:    0,
		Reason:      reason,
		Metadata: map[string]string{
			"role":   string(role),
			"before": string(beforeJSON),
			"after":  string(afterJSON),
		},
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func rolePermissionsTx(ctx context.Context, tx pgx.Tx, role auth.Role) ([]authz.Permission, error) {
	rows, err := tx.Query(ctx, `
		SELECT permission_code
		FROM role_permissions
		WHERE role_code = $1
		ORDER BY permission_code
	`, string(role))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := make([]authz.Permission, 0)
	for rows.Next() {
		var permissionCode string
		if err := rows.Scan(&permissionCode); err != nil {
			return nil, err
		}
		permissions = append(permissions, authz.Permission(permissionCode))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return permissions, nil
}

// PermissionMatrix is the GET /admin/roles payload: the whole permission
// directory plus every role's current set (admin/root synthesized full).
type PermissionMatrix struct {
	Permissions []PermissionSpecView `json:"permissions"`
	Roles       []RolePermissionView `json:"roles"`
}

type PermissionSpecView struct {
	Code      string `json:"code"`
	Scope     string `json:"scope"`
	Delegable bool   `json:"delegable"`
	Consumer  string `json:"consumer"`
}

type RolePermissionView struct {
	Code        string   `json:"code"`
	Scope       string   `json:"scope"`
	Locked      bool     `json:"locked"`
	Permissions []string `json:"permissions"`
}

type ReplaceRolePermissionsInput struct {
	// Permissions is a pointer-free slice so a missing/null field is
	// distinguishable from the legal empty array (which clears the role).
	Permissions []string `json:"permissions"`
	Reason      string   `json:"reason"`
}

// RolePermissionMatrix returns the directory and role sets for the admin
// matrix. It is gated by role.permission.manage, which only admin/root hold.
func (s *Service) RolePermissionMatrix(ctx context.Context, actor auth.Actor) (PermissionMatrix, error) {
	if err := authorize(actor, authz.PermissionRolePermissionManage); err != nil {
		return PermissionMatrix{}, err
	}
	if err := s.requireRolePermissionStore(); err != nil {
		return PermissionMatrix{}, err
	}
	roles, err := s.rolePermissions.ListRoles(ctx)
	if err != nil {
		return PermissionMatrix{}, err
	}
	byRole, err := s.rolePermissions.ListPermissions(ctx)
	if err != nil {
		return PermissionMatrix{}, err
	}
	specs := authz.Catalog()
	matrix := PermissionMatrix{Permissions: make([]PermissionSpecView, 0, len(specs))}
	for _, spec := range specs {
		matrix.Permissions = append(matrix.Permissions, PermissionSpecView{
			Code:      string(spec.Code),
			Scope:     string(spec.Scope),
			Delegable: spec.Delegable,
			Consumer:  spec.Consumer,
		})
	}
	matrix.Roles = make([]RolePermissionView, 0, len(roles))
	for _, role := range roles {
		matrix.Roles = append(matrix.Roles, newRolePermissionView(role, byRole[role]))
	}
	return matrix, nil
}

// ReplaceRolePermissions validates and applies one role's whole permission set.
// Validation is fail-closed: an unknown, non-delegable, or out-of-scope code
// rejects the entire request instead of partially applying it.
func (s *Service) ReplaceRolePermissions(ctx context.Context, actor auth.Actor, roleValue string, input ReplaceRolePermissionsInput) (RolePermissionView, error) {
	if err := authorize(actor, authz.PermissionRolePermissionManage); err != nil {
		return RolePermissionView{}, err
	}
	if input.Permissions == nil {
		return RolePermissionView{}, apperror.BadRequest("role.permissions_required", "permissions is required")
	}
	role, err := auth.ParseRole(roleValue)
	if err != nil {
		return RolePermissionView{}, apperror.NotFound("role.not_found", "role not found")
	}
	if authz.IsFullAccessRole(role) {
		return RolePermissionView{}, apperror.Conflict("role.locked", "this role's permissions are locked")
	}
	if err := s.requireRolePermissionStore(); err != nil {
		return RolePermissionView{}, err
	}
	permissions, err := normalizeRolePermissions(role, input.Permissions)
	if err != nil {
		return RolePermissionView{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" || len(reason) > 500 {
		return RolePermissionView{}, apperror.BadRequest("role.permission_reason_required", "reason is required and must be at most 500 bytes")
	}
	if _, err := s.rolePermissions.Replace(ctx, role, permissions, actor.UserID, reason); err != nil {
		return RolePermissionView{}, err
	}
	return newRolePermissionView(role, permissions), nil
}

func normalizeRolePermissions(role auth.Role, codes []string) ([]authz.Permission, error) {
	wantScope := roleScope(role)
	seen := make(map[authz.Permission]struct{}, len(codes))
	permissions := make([]authz.Permission, 0, len(codes))
	for _, code := range codes {
		permission := authz.Permission(code)
		spec, ok := authz.Lookup(permission)
		if !ok {
			return nil, apperror.BadRequest("role.permission_invalid", "unknown permission "+code)
		}
		if !spec.Delegable {
			return nil, apperror.BadRequest("role.permission_not_delegable", "permission cannot be delegated: "+code)
		}
		if spec.Scope != wantScope {
			return nil, apperror.BadRequest("role.permission_scope_mismatch", "permission scope does not match the role: "+code)
		}
		if _, dup := seen[permission]; dup {
			continue
		}
		seen[permission] = struct{}{}
		permissions = append(permissions, permission)
	}
	sort.Slice(permissions, func(i, j int) bool { return permissions[i] < permissions[j] })
	return permissions, nil
}

func newRolePermissionView(role auth.Role, permissions []authz.Permission) RolePermissionView {
	locked := authz.IsFullAccessRole(role)
	if locked {
		permissions = authz.AllPermissions()
	}
	return RolePermissionView{
		Code:        string(role),
		Scope:       string(roleScope(role)),
		Locked:      locked,
		Permissions: permissionCodes(permissions),
	}
}

func roleScope(role auth.Role) authz.PermissionScope {
	// Scope is derived from the role code, not the stored roles.scope column:
	// the code is part of the compile-time role vocabulary and cannot drift
	// from the permission directory.
	if auth.IsContestRole(role) {
		return authz.ScopeContest
	}
	return authz.ScopeGlobal
}

func permissionCodes(permissions []authz.Permission) []string {
	codes := make([]string, len(permissions))
	for i, permission := range permissions {
		codes[i] = string(permission)
	}
	sort.Strings(codes)
	return codes
}

func equalPermissions(a, b []authz.Permission) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RolePermissionMatrix returns the admin permission matrix.
func (h *Handler) RolePermissionMatrix(c *gin.Context) {
	matrix, err := h.service.RolePermissionMatrix(c.Request.Context(), actorFromGin(c))
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, matrix)
}

// ReplaceRolePermissions replaces one role's permission set.
func (h *Handler) ReplaceRolePermissions(c *gin.Context) {
	var input ReplaceRolePermissionsInput
	if !bindJSON(c, &input) {
		return
	}
	view, err := h.service.ReplaceRolePermissions(c.Request.Context(), actorFromGin(c), c.Param("role"), input)
	if err != nil {
		httpapi.Error(c, err)
		return
	}
	httpapi.OK(c, view)
}
