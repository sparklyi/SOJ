package problem

import (
	"context"
	"io"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/storage"
)

type testcaseArchiveReaderStub struct{}

func (testcaseArchiveReaderStub) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return nil, storage.ObjectInfo{}, nil
}

type testcaseArchiveWriterStub struct{}

func (testcaseArchiveWriterStub) Put(context.Context, storage.Object) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}

func (testcaseArchiveWriterStub) Delete(context.Context, string) error {
	return nil
}

type problemReaderStoreStub struct{}

func (problemReaderStoreStub) GetProblem(context.Context, int64) (ProblemRecord, error) {
	return ProblemRecord{ID: 7, OwnerUserID: 3, Status: StatusPublished, Visibility: VisibilityPublic}, nil
}

func (problemReaderStoreStub) ListProblems(context.Context, ListProblemsFilter) ([]ProblemRecord, error) {
	return nil, nil
}

func (problemReaderStoreStub) ListProblemsByCursor(context.Context, ListProblemsFilter) ([]ProblemRecord, error) {
	return nil, nil
}

func (problemReaderStoreStub) CountProblems(context.Context, ListProblemsFilter) (int64, error) {
	return 0, nil
}

func (problemReaderStoreStub) GetCurrentProblemStatement(context.Context, int64) (Statement, error) {
	return Statement{}, nil
}

func (problemReaderStoreStub) ListProblemTags(context.Context, int64) ([]Tag, error) {
	return nil, nil
}

func (problemReaderStoreStub) GetCurrentReadyTestcaseSet(context.Context, int64) (TestcaseSetRecord, error) {
	return TestcaseSetRecord{}, nil
}

func (problemReaderStoreStub) GetLatestCompletedProblemCheckRun(context.Context, int64, int64, int64) (ProblemCheckRunRecord, error) {
	return ProblemCheckRunRecord{}, nil
}

func (problemReaderStoreStub) ListProblemCheckFindings(context.Context, int64) ([]ProblemCheckFindingRecord, error) {
	return nil, nil
}

func (problemReaderStoreStub) GetProblemStats(context.Context, int64) (ProblemStats, error) {
	return ProblemStats{}, nil
}

func TestProblemReaderUsesOnlyReadStore(t *testing.T) {
	reader := NewProblemReader(problemReaderStoreStub{}, testcaseArchiveReaderStub{})

	got, err := reader.GetProblem(t.Context(), auth.Actor{}, 7)
	if err != nil {
		t.Fatalf("GetProblem() error = %v", err)
	}
	if got.ID != 7 {
		t.Fatalf("GetProblem().ID = %d, want 7", got.ID)
	}
}

type problemAuthoringStoreStub struct {
	tx problemAuthoringTxStub
}

func (s *problemAuthoringStoreStub) GetProblem(context.Context, int64) (ProblemRecord, error) {
	return ProblemRecord{ID: 7, OwnerUserID: 3, Status: StatusDraft}, nil
}

func (s *problemAuthoringStoreStub) WithProblemAuthoringTx(ctx context.Context, fn func(context.Context, problemAuthoringTx) error) error {
	return fn(ctx, &s.tx)
}

type problemAuthoringTxStub struct {
	nextID int64
}

func (s *problemAuthoringTxStub) CreateProblem(_ context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error) {
	s.nextID++
	return ProblemRecord{ID: s.nextID, OwnerUserID: ownerUserID, Title: input.Title, Status: StatusDraft}, nil
}

func (*problemAuthoringTxStub) LockProblemForUpdate(context.Context, int64) (ProblemRecord, error) {
	return ProblemRecord{ID: 7, OwnerUserID: 3, Status: StatusDraft}, nil
}

func (*problemAuthoringTxStub) UpdateProblem(_ context.Context, id int64, _ UpdateProblemInput) (ProblemRecord, error) {
	return ProblemRecord{ID: id, OwnerUserID: 3, Status: StatusDraft}, nil
}

func (*problemAuthoringTxStub) ArchiveProblem(_ context.Context, id int64) (ProblemRecord, error) {
	return ProblemRecord{ID: id, OwnerUserID: 3, Status: StatusArchived}, nil
}

func (*problemAuthoringTxStub) NextProblemStatementVersion(context.Context, int64) (int32, error) {
	return 1, nil
}

func (*problemAuthoringTxStub) ClearCurrentProblemStatement(context.Context, int64) error {
	return nil
}

func (*problemAuthoringTxStub) CreateProblemStatement(_ context.Context, problemID int64, version int32, input CreateStatementInput) (Statement, error) {
	return Statement{ID: 1, ProblemID: problemID, Version: version, Title: input.Title}, nil
}

func (*problemAuthoringTxStub) ReplaceProblemTags(context.Context, int64, []TagInput) ([]Tag, error) {
	return nil, nil
}

func (*problemAuthoringTxStub) NextTestcaseSetVersion(context.Context, int64) (int32, error) {
	return 1, nil
}

func (*problemAuthoringTxStub) ClearCurrentTestcaseSet(context.Context, int64) error {
	return nil
}

func (*problemAuthoringTxStub) CreateTestcaseSet(_ context.Context, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error) {
	return TestcaseSetRecord{ID: 1, ProblemID: problemID, Version: version, StorageKey: storageKey, ChecksumSHA256: checksum, SizeBytes: sizeBytes, CaseCount: caseCount, CreatedBy: createdBy}, nil
}

