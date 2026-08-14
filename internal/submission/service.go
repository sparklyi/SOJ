package submission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/judge"
)

const (
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusAccepted    = "accepted"
	StatusWrongAnswer = "wrong_answer"
	StatusCompileErr  = "compile_error"
	StatusRuntimeErr  = "runtime_error"
	StatusTimeLimit   = "time_limit"
	StatusMemoryLimit = "memory_limit"
	StatusOutputLimit = "output_limit"
	StatusSystemErr   = "system_error"
	StatusCanceled    = "canceled"

	defaultRunShortWait       = 3 * time.Second
	defaultRunTimeout         = 2 * time.Minute
	defaultRunParallelism     = 1
	defaultRunFinalizeTimeout = 5 * time.Second
)

type SourceObject struct {
	StorageKey     string
	ChecksumSHA256 string
	SizeBytes      int64
	ContentType    string
}

type MemorySourceStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemorySourceStore() *MemorySourceStore {
	return &MemorySourceStore{objects: make(map[string][]byte)}
}

func (s *MemorySourceStore) Put(_ context.Context, ownerType string, ownerID int64, source []byte) (SourceObject, error) {
	sum := sha256.Sum256(source)
	checksum := hex.EncodeToString(sum[:])
	key := fmt.Sprintf("%s/%d/%s", ownerType, ownerID, checksum)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = append([]byte(nil), source...)
	return SourceObject{StorageKey: key, ChecksumSHA256: checksum, SizeBytes: int64(len(source)), ContentType: "text/plain; charset=utf-8"}, nil
}

func (s *MemorySourceStore) Get(_ context.Context, storageKey string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.objects[storageKey]
	if !ok {
		return nil, apperror.NotFound("source_not_found", "source artifact not found")
	}
	return append([]byte(nil), source...), nil
}

type ContestSubmissionPolicy interface {
	ValidateSubmission(ctx context.Context, actor auth.Actor, problemID, contestID int64) error
}

type ContestResultVisibilityPolicy interface {
	SubmissionResultVisibility(ctx context.Context, actor auth.Actor, submission ContestSubmissionVisibility) (SubmissionResultVisibility, error)
}

type ContestResultVisibilityBatchPolicy interface {
	ContestResultVisibilityPolicy
	SubmissionResultVisibilities(ctx context.Context, actor auth.Actor, submissions []ContestSubmissionVisibility) (map[int64]SubmissionResultVisibility, error)
}

type ContestSubmissionVisibility struct {
	ID          int64
	UserID      int64
	ProblemID   int64
	ContestID   int64
	SubmittedAt time.Time
	JudgedAt    *time.Time
}

type SubmissionResultVisibility struct {
	ShowResult           bool
	ShowCases            bool
	ShowAdminDiagnostics bool
	Visibility           string
}

// Service is the HTTP-facing composition of submission use cases.
type Service struct {
	creator   *SubmissionCreator
	reader    *SubmissionReader
	runs      *RunService
	languages *LanguageService
	completer *SubmissionCompleter
}

func NewService(creator *SubmissionCreator, reader *SubmissionReader, runs *RunService, languages *LanguageService, completer *SubmissionCompleter) *Service {
	return &Service{
		creator:   creator,
		reader:    reader,
		runs:      runs,
		languages: languages,
		completer: completer,
	}
}

type CreateSubmissionInput struct {
	ProblemID  int64
	ContestID  *int64
	LanguageID int64
	Source     []byte
}

type CreateSubmissionOutput struct {
	Submission SubmissionRecord
	Task       JudgeTaskRecord
	StreamID   string
}

type CreateRunInput struct {
	ProblemID  int64
	LanguageID int64
	Source     []byte
	Stdin      string
}

type CreateRunOutput struct {
	Run RunRecord
}

type SubmissionView struct {
	Submission       SubmissionRecord
	Result           *SubmissionResultRecord
	Cases            []JudgeCaseResultRecord
	AdminDiagnostics *JudgeAttemptRecord
	Visibility       string
}

type ListOwnSubmissionsCursorInput struct {
	Cursor *SubmissionCursor
	Limit  int32
}

type SubmissionCursorPage struct {
	Items      []SubmissionView
	NextCursor *SubmissionCursor
}

func (s *Service) CreateSubmission(ctx context.Context, actor auth.Actor, input CreateSubmissionInput) (CreateSubmissionOutput, error) {
	return s.creator.CreateSubmission(ctx, actor, input)
}

