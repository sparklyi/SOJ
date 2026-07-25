package problem

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

type problemCheckStore interface {
	GetProblem(ctx context.Context, id int64) (ProblemRecord, error)
	GetCurrentProblemStatement(ctx context.Context, problemID int64) (Statement, error)
	GetCurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error)
	GetProblemCheckRun(ctx context.Context, id int64) (ProblemCheckRunRecord, error)
	ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error)
	WithProblemCheckTx(ctx context.Context, fn func(context.Context, problemCheckTx) error) error
}

type problemCheckTx interface {
	CreateProblemCheckRun(ctx context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error)
	CreateProblemCheckFinding(ctx context.Context, input CreateProblemCheckFindingInput) (ProblemCheckFindingRecord, error)
	CompleteProblemCheckRun(ctx context.Context, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error)
}

// ProblemCheckService runs and reads problem validation checks.
type ProblemCheckService struct {
	store    problemCheckStore
	archives testcaseArchiveReader
	now      func() time.Time
}

// NewProblemCheckService builds a problem check service.
// It panics if store is nil.
func NewProblemCheckService(store problemCheckStore, archives testcaseArchiveReader, now func() time.Time) *ProblemCheckService {
	if store == nil {
		panic("problem check store is required")
	}
	if now == nil {
		now = time.Now
	}
	return &ProblemCheckService{store: store, archives: archives, now: now}
}

func (s *ProblemCheckService) RunProblemCheck(ctx context.Context, actor auth.Actor, problemID int64) (ProblemCheckResult, error) {
	p, err := s.store.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	if err := canWriteProblem(actor, p); err != nil {
		return ProblemCheckResult{}, err
	}

	statement, err := s.store.GetCurrentProblemStatement(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	set, err := s.store.GetCurrentReadyTestcaseSet(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}

	findings := validateProblemCheckStatementSamples(statement)
	storageReadable := false
	zipReadable := false
	caseCount := 0
	if s.archives == nil {
		findings = append(findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     "testcase.storage_unreadable",
			message:  "testcase object storage is unavailable",
			details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
		})
	} else if strings.TrimSpace(set.StorageKey) == "" {
		findings = append(findings, problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     "testcase.storage_unreadable",
			message:  "testcase archive storage key is missing",
		})
	} else {
		body, _, err := s.archives.Get(ctx, set.StorageKey)
		if err != nil {
			findings = append(findings, problemCheckFindingDraft{
				severity: ProblemCheckSeverityError,
				code:     "testcase.storage_unreadable",
				message:  "testcase archive cannot be read from storage",
				details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
			})
		} else {
			storageReadable = true
			data, err := readAllAndClose(body, defaultMaxTestcaseArchiveBytes)
			if err != nil {
				if resourceErr, ok := err.(*testcaseArchiveResourceError); ok {
					findings = append(findings, problemCheckFindingDraft{
						severity: ProblemCheckSeverityError,
						code:     resourceErr.code,
						message:  resourceErr.message,
						details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
					})
				} else {
					storageReadable = false
					findings = append(findings, problemCheckFindingDraft{
						severity: ProblemCheckSeverityError,
						code:     "testcase.storage_unreadable",
						message:  "testcase archive cannot be read from storage",
						details:  problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
					})
				}
			} else {
				archiveResult := validateProblemCheckArchive(data, set)
				zipReadable = archiveResult.zipReadable
				caseCount = archiveResult.caseCount
				findings = append(findings, archiveResult.findings...)
			}
		}
	}

	summary := problemCheckSummary(set.CaseCount, caseCount, storageReadable, zipReadable, findings)
	summaryJSON, err := marshalProblemCheckSummary(summary)
	if err != nil {
		return ProblemCheckResult{}, err
	}

	var runRecord ProblemCheckRunRecord
	persistedFindings := make([]ProblemCheckFinding, 0, len(findings))
	err = s.store.WithProblemCheckTx(ctx, func(ctx context.Context, tx problemCheckTx) error {
		run, err := tx.CreateProblemCheckRun(ctx, CreateProblemCheckRunInput{
			ProblemID:     problemID,
			StatementID:   statement.ID,
			TestcaseSetID: set.ID,
			RequestedBy:   actor.UserID,
			Status:        ProblemCheckStatusRunning,
			Summary:       json.RawMessage(`{}`),
		})
		if err != nil {
			return err
		}
		for _, finding := range findings {
			record, err := tx.CreateProblemCheckFinding(ctx, CreateProblemCheckFindingInput{
				RunID:       run.ID,
				Severity:    finding.severity,
				Code:        finding.code,
				Message:     finding.message,
				CaseIndex:   finding.caseIndex,
				TestcaseKey: finding.testcaseKey,
				Details:     finding.details,
			})
			if err != nil {
				return err
			}
			persistedFindings = append(persistedFindings, problemCheckFindingFromRecord(record))
		}
		runRecord, err = tx.CompleteProblemCheckRun(ctx, CompleteProblemCheckRunInput{
			ID:         run.ID,
			Summary:    summaryJSON,
			FinishedAt: s.now(),
		})
		return err
	})
	if err != nil {
		return ProblemCheckResult{}, err
	}
	return ProblemCheckResult{Run: problemCheckRunFromRecord(runRecord), Findings: persistedFindings}, nil
}

func (s *ProblemCheckService) GetProblemCheck(ctx context.Context, actor auth.Actor, problemID int64, checkID int64) (ProblemCheckResult, error) {
	p, err := s.store.GetProblem(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	if err := canWriteProblem(actor, p); err != nil {
		return ProblemCheckResult{}, err
	}
	run, err := s.store.GetProblemCheckRun(ctx, checkID)
	if err != nil {
		return ProblemCheckResult{}, problemCheckNotFoundErr(err)
	}
	if run.ProblemID != problemID {
		return ProblemCheckResult{}, apperror.NotFound("problem_check.not_found", "problem check not found")
	}
	records, err := s.store.ListProblemCheckFindings(ctx, checkID)
	if err != nil {
		return ProblemCheckResult{}, err
	}
	findings := make([]ProblemCheckFinding, 0, len(records))
	for _, record := range records {
		findings = append(findings, problemCheckFindingFromRecord(record))
	}
	return ProblemCheckResult{Run: problemCheckRunFromRecord(run), Findings: findings}, nil
}
