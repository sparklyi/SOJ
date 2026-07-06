package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCheckReadyUsesOptionalReadinessChecker(t *testing.T) {
	wantErr := errors.New("bucket unavailable")
	store := &readyStore{err: wantErr}

	err := CheckReady(context.Background(), store)
	if !errors.Is(err, wantErr) {
		t.Fatalf("CheckReady error = %v, want %v", err, wantErr)
	}
	if !store.called {
		t.Fatal("CheckReady did not call optional Ready method")
	}
}

func TestCheckReadyPassesWhenStorageHasNoReadinessChecker(t *testing.T) {
	if err := CheckReady(context.Background(), noReadyStore{}); err != nil {
		t.Fatalf("CheckReady error = %v", err)
	}
}

type readyStore struct {
	noReadyStore
	err    error
	called bool
}

func (s *readyStore) Ready(context.Context) error {
	s.called = true
	return s.err
}

type noReadyStore struct{}

func (noReadyStore) Put(context.Context, Object) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}

func (noReadyStore) Get(context.Context, string) (io.ReadCloser, ObjectInfo, error) {
	return io.NopCloser(strings.NewReader("")), ObjectInfo{}, nil
}

func (noReadyStore) Delete(context.Context, string) error {
	return nil
}

func (noReadyStore) Stat(context.Context, string) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
