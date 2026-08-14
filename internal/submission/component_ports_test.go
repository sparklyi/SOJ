package submission

import (
	"context"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/judge"
	"SOJ/internal/problem"
)

type submissionCreatorStoreStub struct {
	nextID int64
}

func (s *submissionCreatorStoreStub) GetEnabledLanguage(context.Context, int64) (LanguageRecord, error) {
	return LanguageRecord{ID: 71, Enabled: true}, nil
}

func (s *submissionCreatorStoreStub) CreateArtifact(_ context.Context, artifact ArtifactRecord) (ArtifactRecord, error) {
	s.nextID++
	artifact.ID = s.nextID
	return artifact, nil
}

func (s *submissionCreatorStoreStub) CreateSubmissionWithTask(_ context.Context, submission SubmissionRecord, _ time.Time) (SubmissionRecord, JudgeTaskRecord, error) {
	s.nextID++
	submission.ID = s.nextID
	s.nextID++
	return submission, JudgeTaskRecord{ID: s.nextID, SubmissionID: submission.ID, Status: "pending"}, nil
}

type submissionCreatorProblemReaderStub struct{}

func (submissionCreatorProblemReaderStub) GetForJudge(context.Context, int64) (problem.Problem, error) {
	return problem.Problem{ID: 1, CurrentTestcaseSetID: 3}, nil
}

type sourceWriterStub struct{}

func (sourceWriterStub) Put(context.Context, string, int64, []byte) (SourceObject, error) {
	return SourceObject{StorageKey: "source", ChecksumSHA256: "checksum", ContentType: "text/plain"}, nil
}

type judgeRunnerStub struct{}

func (judgeRunnerStub) Judge(context.Context, judge.Request) (judge.Result, error) {
	return judge.Result{Verdict: judge.VerdictAccepted}, nil
}

type languageProviderStub struct{}

func (languageProviderStub) Languages(context.Context) ([]judge.Language, error) {
	return nil, nil
}

type contestSubmissionPolicyStub struct{}

func (contestSubmissionPolicyStub) ValidateSubmission(context.Context, auth.Actor, int64, int64) error {
	return nil
}