func (*problemAuthoringTxStub) CreateArtifact(_ context.Context, artifact ArtifactRecord) (ArtifactRecord, error) {
	artifact.ID = 1
	return artifact, nil
}

func (*problemAuthoringTxStub) GetCurrentProblemStatement(context.Context, int64) (Statement, error) {
	return Statement{}, nil
}

func (*problemAuthoringTxStub) GetCurrentReadyTestcaseSet(context.Context, int64) (TestcaseSetRecord, error) {
	return TestcaseSetRecord{}, nil
}

func (*problemAuthoringTxStub) GetLatestCompletedProblemCheckRun(context.Context, int64, int64, int64) (ProblemCheckRunRecord, error) {
	return ProblemCheckRunRecord{}, nil
}

func (*problemAuthoringTxStub) ListProblemCheckFindings(context.Context, int64) ([]ProblemCheckFindingRecord, error) {
	return nil, nil
}

func TestProblemAuthoringUsesOnlyAuthoringTransaction(t *testing.T) {
	store := &problemAuthoringStoreStub{}
	authoring := NewProblemAuthoring(store, testcaseArchiveWriterStub{})

	created, err := authoring.CreateProblem(t.Context(), auth.Actor{UserID: 3, Role: auth.RoleUser}, CreateProblemInput{
		Title:         "Sum",
		Slug:          "sum",
		Difficulty:    DifficultyEasy,
		Visibility:    VisibilityPrivate,
		TimeLimitMS:   1000,
		MemoryLimitKB: 65536,
	})
	if err != nil {
		t.Fatalf("CreateProblem() error = %v", err)
	}
	if created.ID == 0 || created.OwnerUserID != 3 {
		t.Fatalf("CreateProblem() = %+v", created)
	}
}

type problemCheckStoreStub struct{}

func (problemCheckStoreStub) GetProblem(context.Context, int64) (ProblemRecord, error) {
	return ProblemRecord{ID: 7, OwnerUserID: 3, Status: StatusDraft}, nil
}

func (problemCheckStoreStub) GetCurrentProblemStatement(context.Context, int64) (Statement, error) {
	return Statement{}, nil
}

func (problemCheckStoreStub) GetCurrentReadyTestcaseSet(context.Context, int64) (TestcaseSetRecord, error) {
	return TestcaseSetRecord{}, nil
}

func (problemCheckStoreStub) GetProblemCheckRun(_ context.Context, id int64) (ProblemCheckRunRecord, error) {
	return ProblemCheckRunRecord{ID: id, ProblemID: 7, Status: ProblemCheckStatusCompleted}, nil
}

func (problemCheckStoreStub) ListProblemCheckFindings(context.Context, int64) ([]ProblemCheckFindingRecord, error) {
	return nil, nil
}

func (problemCheckStoreStub) WithProblemCheckTx(ctx context.Context, fn func(context.Context, problemCheckTx) error) error {
	return fn(ctx, problemCheckTxStub{})
}

type problemCheckTxStub struct{}

func (problemCheckTxStub) CreateProblemCheckRun(_ context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return ProblemCheckRunRecord{ID: 1, ProblemID: input.ProblemID}, nil
}

func (problemCheckTxStub) CreateProblemCheckFinding(_ context.Context, input CreateProblemCheckFindingInput) (ProblemCheckFindingRecord, error) {
	return ProblemCheckFindingRecord{ID: 1, RunID: input.RunID}, nil
}

func (problemCheckTxStub) CompleteProblemCheckRun(_ context.Context, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	return ProblemCheckRunRecord{ID: input.ID, ProblemID: 7, Status: ProblemCheckStatusCompleted, FinishedAt: input.FinishedAt}, nil
}

func TestProblemCheckServiceUsesOnlyCheckStore(t *testing.T) {
	checks := NewProblemCheckService(problemCheckStoreStub{}, testcaseArchiveReaderStub{}, func() time.Time { return time.Unix(10, 0).UTC() })

	got, err := checks.GetProblemCheck(t.Context(), auth.Actor{UserID: 3, Role: auth.RoleUser}, 7, 9)
	if err != nil {
		t.Fatalf("GetProblemCheck() error = %v", err)
	}
	if got.Run.ID != 9 || got.Run.ProblemID != 7 {
		t.Fatalf("GetProblemCheck() = %+v", got)
	}
}

func TestProblemConstructorsRejectMissingRequiredDependencies(t *testing.T) {
	reader := NewProblemReader(problemReaderStoreStub{}, testcaseArchiveReaderStub{})
	authoring := NewProblemAuthoring(&problemAuthoringStoreStub{}, testcaseArchiveWriterStub{})
	checks := NewProblemCheckService(problemCheckStoreStub{}, testcaseArchiveReaderStub{}, nil)

	tests := []struct {
		name string
		new  func()
	}{
		{name: "reader store", new: func() { NewProblemReader(nil, testcaseArchiveReaderStub{}) }},
		{name: "authoring store", new: func() { NewProblemAuthoring(nil, testcaseArchiveWriterStub{}) }},
		{name: "check store", new: func() { NewProblemCheckService(nil, testcaseArchiveReaderStub{}, nil) }},
		{name: "service reader", new: func() { NewService(nil, authoring, checks) }},
		{name: "service authoring", new: func() { NewService(reader, nil, checks) }},
		{name: "service checks", new: func() { NewService(reader, authoring, nil) }},
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