func (s *Service) GetSubmission(ctx context.Context, actor auth.Actor, id int64) (SubmissionView, error) {
	return s.reader.GetSubmission(ctx, actor, id)
}

func (s *Service) ListSubmissions(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) ([]SubmissionView, int64, error) {
	return s.reader.ListSubmissions(ctx, actor, input)
}

func (s *Service) ListSubmissionsByCursor(ctx context.Context, actor auth.Actor, input ListSubmissionsInput) (SubmissionCursorPage, error) {
	return s.reader.ListSubmissionsByCursor(ctx, actor, input)
}

func (s *Service) ListOwnSubmissionsByCursor(ctx context.Context, actor auth.Actor, input ListOwnSubmissionsCursorInput) (SubmissionCursorPage, error) {
	return s.reader.ListOwnSubmissionsByCursor(ctx, actor, input)
}

func (s *Service) CreateRun(ctx context.Context, actor auth.Actor, input CreateRunInput) (CreateRunOutput, error) {
	return s.runs.CreateRun(ctx, actor, input)
}

func (s *Service) GetRun(ctx context.Context, actor auth.Actor, id int64) (RunRecord, error) {
	return s.runs.GetRun(ctx, actor, id)
}

func (s *Service) CompleteRun(ctx context.Context, runID int64, result judge.Result) (RunRecord, error) {
	return s.runs.CompleteRun(ctx, runID, result)
}

func (s *Service) CompleteSubmission(ctx context.Context, submissionID int64, result judge.Result) (SubmissionRecord, error) {
	return s.completer.CompleteSubmission(ctx, submissionID, result)
}

func (s *Service) ListLanguages(ctx context.Context, actor auth.Actor, input ListLanguagesInput) ([]LanguageRecord, int64, error) {
	return s.languages.ListLanguages(ctx, actor, input)
}

func (s *Service) ListPublicLanguages(ctx context.Context, actor auth.Actor, input ListLanguagesInput) ([]LanguageRecord, int64, error) {
	return s.languages.ListPublicLanguages(ctx, actor, input)
}

func (s *Service) SyncLanguages(ctx context.Context, actor auth.Actor) ([]LanguageRecord, error) {
	return s.languages.SyncLanguages(ctx, actor)
}

func (s *Service) UpdateLanguage(ctx context.Context, actor auth.Actor, id int64, input UpdateLanguageInput) (LanguageRecord, error) {
	return s.languages.UpdateLanguage(ctx, actor, id, input)
}

// Close stops direct run execution and waits for admitted work to finish.
func (s *Service) Close(ctx context.Context) error {
	return s.runs.Close(ctx)
}

func contestSubmissionVisibility(record SubmissionRecord) ContestSubmissionVisibility {
	return ContestSubmissionVisibility{
		ID:          record.ID,
		UserID:      record.UserID,
		ProblemID:   record.ProblemID,
		ContestID:   *record.ContestID,
		SubmittedAt: record.SubmittedAt,
		JudgedAt:    record.JudgedAt,
	}
}

type ListLanguagesInput struct {
	Enabled *bool
	Engine  *string
	Offset  int32
	Limit   int32
}

type UpdateLanguageInput struct {
	Enabled              *bool
	DefaultTimeLimitMS   *int32
	DefaultMemoryLimitKB *int32
}

func terminalStatus(status string) bool {
	switch status {
	case StatusAccepted, StatusWrongAnswer, StatusCompileErr, StatusRuntimeErr, StatusTimeLimit, StatusMemoryLimit, StatusOutputLimit, StatusSystemErr, StatusCanceled:
		return true
	default:
		return false
	}
}

func dbStatus(verdict judge.Verdict) string {
	switch verdict {
	case judge.VerdictAccepted:
		return StatusAccepted
	case judge.VerdictWrongAnswer:
		return StatusWrongAnswer
	case judge.VerdictCompileError:
		return StatusCompileErr
	case judge.VerdictRuntimeError:
		return StatusRuntimeErr
	case judge.VerdictTimeLimitExceeded, judge.VerdictTimeLimit:
		return StatusTimeLimit
	case judge.VerdictMemoryLimitExceeded, judge.VerdictMemoryLimit:
		return StatusMemoryLimit
	case judge.VerdictOutputLimit:
		return StatusOutputLimit
	case judge.VerdictSystemError:
		return StatusSystemErr
	case judge.VerdictCanceled:
		return StatusCanceled
	default:
		return StatusSystemErr
	}
}
