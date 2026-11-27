package submission

import (
	"testing"

	"SOJ/internal/judge"
)

func TestJudgeSummary(t *testing.T) {
	tests := []struct {
		name        string
		result      judge.Result
		wantIndex   *int32
		wantGroup   string
		wantChecker string
	}{
		{
			name: "explicit index and diagnostic come from the first failed case",
			result: judge.Result{
				Verdict: judge.VerdictWrongAnswer,
				Cases: []judge.CaseResult{
					{Index: 1, GroupName: "samples", Verdict: judge.VerdictAccepted, CheckerMessage: "sample output matched"},
					{Index: 7, GroupName: "edge-cases", Verdict: judge.VerdictWrongAnswer, CheckerMessage: "expected 3, got 4"},
				},
			},
			wantIndex:   int32Ptr(7),
			wantGroup:   "edge-cases",
			wantChecker: "expected 3, got 4",
		},
		{
			name: "zero index falls back to the one-based position",
			result: judge.Result{
				Verdict: judge.VerdictWrongAnswer,
				Cases: []judge.CaseResult{
					{Index: 0, GroupName: "samples", Verdict: judge.VerdictAccepted},
					{Index: 0, GroupName: "edge-cases", Verdict: judge.VerdictWrongAnswer, CheckerMessage: "wrong answer on the second case"},
				},
			},
			wantIndex:   int32Ptr(2),
			wantGroup:   "edge-cases",
			wantChecker: "wrong answer on the second case",
		},
		{
			name: "all accepted leaves the failure fields unset",
			result: judge.Result{
				Verdict: judge.VerdictAccepted,
				Cases: []judge.CaseResult{
					{Index: 1, GroupName: "samples", Verdict: judge.VerdictAccepted},
					{Index: 2, GroupName: "edge-cases", Verdict: judge.VerdictAccepted},
				},
			},
		},
		{
			name: "missing checker message falls back without changing the failed case",
			result: judge.Result{
				Verdict: judge.VerdictWrongAnswer,
				Cases: []judge.CaseResult{
					{Index: 1, GroupName: "samples", Verdict: judge.VerdictAccepted},
					{Index: 3, GroupName: "edge-cases", Verdict: judge.VerdictRuntimeError},
					{Index: 4, GroupName: "edge-cases", Verdict: judge.VerdictWrongAnswer, CheckerMessage: "case four detail"},
				},
			},
			wantIndex:   int32Ptr(3),
			wantGroup:   "edge-cases",
			wantChecker: "case four detail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := judgeSummary(tt.result)

			switch {
			case tt.wantIndex == nil:
				if summary.FirstFailedCaseIndex != nil {
					t.Fatalf("FirstFailedCaseIndex = %d, want nil", *summary.FirstFailedCaseIndex)
				}
			case summary.FirstFailedCaseIndex == nil:
				t.Fatalf("FirstFailedCaseIndex = nil, want %d", *tt.wantIndex)
			case *summary.FirstFailedCaseIndex != *tt.wantIndex:
				t.Fatalf("FirstFailedCaseIndex = %d, want %d", *summary.FirstFailedCaseIndex, *tt.wantIndex)
			}

			if summary.FirstFailedGroup != tt.wantGroup {
				t.Fatalf("FirstFailedGroup = %q, want %q", summary.FirstFailedGroup, tt.wantGroup)
			}
			if summary.CheckerMessage != tt.wantChecker {
				t.Fatalf("CheckerMessage = %q, want %q", summary.CheckerMessage, tt.wantChecker)
			}
		})
	}
}
