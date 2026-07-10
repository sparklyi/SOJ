package problem

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/storage"
)

func TestProblemAuthorizationAllowsOwnerAndAdmin(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := NewService(repo, &fakeStorage{})
	title := "Updated"

	_, err := service.UpdateProblem(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1, UpdateProblemInput{Title: &title})
	assertAppCode(t, err, "problem.forbidden")

	if _, err := service.UpdateProblem(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1, UpdateProblemInput{Title: &title}); err != nil {
		t.Fatalf("owner update failed: %v", err)
	}
	if _, err := service.UpdateProblem(context.Background(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, 1, UpdateProblemInput{Title: &title}); err != nil {
		t.Fatalf("admin update failed: %v", err)
	}
}

func TestCreateStatementSwitchesCurrentVersion(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := NewService(repo, &fakeStorage{})
	actor := auth.Actor{UserID: 10, Role: auth.RoleUser}

	first, err := service.CreateStatement(context.Background(), actor, 1, CreateStatementInput{Title: "A", Description: "desc"})
	if err != nil {
		t.Fatalf("create first statement: %v", err)
	}
	second, err := service.CreateStatement(context.Background(), actor, 1, CreateStatementInput{Title: "B", Description: "desc"})
	if err != nil {
		t.Fatalf("create second statement: %v", err)
	}

	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("unexpected versions: first=%d second=%d", first.Version, second.Version)
	}
	if repo.statements[first.ID].IsCurrent {
		t.Fatalf("first statement should no longer be current")
	}
	if !repo.statements[second.ID].IsCurrent || repo.currentStatement[1] != second.ID {
		t.Fatalf("second statement should be current")
	}
}

func TestUploadTestcaseArchiveValidationFailure(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	store := &fakeStorage{}
	service := NewService(repo, store)

	archive := zipArchive(t, map[string]string{"input1.txt": "1\n", "output1.txt": "1\n"})
	_, err := service.UploadTestcaseArchive(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1, UploadTestcaseInput{
		Content:        archive,
		CaseCount:      2,
		ChecksumSHA256: sha256Hex(archive),
	})
	assertAppCode(t, err, "problem.testcase_not_ready")
	if len(store.puts) != 0 {
		t.Fatalf("invalid archive should not be written to object storage")
	}
}

func TestUploadTestcaseArchiveRejectsIllegalFileName(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := NewService(repo, &fakeStorage{})
	archive := zipArchive(t, map[string]string{
		"input1.txt":  "1\n",
		"output1.txt": "1\n",
		"README.md":   "ignored by old implementation\n",
	})

	_, err := service.UploadTestcaseArchive(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1, UploadTestcaseInput{
		Content:        archive,
		CaseCount:      1,
		ChecksumSHA256: sha256Hex(archive),
	})
	assertAppCode(t, err, "problem.testcase_not_ready")
	assertHTTPStatus(t, err, 422)
}

func TestUploadTestcaseArchiveDeletesObjectWhenTransactionFails(t *testing.T) {
	repo := newFakeRepository()
	repo.failCreateTestcaseSet = true
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	store := &fakeStorage{}
	service := NewService(repo, store)
	archive := zipArchive(t, map[string]string{"input1.txt": "1\n", "output1.txt": "1\n"})

	_, err := service.UploadTestcaseArchive(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1, UploadTestcaseInput{
		Content:        archive,
		CaseCount:      1,
		ChecksumSHA256: sha256Hex(archive),
	})
	if err == nil {
		t.Fatalf("expected transaction failure")
	}
	if len(store.deletes) != 1 {
		t.Fatalf("expected uploaded object to be deleted after transaction failure, got deletes=%v", store.deletes)
	}
}