func TestSubmissionCreatorUsesOnlyCreationStore(t *testing.T) {
	contestID := int64(2)
	creator := NewSubmissionCreator(SubmissionCreatorOptions{
		Store:         &submissionCreatorStoreStub{},
		ProblemReader: submissionCreatorProblemReaderStub{},
		SourceStore:   sourceWriterStub{},
		ContestPolicy: contestSubmissionPolicyStub{},
		Now:           func() time.Time { return time.Unix(10, 0).UTC() },
	})

	created, err := creator.CreateSubmission(t.Context(), auth.Actor{UserID: 7, Role: auth.RoleUser}, CreateSubmissionInput{
		ProblemID:  1,
		ContestID:  &contestID,
		LanguageID: 71,
		Source:     []byte("package main"),
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if created.Submission.ID == 0 || created.Task.SubmissionID != created.Submission.ID || created.Submission.TestcaseSetID != 3 {
		t.Fatalf("CreateSubmission() = %+v", created)
	}
}

type submissionReaderStoreStub struct {
	record SubmissionRecord
}

type contestResultVisibilityPolicyStub struct{}

func (contestResultVisibilityPolicyStub) SubmissionResultVisibility(context.Context, auth.Actor, ContestSubmissionVisibility) (SubmissionResultVisibility, error) {
	return SubmissionResultVisibility{ShowResult: true, ShowCases: true, Visibility: "visible"}, nil
}

func (s submissionReaderStoreStub) GetSubmission(context.Context, int64) (SubmissionRecord, error) {
	return s.record, nil
}

func (submissionReaderStoreStub) ListSubmissions(context.Context, ListSubmissionsInput) ([]SubmissionRecord, int64, error) {
	return nil, 0, nil
}

func (submissionReaderStoreStub) ListSubmissionsByCursor(context.Context, ListSubmissionsInput) ([]SubmissionRecord, error) {
	return nil, nil
}

func (submissionReaderStoreStub) ListSubmissionsByUserBefore(context.Context, int64, SubmissionCursor, int32) ([]SubmissionRecord, error) {
	return nil, nil
}

func (submissionReaderStoreStub) ListSubmissionSummaries(context.Context, []int64, bool) (map[int64]SubmissionListSummary, error) {
	return nil, nil
}

func (submissionReaderStoreStub) GetSubmissionResult(context.Context, int64) (SubmissionResultRecord, error) {
	return SubmissionResultRecord{}, nil
}

func (submissionReaderStoreStub) GetLatestJudgeAttemptBySubmissionID(context.Context, int64) (JudgeAttemptRecord, error) {
	return JudgeAttemptRecord{}, nil
}

func (submissionReaderStoreStub) ListJudgeCaseResults(context.Context, int64) ([]JudgeCaseResultRecord, error) {
	return nil, nil
}

func TestSubmissionReaderUsesOnlyReaderStore(t *testing.T) {
	contestID := int64(2)
	reader := NewSubmissionReader(submissionReaderStoreStub{record: SubmissionRecord{ID: 1, UserID: 7, ContestID: &contestID, Status: StatusQueued}}, contestResultVisibilityPolicyStub{})

	view, err := reader.GetSubmission(t.Context(), auth.Actor{UserID: 7, Role: auth.RoleUser}, 1)
	if err != nil {
		t.Fatalf("GetSubmission() error = %v", err)
	}
	if view.Submission.ID != 1 {
		t.Fatalf("GetSubmission() = %+v, want submission 1", view)
	}
}

type runStoreStub struct{}

func (runStoreStub) GetEnabledLanguage(context.Context, int64) (LanguageRecord, error) {
	return LanguageRecord{ID: 71, Enabled: true}, nil
}

func (runStoreStub) CreateArtifact(_ context.Context, artifact ArtifactRecord) (ArtifactRecord, error) {
	artifact.ID = 1
	return artifact, nil
}

func (runStoreStub) CreateRun(_ context.Context, run RunRecord) (RunRecord, error) {
	run.ID = 1
	return run, nil
}

func (runStoreStub) GetRun(_ context.Context, id int64) (RunRecord, error) {
	return RunRecord{ID: id, UserID: 7, Status: StatusQueued}, nil
}

func (runStoreStub) UpdateRunStatus(_ context.Context, id int64, _ judge.Result) (RunRecord, error) {
	return RunRecord{ID: id}, nil
}

func TestRunServiceUsesOnlyRunStore(t *testing.T) {
	service := NewRunService(RunServiceOptions{
		Store:         runStoreStub{},
		ProblemReader: submissionCreatorProblemReaderStub{},
		SourceStore:   sourceWriterStub{},
		Judge:         judgeRunnerStub{},
	})

	run, err := service.GetRun(t.Context(), auth.Actor{UserID: 7, Role: auth.RoleUser}, 1)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.ID != 1 {
		t.Fatalf("GetRun() = %+v, want run 1", run)
	}
}

type languageStoreStub struct{}

func (languageStoreStub) ListLanguages(context.Context, ListLanguagesInput) ([]LanguageRecord, int64, error) {
	return []LanguageRecord{{ID: 71, Name: "Go", Enabled: true}}, 1, nil
}

func (languageStoreStub) UpsertLanguage(context.Context, judge.Language) (LanguageRecord, error) {
	return LanguageRecord{}, nil
}

func (languageStoreStub) UpdateLanguage(context.Context, int64, UpdateLanguageInput) (LanguageRecord, error) {
	return LanguageRecord{}, nil
}

func TestLanguageServiceUsesOnlyLanguageStore(t *testing.T) {
	service := NewLanguageService(languageStoreStub{}, languageProviderStub{})

	items, total, err := service.ListPublicLanguages(t.Context(), auth.Actor{UserID: 7, Role: auth.RoleUser}, ListLanguagesInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListPublicLanguages() error = %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != 71 {
		t.Fatalf("ListPublicLanguages() = (%+v, %d), want Go language", items, total)
	}
}

type submissionCompletionStoreStub struct {
	current SubmissionRecord
}

func (s *submissionCompletionStoreStub) GetSubmission(context.Context, int64) (SubmissionRecord, error) {
	return s.current, nil
}

func (s *submissionCompletionStoreStub) CompleteSubmissionWithResult(_ context.Context, _ int64, result judge.Result, score int32) (SubmissionRecord, error) {
	s.current.Status = string(result.Verdict)
	s.current.Score = score
	return s.current, nil
}

func TestSubmissionCompleterUsesOnlyCompletionStore(t *testing.T) {
	store := &submissionCompletionStoreStub{current: SubmissionRecord{ID: 1, Status: StatusRunning}}
	completer := NewSubmissionCompleter(store)

	completed, err := completer.CompleteSubmission(t.Context(), 1, judge.Result{Verdict: judge.VerdictAccepted})
	if err != nil {
		t.Fatalf("CompleteSubmission() error = %v", err)
	}
	if completed.Status != StatusAccepted || completed.Score != 100 {
		t.Fatalf("CompleteSubmission() = %+v", completed)
	}
}
