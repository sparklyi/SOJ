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

// SubmissionCreator creates a submission and its pending judge task.
type SubmissionCreator struct {
	store         submissionCreationStore
	problems      problem.Reader
	testcases     problem.TestcaseResolver
	sourceStore   sourceWriter
	contestPolicy ContestSubmissionPolicy
	now           func() time.Time
}

type SubmissionCreatorOptions struct {
	Store            submissionCreationStore
	ProblemReader    problem.Reader
	TestcaseResolver problem.TestcaseResolver
	SourceStore      sourceWriter
	ContestPolicy    ContestSubmissionPolicy
	Now              func() time.Time
}

func NewSubmissionCreator(options SubmissionCreatorOptions) *SubmissionCreator {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SubmissionCreator{
		store:         options.Store,
		problems:      options.ProblemReader,
		testcases:     options.TestcaseResolver,
		sourceStore:   options.SourceStore,
		contestPolicy: options.ContestPolicy,
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
	if _, err := s.problems.GetForJudge(ctx, input.ProblemID); err != nil {
		return CreateSubmissionOutput{}, err
	}
	if input.ContestID != nil && s.contestPolicy != nil {
		if err := s.contestPolicy.ValidateSubmission(ctx, actor, input.ProblemID, *input.ContestID); err != nil {
			return CreateSubmissionOutput{}, err
		}
	}
	testcaseSet, err := s.testcases.CurrentReadyTestcaseSet(ctx, input.ProblemID)
	if err != nil {
		return CreateSubmissionOutput{}, err
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
		TestcaseSetID:    testcaseSet.ID,
		Status:           StatusQueued,
		SourceArtifactID: artifact.ID,
	}, s.now())
	if err != nil {
		return CreateSubmissionOutput{}, err
	}
	return CreateSubmissionOutput{Submission: submission, Task: task}, nil
}