func TestNormalizeListFilterKeepsOwnerPrivateVisibility(t *testing.T) {
	filter := normalizeListFilter(auth.Actor{UserID: 10, Role: auth.RoleUser}, ListProblemsFilter{Status: StatusDraft, Page: 2, PageSize: 10})

	if filter.Status != StatusDraft {
		t.Fatalf("owner-visible list should preserve requested status, got %q", filter.Status)
	}
	if filter.Visibility != "" {
		t.Fatalf("owner-visible list should not force public visibility, got %q", filter.Visibility)
	}
	if filter.ViewerUserID != 10 || filter.IncludeAll {
		t.Fatalf("unexpected visibility scope: %+v", filter)
	}
	if filter.Limit != 10 || filter.Offset != 10 {
		t.Fatalf("unexpected page normalization: limit=%d offset=%d", filter.Limit, filter.Offset)
	}
}

func TestCurrentReadyTestcaseSetLoadsCasesFromArchive(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusPublished, Visibility: VisibilityPublic, TimeLimitMS: 10000, MemoryLimitKB: 262144}
	archive := zipArchive(t, map[string]string{
		"input2.txt":  "2 3\n",
		"output2.txt": "5\n",
		"input1.txt":  "1 1\n",
		"output1.txt": "2\n",
	})
	store := &fakeStorage{objects: map[string][]byte{"cases.zip": archive}}
	repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 3, StorageKey: "cases.zip", CaseCount: 2, Status: TestcaseStatusReady, IsCurrent: true}
	repo.currentTestcase[1] = 7
	service := NewService(repo, store)

	got, err := service.CurrentReadyTestcaseSet(context.Background(), 1)
	if err != nil {
		t.Fatalf("CurrentReadyTestcaseSet returned error: %v", err)
	}
	if got.ID != 7 || len(got.Cases) != 2 {
		t.Fatalf("set = %+v", got)
	}
	if got.Cases[0].InputKey != "1 1\n" || got.Cases[0].OutputKey != "2\n" {
		t.Fatalf("first case = %+v", got.Cases[0])
	}
	if got.Cases[1].InputKey != "2 3\n" || got.Cases[1].OutputKey != "5\n" {
		t.Fatalf("second case = %+v", got.Cases[1])
	}
	if got.Cases[0].TimeLimit != 10*time.Second || got.Cases[0].MemoryKB != 262144 {
		t.Fatalf("case limits = %s/%d, want 10s/262144", got.Cases[0].TimeLimit, got.Cases[0].MemoryKB)
	}
}

func TestCurrentReadyTestcaseSetRequiresStorage(t *testing.T) {
	repo := newFakeRepository()
	repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 3, StorageKey: "cases.zip", CaseCount: 1, Status: TestcaseStatusReady, IsCurrent: true}
	repo.currentTestcase[1] = 7
	service := NewService(repo, nil)

	_, err := service.CurrentReadyTestcaseSet(context.Background(), 1)
	assertAppCode(t, err, "service_unavailable")
}

func TestConcurrentUploadSerializesVersionAllocation(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	store := &fakeStorage{delay: 20 * time.Millisecond}
	service := NewService(repo, store)
	archive := zipArchive(t, map[string]string{"input1.txt": "1\n", "output1.txt": "1\n"})
	actor := auth.Actor{UserID: 10, Role: auth.RoleUser}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.UploadTestcaseArchive(context.Background(), actor, 1, UploadTestcaseInput{Content: archive, CaseCount: 1, ChecksumSHA256: sha256Hex(archive)})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("upload failed: %v", err)
		}
	}

	versions := make([]int, 0, len(repo.testcaseSets))
	for _, set := range repo.testcaseSets {
		versions = append(versions, int(set.Version))
	}
	sort.Ints(versions)
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("expected serialized versions [1 2], got %v", versions)
	}
	if repo.currentTestcase[1] == 0 || !repo.testcaseSets[repo.currentTestcase[1]].IsCurrent {
		t.Fatalf("latest testcase set should be current")
	}
	if len(store.puts) != 2 {
		t.Fatalf("expected two object writes, got %d", len(store.puts))
	}
}

