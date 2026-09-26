package problem

import (
	"bytes"
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
	GetCurrentTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error)
	GetProblemCheckRun(ctx context.Context, id int64) (ProblemCheckRunRecord, error)
	ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error)
	WithProblemCheckTx(ctx context.Context, fn func(context.Context, problemCheckTx) error) error
}

type problemCheckTx interface {
	CreateProblemCheckRun(ctx context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error)
	CreateProblemCheckFindings(ctx context.Context, inputs []CreateProblemCheckFindingInput) ([]ProblemCheckFindingRecord, error)
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
	set, err := s.store.GetCurrentTestcaseSet(ctx, problemID)
	if err != nil {
		return ProblemCheckResult{}, err
	}

	findings := make([]problemCheckFindingDraft, 0, 4)
	storageReadable := false
	zipReadable := false
	caseCount := 0
	switch {
	case s.archives == nil:
		findings = append(findings, storageUnreadableFinding(set.StorageKey, "testcase object storage is unavailable"))
	case strings.TrimSpace(set.StorageKey) == "":
		findings = append(findings, storageUnreadableFinding("", "testcase archive storage key is missing"))
	default:
		body, _, err := s.archives.Get(ctx, set.StorageKey)
		if err != nil {
			findings = append(findings, storageUnreadableFinding(set.StorageKey, "testcase archive cannot be read from storage"))
			break
		}
		storageReadable = true
		data, err := readAllAndClose(body, MaxTestcaseArchiveBytes)
		if err != nil {
			findings = append(findings, archiveErrorFinding(err, set.StorageKey))
			break
		}
		manifest, archiveFindings := VerifyArchive(bytes.NewReader(data), int64(len(data)), set.ChecksumSHA256)
		for _, finding := range archiveFindings {
			findings = append(findings, problemCheckFindingDraft{
				severity:    finding.Severity,
				code:        finding.Code,
				message:     finding.Message,
				testcaseKey: finding.File,
				details:     problemCheckDetails(map[string]any{"storage_key": set.StorageKey}),
			})
		}
		zipReadable = !archiveUnreadable(archiveFindings)
		caseCount = len(manifest.Cases)
		if zipReadable && int32(caseCount) != set.CaseCount {
			findings = append(findings, problemCheckFindingDraft{
				severity: ProblemCheckSeverityError,
				code:     codeCaseCountMismatch,
				message:  "case count does not match the stored testcase set",
				details: problemCheckDetails(map[string]any{
					"expected_case_count": set.CaseCount,
					"actual_case_count":   caseCount,
				}),
			})
		}
	}

	summary := problemCheckSummary(caseCount, storageReadable, zipReadable, findings)
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
		if len(findings) > 0 {
			inputs := make([]CreateProblemCheckFindingInput, 0, len(findings))
			for _, finding := range findings {
				inputs = append(inputs, CreateProblemCheckFindingInput{
					RunID:       run.ID,
					Severity:    finding.severity,
					Code:        finding.code,
					Message:     finding.message,
					CaseIndex:   finding.caseIndex,
					TestcaseKey: finding.testcaseKey,
					Details:     finding.details,
				})
			}
			records, err := tx.CreateProblemCheckFindings(ctx, inputs)
			if err != nil {
				return err
			}
			for _, record := range records {
				persistedFindings = append(persistedFindings, problemCheckFindingFromRecord(record))
			}
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

func storageUnreadableFinding(storageKey, message string) problemCheckFindingDraft {
	details := map[string]any{}
	if storageKey != "" {
		details["storage_key"] = storageKey
	}
	return problemCheckFindingDraft{
		severity: ProblemCheckSeverityError,
		code:     "testcase.storage_unreadable",
		message:  message,
		details:  problemCheckDetails(details),
	}
}

func archiveErrorFinding(err error, storageKey string) problemCheckFindingDraft {
	if appErr, ok := apperror.From(err); ok {
		return problemCheckFindingDraft{
			severity: ProblemCheckSeverityError,
			code:     appErr.Code,
			message:  appErr.Message,
			details:  problemCheckDetails(map[string]any{"storage_key": storageKey}),
		}
	}
	// The archive was fetched but could not be fully read: that is a storage
	// failure, not a malformed archive.
	return storageUnreadableFinding(storageKey, "testcase archive cannot be read from storage")
}

// archiveUnreadable reports whether the archive could not even be opened as a
// zip or failed checksum verification, as opposed to merely failing pairing.
func archiveUnreadable(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Code == codeZipInvalid || finding.Code == codeArchiveCorrupted {
			return true
		}
	}
	return false
}
