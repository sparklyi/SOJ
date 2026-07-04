package observability

import (
	"context"
	"errors"
	"testing"
)

func TestReadinessCheckReportsDependencyFailure(t *testing.T) {
	wantErr := errors.New("postgres unavailable")
	checker := NewReadiness(map[string]CheckFunc{
		"postgres": func(context.Context) error { return wantErr },
	})

	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("Check() error = nil, want dependency failure")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Check() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestReadinessCheckPassesWithNoFailures(t *testing.T) {
	checker := NewReadiness(map[string]CheckFunc{
		"postgres": func(context.Context) error { return nil },
	})

	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}
