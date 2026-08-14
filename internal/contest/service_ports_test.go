package contest

import (
	"context"
	"testing"
	"time"

	"SOJ/internal/auth"
)

type contestReaderStoreStub struct{}

func (contestReaderStoreStub) GetContest(context.Context, int64) (ContestRecord, error) {
	return ContestRecord{ID: 1, OwnerUserID: 7, Visibility: VisibilityPublic, Status: StatusPublished}, nil
}

func (contestReaderStoreStub) ListContests(context.Context, ListContestFilter) ([]ContestRecord, int64, error) {
	return nil, 0, nil
}

func (contestReaderStoreStub) ListContestsByCursor(context.Context, ListContestFilter) ([]ContestRecord, error) {
	return nil, nil
}

func (contestReaderStoreStub) ListContestProblems(context.Context, int64) ([]ContestProblem, error) {
	return []ContestProblem{{ProblemID: 101, Alias: "A"}}, nil
}

func (contestReaderStoreStub) GetRegistration(context.Context, int64, int64) (ContestRegistration, error) {
	return ContestRegistration{}, nil
}

func (contestReaderStoreStub) ListRegistrations(context.Context, int64) ([]ContestRegistration, error) {
	return nil, nil
}

type contestAuthoringStoreStub struct{}

func (contestAuthoringStoreStub) GetContest(context.Context, int64) (ContestRecord, error) {
	return ContestRecord{ID: 1, OwnerUserID: 7, Visibility: VisibilityPublic, Status: StatusDraft}, nil
}

func (contestAuthoringStoreStub) ArchiveContest(_ context.Context, id int64) (ContestRecord, error) {
	return ContestRecord{ID: id, Status: StatusArchived}, nil
}

func (contestAuthoringStoreStub) WithTx(ctx context.Context, fn func(context.Context, contestTransaction) error) error {
	return fn(ctx, contestTransactionStub{})
}

type contestRegistrationWriterStub struct{}

func (contestRegistrationWriterStub) CreateRegistration(_ context.Context, registration ContestRegistration) (ContestRegistration, error) {
	return registration, nil
}

type scoreboardStoreStub struct{}

func (scoreboardStoreStub) ListProblemResults(context.Context, int64) ([]ContestProblemResult, error) {
	return nil, nil
}

func (scoreboardStoreStub) ListScoreboardRegistrations(context.Context, int64, scoreboardRowsQuery) ([]ContestRegistration, error) {
	return nil, nil
}

func (scoreboardStoreStub) ListProblemResultsForUsers(context.Context, int64, []int64) ([]ContestProblemResult, error) {
	return nil, nil
}

func (scoreboardStoreStub) ListTerminalSubmissions(context.Context, int64) ([]ContestSubmissionResult, error) {
	return nil, nil
}

func (scoreboardStoreStub) ListScoreSnapshotCandidates(context.Context, time.Time, int32) ([]ScoreSnapshotCandidate, error) {
	return nil, nil
}

func (scoreboardStoreStub) CreateScoreSnapshot(context.Context, ScoreboardSnapshot) (ScoreboardSnapshot, error) {
	return ScoreboardSnapshot{}, nil
}

func (scoreboardStoreStub) LatestScoreSnapshot(context.Context, int64, ScoreboardView) (ScoreboardSnapshot, error) {
	return ScoreboardSnapshot{}, nil
}

func (scoreboardStoreStub) ScoreSnapshotPage(context.Context, int64, ScoreboardView, int32, int32) (scoreSnapshotPageResult, error) {
	return scoreSnapshotPageResult{}, nil
}

type contestTransactionStub struct{}

func (contestTransactionStub) CreateContest(context.Context, ContestRecord) (ContestRecord, error) {
	return ContestRecord{ID: 1}, nil
}

func (contestTransactionStub) UpdateContest(context.Context, int64, ContestUpdateInput) (ContestRecord, error) {
	return ContestRecord{ID: 1}, nil
}

func (contestTransactionStub) ReplaceContestProblems(context.Context, int64, []ContestProblem) error {
	return nil
}

func TestContestComponentsUseFocusedPorts(t *testing.T) {
	reader := NewContestReader(contestReaderStoreStub{}, nil)
	authoring := NewContestAuthoring(contestAuthoringStoreStub{}, reader)
	policy := NewContestPolicy(reader, contestRegistrationWriterStub{})
	scoreboard := NewScoreboardService(reader, scoreboardStoreStub{})

	if _, err := reader.GetContest(t.Context(), auth.Anonymous("request"), 1); err != nil {
		t.Fatalf("ContestReader.GetContest() error = %v", err)
	}
	if _, err := authoring.DeleteContest(t.Context(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, 1); err != nil {
		t.Fatalf("ContestAuthoring.DeleteContest() error = %v", err)
	}
	if _, err := policy.Register(t.Context(), auth.Actor{UserID: 8, Role: auth.RoleUser}, 1, RegistrationInput{DisplayName: "alice", Email: "alice@example.com"}); err != nil {
		t.Fatalf("ContestPolicy.Register() error = %v", err)
	}
	if _, err := scoreboard.GenerateDueScoreSnapshots(t.Context(), 1); err != nil {
		t.Fatalf("ScoreboardService.GenerateDueScoreSnapshots() error = %v", err)
	}
}

func TestServiceAuthorizeContestRejudgeUsesReaderComponent(t *testing.T) {
	reader := NewContestReader(contestReaderStoreStub{}, nil)
	service := NewService(
		reader,
		NewContestAuthoring(contestAuthoringStoreStub{}, reader),
		NewContestPolicy(reader, contestRegistrationWriterStub{}),
		NewScoreboardService(reader, scoreboardStoreStub{}),
	)

	if err := service.AuthorizeContestRejudge(t.Context(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, 1); err != nil {
		t.Fatalf("AuthorizeContestRejudge() error = %v", err)
	}
}

func TestContestConstructorsRejectMissingDependencies(t *testing.T) {
	reader := NewContestReader(contestReaderStoreStub{}, nil)
	tests := []struct {
		name string
		new  func()
	}{
		{name: "reader store", new: func() { NewContestReader(nil, nil) }},
		{name: "authoring store", new: func() { NewContestAuthoring(nil, reader) }},
		{name: "authoring reader", new: func() { NewContestAuthoring(contestAuthoringStoreStub{}, nil) }},
		{name: "policy reader", new: func() { NewContestPolicy(nil, contestRegistrationWriterStub{}) }},
		{name: "policy writer", new: func() { NewContestPolicy(reader, nil) }},
		{name: "scoreboard reader", new: func() { NewScoreboardService(nil, scoreboardStoreStub{}) }},
		{name: "scoreboard store", new: func() { NewScoreboardService(reader, nil) }},
		{name: "service reader", new: func() {
			NewService(nil, NewContestAuthoring(contestAuthoringStoreStub{}, reader), NewContestPolicy(reader, contestRegistrationWriterStub{}), NewScoreboardService(reader, scoreboardStoreStub{}))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("constructor did not panic")
				}
			}()
			tt.new()
		})
	}
}
