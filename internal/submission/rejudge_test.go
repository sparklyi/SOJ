package submission

import (
	"context"
	"errors"
	"testing"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

func TestCreateRejudgeBatchValidatesTargetReasonAndAuthorization(t *testing.T) {
	tests := []struct {
		name     string
		actor    auth.Actor
		input    CreateRejudgeBatchInput
		policy   *fakeRejudgePolicy
		wantCode string
	}{
		{name: "anonymous", input: CreateRejudgeBatchInput{ProblemID: int64Ptr(1), Reason: "fixed data"}, policy: &fakeRejudgePolicy{}, wantCode: "auth_required"},
		{name: "missing target", actor: auth.Actor{UserID: 1, Role: auth.RoleUser}, input: CreateRejudgeBatchInput{Reason: "fixed data"}, policy: &fakeRejudgePolicy{}, wantCode: "rejudge.target_invalid"},
		{name: "both targets", actor: auth.Actor{UserID: 1, Role: auth.RoleUser}, input: CreateRejudgeBatchInput{ProblemID: int64Ptr(1), ContestID: int64Ptr(2), Reason: "fixed data"}, policy: &fakeRejudgePolicy{}, wantCode: "rejudge.target_invalid"},
		{name: "missing reason", actor: auth.Actor{UserID: 1, Role: auth.RoleUser}, input: CreateRejudgeBatchInput{ProblemID: int64Ptr(1)}, policy: &fakeRejudgePolicy{}, wantCode: "rejudge.reason_required"},
		{name: "problem forbidden", actor: auth.Actor{UserID: 1, Role: auth.RoleUser}, input: CreateRejudgeBatchInput{ProblemID: int64Ptr(1), Reason: "fixed data"}, policy: &fakeRejudgePolicy{problemErr: apperror.Forbidden("problem.not_allowed", "denied")}, wantCode: "problem.not_allowed"},
		{name: "contest not ended", actor: auth.Actor{UserID: 1, Role: auth.RoleUser}, input: CreateRejudgeBatchInput{ContestID: int64Ptr(2), Reason: "fixed data"}, policy: &fakeRejudgePolicy{contestErr: apperror.Conflict("rejudge.contest_not_ended", "contest must be ended")}, wantCode: "rejudge.contest_not_ended"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewRejudgeService(&fakeRejudgeRepository{}, tt.policy, time.Now)
			_, err := service.CreateBatch(context.Background(), tt.actor, tt.input)
			assertRejudgeCode(t, err, tt.wantCode)
		})
	}
}

func TestCreateRejudgeBatchDelegatesAtomicCreation(t *testing.T) {
	repo := &fakeRejudgeRepository{created: RejudgeBatchRecord{ID: 7, ProblemID: int64Ptr(11), RequestedBy: 5, Status: RejudgeBatchStatusQueued, TotalCount: 3}}
	policy := &fakeRejudgePolicy{}
	now := time.Unix(100, 0).UTC()
	service := NewRejudgeService(repo, policy, func() time.Time { return now })

	batch, err := service.CreateBatch(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRejudgeBatchInput{ProblemID: int64Ptr(11), Reason: "  testcase correction  "})
	if err != nil {
		t.Fatalf("CreateBatch returned error: %v", err)
	}
	if batch.ID != 7 || repo.createInput.RequestedBy != 5 || repo.createInput.Reason != "testcase correction" || !repo.createInput.NextRunAt.Equal(now) {
		t.Fatalf("batch=%+v input=%+v", batch, repo.createInput)
	}
	if policy.problemID != 11 {
		t.Fatalf("authorized problem = %d, want 11", policy.problemID)
	}
}

func TestCreateRejudgeBatchMapsEmptySelection(t *testing.T) {
	repo := &fakeRejudgeRepository{createErr: ErrNoRejudgeSubmissions}
	service := NewRejudgeService(repo, &fakeRejudgePolicy{}, time.Now)

	_, err := service.CreateBatch(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, CreateRejudgeBatchInput{ProblemID: int64Ptr(11), Reason: "retry"})

	assertRejudgeCode(t, err, "rejudge.no_submissions")
}

func TestCancelRejudgeBatchAuthorizesTarget(t *testing.T) {
	repo := &fakeRejudgeRepository{batch: RejudgeBatchRecord{ID: 9, ContestID: int64Ptr(22), RequestedBy: 5, Status: RejudgeBatchStatusRunning}}
	policy := &fakeRejudgePolicy{}
	service := NewRejudgeService(repo, policy, time.Now)

	batch, err := service.CancelBatch(context.Background(), auth.Actor{UserID: 5, Role: auth.RoleUser}, 9, "operator canceled")
	if err != nil {
		t.Fatalf("CancelBatch returned error: %v", err)
	}
	if batch.Status != RejudgeBatchStatusCanceled || policy.contestID != 22 || repo.cancelReason != "operator canceled" {
		t.Fatalf("batch=%+v contest=%d reason=%q", batch, policy.contestID, repo.cancelReason)
	}
}

func assertRejudgeCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want code %q", want)
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != want {
		t.Fatalf("error = %v, want code %q", err, want)
	}
}

type fakeRejudgePolicy struct {
	problemID      int64
	contestID      int64
	problemErr     error
	contestAuthErr error
	contestErr     error
}

func (p *fakeRejudgePolicy) AuthorizeProblemRejudge(ctx context.Context, actor auth.Actor, problemID int64) error {
	p.problemID = problemID
	return p.problemErr
}

func (p *fakeRejudgePolicy) AuthorizeContestRejudge(ctx context.Context, actor auth.Actor, contestID int64) error {
	p.contestID = contestID
	return p.contestAuthErr
}

func (p *fakeRejudgePolicy) ValidateContestRejudgeTarget(ctx context.Context, contestID int64) error {
	return p.contestErr
}

type fakeRejudgeRepository struct {
	created      RejudgeBatchRecord
	batch        RejudgeBatchRecord
	createInput  CreateRejudgeBatchRecordInput
	createErr    error
	cancelReason string
}

func (r *fakeRejudgeRepository) CreateRejudgeBatchWithItems(ctx context.Context, input CreateRejudgeBatchRecordInput) (RejudgeBatchRecord, error) {
	r.createInput = input
	return r.created, r.createErr
}

func (r *fakeRejudgeRepository) GetRejudgeBatch(ctx context.Context, id int64) (RejudgeBatchRecord, error) {
	if r.batch.ID == 0 {
		return RejudgeBatchRecord{}, errors.New("not found")
	}
	return r.batch, nil
}

func (r *fakeRejudgeRepository) ListRejudgeBatches(ctx context.Context, input ListRejudgeBatchesInput) ([]RejudgeBatchRecord, int64, error) {
	return nil, 0, nil
}

func (r *fakeRejudgeRepository) ListRejudgeBatchItems(ctx context.Context, batchID int64) ([]RejudgeBatchItemRecord, error) {
	return nil, nil
}

func (r *fakeRejudgeRepository) CancelRejudgeBatch(ctx context.Context, id int64, reason string) (RejudgeBatchRecord, error) {
	r.cancelReason = reason
	r.batch.Status = RejudgeBatchStatusCanceled
	return r.batch, nil
}
