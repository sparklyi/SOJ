package contest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"SOJ/internal/audit"
	"SOJ/internal/auth"
	"SOJ/internal/authz"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresContestRoleWritesScopedAudit(t *testing.T) {
	dsn := os.Getenv("SOJ_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SOJ_TEST_DATABASE_DSN is not set")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	base := (time.Now().UnixNano() & ((1 << 49) - 1)) * 8
	actorID, targetID, contestID, unknownActorID := base+1, base+2, base+3, base+4
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM audit_events WHERE object_type = 'contest' AND object_id = $1`, contestID); err != nil {
			t.Errorf("cleanup audit events: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM contests WHERE id = $1`, contestID); err != nil {
			t.Errorf("cleanup contest: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, actorID, targetID); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})
	for _, id := range []int64{actorID, targetID} {
		username := fmt.Sprintf("it-contest-audit-%d", id)
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, username, status)
			VALUES ($1, $2, 'hash', $3, 'active')`, id, username+"@example.test", username); err != nil {
			t.Fatalf("seed contest role user: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO contests (id, owner_user_id, title, visibility, start_at, end_at, freeze_at)
		VALUES ($1, $2, 'Contest role audit', 'private', now(), now() + interval '2 hours', now() + interval '1 hour')`, contestID, actorID); err != nil {
		t.Fatalf("seed contest: %v", err)
	}

	roles := NewPostgresContestRoleStore(pool)
	role := auth.RoleContestManager
	assignment, err := roles.GrantContestRole(ctx, contestID, targetID, role, actorID, "  assign manager  ")
	if err != nil {
		t.Fatalf("grant role: %v", err)
	}
	if assignment.ContestID != contestID || assignment.UserID != targetID || assignment.Role != role {
		t.Fatalf("assignment = %+v", assignment)
	}
	if _, err := roles.GrantContestRole(ctx, contestID, targetID, role, actorID, "duplicate"); err == nil {
		t.Fatal("duplicate grant unexpectedly succeeded")
	}

	store := audit.NewPostgresStore(db.New(pool))
	service := audit.NewService(store)
	filter := audit.ListFilter{ObjectType: audit.ObjectContest, ObjectID: contestID, Page: 1, PageSize: 10}
	access, err := roles.ListContestAccess(ctx, contestID, targetID)
	if err != nil || !slices.Contains(access.Permissions, authz.PermissionContestManage) {
		t.Fatalf("contest access = %+v, %v, want contest.manage", access, err)
	}
	scopedActor := auth.Actor{UserID: targetID, Roles: access.Roles, Permissions: access.Permissions}
	if _, err := service.ListEvents(ctx, scopedActor, filter); err == nil {
		t.Fatal("contest manager unexpectedly gained access to the shared audit log")
	}

	// Revoke itself does not reference the actor. A missing actor instead
	// fails the audit foreign key, so the audit failure must roll it back.
	err = roles.RevokeContestRole(ctx, contestID, targetID, role, unknownActorID, "must roll back")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("revoke with unregistered actor = %v, want foreign key failure", err)
	}
	active, err := roles.ListContestRoleAssignments(ctx, contestID)
	if err != nil || len(active) != 1 || active[0].ID != assignment.ID {
		t.Fatalf("assignments after failed audit = %+v, %v, want the original grant", active, err)
	}

	if err := roles.RevokeContestRole(ctx, contestID, targetID, role, actorID, "  remove manager  "); err != nil {
		t.Fatalf("revoke role: %v", err)
	}
	if err := roles.RevokeContestRole(ctx, contestID, targetID, role, actorID, "already removed"); err == nil {
		t.Fatal("duplicate revoke unexpectedly succeeded")
	}

	admin := auth.Actor{UserID: actorID, Roles: []auth.Role{auth.RoleAdmin}, Permissions: authz.AllPermissions()}
	page, err := service.ListEvents(ctx, admin, filter)
	if err != nil || page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("contest audit = %+v, %v, want one grant and one revoke", page, err)
	}
	for i, want := range []struct {
		action audit.Action
		reason string
	}{
		{audit.ActionContestRoleRevoked, "remove manager"},
		{audit.ActionContestRoleGranted, "assign manager"},
	} {
		event := page.Items[i]
		if event.Action != want.action || event.ObjectType != audit.ObjectContest ||
			event.ObjectID == nil || *event.ObjectID != contestID ||
			event.ActorUserID == nil || *event.ActorUserID != actorID || event.Reason != want.reason {
			t.Fatalf("audit event %d = %+v, want %s on contest %d by actor %d (%q)", i, event, want.action, contestID, actorID, want.reason)
		}
		var metadata map[string]string
		if err := json.Unmarshal(event.Metadata, &metadata); err != nil {
			t.Fatalf("decode contest role audit metadata: %v", err)
		}
		if metadata["user_id"] != strconv.FormatInt(targetID, 10) || metadata["role"] != string(role) {
			t.Fatalf("contest role audit metadata = %v, want target %d and role %s", metadata, targetID, role)
		}
	}

	filter.Action = audit.ActionContestRoleGranted
	page, err = service.ListEvents(ctx, admin, filter)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Action != filter.Action {
		t.Fatalf("grant-only audit = %+v, %v, want one grant", page, err)
	}
}