func TestRunProblemCheckRequiresOwnerOrAdmin(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[]`, zipArchive(t, map[string]string{
		"input1.txt":  "1\n",
		"output1.txt": "1\n",
	}), 1)
	service := NewService(repo, store)

	_, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1)
	assertAppCode(t, err, "problem.forbidden")

	if _, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1); err != nil {
		t.Fatalf("owner check failed: %v", err)
	}
	if _, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 99, Role: auth.RoleAdmin}, 1); err != nil {
		t.Fatalf("admin check failed: %v", err)
	}
}

func TestRunProblemCheckPersistsCompletedRunAndSummary(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[{"input":"1 1\n","output":"2\n"}]`, zipArchive(t, map[string]string{
		"input1.txt":  "1 1\n",
		"output1.txt": "2\n",
		"input2.txt":  "2 3\n",
		"output2.txt": "5\n",
	}), 2)
	service := NewService(repo, store)
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }

	result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1)
	if err != nil {
		t.Fatalf("RunProblemCheck returned error: %v", err)
	}

	if result.Run.ID == 0 || result.Run.Status != ProblemCheckStatusCompleted {
		t.Fatalf("run = %+v", result.Run)
	}
	if result.Run.ProblemID != 1 || result.Run.TestcaseSetID != 7 || result.Run.RequestedBy != 10 {
		t.Fatalf("run linkage = %+v", result.Run)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %+v", result.Findings)
	}
	if result.Run.Summary.CaseCount != 2 || result.Run.Summary.ExpectedCaseCount != 2 {
		t.Fatalf("summary case counts = %+v", result.Run.Summary)
	}
	if !result.Run.Summary.StorageReadable || !result.Run.Summary.ZipReadable {
		t.Fatalf("summary readability = %+v", result.Run.Summary)
	}
	if len(repo.checkRuns) != 1 || repo.checkRuns[result.Run.ID].Status != ProblemCheckStatusCompleted {
		t.Fatalf("persisted runs = %+v", repo.checkRuns)
	}
}

func TestRunProblemCheckReportsArchiveFindings(t *testing.T) {
	tests := []struct {
		name      string
		archive   []byte
		caseCount int32
		wantCodes []string
	}{
		{
			name:      "unreadable storage object",
			archive:   nil,
			caseCount: 1,
			wantCodes: []string{"testcase.storage_unreadable"},
		},
		{
			name:      "invalid zip",
			archive:   []byte("not a zip"),
			caseCount: 1,
			wantCodes: []string{"testcase.zip_invalid"},
		},
		{
			name:      "empty archive",
			archive:   zipArchive(t, nil),
			caseCount: 1,
			wantCodes: []string{"testcase.archive_empty", "testcase.case_count_mismatch"},
		},
		{
			name: "missing pairs and case count mismatch",
			archive: zipArchive(t, map[string]string{
				"input1.txt":  "1\n",
				"output2.txt": "2\n",
			}),
			caseCount: 2,
			wantCodes: []string{"testcase.output_missing", "testcase.input_missing", "testcase.case_count_mismatch"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			store := &fakeStorage{}
			seedProblemCheckData(t, repo, store, `[]`, tt.archive, tt.caseCount)
			service := NewService(repo, store)

			result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1)
			if err != nil {
				t.Fatalf("RunProblemCheck returned error: %v", err)
			}

			if result.Run.Status != ProblemCheckStatusCompleted {
				t.Fatalf("run status = %s, want completed", result.Run.Status)
			}
			assertFindingCodes(t, result.Findings, tt.wantCodes)
			if result.Run.Summary.ErrorCount != len(tt.wantCodes) {
				t.Fatalf("summary = %+v, want %d errors", result.Run.Summary, len(tt.wantCodes))
			}
			if len(repo.checkFindings[result.Run.ID]) != len(tt.wantCodes) {
				t.Fatalf("persisted findings = %+v", repo.checkFindings)
			}
		})
	}
}

