package submission

import (
	"context"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/problem"
)

type submissionCreationStore interface {
	GetEnabledLanguage(context.Context, int64) (LanguageRecord, error)
	CreateArtifact(context.Context, ArtifactRecord) (ArtifactRecord, error)
	CreateSubmissionWithTask(context.Context, SubmissionRecord, time.Time) (SubmissionRecord, JudgeTaskRecord, error)
}

// statsRefresher 让提交创建后刷新站级聚合缓存。接口留在本包而不是
// 引 stats 包：依赖方向是「领域服务通知缓存」，不是「领域服务依赖缓存」。
type statsRefresher interface {
	Refresh(context.Context)
}

// SubmissionCreator creates a submission and its pending judge task.
type SubmissionCreator struct {
	store         submissionCreationStore
	problems      problem.Reader
	sourceStore   sourceWriter
	contestPolicy ContestSubmissionPolicy
	stats         statsRefresher
	now           func() time.Time
}

type SubmissionCreatorOptions struct {
	Store         submissionCreationStore
	ProblemReader problem.Reader
	SourceStore   sourceWriter
	ContestPolicy ContestSubmissionPolicy
	// Stats 可选：提交落库成功后刷新站级聚合（题目数/提交数/语言数）。
	Stats statsRefresher
	Now   func() time.Time
}

func NewSubmissionCreator(options SubmissionCreatorOptions) *SubmissionCreator {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SubmissionCreator{
		store:         options.Store,
		problems:      options.ProblemReader,
		sourceStore:   options.SourceStore,
		contestPolicy: options.ContestPolicy,
		stats:         options.Stats,
		now:           now,
	}
}

func (s *SubmissionCreator) CreateSubmission(ctx context.Context, actor auth.Actor, input CreateSubmissionInput) (CreateSubmissionOutput, error) {
	if !actor.Authenticated() {
		return CreateSubmissionOutput{}, apperror.Unauthorized("auth_required", "authentication required")
	}
	if len(input.Source) == 0 {
		return CreateSubmissionOutput{}, apperror.BadRequest("source_required", "source is required")
	}
	problemForJudge, err := s.problems.GetForJudge(ctx, input.ProblemID)
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	if input.ContestID != nil && s.contestPolicy != nil {
		if err := s.contestPolicy.ValidateSubmission(ctx, actor, input.ProblemID, *input.ContestID); err != nil {
			return CreateSubmissionOutput{}, err
		}
	}
	if _, err := s.store.GetEnabledLanguage(ctx, input.LanguageID); err != nil {
		return CreateSubmissionOutput{}, err
	}

	object, err := s.sourceStore.Put(ctx, "submission", actor.UserID, input.Source)
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	artifact, err := s.store.CreateArtifact(ctx, ArtifactRecord{
		OwnerType:      "submission",
		OwnerID:        actor.UserID,
		Kind:           "source",
		StorageKey:     object.StorageKey,
		ChecksumSHA256: object.ChecksumSHA256,
		SizeBytes:      object.SizeBytes,
		ContentType:    object.ContentType,
	})
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	submission, task, err := s.store.CreateSubmissionWithTask(ctx, SubmissionRecord{
		UserID:           actor.UserID,
		ProblemID:        input.ProblemID,
		ContestID:        input.ContestID,
		LanguageID:       input.LanguageID,
		TestcaseSetID:    problemForJudge.CurrentTestcaseSetID,
		Status:           StatusQueued,
		SourceArtifactID: artifact.ID,
	}, s.now())
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	// PG 已提交成功，现在才轮到缓存：先 PG 后 Redis 的顺序在这里兑现。
	// 刷新失败不影响提交本身（Refresh 内部吞错记日志）。
	if s.stats != nil {
		s.stats.Refresh(ctx)
	}
	return CreateSubmissionOutput{Submission: submission, Task: task}, nil
}
