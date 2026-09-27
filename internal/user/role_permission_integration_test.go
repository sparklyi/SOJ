package user

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/authz"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires a disposable Postgres database seeded by the repository migrations:
//
//	SOJ_TEST_DATABASE_DSN=postgres://... go test ./internal/user/...
func newUserIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SOJ_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SOJ_TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return pool
}

func insertIntegrationUser(t *testing.T, pool *pgxpool.Pool, userID int64, name string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password_hash, username, status)
		VALUES ($1, $2, 'hash', $3, 'active')
	`, userID, fmt.Sprintf("%s-%d@example.test", name, userID), fmt.Sprintf("%s-%d", name, userID)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func TestIntegrationRolePermissionSeedMatchesDirectory(t *testing.T) {
	pool := newUserIntegrationPool(t)
	ctx := context.Background()

	want := map[auth.Role][]string{
		auth.RoleAuthor:         {"problem.create", "problem.edit_own", "problem.submit_review"},
		auth.RoleReviewer:       {"problem.publish", "problem.review"},
		auth.RoleOperator:       {"judge.inspect", "submission.rejudge"},
		auth.RoleContestStaff:   {"contest.read"},
		auth.RoleContestManager: {"contest.manage", "contest.read"},
		auth.RoleContestJudge:   {"contest.judge", "contest.read"},
	}
	rows, err := pool.Query(ctx, `SELECT role_code, permission_code FROM role_permissions ORDER BY role_code, permission_code`)
	if err != nil {
		t.Fatalf("query role_permissions: %v", err)
	}
	defer rows.Close()

	got := make(map[auth.Role][]string)
	for rows.Next() {
		var roleCode, permissionCode string
		if err := rows.Scan(&roleCode, &permissionCode); err != nil {
			t.Fatalf("scan role_permissions: %v", err)
		}
		// Every stored code must be in the Go directory; a code removed from
		// the directory without a migration would otherwise dangle here.
		if _, ok := authz.Lookup(authz.Permission(permissionCode)); !ok {
			t.Fatalf("role_permissions contains %q, which is not in the directory", permissionCode)
		}
		role, err := auth.ParseRole(roleCode)
		if err != nil {
			t.Fatalf("role_permissions has unknown role %q", roleCode)
		}
		got[role] = append(got[role], permissionCode)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate role_permissions: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("seeded roles = %v, want %v", got, want)
	}
	for role, wantCodes := range want {
		if !equalStrings(got[role], wantCodes) {
			t.Fatalf("seed for %s = %v, want %v", role, got[role], wantCodes)
		}
	}
	for _, role := range []auth.Role{auth.RoleUser, auth.RoleAdmin, auth.RoleRoot} {
		if _, ok := got[role]; ok {
			t.Fatalf("%s must not have role_permissions rows", role)
		}
	}
}

func TestIntegrationReplaceRolePermissionsWritesAuditOnce(t *testing.T) {
	pool := newUserIntegrationPool(t)
	ctx := context.Background()
	store := NewPostgresRolePermissionStore(pool)
	userID := (time.Now().UnixNano() & ((1 << 50) - 1)) * 16
	insertIntegrationUser(t, pool, userID, "it-matrix")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	original, err := store.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("ListPermissions(): %v", err)
	}
	t.Cleanup(func() {
		// Restore the migration seed so later assertions and other tests see it.
		_, _ = store.Replace(context.Background(), auth.RoleOperator, original[auth.RoleOperator], userID, "restore")
	})

	changed, err := store.Replace(ctx, auth.RoleOperator, []authz.Permission{authz.PermissionJudgeInspect}, userID, "narrow operator")
	if err != nil {
		t.Fatalf("Replace(): %v", err)
	}
	if !changed {
		t.Fatal("Replace() changed = false, want true")
	}

	var stored []string
	rows, err := pool.Query(ctx, `SELECT permission_code FROM role_permissions WHERE role_code = 'operator' ORDER BY permission_code`)
	if err != nil {
		t.Fatalf("query operator rows: %v", err)
	}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		stored = append(stored, code)
	}
	rows.Close()
	if !equalStrings(stored, []string{"judge.inspect"}) {
		t.Fatalf("stored operator permissions = %v, want [judge.inspect]", stored)
	}

	var (
		action     string
		objectType string
		objectID   pgtype.Int8
		reason     string
		metadata   []byte
	)
	if err := pool.QueryRow(ctx, `
		SELECT action, object_type, object_id, reason, metadata
		FROM audit_events
		WHERE actor_user_id = $1 AND action = 'role.permissions.updated'
		ORDER BY id DESC
		LIMIT 1
	`, userID).Scan(&action, &objectType, &objectID, &reason, &metadata); err != nil {
		t.Fatalf("query audit event: %v", err)
	}
	if action != "role.permissions.updated" || objectType != "role" || objectID.Valid || reason != "narrow operator" {
		t.Fatalf("audit event = (%s, %s, %+v, %s), want role permissions update with NULL object id", action, objectType, objectID, reason)
	}
	var fields map[string]string
	if err := json.Unmarshal(metadata, &fields); err != nil {
		t.Fatalf("metadata is not JSON: %v", err)
	}
	if fields["role"] != "operator" || fields["before"] == "" || fields["after"] != `["judge.inspect"]` {
		t.Fatalf("metadata = %v, want role/before/after", fields)
	}

	// A second identical write is a no-op: no table change and no new audit row.
	var countBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_user_id = $1`, userID).Scan(&countBefore); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	changed, err = store.Replace(ctx, auth.RoleOperator, []authz.Permission{authz.PermissionJudgeInspect}, userID, "narrow operator")
	if err != nil {
		t.Fatalf("Replace() again: %v", err)
	}
	if changed {
		t.Fatal("Replace() again changed = true, want false for an unchanged set")
	}
	var countAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_user_id = $1`, userID).Scan(&countAfter); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if countAfter != countBefore {
		t.Fatalf("audit rows = %d after no-op, want %d", countAfter, countBefore)
	}
}

func TestIntegrationResolveActorJoinsRolePermissions(t *testing.T) {
	pool := newUserIntegrationPool(t)
	ctx := context.Background()
	repo := NewPostgresRoleRepository(pool)
	base := (time.Now().UnixNano() & ((1 << 50) - 1)) * 16
	authorID, adminID := base+1, base+2
	insertIntegrationUser(t, pool, authorID, "it-author")
	insertIntegrationUser(t, pool, adminID, "it-admin")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []int64{authorID, adminID})
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_role_assignments (user_id, role_code) VALUES ($1, 'author'), ($2, 'admin')
	`, authorID, adminID); err != nil {
		t.Fatalf("seed assignments: %v", err)
	}

	state, err := repo.ResolveActor(ctx, authorID)
	if err != nil {
		t.Fatalf("ResolveActor(author): %v", err)
	}
	if !equalRoles(state.Roles, []auth.Role{auth.RoleAuthor}) {
		t.Fatalf("author roles = %v, want [author]", state.Roles)
	}
	if !equalPermissionSet(state.Permissions, seededPermissions(auth.RoleAuthor)) {
		t.Fatalf("author permissions = %v, want the seed", state.Permissions)
	}

	admin, err := repo.ResolveActor(ctx, adminID)
	if err != nil {
		t.Fatalf("ResolveActor(admin): %v", err)
	}
	if !equalPermissionSet(admin.Permissions, authz.AllPermissions()) {
		t.Fatalf("admin permissions = %v, want full access", admin.Permissions)
	}
}

func TestIntegrationResolveActorRejectsUnknownPermission(t *testing.T) {
	pool := newUserIntegrationPool(t)
	ctx := context.Background()
	repo := NewPostgresRoleRepository(pool)
	userID := (time.Now().UnixNano() & ((1 << 50) - 1)) * 16
	insertIntegrationUser(t, pool, userID, "it-bad-perm")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM role_permissions WHERE role_code = 'author' AND permission_code = 'problem.explode'`)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_role_assignments (user_id, role_code) VALUES ($1, 'author')
	`, userID); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO role_permissions (role_code, permission_code) VALUES ('author', 'problem.explode')
	`); err != nil {
		t.Fatalf("seed bad permission: %v", err)
	}

	_, err := repo.ResolveActor(ctx, userID)
	if err == nil || !strings.Contains(err.Error(), "invalid permission assignment") {
		t.Fatalf("ResolveActor() error = %v, want invalid permission assignment", err)
	}
}
