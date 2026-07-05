package submission

import (
	"context"
	"fmt"
	"time"

	"SOJ/internal/judge"
)

type memoryRepo struct {
	nextID              int64
	artifacts           map[int64]ArtifactRecord
	submissions         map[int64]SubmissionRecord
	runs                map[int64]RunRecord
	tasks               map[int64]JudgeTaskRecord
	languages           map[int64]LanguageRecord
	attempts            map[int64]JudgeAttemptRecord
	cases               map[int64][]JudgeCaseResultRecord
	results             map[int64]SubmissionResultRecord
	submissionUpdates   int
	events              []string
	failCreateJudgeTask error
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		nextID:      100,
		artifacts:   make(map[int64]ArtifactRecord),
		submissions: make(map[int64]SubmissionRecord),
		runs:        make(map[int64]RunRecord),
		tasks:       make(map[int64]JudgeTaskRecord),
		languages:   make(map[int64]LanguageRecord),
		attempts:    make(map[int64]JudgeAttemptRecord),
		cases:       make(map[int64][]JudgeCaseResultRecord),
		results:     make(map[int64]SubmissionResultRecord),
	}
}

func (r *memoryRepo) id() int64 {
	r.nextID++
	return r.nextID
}

func (r *memoryRepo) CreateArtifact(ctx context.Context, arg ArtifactRecord) (ArtifactRecord, error) {
	arg.ID = r.id()
	r.artifacts[arg.ID] = arg
	return arg, nil
}
func (r *memoryRepo) GetArtifact(ctx context.Context, id int64) (ArtifactRecord, error) {
	return r.artifacts[id], nil
}
func (r *memoryRepo) CreateSubmission(ctx context.Context, arg SubmissionRecord) (SubmissionRecord, error) {
	arg.ID = r.id()
	r.submissions[arg.ID] = arg
	return arg, nil
}
func (r *memoryRepo) CreateSubmissionWithTask(ctx context.Context, arg SubmissionRecord, nextRunAt time.Time) (SubmissionRecord, JudgeTaskRecord, error) {
	if r.failCreateJudgeTask != nil {
		return SubmissionRecord{}, JudgeTaskRecord{}, r.failCreateJudgeTask
	}
	submission, err := r.CreateSubmission(ctx, arg)
	if err != nil {
		return SubmissionRecord{}, JudgeTaskRecord{}, err
	}
	task, err := r.CreateJudgeTask(ctx, submission.ID, nextRunAt)
	if err != nil {
		delete(r.submissions, submission.ID)
		return SubmissionRecord{}, JudgeTaskRecord{}, err
	}
	return submission, task, nil
}
func (r *memoryRepo) GetSubmission(ctx context.Context, id int64) (SubmissionRecord, error) {
	return r.submissions[id], nil
}
func (r *memoryRepo) ListSubmissions(ctx context.Context, input ListSubmissionsInput) ([]SubmissionRecord, int64, error) {
	var rows []SubmissionRecord
	for _, row := range r.submissions {
		if input.UserID != nil && row.UserID != *input.UserID {
			continue
		}
		if input.ProblemID != nil && row.ProblemID != *input.ProblemID {
			continue
		}
		if input.ContestID != nil {
			if row.ContestID == nil || *row.ContestID != *input.ContestID {
				continue
			}
		}
		if input.Status != nil && row.Status != *input.Status {
			continue
		}
		rows = append(rows, row)
	}
	return rows, int64(len(rows)), nil
}
func (r *memoryRepo) MarkSubmissionRunning(ctx context.Context, id int64) (SubmissionRecord, error) {
	row := r.submissions[id]
	if row.Status == StatusQueued {
		row.Status = StatusRunning
	}
	r.submissions[id] = row
	return row, nil
}
func (r *memoryRepo) MarkSubmissionQueued(ctx context.Context, id int64, reason string) (SubmissionRecord, error) {
	row := r.submissions[id]
	if !terminalStatus(row.Status) {
		row.Status = StatusQueued
		row.ErrorMessage = stringPtr(reason)
	}
	r.submissions[id] = row
	return row, nil
}
func (r *memoryRepo) MarkSubmissionSystemError(ctx context.Context, id int64, reason string) (SubmissionRecord, error) {
	row := r.submissions[id]
	if !terminalStatus(row.Status) {
		row.Status = StatusSystemErr
		row.ErrorMessage = stringPtr(reason)
	}
	r.submissions[id] = row
	return row, nil
}
func (r *memoryRepo) CompleteSubmissionWithResult(ctx context.Context, id int64, result judge.Result, score int32) (SubmissionRecord, error) {
	row := r.submissions[id]
	row.Status = dbStatus(result.Verdict)
	row.Score = score
	row.TimeMS = int32Ptr(int32(result.TimeMS))
	row.MemoryKB = int32Ptr(int32(result.MemoryKB))
	row.ErrorMessage = stringPtr(result.ErrorMessage)
	r.submissionUpdates++
	r.submissions[id] = row
	attemptNo := int32(1)
	for _, attempt := range r.attempts {
		if attempt.SubmissionID != nil && *attempt.SubmissionID == id && attempt.AttemptNo >= attemptNo {
			attemptNo = attempt.AttemptNo + 1
		}
	}
	attemptID := r.id()
	attempt := JudgeAttemptRecord{
		ID:                   attemptID,
		SubmissionID:         &row.ID,
		AttemptNo:            attemptNo,
		ProtocolVersion:      judge.ProtocolVersion,
		JudgeCoreVersion:     result.Manifest.JudgeCoreVersion,
		JudgeEngine:          judge.EngineSOJAgent,
		JudgeAgentID:         stringPtr(result.Manifest.JudgeAgentID),
		LanguageID:           row.LanguageID,
		LanguageRuntime:      stringPtr(result.Manifest.LanguageRuntime),
		SandboxBackend:       stringPtr(result.Manifest.SandboxBackend),
		SandboxProfile:       stringPtr(result.Manifest.SandboxProfile),
		TestcaseSetID:        &row.TestcaseSetID,
		TestcaseSetHash:      stringPtr(result.Manifest.TestcaseSetHash),
		CheckerHash:          stringPtr(result.Manifest.CheckerHash),
		ValidatorHash:        stringPtr(result.Manifest.ValidatorHash),
		Status:               "finished",
		Verdict:              stringPtr(string(result.Verdict)),
		Score:                score,
		TimeMS:               int32Ptr(int32(result.TimeMS)),
		MemoryKB:             int32Ptr(int32(result.MemoryKB)),
		FirstFailedCaseIndex: firstFailedCaseIndex(result.Cases),
		FirstFailedGroup:     firstFailedGroup(result.Cases),
		CheckerMessage:       firstCheckerMessage(result.Cases),
		ErrorMessage:         stringPtr(result.ErrorMessage),
		TraceID:              stringPtr(result.Manifest.TraceID),
	}
	if attempt.JudgeCoreVersion == "" {
		attempt.JudgeCoreVersion = judge.ProtocolVersion
	}
	r.attempts[attemptID] = attempt
	for _, item := range result.Cases {
		r.cases[attemptID] = append(r.cases[attemptID], JudgeCaseResultRecord{
			ID:                r.id(),
			AttemptID:         attemptID,
			CaseIndex:         int32(item.Index),
			GroupName:         stringPtr(item.GroupName),
			TestcaseKey:       stringPtr(item.TestcaseKey),
			Status:            dbStatus(item.Verdict),
			Score:             item.Score,
			TimeMS:            int32Ptr(int32(item.TimeMS)),
			MemoryKB:          int32Ptr(int32(item.MemoryKB)),
			ExitCode:          item.ExitCode,
			Signal:            stringPtr(item.Signal),
			CheckerMessage:    stringPtr(item.CheckerMessage),
			OutputDiffSummary: stringPtr(item.OutputDiffSummary),
		})
	}
	r.results[id] = SubmissionResultRecord{
		SubmissionID:         id,
		AttemptID:            attemptID,
		Status:               row.Status,
		Score:                score,
		TimeMS:               int32Ptr(int32(result.TimeMS)),
		MemoryKB:             int32Ptr(int32(result.MemoryKB)),
		FirstFailedCaseIndex: firstFailedCaseIndex(result.Cases),
		FirstFailedGroup:     firstFailedGroup(result.Cases),
		SafeSummary:          []byte(`{"verdict":"` + string(result.Verdict) + `"}`),
	}
	return row, nil
}
func (r *memoryRepo) GetLatestJudgeAttemptBySubmissionID(ctx context.Context, submissionID int64) (JudgeAttemptRecord, error) {
	var latest JudgeAttemptRecord
	for _, attempt := range r.attempts {
		if attempt.SubmissionID == nil || *attempt.SubmissionID != submissionID {
			continue
		}
		if latest.ID == 0 || attempt.AttemptNo > latest.AttemptNo || (attempt.AttemptNo == latest.AttemptNo && attempt.ID > latest.ID) {
			latest = attempt
		}
	}
	if latest.ID == 0 {
		return JudgeAttemptRecord{}, fmt.Errorf("judge attempt not found")
	}
	return latest, nil
}
func (r *memoryRepo) ListJudgeCaseResults(ctx context.Context, attemptID int64) ([]JudgeCaseResultRecord, error) {
	return append([]JudgeCaseResultRecord(nil), r.cases[attemptID]...), nil
}
func (r *memoryRepo) GetSubmissionResult(ctx context.Context, submissionID int64) (SubmissionResultRecord, error) {
	row, ok := r.results[submissionID]
	if !ok {
		return SubmissionResultRecord{}, fmt.Errorf("submission result not found")
	}
	return row, nil
}
func (r *memoryRepo) CreateJudgeTask(ctx context.Context, submissionID int64, nextRunAt time.Time) (JudgeTaskRecord, error) {
	if r.failCreateJudgeTask != nil {
		return JudgeTaskRecord{}, r.failCreateJudgeTask
	}
	row := JudgeTaskRecord{ID: r.id(), SubmissionID: submissionID, Status: "pending", NextRunAt: nextRunAt}
	r.tasks[row.ID] = row
	return row, nil
}
func (r *memoryRepo) GetJudgeTask(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	return r.tasks[id], nil
}
func (r *memoryRepo) ClaimPendingJudgeTasks(ctx context.Context, limit int32) ([]JudgeTaskRecord, error) {
	var rows []JudgeTaskRecord
	for _, task := range r.tasks {
		if task.Status == "pending" {
			rows = append(rows, task)
		}
	}
	return rows, nil
}
func (r *memoryRepo) MarkJudgeTaskDispatching(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	row := r.tasks[id]
	row.Status = "dispatching"
	r.tasks[id] = row
	return row, nil
}
func (r *memoryRepo) MarkJudgeTaskDispatched(ctx context.Context, id int64, streamID string) (JudgeTaskRecord, error) {
	row := r.tasks[id]
	row.Status = "dispatched"
	row.StreamID = streamID
	r.tasks[id] = row
	return row, nil
}
func (r *memoryRepo) MarkJudgeTaskRunning(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	row := r.tasks[id]
	row.Status = "running"
	r.tasks[id] = row
	return row, nil
}
func (r *memoryRepo) MarkJudgeTaskDone(ctx context.Context, id int64) (JudgeTaskRecord, error) {
	row := r.tasks[id]
	row.Status = "done"
	r.tasks[id] = row
	return row, nil
}
func (r *memoryRepo) RetryJudgeTask(ctx context.Context, id int64, nextRunAt time.Time, reason string) (JudgeTaskRecord, error) {
	row := r.tasks[id]
	row.Status = "pending"
	row.Attempts++
	row.NextRunAt = nextRunAt
	row.LastError = reason
	r.tasks[id] = row
	return row, nil
}
func (r *memoryRepo) MarkJudgeTaskDead(ctx context.Context, id int64, reason string) (JudgeTaskRecord, error) {
	row := r.tasks[id]
	row.Status = "dead"
	row.LastError = reason
	r.tasks[id] = row
	r.events = append(r.events, "db_dead")
	return row, nil
}
func (r *memoryRepo) CreateRun(ctx context.Context, arg RunRecord) (RunRecord, error) {
	arg.ID = r.id()
	r.runs[arg.ID] = arg
	return arg, nil
}
func (r *memoryRepo) GetRun(ctx context.Context, id int64) (RunRecord, error) {
	return r.runs[id], nil
}
func (r *memoryRepo) UpdateRunStatus(ctx context.Context, id int64, result judge.Result) (RunRecord, error) {
	row := r.runs[id]
	row.Status = dbStatus(result.Verdict)
	row.Stdout = result.Stdout
	row.Stderr = result.Stderr
	row.CompileOutput = result.CompileOutput
	row.TimeMS = int32Ptr(int32(result.TimeMS))
	row.MemoryKB = int32Ptr(int32(result.MemoryKB))
	row.ErrorMessage = stringPtr(result.ErrorMessage)
	r.runs[id] = row
	return row, nil
}
func (r *memoryRepo) ResetStaleJudgeTasks(ctx context.Context, staleBefore time.Time, reason string) ([]JudgeTaskRecord, error) {
	var rows []JudgeTaskRecord
	for id, row := range r.tasks {
		if row.Status != "dispatching" && row.Status != "running" {
			continue
		}
		row.Status = "pending"
		row.LastError = reason
		r.tasks[id] = row
		if submission := r.submissions[row.SubmissionID]; submission.Status == StatusRunning {
			submission.Status = StatusQueued
			submission.ErrorMessage = stringPtr(reason)
			r.submissions[row.SubmissionID] = submission
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func (r *memoryRepo) MarkStaleRunsSystemError(ctx context.Context, staleBefore time.Time, reason string) ([]RunRecord, error) {
	var rows []RunRecord
	for id, row := range r.runs {
		if row.Status == StatusQueued || row.Status == StatusRunning {
			row.Status = StatusSystemErr
			row.ErrorMessage = stringPtr(reason)
			r.runs[id] = row
			rows = append(rows, row)
		}
	}
	return rows, nil
}
func (r *memoryRepo) GetEnabledLanguage(ctx context.Context, id int64) (LanguageRecord, error) {
	row := r.languages[id]
	if !row.Enabled {
		return LanguageRecord{}, fmt.Errorf("language disabled")
	}
	return row, nil
}
func (r *memoryRepo) ListLanguages(ctx context.Context, arg ListLanguagesInput) ([]LanguageRecord, int64, error) {
	var rows []LanguageRecord
	for _, row := range r.languages {
		rows = append(rows, row)
	}
	return rows, int64(len(rows)), nil
}
func (r *memoryRepo) UpsertLanguage(ctx context.Context, language judge.Language) (LanguageRecord, error) {
	row := LanguageRecord{ID: language.ID, Engine: judge.EngineSOJAgent, Name: language.Name, Enabled: language.Enabled}
	r.languages[row.ID] = row
	return row, nil
}
func (r *memoryRepo) UpdateLanguage(ctx context.Context, id int64, arg UpdateLanguageInput) (LanguageRecord, error) {
	row := r.languages[id]
	if arg.Enabled != nil {
		row.Enabled = *arg.Enabled
	}
	r.languages[id] = row
	return row, nil
}

func int32Ptr(value int32) *int32 { return &value }
func firstFailedCaseIndex(cases []judge.CaseResult) *int32 {
	for _, item := range cases {
		if item.Verdict != judge.VerdictAccepted {
			value := int32(item.Index)
			return &value
		}
	}
	return nil
}
func firstFailedGroup(cases []judge.CaseResult) *string {
	for _, item := range cases {
		if item.Verdict != judge.VerdictAccepted {
			return stringPtr(item.GroupName)
		}
	}
	return nil
}
func firstCheckerMessage(cases []judge.CaseResult) *string {
	for _, item := range cases {
		if item.CheckerMessage != "" {
			return stringPtr(item.CheckerMessage)
		}
	}
	return nil
}
func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
