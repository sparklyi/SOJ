package observability

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestReadinessRecordsDependencyResults(t *testing.T) {
	wantErr := errors.New("redis unavailable")
	recorder := &recordingReadinessMetrics{}
	checker := NewReadinessWithMetrics(map[string]CheckFunc{
		"postgres": func(context.Context) error { return nil },
		"redis":    func(context.Context) error { return wantErr },
	}, recorder)

	err := checker.Check(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Check() error = %v, want wrapped %v", err, wantErr)
	}
	if !recorder.saw("postgres", "success") {
		t.Fatalf("missing postgres success metric: %+v", recorder.records)
	}
	if !recorder.saw("redis", "error") {
		t.Fatalf("missing redis error metric: %+v", recorder.records)
	}
}

type readinessMetricRecord struct {
	dependency string
	result     string
	duration   time.Duration
}

type recordingReadinessMetrics struct {
	records []readinessMetricRecord
}

func (m *recordingReadinessMetrics) RecordReadinessCheck(dependency, result string, duration time.Duration) {
	m.records = append(m.records, readinessMetricRecord{dependency: dependency, result: result, duration: duration})
}

func (m *recordingReadinessMetrics) saw(dependency, result string) bool {
	for _, record := range m.records {
		if record.dependency == dependency && record.result == result {
			return true
		}
	}
	return false
}
