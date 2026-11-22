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

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		secure   bool
		want     string
		wantSec  bool
		wantErr  bool
	}{
		{name: "bare DNS host and port", endpoint: "minio:9000", secure: false, want: "minio:9000", wantSec: false},
		{name: "bare DNS preserves secure flag", endpoint: "minio:9000", secure: true, want: "minio:9000", wantSec: true},
		{name: "bare IPv4 host and port", endpoint: "127.0.0.1:9000", secure: false, want: "127.0.0.1:9000", wantSec: false},
		{name: "bare IPv4 preserves secure flag", endpoint: "192.168.1.10:9000", secure: true, want: "192.168.1.10:9000", wantSec: true},
		{name: "bare bracketed IPv6 host and port", endpoint: "[::1]:9000", secure: false, want: "[::1]:9000", wantSec: false},
		{name: "bare bracketed IPv6 preserves secure flag", endpoint: "[2001:db8::1]:9000", secure: true, want: "[2001:db8::1]:9000", wantSec: true},
		{name: "explicit http clears secure", endpoint: "http://minio:9000", secure: true, want: "minio:9000", wantSec: false},
		{name: "explicit https sets secure", endpoint: "https://minio:9000", secure: false, want: "minio:9000", wantSec: true},
		{name: "explicit http IPv4", endpoint: "http://127.0.0.1:9000", secure: true, want: "127.0.0.1:9000", wantSec: false},
		{name: "explicit https bracketed IPv6", endpoint: "https://[::1]:9000", secure: false, want: "[::1]:9000", wantSec: true},
		{name: "unsupported scheme", endpoint: "ftp://minio:9000", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotSec, err := normalizeEndpoint(test.endpoint, test.secure)
			if test.wantErr {
				if err == nil {
					t.Fatal("normalizeEndpoint() error = nil, want unsupported scheme")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEndpoint() error = %v", err)
			}
			if got != test.want || gotSec != test.wantSec {
				t.Fatalf("normalizeEndpoint() = %q, %v, want %q, %v", got, gotSec, test.want, test.wantSec)
			}
		})
	}
}
