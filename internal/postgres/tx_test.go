package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestWithTxJoinsRollbackFailure(t *testing.T) {
	callbackErr := errors.New("callback failed")
	rollbackErr := errors.New("rollback failed")
	tx := &recordingTx{rollbackErr: rollbackErr}

	err := WithTx(context.Background(), recordingTxRunner{tx: tx}, func(pgx.Tx) error {
		return callbackErr
	})

	if !errors.Is(err, callbackErr) {
		t.Fatalf("WithTx error = %v, want callback error %v", err, callbackErr)
	}
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("WithTx error = %v, want rollback error %v", err, rollbackErr)
	}
	if !tx.rolledBack {
		t.Fatal("WithTx did not roll back the transaction")
	}
}

func TestWithTxIgnoresRollbackAfterCommit(t *testing.T) {
	tx := &recordingTx{rollbackErr: pgx.ErrTxClosed}

	err := WithTx(context.Background(), recordingTxRunner{tx: tx}, func(pgx.Tx) error {
		return nil
	})

	if err != nil {
		t.Fatalf("WithTx error = %v, want nil", err)
	}
	if !tx.committed || !tx.rolledBack {
		t.Fatalf("transaction state = committed:%t rolled_back:%t, want both true", tx.committed, tx.rolledBack)
	}
}

func TestWithTxRollsBackOnceWhenCallbackPanics(t *testing.T) {
	sentinel := errors.New("callback panicked")
	tx := &recordingTx{}

	defer func() {
		recovered := recover()
		//nolint:errorlint // the panic value is the sentinel itself; WithTx must not wrap it
		if recovered != sentinel {
			t.Fatalf("recovered %v, want the callback's sentinel %v", recovered, sentinel)
		}
		if tx.rollbackCalls != 1 {
			t.Fatalf("Rollback calls = %d, want 1", tx.rollbackCalls)
		}
		if !tx.rolledBack {
			t.Fatal("WithTx did not roll back the transaction")
		}
		if tx.committed {
			t.Fatal("WithTx committed a transaction whose callback panicked")
		}
	}()

	_ = WithTx(context.Background(), recordingTxRunner{tx: tx}, func(pgx.Tx) error {
		panic(sentinel)
	})
}

type recordingTxRunner struct {
	tx pgx.Tx
}

func (r recordingTxRunner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return r.tx, nil
}

type recordingTx struct {
	pgx.Tx
	commitErr     error
	rollbackErr   error
	committed     bool
	rolledBack    bool
	rollbackCalls int
}

func (tx *recordingTx) Commit(context.Context) error {
	tx.committed = true
	return tx.commitErr
}

func (tx *recordingTx) Rollback(context.Context) error {
	tx.rolledBack = true
	tx.rollbackCalls++
	return tx.rollbackErr
}
