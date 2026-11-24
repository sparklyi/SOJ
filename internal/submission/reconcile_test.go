package submission

import (
	"context"
	"errors"
	"strings"
	"testing"

	"SOJ/internal/judge"
	"SOJ/internal/language"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// reconcileRecordingTx records the statements run inside the reconciliation
// transaction and can be told to fail a chosen upsert slug. It implements
// pgx.Tx by embedding the interface; only the methods the reconciliation uses
// are overridden, and any unexpected call panics so the test cannot silently
// pass by skipping a phase.
type reconcileRecordingTx struct {
	pgx.Tx
	failSlug string
	upserts  []string
	disabled bool
	committed bool
	rolledBack bool
}

func (tx *reconcileRecordingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	name, _, _ := strings.Cut(strings.TrimPrefix(sql, "-- name: "), " ")
	if name == "UpsertCatalogLanguage" {
		slug := args[1].(string)
		tx.upserts = append(tx.upserts, slug)
		if slug == tx.failSlug {
			return reconcileErrRow{err: errors.New("boom")}
		}
	}
	return reconcileEmptyRow{}
}

func (tx *reconcileRecordingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	name, _, _ := strings.Cut(strings.TrimPrefix(sql, "-- name: "), " ")
	if name == "DisableLanguagesNotListed" {
		tx.disabled = true
	}
	return pgconn.CommandTag{}, nil
}

func (tx *reconcileRecordingTx) Commit(ctx context.Context) error {
	tx.committed = true
	return nil
}

func (tx *reconcileRecordingTx) Rollback(ctx context.Context) error {
	tx.rolledBack = true
	return nil
}

// reconcileRunner hands out the same recording transaction for every BeginTx so
// the test can inspect what ran and whether it committed or rolled back.
type reconcileRunner struct{ tx *reconcileRecordingTx }

func (r reconcileRunner) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return r.tx, nil
}

type reconcileErrRow struct{ err error }

func (r reconcileErrRow) Scan(dest ...any) error { return r.err }

type reconcileEmptyRow struct{}

func (reconcileEmptyRow) Scan(dest ...any) error { return nil }

func TestReconcileLanguagesRollsBackWhenLaterUpsertFails(t *testing.T) {
	tx := &reconcileRecordingTx{failSlug: "python"}
	repo := NewSQLRepositoryWithTxRunner(db.New(tx), reconcileRunner{tx: tx})
	profiles := []language.Profile{
		{Slug: "go", Name: "Go", TimeLimitMS: 1000, MemoryLimitKB: 262144},
		{Slug: "python", Name: "Python", TimeLimitMS: 2000, MemoryLimitKB: 262144},
	}
	err := repo.ReconcileLanguages(t.Context(), profiles)
	if err == nil || !strings.Contains(err.Error(), "upsert language python") {
		t.Fatalf("expected upsert error for python, got %v", err)
	}
	if len(tx.upserts) != 2 {
		t.Fatalf("expected two upsert attempts, got %v", tx.upserts)
	}
	if tx.disabled {
		t.Error("disable phase ran despite an earlier upsert failing")
	}
	if tx.committed {
		t.Error("transaction committed despite a failed upsert; earlier writes would be persisted")
	}
	if !tx.rolledBack {
		t.Error("transaction was not rolled back")
	}
}

func TestReconcileLanguagesCommitsEmptyCatalog(t *testing.T) {
	tx := &reconcileRecordingTx{}
	repo := NewSQLRepositoryWithTxRunner(db.New(tx), reconcileRunner{tx: tx})
	if err := repo.ReconcileLanguages(t.Context(), nil); err != nil {
		t.Fatalf("reconcile empty catalog: %v", err)
	}
	if !tx.disabled {
		t.Error("disable phase did not run for empty catalog")
	}
	if !tx.committed {
		t.Error("empty catalog did not commit")
	}
}

var _ = judge.EngineSOJAgent