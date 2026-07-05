package problem

import (
	"archive/zip"
	"bytes"
	"context"
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

type fakeRepository struct {
	mu                    sync.Mutex
	problems              map[int64]ProblemRecord
	statements            map[int64]Statement
	currentStatement      map[int64]int64
	testcaseSets          map[int64]TestcaseSetRecord
	currentTestcase       map[int64]int64
	tags                  map[int64][]Tag
	nextProblemID         int64
	nextTagID             int64
	nextStatementID       int64
	nextTestcaseID        int64
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
