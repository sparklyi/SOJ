package submission

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingReconciliationStore struct {
	runs       []RunRecord
	tasks      []JudgeTaskRecord
	runErr     error
	taskErr    error
	runCutoff  time.Time
	taskCutoff time.Time
	runReason  string
	taskReason string
}

func (s *recordingReconciliationStore) MarkStaleRunsSystemError(
	_ context.Context,
	cutoff time.Time,
	reason string,
) ([]RunRecord, error) {
	s.runCutoff = cutoff
	s.runReason = reason
	return s.runs, s.runErr
}

func (s *recordingReconciliationStore) ResetStaleJudgeTasks(
	_ context.Context,
	cutoff time.Time,
	reason string,
) ([]JudgeTaskRecord, error) {
	s.taskCutoff = cutoff
	s.taskReason = reason
	return s.tasks, s.taskErr
}

type reconciliationMetricCall struct {
	action string
	result string
	count  int
}

type recordingReconciliationMetrics struct {
	calls []reconciliationMetricCall
}

func (m *recordingReconciliationMetrics) RecordReconcilerAction(
	action, result string,
	count int,
) {
	m.calls = append(m.calls, reconciliationMetricCall{
		action: action,
		result: result,
		count:  count,
	})
}

func TestMarkStaleRunsOutcomes(t *testing.T) {
	now := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)
	maxAge := 17 * time.Minute
	cutoff := now.Add(-maxAge)
	sentinelErr := errors.New("mark stale runs failed")

	tests := []struct {
		name       string
		runs       []RunRecord
		storeErr   error
		wantCount  int
		wantErr    error
		wantMetric reconciliationMetricCall
	}{
		{
			name:      "multiple rows",
			runs:      []RunRecord{{ID: 1}, {ID: 2}, {ID: 3}},
			wantCount: 3,
			wantMetric: reconciliationMetricCall{
				action: "mark_stale_runs",
				result: "success",
				count:  3,
			},
		},
		{
			name:      "no rows",
			runs:      nil,
			wantCount: 0,
			wantMetric: reconciliationMetricCall{
				action: "mark_stale_runs",
				result: "success",
				count:  0,
			},
		},
		{
			name:      "store error",
			storeErr:  sentinelErr,
			wantCount: 0,
			wantErr:   sentinelErr,
			wantMetric: reconciliationMetricCall{
				action: "mark_stale_runs",
				result: "error",
				count:  1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingReconciliationStore{
				runs:   tt.runs,
				runErr: tt.storeErr,
			}
			metrics := &recordingReconciliationMetrics{}
			reconciler := NewReconciler(
				store,
				nil,
				nil,
				func() time.Time { return now },
				metrics,
			)

			gotCount, err := reconciler.MarkStaleRuns(t.Context(), maxAge)

			if gotCount != tt.wantCount {
				t.Fatalf("MarkStaleRuns() count = %d, want %d", gotCount, tt.wantCount)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MarkStaleRuns() error = %v, want %v", err, tt.wantErr)
			}
			if !store.runCutoff.Equal(cutoff) {
				t.Errorf("cutoff = %v, want %v", store.runCutoff, cutoff)
			}
			if store.runReason != "run reconciliation marked stale run as system_error" {
				t.Errorf("reason = %q", store.runReason)
			}
			if len(metrics.calls) != 1 || metrics.calls[0] != tt.wantMetric {
				t.Fatalf("metrics = %#v, want one call %#v", metrics.calls, tt.wantMetric)
			}
		})
	}
}

func TestResetStaleTasksOutcomes(t *testing.T) {
	now := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)
	maxAge := 23 * time.Minute
	cutoff := now.Add(-maxAge)
	sentinelErr := errors.New("reset stale tasks failed")

	tests := []struct {
		name       string
		tasks      []JudgeTaskRecord
		storeErr   error
		wantCount  int
		wantErr    error
		wantMetric reconciliationMetricCall
	}{
		{
			name:      "multiple rows",
			tasks:     []JudgeTaskRecord{{ID: 1}, {ID: 2}},
			wantCount: 2,
			wantMetric: reconciliationMetricCall{
				action: "reset_stale_tasks",
				result: "success",
				count:  2,
			},
		},
		{
			name:      "no rows",
			tasks:     nil,
			wantCount: 0,
			wantMetric: reconciliationMetricCall{
				action: "reset_stale_tasks",
				result: "success",
				count:  0,
			},
		},
		{
			name:      "store error",
			storeErr:  sentinelErr,
			wantCount: 0,
			wantErr:   sentinelErr,
			wantMetric: reconciliationMetricCall{
				action: "reset_stale_tasks",
				result: "error",
				count:  1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingReconciliationStore{
				tasks:   tt.tasks,
				taskErr: tt.storeErr,
			}
			metrics := &recordingReconciliationMetrics{}
			reconciler := NewReconciler(
				store,
				nil,
				nil,
				func() time.Time { return now },
				metrics,
			)

			gotCount, err := reconciler.ResetStaleTasks(t.Context(), maxAge)

			if gotCount != tt.wantCount {
				t.Fatalf("ResetStaleTasks() count = %d, want %d", gotCount, tt.wantCount)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResetStaleTasks() error = %v, want %v", err, tt.wantErr)
			}
			if !store.taskCutoff.Equal(cutoff) {
				t.Errorf("cutoff = %v, want %v", store.taskCutoff, cutoff)
			}
			if store.taskReason != "judge task reconciliation reset stale task to pending" {
				t.Errorf("reason = %q", store.taskReason)
			}
			if len(metrics.calls) != 1 || metrics.calls[0] != tt.wantMetric {
				t.Fatalf("metrics = %#v, want one call %#v", metrics.calls, tt.wantMetric)
			}
		})
	}
}
