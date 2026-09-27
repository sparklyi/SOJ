package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires a disposable Postgres database seeded by the repository migrations:
//
//	SOJ_TEST_DATABASE_DSN=postgres://... go test ./internal/audit/...
func newIntegrationStore(t *testing.T) (*PostgresStore, *pgxpool.Pool) {
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
	return NewPostgresStore(db.New(pool)), pool
}

func TestPostgresStoreFiltersByObjectAndResolvesActor(t *testing.T) {
	store, pool := newIntegrationStore(t)
	ctx := context.Background()
	base := (time.Now().UnixNano() & ((1 << 50) - 1)) * 16
	userID, objectID := base+1, base+2
	username := fmt.Sprintf("it-audit-%d", userID)

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, username, status)
		VALUES ($1, $2, 'hash', $3, 'active')`,
		userID, fmt.Sprintf("it-audit-%d@example.test", userID), username); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := Insert(ctx, pool, Event{
		ActorUserID: userID,
		Action:      ActionProblemArchived,
		ObjectType:  ObjectProblem,
		ObjectID:    objectID,
		Reason:      "retired",
		Metadata:    map[string]string{"previous_status": "published"},
	}); err != nil {
		t.Fatalf("Insert(archive) error = %v", err)
	}
	if err := Insert(ctx, pool, Event{
		ActorUserID: userID,
		Action:      ActionProblemRestored,
		ObjectType:  ObjectProblem,
		ObjectID:    objectID,
	}); err != nil {
		t.Fatalf("Insert(restore) error = %v", err)
	}
	// A different object and type must not leak into the filtered page.
	if err := Insert(ctx, pool, Event{
		ActorUserID: userID,
		Action:      ActionUserDisabled,
		ObjectType:  ObjectUser,
		ObjectID:    objectID,
	}); err != nil {
		t.Fatalf("Insert(user) error = %v", err)
	}

	page, total, err := store.ListEvents(ctx, ListFilter{ObjectType: ObjectProblem, ObjectID: objectID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if total != 2 || len(page) != 2 {
		t.Fatalf("ListEvents() = %d rows / total %d, want 2", len(page), total)
	}
	if page[0].Action != ActionProblemRestored || page[1].Action != ActionProblemArchived {
		t.Fatalf("ListEvents() order = [%s, %s], want newest first", page[0].Action, page[1].Action)
	}
	if page[0].ActorUsername != username {
		t.Fatalf("actor username = %q, want %q", page[0].ActorUsername, username)
	}
	var metadata map[string]string
	if err := json.Unmarshal(page[1].Metadata, &metadata); err != nil {
		t.Fatalf("metadata is not JSON: %v", err)
	}
	if metadata["previous_status"] != "published" {
		t.Fatalf("metadata = %v, want previous_status", metadata)
	}

	byAction, total, err := store.ListEvents(ctx, ListFilter{
		ObjectType: ObjectProblem,
		ObjectID:   objectID,
		Action:     ActionProblemArchived,
		Page:       1,
		PageSize:   20,
	})
	if err != nil {
		t.Fatalf("ListEvents(action) error = %v", err)
	}
	if total != 1 || len(byAction) != 1 || byAction[0].Action != ActionProblemArchived {
		t.Fatalf("ListEvents(action) = %+v / total %d, want one archive event", byAction, total)
	}
}
