package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresBaselineRejectsInitializedSchemasAndIsRepeatable(t *testing.T) {
	dsn := os.Getenv("SOJ_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SOJ_TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_schema_%t", legacy), func(t *testing.T) {
			schema := pgx.Identifier{fmt.Sprintf("migrate_baseline_%d", time.Now().UnixNano())}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, err := conn.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + schema)
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
				defer done()
				if _, err := conn.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
					t.Error(err)
				}
			})
			exec("SET search_path TO " + schema + ", public")
			dir := filepath.Join("..", "migrations")
			var output bytes.Buffer
			if legacy {
				exec("CREATE TABLE legacy_marker (id int PRIMARY KEY)")
				if err := migrateUp(ctx, conn, dir, &output); err == nil || !strings.Contains(err.Error(), "database already has tables or migration history") {
					t.Fatalf("legacy migrate returned %v", err)
				}
				var count int
				if err := conn.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema()").Scan(&count); err != nil || count != 1 {
					t.Fatalf("legacy schema changed: count=%d err=%v", count, err)
				}
				return
			}
			if err := migrateUp(ctx, conn, dir, &output); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != "applied "+baselineVersion+".up.sql\n" {
				t.Fatalf("first migration output = %q", got)
			}
			output.Reset()
			if err := migrateUp(ctx, conn, dir, &output); err != nil || output.Len() != 0 {
				t.Fatalf("rerun migration output=%q err=%v", output.String(), err)
			}
		})
	}
}