func TestRunProblemCheckReportsStatementSampleJSONFindings(t *testing.T) {
	tests := []struct {
		name    string
		samples string
		code    string
	}{
		{name: "invalid json", samples: `{`, code: "statement.samples_invalid"},
		{name: "null", samples: `null`, code: "statement.samples_invalid"},
		{name: "not an array", samples: `{"input":"1\n","output":"1\n"}`, code: "statement.samples_invalid"},
		{name: "missing output", samples: `[{"input":"1\n"}]`, code: "statement.samples_invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			store := &fakeStorage{}
			seedProblemCheckData(t, repo, store, tt.samples, zipArchive(t, map[string]string{
				"input1.txt":  "1\n",
				"output1.txt": "1\n",
			}), 1)
			service := NewService(repo, store)

			result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1)
			if err != nil {
				t.Fatalf("RunProblemCheck returned error: %v", err)
			}

			assertFindingCodes(t, result.Findings, []string{tt.code})
		})
	}
}

func TestGetProblemCheckReturnsCheckNotFoundForMissingRun(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[]`, zipArchive(t, map[string]string{
		"input1.txt":  "1\n",
		"output1.txt": "1\n",
	}), 1)
	service := NewService(repo, store)

	_, err := service.GetProblemCheck(context.Background(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1, 99)

	assertAppCode(t, err, "problem_check.not_found")
}

func seedProblemCheckData(t *testing.T, repo *fakeRepository, store *fakeStorage, samples string, archive []byte, caseCount int32) {
	t.Helper()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate, CurrentStatementID: 3, CurrentTestcaseSetID: 7, CurrentTestcaseStatus: TestcaseStatusReady}
	repo.statements[3] = Statement{ID: 3, ProblemID: 1, Version: 1, Title: "A", Description: "desc", Samples: json.RawMessage(samples), IsCurrent: true}
	repo.currentStatement[1] = 3
	repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 1, StorageKey: "cases.zip", CaseCount: caseCount, Status: TestcaseStatusReady, IsCurrent: true}
	repo.currentTestcase[1] = 7
	if archive != nil {
		store.objects = map[string][]byte{"cases.zip": archive}
	}
}

func assertFindingCodes(t *testing.T, findings []ProblemCheckFinding, want []string) {
	t.Helper()
	got := make([]string, 0, len(findings))
	for _, finding := range findings {
		got = append(got, finding.Code)
		if finding.Severity != ProblemCheckSeverityError {
			t.Fatalf("finding %s severity = %s, want error", finding.Code, finding.Severity)
		}
	}
	sort.Strings(got)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("finding codes = %v, want %v; findings=%+v", got, want, findings)
	}
}

type fakeRepository struct {
	mu                    sync.Mutex
	problems              map[int64]ProblemRecord
	statements            map[int64]Statement
	currentStatement      map[int64]int64
	testcaseSets          map[int64]TestcaseSetRecord
	currentTestcase       map[int64]int64
	checkRuns             map[int64]ProblemCheckRunRecord
	checkFindings         map[int64][]ProblemCheckFindingRecord
	tags                  map[int64][]Tag
	nextProblemID         int64
	nextTagID             int64
	nextStatementID       int64
	nextTestcaseID        int64
	nextCheckRunID        int64
	nextCheckFindingID    int64
	nextArtifactID        int64
	failCreateTestcaseSet bool
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		problems:         map[int64]ProblemRecord{},
		statements:       map[int64]Statement{},
		currentStatement: map[int64]int64{},
		testcaseSets:     map[int64]TestcaseSetRecord{},
		currentTestcase:  map[int64]int64{},
		checkRuns:        map[int64]ProblemCheckRunRecord{},
		checkFindings:    map[int64][]ProblemCheckFindingRecord{},
		tags:             map[int64][]Tag{},
	}
}

func (r *fakeRepository) WithTx(ctx context.Context, fn func(context.Context, Repository) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(ctx, r)
}

func (r *fakeRepository) CreateProblem(ctx context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error) {
	r.nextProblemID++
	p := ProblemRecord{
		ID:            r.nextProblemID,
		OwnerUserID:   ownerUserID,
		Title:         input.Title,
		Slug:          input.Slug,
		Difficulty:    input.Difficulty,
		Visibility:    input.Visibility,
		Status:        StatusDraft,
		TimeLimitMS:   input.TimeLimitMS,
		MemoryLimitKB: input.MemoryLimitKB,
	}
	r.problems[p.ID] = p
	return p, nil
}

func (r *fakeRepository) GetProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, ok := r.problems[id]
	if !ok {
		return ProblemRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return p, nil
}

func (r *fakeRepository) ListProblems(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error) {
	return nil, errors.New("not implemented")
}

func (r *fakeRepository) CountProblems(ctx context.Context, filter ListProblemsFilter) (int64, error) {
	return 0, errors.New("not implemented")
}

func (r *fakeRepository) UpdateProblem(ctx context.Context, id int64, input UpdateProblemInput) (ProblemRecord, error) {
	p, err := r.GetProblem(ctx, id)
	if err != nil {
		return ProblemRecord{}, err
	}
	if input.Title != nil {
		p.Title = *input.Title
	}
	if input.Status != nil {
		p.Status = *input.Status
	}
	r.problems[id] = p
	return p, nil
}

func (r *fakeRepository) ArchiveProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.GetProblem(ctx, id)
	if err != nil {
		return ProblemRecord{}, err
	}
	p.Status = StatusArchived
	r.problems[id] = p
	return p, nil
}

func (r *fakeRepository) LockProblemForUpdate(ctx context.Context, id int64) (ProblemRecord, error) {
	return r.GetProblem(ctx, id)
}

func (r *fakeRepository) NextProblemStatementVersion(ctx context.Context, problemID int64) (int32, error) {
	var max int32
	for _, statement := range r.statements {
		if statement.ProblemID == problemID && statement.Version > max {
			max = statement.Version
		}
	}
	return max + 1, nil
}

func (r *fakeRepository) ClearCurrentProblemStatement(ctx context.Context, problemID int64) error {
	for id, statement := range r.statements {
		if statement.ProblemID == problemID {
			statement.IsCurrent = false
			r.statements[id] = statement
		}
	}
	delete(r.currentStatement, problemID)
	return nil
}

func (r *fakeRepository) CreateProblemStatement(ctx context.Context, problemID int64, version int32, input CreateStatementInput) (Statement, error) {
	r.nextStatementID++
	statement := Statement{ID: r.nextStatementID, ProblemID: problemID, Version: version, Title: input.Title, Description: input.Description, IsCurrent: input.MakeCurrent}
	r.statements[statement.ID] = statement
	if statement.IsCurrent {
		r.currentStatement[problemID] = statement.ID
		p := r.problems[problemID]
		p.CurrentStatementID = statement.ID
		r.problems[problemID] = p
	}
	return statement, nil
}

func (r *fakeRepository) GetCurrentProblemStatement(ctx context.Context, problemID int64) (Statement, error) {
	id := r.currentStatement[problemID]
	if id == 0 {
		return Statement{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return r.statements[id], nil
}

func (r *fakeRepository) ReplaceProblemTags(ctx context.Context, problemID int64, tags []TagInput) ([]Tag, error) {
	replaced := make([]Tag, 0, len(tags))
	for _, input := range tags {
		r.nextTagID++
		replaced = append(replaced, Tag{ID: r.nextTagID, Name: input.Name, Slug: input.Slug})
	}
	r.tags[problemID] = replaced
	return replaced, nil
}

func (r *fakeRepository) ListProblemTags(ctx context.Context, problemID int64) ([]Tag, error) {
	return append([]Tag(nil), r.tags[problemID]...), nil
}

func (r *fakeRepository) NextTestcaseSetVersion(ctx context.Context, problemID int64) (int32, error) {
	var max int32
	for _, set := range r.testcaseSets {
		if set.ProblemID == problemID && set.Version > max {
			max = set.Version
		}
	}
	return max + 1, nil
}

func (r *fakeRepository) ClearCurrentTestcaseSet(ctx context.Context, problemID int64) error {
	for id, set := range r.testcaseSets {
		if set.ProblemID == problemID {
			set.IsCurrent = false
			r.testcaseSets[id] = set
		}
	}
	delete(r.currentTestcase, problemID)
	return nil
}

func (r *fakeRepository) CreateTestcaseSet(ctx context.Context, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error) {
	if r.failCreateTestcaseSet {
		return TestcaseSetRecord{}, errors.New("create testcase set failed")
	}
	r.nextTestcaseID++
	set := TestcaseSetRecord{ID: r.nextTestcaseID, ProblemID: problemID, Version: version, StorageKey: storageKey, ChecksumSHA256: checksum, SizeBytes: sizeBytes, CaseCount: caseCount, Status: TestcaseStatusReady, IsCurrent: true, CreatedBy: createdBy}
	r.testcaseSets[set.ID] = set
	r.currentTestcase[problemID] = set.ID
	p := r.problems[problemID]
	p.CurrentTestcaseSetID = set.ID
	p.CurrentTestcaseStatus = set.Status
	r.problems[problemID] = p
	return set, nil
}

func (r *fakeRepository) GetCurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error) {
	id := r.currentTestcase[problemID]
	if id == 0 {
		return TestcaseSetRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	set := r.testcaseSets[id]
	if set.Status != TestcaseStatusReady {
		return TestcaseSetRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return set, nil
}

func (r *fakeRepository) CreateProblemCheckRun(ctx context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	r.nextCheckRunID++
	now := time.Now()
	status := input.Status
	if status == "" {
		status = ProblemCheckStatusQueued
	}
	run := ProblemCheckRunRecord{
		ID:            r.nextCheckRunID,
		ProblemID:     input.ProblemID,
		TestcaseSetID: input.TestcaseSetID,
		RequestedBy:   input.RequestedBy,
		Status:        status,
		Summary:       jsonRawFromDB(input.Summary),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	r.checkRuns[run.ID] = run
	return run, nil
}

func (r *fakeRepository) GetProblemCheckRun(ctx context.Context, id int64) (ProblemCheckRunRecord, error) {
	run, ok := r.checkRuns[id]
	if !ok {
		return ProblemCheckRunRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return run, nil
}

func (r *fakeRepository) ListProblemCheckRuns(ctx context.Context, filter ListProblemCheckRunsFilter) ([]ProblemCheckRunRecord, error) {
	runs := make([]ProblemCheckRunRecord, 0)
	for _, run := range r.checkRuns {
		if run.ProblemID == filter.ProblemID {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].ID > runs[j].ID
		}
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
	if filter.Offset >= int32(len(runs)) {
		return nil, nil
	}
	start := int(filter.Offset)
	end := start + int(filter.Limit)
	if filter.Limit <= 0 {
		end = start
	}
	if end > len(runs) {
		end = len(runs)
	}
	return append([]ProblemCheckRunRecord(nil), runs[start:end]...), nil
}

func (r *fakeRepository) CompleteProblemCheckRun(ctx context.Context, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	run, ok := r.checkRuns[input.ID]
	if !ok || (run.Status != ProblemCheckStatusQueued && run.Status != ProblemCheckStatusRunning) {
		return ProblemCheckRunRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	now := time.Now()
	finishedAt := input.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = now
	}
	run.Status = ProblemCheckStatusCompleted
	run.Summary = jsonRawFromDB(input.Summary)
	run.ErrorMessage = ""
	run.FinishedAt = finishedAt
	run.UpdatedAt = now
	r.checkRuns[run.ID] = run
	return run, nil
}

func (r *fakeRepository) FailProblemCheckRun(ctx context.Context, input FailProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	run, ok := r.checkRuns[input.ID]
	if !ok || (run.Status != ProblemCheckStatusQueued && run.Status != ProblemCheckStatusRunning) {
		return ProblemCheckRunRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	now := time.Now()
	finishedAt := input.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = now
	}
	run.Status = ProblemCheckStatusFailed
	run.Summary = jsonRawFromDB(input.Summary)
	run.ErrorMessage = input.ErrorMessage
	run.FinishedAt = finishedAt
	run.UpdatedAt = now
	r.checkRuns[run.ID] = run
	return run, nil
}

func (r *fakeRepository) CreateProblemCheckFinding(ctx context.Context, input CreateProblemCheckFindingInput) (ProblemCheckFindingRecord, error) {
	r.nextCheckFindingID++
	finding := ProblemCheckFindingRecord{
		ID:          r.nextCheckFindingID,
		RunID:       input.RunID,
		Severity:    input.Severity,
		Code:        input.Code,
		Message:     input.Message,
		CaseIndex:   input.CaseIndex,
		TestcaseKey: input.TestcaseKey,
		Details:     jsonRawFromDB(input.Details),
		CreatedAt:   time.Now(),
	}
	r.checkFindings[finding.RunID] = append(r.checkFindings[finding.RunID], finding)
	return finding, nil
}

func (r *fakeRepository) GetProblemCheckFinding(ctx context.Context, id int64) (ProblemCheckFindingRecord, error) {
	for _, findings := range r.checkFindings {
		for _, finding := range findings {
			if finding.ID == id {
				return finding, nil
			}
		}
	}
	return ProblemCheckFindingRecord{}, apperror.NotFound("problem.not_found", "problem not found")
}

func (r *fakeRepository) ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error) {
	findings := append([]ProblemCheckFindingRecord(nil), r.checkFindings[runID]...)
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].ID < findings[j].ID
	})
	return findings, nil
}

func (r *fakeRepository) CreateArtifact(ctx context.Context, artifact ArtifactRecord) (ArtifactRecord, error) {
	r.nextArtifactID++
	artifact.ID = r.nextArtifactID
	return artifact, nil
}

func (r *fakeRepository) GetProblemStats(ctx context.Context, problemID int64) (ProblemStats, error) {
	return ProblemStats{}, errors.New("not implemented")
}

type fakeStorage struct {
	mu      sync.Mutex
	delay   time.Duration
	puts    []string
	deletes []string
	objects map[string][]byte
}

func (s *fakeStorage) Put(ctx context.Context, object storage.Object) (storage.ObjectInfo, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	var data []byte
	if object.Body != nil {
		data, _ = io.ReadAll(object.Body)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts = append(s.puts, object.Key)
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[object.Key] = append([]byte(nil), data...)
	return storage.ObjectInfo{Key: object.Key, Size: object.Size, ContentType: object.ContentType}, nil
}

func (s *fakeStorage) Get(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, storage.ObjectInfo{}, apperror.NotFound("testcase.archive_not_found", "testcase archive not found")
	}
	return io.NopCloser(bytes.NewReader(data)), storage.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}

func (s *fakeStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, key)
	delete(s.objects, key)
	return nil
}

func (s *fakeStorage) Stat(ctx context.Context, key string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, errors.New("not implemented")
}

func zipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func assertAppCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", code)
	}
	appErr, ok := apperror.From(err)
	if !ok {
		t.Fatalf("expected app error %s, got %T %v", code, err, err)
	}
	if !strings.EqualFold(appErr.Code, code) {
		t.Fatalf("expected error code %s, got %s", code, appErr.Code)
	}
}

func assertHTTPStatus(t *testing.T, err error, status int) {
	t.Helper()
	appErr, ok := apperror.From(err)
	if !ok {
		t.Fatalf("expected app error, got %T %v", err, err)
	}
	if appErr.HTTPStatus != status {
		t.Fatalf("expected HTTP status %d, got %d", status, appErr.HTTPStatus)
	}
}
