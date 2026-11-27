package problem

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/audit"
	"SOJ/internal/auth"
	"SOJ/internal/storage"
)

func TestProblemAuthorizationAllowsOwnerAndAdmin(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	store := &fakeStorage{}
	service := newProblemService(repo, store)
	title := "Updated"

	_, err := service.UpdateProblem(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1, UpdateProblemInput{Title: &title})
	assertAppCode(t, err, "problem.forbidden")

	if _, err := service.UpdateProblem(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1, UpdateProblemInput{Title: &title}); err != nil {
		t.Fatalf("owner update failed: %v", err)
	}
	if _, err := service.UpdateProblem(context.Background(), auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}, 1, UpdateProblemInput{Title: &title}); err != nil {
		t.Fatalf("admin update failed: %v", err)
	}
}

func newProblemService(repo *fakeRepository, objectStore *fakeStorage) *Service {
	var readerArchives testcaseArchiveReader
	var authoringArchives testcaseArchiveWriter
	if objectStore != nil {
		readerArchives = objectStore
		authoringArchives = objectStore
	}
	return NewService(
		NewProblemReader(repo, readerArchives),
		NewProblemAuthoring(repo, authoringArchives),
		NewProblemCheckService(repo, readerArchives, time.Now),
	)
}

func TestAuthorizeProblemRejudgeUsesOperatorPermission(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusPublished, Visibility: VisibilityPublic}
	service := newProblemService(repo, &fakeStorage{})

	if err := service.AuthorizeProblemRejudge(t.Context(), auth.Actor{UserID: 10, Role: auth.RoleUser}, 1); err == nil {
		t.Fatal("ordinary user should not rejudge a problem")
	}
	if err := service.AuthorizeProblemRejudge(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleOperator}, Permissions: seededPermissions(auth.RoleOperator)}, 1); err != nil {
		t.Fatalf("operator authorization returned error: %v", err)
	}
	if err := service.AuthorizeProblemRejudge(t.Context(), auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}, 1); err != nil {
		t.Fatalf("admin authorization returned error: %v", err)
	}
	err := service.AuthorizeProblemRejudge(t.Context(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1)
	assertAppCode(t, err, "problem.forbidden")
}

func TestUpdateProblemRejectsStatusMutation(t *testing.T) {
	repo := newFakeRepository()
	seedPublishableProblem(repo)
	service := newProblemService(repo, &fakeStorage{})
	status := StatusPublished

	_, err := service.UpdateProblem(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1, UpdateProblemInput{Status: &status})
	assertAppCode(t, err, "problem.status_managed_by_review")
}

func TestUpdateProblemTitle(t *testing.T) {
	author := auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}
	for _, blank := range []string{"", "   "} {
		repo := newFakeRepository()
		repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Title: "Original", Status: StatusDraft, Visibility: VisibilityPrivate}
		service := newProblemService(repo, &fakeStorage{})
		title := blank

		_, err := service.UpdateProblem(t.Context(), author, 1, UpdateProblemInput{Title: &title})
		assertAppCode(t, err, "problem.title_required")
		if got := repo.problems[1].Title; got != "Original" {
			t.Fatalf("title after rejected blank update = %q, want %q", got, "Original")
		}
	}

	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Title: "Original", Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})
	if _, err := service.UpdateProblem(t.Context(), author, 1, UpdateProblemInput{}); err != nil {
		t.Fatalf("omitted title returned error: %v", err)
	}
	if got := repo.problems[1].Title; got != "Original" {
		t.Fatalf("title after omitted update = %q, want %q", got, "Original")
	}
	renamed := "Renamed"
	if _, err := service.UpdateProblem(t.Context(), author, 1, UpdateProblemInput{Title: &renamed}); err != nil {
		t.Fatalf("valid rename returned error: %v", err)
	}
	if got := repo.problems[1].Title; got != "Renamed" {
		t.Fatalf("title after rename = %q, want %q", got, "Renamed")
	}
}

func TestRestoreProblemRequiresManageAll(t *testing.T) {
	repo := newFakeRepository()
	seedPublishableProblem(repo)
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusArchived, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})

	_, err := service.RestoreProblem(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
	assertAppCode(t, err, "problem.forbidden")
}

func TestRestoreProblemRejectsProblemThatIsNotArchived(t *testing.T) {
	repo := newFakeRepository()
	seedPublishableProblem(repo)
	service := newProblemService(repo, &fakeStorage{})

	_, err := service.RestoreProblem(t.Context(), auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}, 1)
	assertAppCode(t, err, "problem.not_archived")
}

func TestRestoreProblemReListsArchivedProblem(t *testing.T) {
	repo := newFakeRepository()
	seedPublishableProblem(repo)
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusArchived, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})

	restored, err := service.RestoreProblem(t.Context(), auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}, 1)
	if err != nil {
		t.Fatalf("RestoreProblem() error = %v", err)
	}
	if restored.Status != StatusPublished {
		t.Fatalf("RestoreProblem() status = %q, want %q", restored.Status, StatusPublished)
	}
}

func TestArchiveRestoreRefreshesSiteStats(t *testing.T) {
	actor := auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}
	commitErr := errors.New("commit failed")
	operations := []struct {
		name    string
		initial string
		final   string
		change  func(*ProblemAuthoring, context.Context, auth.Actor, int64) (ProblemRecord, error)
	}{
		{"archive", StatusPublished, StatusArchived, (*ProblemAuthoring).ArchiveProblem},
		{"restore", StatusArchived, StatusPublished, (*ProblemAuthoring).RestoreProblem},
	}
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			for _, tt := range []struct {
				name          string
				visibility    string
				commitErr     error
				wantRefreshes int
			}{
				{name: "public", visibility: VisibilityPublic, wantRefreshes: 1},
				{name: "private", visibility: VisibilityPrivate},
				{name: "failed commit", visibility: VisibilityPublic, commitErr: commitErr},
			} {
				t.Run(tt.name, func(t *testing.T) {
					store := &revisionTestStore{fakeRepository: newFakeRepository(), commitErr: tt.commitErr}
					store.problems[1] = ProblemRecord{ID: 1, OwnerUserID: actor.UserID, Status: op.initial, Visibility: tt.visibility}
					stats := &revisionStatsRecorder{store: store}
					authoring := NewProblemAuthoring(store, nil, WithAuthoringStatsRefresher(stats))

					problem, err := op.change(authoring, t.Context(), actor, 1)
					if !errors.Is(err, tt.commitErr) {
						t.Fatalf("change error = %v, want %v", err, tt.commitErr)
					}
					if err == nil && problem.Status != op.final {
						t.Errorf("problem status = %q, want %q", problem.Status, op.final)
					}
					if stats.beforeCommit || stats.calls != tt.wantRefreshes {
						t.Errorf("refresh calls = %d, before commit = %t, want %d after commit", stats.calls, stats.beforeCommit, tt.wantRefreshes)
					}
				})
			}
		})
	}
}

func TestSavingNewStatementInvalidatesPreviousValidCheck(t *testing.T) {
	repo := newFakeRepository()
	seedPublishableProblem(repo)
	repo.nextStatementID = 3
	repo.checkRuns[1] = ProblemCheckRunRecord{
		ID: 1, ProblemID: 1, StatementID: 3, TestcaseSetID: 7, Status: ProblemCheckStatusCompleted,
		Summary: json.RawMessage(`{"valid":true,"error_count":0}`),
	}
	store := &fakeStorage{}
	service := newProblemService(repo, store)
	actor := auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}

	statement, err := service.CreateStatement(t.Context(), actor, 1, CreateStatementInput{
		Title:       "Updated statement",
		Description: "updated description",
		MakeCurrent: true,
	})
	if err != nil {
		t.Fatalf("CreateStatement returned error: %v", err)
	}
	if statement.ID == 3 {
		t.Fatalf("statement ID = %d, want a new version", statement.ID)
	}

	state, err := service.GetProblemAuthoringState(t.Context(), actor, 1)
	if err != nil {
		t.Fatalf("GetProblemAuthoringState returned error: %v", err)
	}
	if state.Publishable || state.LatestCheck != nil {
		t.Fatalf("new statement reused previous check: %+v", state)
	}
	if got := blockerCodes(state.Blockers); strings.Join(got, ",") != "problem.check_required" {
		t.Fatalf("blockers = %v, want problem.check_required", got)
	}

	status := StatusPublished
	_, err = service.UpdateProblem(t.Context(), actor, 1, UpdateProblemInput{Status: &status})
	assertAppCode(t, err, "problem.status_managed_by_review")

	store.objects = map[string][]byte{"cases.zip": zipArchive(t, map[string]string{
		"1.in":  "1\n",
		"1.ans": "1\n",
	})}
	check, err := service.RunProblemCheck(t.Context(), actor, 1)
	if err != nil {
		t.Fatalf("RunProblemCheck returned error: %v", err)
	}
	if check.Run.StatementID != statement.ID || !check.Run.Summary.Valid {
		t.Fatalf("new check = %+v, want valid check for statement %d", check.Run, statement.ID)
	}

	_, err = service.UpdateProblem(t.Context(), actor, 1, UpdateProblemInput{Status: &status})
	assertAppCode(t, err, "problem.status_managed_by_review")
}

func TestProblemAuthoringStateReportsPublishBlockers(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})

	state, err := service.GetProblemAuthoringState(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)

	if err != nil {
		t.Fatalf("GetProblemAuthoringState returned error: %v", err)
	}
	if state.Publishable || state.Statement != nil || state.TestcaseSet != nil || state.LatestCheck != nil {
		t.Fatalf("unexpected state: %+v", state)
	}
	if got := blockerCodes(state.Blockers); strings.Join(got, ",") != "problem.statement_required,problem.testcase_required" {
		t.Fatalf("blockers = %v", got)
	}
}

func TestProblemAuthoringStateReturnsCurrentValidCheck(t *testing.T) {
	repo := newFakeRepository()
	seedPublishableProblem(repo)
	repo.checkRuns[1] = ProblemCheckRunRecord{
		ID: 1, ProblemID: 1, StatementID: 3, TestcaseSetID: 7, Status: ProblemCheckStatusCompleted,
		Summary: json.RawMessage(`{"valid":true}`),
	}
	service := newProblemService(repo, &fakeStorage{})

	state, err := service.GetProblemAuthoringState(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)

	if err != nil {
		t.Fatalf("GetProblemAuthoringState returned error: %v", err)
	}
	if !state.Publishable || state.Statement == nil || state.TestcaseSet == nil || state.LatestCheck == nil {
		t.Fatalf("unexpected state: %+v", state)
	}
	if len(state.Blockers) != 0 || state.LatestCheck.TestcaseSetID != 7 {
		t.Fatalf("unexpected blockers/check: %+v", state)
	}
}

func TestAnonymousProblemReadsFollowPublicVisibility(t *testing.T) {
	// 站点策略：题目是公共资产。`canReadProblem` 放行 published+public，
	// 匿名访客因此只读得到公开题；列表/翻页不再有登录墙，可见性由存储层过滤。
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, Title: "Public", Slug: "public", Status: StatusPublished, Visibility: VisibilityPublic, CurrentStatementID: 3, CurrentTestcaseSetID: 7}
	repo.statements[3] = Statement{ID: 3, ProblemID: 1, IsCurrent: true}
	repo.currentStatement[1] = 3
	repo.problems[2] = ProblemRecord{ID: 2, Title: "Draft", Slug: "draft", Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})
	anonymous := auth.Anonymous("req")

	if _, err := service.ListProblems(t.Context(), anonymous, ListProblemsFilter{}); err != nil {
		t.Fatalf("anonymous ListProblems returned error: %v", err)
	}
	if _, err := service.ListProblemsByCursor(t.Context(), anonymous, ListProblemsFilter{}); err != nil {
		t.Fatalf("anonymous ListProblemsByCursor returned error: %v", err)
	}
	if _, err := service.GetProblem(t.Context(), anonymous, 1); err != nil {
		t.Fatalf("anonymous GetProblem(public) returned error: %v", err)
	}
	if _, err := service.CurrentStatement(t.Context(), anonymous, 1); err != nil {
		t.Fatalf("anonymous CurrentStatement(public) returned error: %v", err)
	}

	_, err := service.GetProblem(t.Context(), anonymous, 2)
	assertAppCode(t, err, "problem.not_found")
}

func TestNormalizeListFilterScopesMineToActor(t *testing.T) {
	filter := normalizeListFilter(auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}, ListProblemsFilter{Mine: true})

	if filter.OwnerUserID != 10 || filter.IncludeAll {
		t.Fatalf("mine filter = %+v", filter)
	}
}

func TestListProblemsByCursorReturnsNextCursor(t *testing.T) {
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	repo := newFakeRepository()
	repo.problems[2] = ProblemRecord{ID: 2, Title: "Second", Slug: "second", Status: StatusPublished, Visibility: VisibilityPublic, CreatedAt: now.Add(-time.Minute)}
	repo.problems[1] = ProblemRecord{ID: 1, Title: "First", Slug: "first", Status: StatusPublished, Visibility: VisibilityPublic, CreatedAt: now.Add(-2 * time.Minute)}
	service := newProblemService(repo, &fakeStorage{})

	viewer := auth.Actor{UserID: 30, Roles: []auth.Role{auth.RoleUser}, Permissions: seededPermissions(auth.RoleUser)}
	page, err := service.ListProblemsByCursor(context.Background(), viewer, ListProblemsFilter{PageSize: 1})
	if err != nil {
		t.Fatalf("ListProblemsByCursor returned error: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 2 {
		t.Fatalf("items = %+v, want problem 2", page.Items)
	}
	if page.NextCursor == nil || page.NextCursor.ID != 2 || !page.NextCursor.CreatedAt.Equal(repo.problems[2].CreatedAt) {
		t.Fatalf("next cursor = %+v, want problem 2 cursor", page.NextCursor)
	}

	second, err := service.ListProblemsByCursor(context.Background(), viewer, ListProblemsFilter{PageSize: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("second cursor page: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != 1 || second.NextCursor != nil {
		t.Fatalf("second cursor page = %+v, want only problem 1 without next cursor", second)
	}
}

func TestCreateStatementSwitchesCurrentVersion(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})
	actor := auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}

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

func uploadArchiveInput(archive []byte) UploadTestcaseInput {
	return UploadTestcaseInput{Source: bytes.NewReader(archive), Size: int64(len(archive))}
}

func TestUploadTestcaseArchiveValidationFailure(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	store := &fakeStorage{}
	service := newProblemService(repo, store)

	// A lone input can never form a case, so the upload must fail before any
	// object is written.
	archive := zipArchive(t, map[string]string{"1.in": "1\n"})
	_, findings, err := service.UploadTestcaseArchive(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1, uploadArchiveInput(archive))
	assertAppCode(t, err, "testcase.archive_invalid")
	assertHTTPStatus(t, err, 422)
	if len(findings) == 0 || !hasFindingCode(findings, codeOutputMissing) {
		t.Fatalf("findings = %+v, want output_missing", findings)
	}
	if len(store.puts) != 0 {
		t.Fatalf("invalid archive should not be written to object storage")
	}

	appErr, ok := apperror.From(err)
	if !ok {
		t.Fatalf("expected app error, got %T", err)
	}
	details, ok := appErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("error details = %#v, want findings map", appErr.Details)
	}
	if _, ok := details["findings"].([]Finding); !ok {
		t.Fatalf("error details findings = %#v", details["findings"])
	}
}

func TestUploadTestcaseArchiveRejectsIllegalFileName(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})
	archive := zipArchive(t, map[string]string{
		"1.in":      "1\n",
		"1.ans":     "1\n",
		"README.md": "documentation\n",
	})

	_, findings, err := service.UploadTestcaseArchive(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1, uploadArchiveInput(archive))
	assertAppCode(t, err, "testcase.archive_invalid")
	assertHTTPStatus(t, err, 422)
	if !hasFindingCode(findings, codeFileNameInvalid) {
		t.Fatalf("findings = %+v, want file_name_invalid", findings)
	}
}

func TestUploadTestcaseArchiveReturnsWarnings(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})
	archive := zipArchive(t, map[string]string{
		"1.in":      "1\n",
		"1.ans":     "1\n",
		".DS_Store": "metadata",
	})

	set, findings, err := service.UploadTestcaseArchive(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1, uploadArchiveInput(archive))
	if err != nil {
		t.Fatalf("UploadTestcaseArchive returned error: %v", err)
	}
	if set.CaseCount != 1 {
		t.Fatalf("case count = %d, want 1", set.CaseCount)
	}
	if len(findings) != 1 || findings[0].Code != codeFileIgnored || findings[0].Severity != ProblemCheckSeverityWarning {
		t.Fatalf("warnings = %+v, want one file_ignored warning", findings)
	}
}

func TestUploadTestcaseArchiveDeletesObjectWhenTransactionFails(t *testing.T) {
	repo := newFakeRepository()
	repo.failCreateTestcaseSet = true
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	store := &fakeStorage{}
	service := newProblemService(repo, store)
	archive := zipArchive(t, map[string]string{"1.in": "1\n", "1.ans": "1\n"})

	_, _, err := service.UploadTestcaseArchive(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1, uploadArchiveInput(archive))
	if err == nil {
		t.Fatalf("expected transaction failure")
	}
	if len(store.deletes) != 1 {
		t.Fatalf("expected uploaded object to be deleted after transaction failure, got deletes=%v", store.deletes)
	}
}

func TestProblemRevisionRefreshesSiteStats(t *testing.T) {
	actor := auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}
	archive := zipArchive(t, map[string]string{"1.in": "1\n", "1.ans": "1\n"})
	revisions := []struct {
		name string
		save func(context.Context, *ProblemAuthoring) error
	}{
		{"statement", func(ctx context.Context, authoring *ProblemAuthoring) error {
			_, err := authoring.CreateStatement(ctx, actor, 1, CreateStatementInput{Description: "Revised statement"})
			return err
		}},
		{"testcases", func(ctx context.Context, authoring *ProblemAuthoring) error {
			_, _, err := authoring.UploadTestcaseArchive(ctx, actor, 1, uploadArchiveInput(archive))
			return err
		}},
	}
	commitErr := errors.New("revision commit failed")
	tests := []struct {
		name          string
		status        string
		visibility    string
		commitErr     error
		wantRefreshes int
	}{
		{name: "public published problem", status: StatusPublished, visibility: VisibilityPublic, wantRefreshes: 1},
		{name: "public draft", status: StatusDraft, visibility: VisibilityPublic},
		{name: "private published problem", status: StatusPublished, visibility: VisibilityPrivate},
		{name: "failed commit", status: StatusPublished, visibility: VisibilityPublic, commitErr: commitErr},
	}
	for _, revision := range revisions {
		t.Run(revision.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					store := &revisionTestStore{fakeRepository: newFakeRepository(), commitErr: tt.commitErr}
					store.problems[1] = ProblemRecord{ID: 1, OwnerUserID: actor.UserID, Status: tt.status, Visibility: tt.visibility}
					stats := &revisionStatsRecorder{store: store}
					authoring := NewProblemAuthoring(store, &fakeStorage{}, WithAuthoringStatsRefresher(stats))

					if err := revision.save(t.Context(), authoring); !errors.Is(err, tt.commitErr) {
						t.Fatalf("revision error = %v, want %v", err, tt.commitErr)
					}
					if tt.commitErr == nil && store.problems[1].Status != StatusDraft {
						t.Errorf("problem status = %q, want draft", store.problems[1].Status)
					}
					if stats.beforeCommit || stats.calls != tt.wantRefreshes {
						t.Errorf("refresh calls = %d, before commit = %t, want %d after commit", stats.calls, stats.beforeCommit, tt.wantRefreshes)
					}
				})
			}
		})
	}
}

type revisionTestStore struct {
	*fakeRepository
	commitErr error
	committed bool
}

func (s *revisionTestStore) WithProblemAuthoringTx(ctx context.Context, fn func(context.Context, problemAuthoringTx) error) error {
	if err := s.fakeRepository.WithProblemAuthoringTx(ctx, fn); err != nil {
		return err
	}
	s.committed = s.commitErr == nil
	return s.commitErr
}

type revisionStatsRecorder struct {
	store        *revisionTestStore
	calls        int
	beforeCommit bool
}

func (r *revisionStatsRecorder) Refresh(context.Context) {
	r.calls++
	r.beforeCommit = r.beforeCommit || !r.store.committed
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

func TestGetCurrentTestcaseSetLoadsCasesFromArchive(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusPublished, Visibility: VisibilityPublic, TimeLimitMS: 10000, MemoryLimitKB: 262144}
	archive := zipArchive(t, map[string]string{
		"2.in":  "2 3\n",
		"2.ans": "5\n",
		"1.in":  "1 1\n",
		"1.ans": "2\n",
	})
	store := &fakeStorage{objects: map[string][]byte{"cases.zip": archive}}
	repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 3, StorageKey: "cases.zip", CaseCount: 2, ChecksumSHA256: sha256Hex(archive), IsCurrent: true}
	repo.currentTestcase[1] = 7
	service := newProblemService(repo, store)

	got, err := service.GetCurrentTestcaseSet(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetCurrentTestcaseSet returned error: %v", err)
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

func TestGetCurrentTestcaseSetRequiresStorage(t *testing.T) {
	repo := newFakeRepository()
	repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 3, StorageKey: "cases.zip", CaseCount: 1, IsCurrent: true}
	repo.currentTestcase[1] = 7
	service := newProblemService(repo, nil)

	_, err := service.GetCurrentTestcaseSet(context.Background(), 1)
	assertAppCode(t, err, "service_unavailable")
}

func TestConcurrentUploadSerializesVersionAllocation(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	store := &fakeStorage{delay: 20 * time.Millisecond}
	service := newProblemService(repo, store)
	archive := zipArchive(t, map[string]string{"1.in": "1\n", "1.ans": "1\n"})
	actor := auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := service.UploadTestcaseArchive(context.Background(), actor, 1, uploadArchiveInput(archive))
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
		"1.in":  "1\n",
		"1.ans": "1\n",
	}), 1)
	service := newProblemService(repo, store)

	_, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 20, Role: auth.RoleUser}, 1)
	assertAppCode(t, err, "problem.forbidden")

	if _, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1); err != nil {
		t.Fatalf("owner check failed: %v", err)
	}
	if _, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}, 1); err != nil {
		t.Fatalf("admin check failed: %v", err)
	}
}

func TestRunProblemCheckPersistsCompletedRunAndSummary(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[{"input":"1 1\n","output":"2\n"}]`, zipArchive(t, map[string]string{
		"1.in":  "1 1\n",
		"1.ans": "2\n",
		"2.in":  "2 3\n",
		"2.ans": "5\n",
	}), 2)
	service := newProblemService(repo, store)
	service.checks.now = func() time.Time { return time.Unix(100, 0).UTC() }

	result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
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
	if result.Run.Summary.CaseCount != 2 {
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
				"1.in":  "1\n",
				"2.ans": "2\n",
			}),
			caseCount: 2,
			wantCodes: []string{"testcase.output_missing", "testcase.input_missing", "testcase.archive_empty", "testcase.case_count_mismatch"},
		},
		{
			name: "high compression ratio",
			archive: zipArchive(t, map[string]string{
				"1.in":  strings.Repeat("a", 1<<20),
				"1.ans": "1\n",
			}),
			caseCount: 1,
			wantCodes: []string{"testcase.compression_ratio_exceeded"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			store := &fakeStorage{}
			seedProblemCheckData(t, repo, store, `[]`, tt.archive, tt.caseCount)
			service := newProblemService(repo, store)

			result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
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

func TestRunProblemCheckReportsBatchFindings(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	// Two missing outputs plus a case-count mismatch produce several findings
	// that must be persisted in one batch with sequential ids.
	seedProblemCheckData(t, repo, store, `[]`, zipArchive(t, map[string]string{
		"1.in":  "1\n",
		"2.in":  "2\n",
		"3.in":  "3\n",
		"3.ans": "3\n",
	}), 3)
	service := newProblemService(repo, store)

	result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
	if err != nil {
		t.Fatalf("RunProblemCheck returned error: %v", err)
	}
	assertFindingCodes(t, result.Findings, []string{"testcase.output_missing", "testcase.output_missing", "testcase.case_count_mismatch"})
	for index, finding := range result.Findings {
		if finding.ID != int64(index+1) {
			t.Fatalf("persisted finding ids = %+v, want sequential from 1", result.Findings)
		}
	}
}

func TestGetProblemCheckReturnsCheckNotFoundForMissingRun(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[]`, zipArchive(t, map[string]string{
		"1.in":  "1\n",
		"1.ans": "1\n",
	}), 1)
	service := newProblemService(repo, store)

	_, err := service.GetProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1, 99)

	assertAppCode(t, err, "problem_check.not_found")
}

func seedProblemCheckData(t *testing.T, repo *fakeRepository, store *fakeStorage, samples string, archive []byte, caseCount int32) {
	t.Helper()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate, CurrentStatementID: 3, CurrentTestcaseSetID: 7}
	repo.statements[3] = Statement{ID: 3, ProblemID: 1, Version: 1, Title: "A", Description: "desc", Samples: json.RawMessage(samples), IsCurrent: true}
	repo.currentStatement[1] = 3
	repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 1, StorageKey: "cases.zip", CaseCount: caseCount, IsCurrent: true}
	repo.currentTestcase[1] = 7
	if archive != nil {
		checksum := sha256Hex(archive)
		repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 1, StorageKey: "cases.zip", CaseCount: caseCount, ChecksumSHA256: checksum, IsCurrent: true}
		store.objects = map[string][]byte{"cases.zip": archive}
	}
}

func seedPublishableProblem(repo *fakeRepository) {
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate, CurrentStatementID: 3, CurrentTestcaseSetID: 7}
	repo.statements[3] = Statement{ID: 3, ProblemID: 1, Version: 1, Title: "A", Description: "desc", Samples: json.RawMessage(`[]`), IsCurrent: true}
	repo.currentStatement[1] = 3
	repo.testcaseSets[7] = TestcaseSetRecord{ID: 7, ProblemID: 1, Version: 1, StorageKey: "cases.zip", CaseCount: 1, IsCurrent: true}
	repo.currentTestcase[1] = 7
}

func blockerCodes(blockers []ProblemAuthoringBlocker) []string {
	codes := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		codes = append(codes, blocker.Code)
	}
	sort.Strings(codes)
	return codes
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

func TestProblemAuthoringFlow(t *testing.T) {
	tests := []struct {
		name         string
		input        authoringFlowInput
		wantStep     string
		wantRemain   int
		wantStatuses map[string]string
	}{
		{
			name:       "fresh problem starts at statement",
			input:      authoringFlowInput{},
			wantStep:   AuthoringStepStatement,
			wantRemain: 4,
		},
		{
			name:       "statement done next is testcase",
			input:      authoringFlowInput{StatementID: 1},
			wantStep:   AuthoringStepTestcase,
			wantRemain: 3,
		},
		{
			name: "matching valid check advances to review",
			input: authoringFlowInput{
				StatementID: 1, TestcaseSetID: 2, HasCheck: true,
				CheckStatementID: 1, CheckTestcaseSetID: 2, CheckValid: true,
			},
			wantStep:   AuthoringStepReview,
			wantRemain: 1,
		},
		{
			name: "stale check does not count as done",
			input: authoringFlowInput{
				StatementID: 5, TestcaseSetID: 2, HasCheck: true,
				CheckStatementID: 1, CheckTestcaseSetID: 2, CheckValid: true,
			},
			wantStep:   AuthoringStepCheck,
			wantRemain: 2,
		},
		{
			name: "in review is fully done",
			input: authoringFlowInput{
				ProblemStatus: StatusInReview, StatementID: 1, TestcaseSetID: 2,
				HasCheck: true, CheckStatementID: 1, CheckTestcaseSetID: 2, CheckValid: true,
			},
			wantStep:   "",
			wantRemain: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := buildProblemAuthoringFlow(tt.input)
			if flow.CurrentStep != tt.wantStep || flow.Remaining != tt.wantRemain {
				t.Fatalf("flow = %+v, want step %q remaining %d", flow, tt.wantStep, tt.wantRemain)
			}
			if len(flow.Steps) != 5 {
				t.Fatalf("steps = %+v, want 5", flow.Steps)
			}
		})
	}
}

func TestProblemAuthoringStateExposesFlowAndBlockerSteps(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})

	state, err := service.GetProblemAuthoringState(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
	if err != nil {
		t.Fatalf("GetProblemAuthoringState returned error: %v", err)
	}
	if state.Flow.CurrentStep != AuthoringStepStatement || state.Flow.Remaining != 4 {
		t.Fatalf("flow = %+v", state.Flow)
	}
	wantSteps := map[string]string{
		AuthoringStepCreate:    AuthoringStepStatusDone,
		AuthoringStepStatement: AuthoringStepStatusTodo,
		AuthoringStepTestcase:  AuthoringStepStatusTodo,
		AuthoringStepCheck:     AuthoringStepStatusTodo,
		AuthoringStepReview:    AuthoringStepStatusTodo,
	}
	for _, step := range state.Flow.Steps {
		if wantSteps[step.Key] != step.Status {
			t.Fatalf("step %s = %s, want %s", step.Key, step.Status, wantSteps[step.Key])
		}
	}
	steps := map[string]string{}
	for _, blocker := range state.Blockers {
		steps[blocker.Code] = blocker.Step
	}
	if steps["problem.statement_required"] != AuthoringStepStatement || steps["problem.testcase_required"] != AuthoringStepTestcase {
		t.Fatalf("blocker steps = %+v", steps)
	}
}

func TestListProblemsBatchesTagLookup(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, Title: "First", Status: StatusPublished, Visibility: VisibilityPublic}
	repo.problems[2] = ProblemRecord{ID: 2, Title: "Second", Status: StatusPublished, Visibility: VisibilityPublic}
	repo.tags[1] = []Tag{{ID: 1, Name: "array", Slug: "array"}}
	repo.tags[2] = []Tag{{ID: 2, Name: "dp", Slug: "dp"}}
	service := newProblemService(repo, &fakeStorage{})

	list, err := service.ListProblems(t.Context(), auth.Actor{Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}, ListProblemsFilter{})
	if err != nil {
		t.Fatalf("ListProblems returned error: %v", err)
	}
	if repo.tagBatchCalls != 1 {
		t.Fatalf("tag batch calls = %d, want 1", repo.tagBatchCalls)
	}
	byID := map[int64][]string{}
	for _, item := range list.Items {
		byID[item.ID] = item.Tags
	}
	if len(byID[1]) != 1 || byID[1][0] != "array" || len(byID[2]) != 1 || byID[2][0] != "dp" {
		t.Fatalf("tags = %+v", byID)
	}
}

func TestCreateStatementValidatesSamplesAndMirrorsTitle(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Title: "Two Sum", Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})
	actor := auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}

	_, err := service.CreateStatement(t.Context(), actor, 1, CreateStatementInput{
		Description: "desc",
		Samples:     []SampleInput{{Input: "", Output: "1"}},
	})
	assertAppCode(t, err, "statement.sample_invalid")

	_, err = service.CreateStatement(t.Context(), actor, 1, CreateStatementInput{
		Description: "desc",
		Samples:     []SampleInput{{Input: strings.Repeat("a", maxSampleFieldBytes+1), Output: "1"}},
	})
	assertAppCode(t, err, "statement.sample_too_large")

	created, err := service.CreateStatement(t.Context(), actor, 1, CreateStatementInput{
		Description: "desc",
		Samples:     []SampleInput{{Input: "1 1\n", Output: "2\n", Explanation: "sum"}},
	})
	if err != nil {
		t.Fatalf("CreateStatement returned error: %v", err)
	}
	if created.Title != "Two Sum" {
		t.Fatalf("statement title = %q, want problem title", created.Title)
	}
}

func TestCreateStatementSampleFieldByteLimits(t *testing.T) {
	repo := newFakeRepository()
	repo.problems[1] = ProblemRecord{ID: 1, OwnerUserID: 10, Title: "Two Sum", Status: StatusDraft, Visibility: VisibilityPrivate}
	service := newProblemService(repo, &fakeStorage{})
	actor := auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}

	exact := strings.Repeat("a", maxSampleFieldBytes)
	created, err := service.CreateStatement(t.Context(), actor, 1, CreateStatementInput{
		Description: "desc",
		Samples:     []SampleInput{{Input: exact, Output: "1", Explanation: exact}},
	})
	if err != nil {
		t.Fatalf("CreateStatement at exact byte limit returned error: %v", err)
	}
	if created.Title != "Two Sum" {
		t.Fatalf("statement title = %q, want problem title after exact-limit accept", created.Title)
	}

	// "你" is 3 UTF-8 bytes, so this explanation is one byte over the 64 KiB limit.
	over := strings.Repeat("a", maxSampleFieldBytes-2) + "你"
	if len(over) != maxSampleFieldBytes+1 {
		t.Fatalf("precondition: oversized explanation is %d bytes, want %d", len(over), maxSampleFieldBytes+1)
	}
	_, err = service.CreateStatement(t.Context(), actor, 1, CreateStatementInput{
		Description: "desc",
		Samples:     []SampleInput{{Input: "1", Output: "1", Explanation: over}},
	})
	assertAppCode(t, err, "statement.sample_too_large")
}

// Direct cases for generateProblemSlug: an ASCII title slugifies into the
// prefix, a padded ASCII title is trimmed first, and a title with no ASCII
// slug base (e.g. CJK) falls back to "problem". The suffix is always six
// lowercase hex characters; we deliberately do not assert exact random output
// or uniqueness across draws.
// slugifyTag is the ASCII-only base generateProblemSlug builds on: it
// lowercases, trims, keeps [a-z0-9], collapses other runs to single dashes
// and drops leading/trailing dashes. Non-ASCII input contributes nothing.
func TestSlugifyTag(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain ascii", input: "Two Sum", want: "two-sum"},
		{name: "padded", input: "  Two Sum  ", want: "two-sum"},
		{name: "uppercase lowercased", input: "ABC123", want: "abc123"},
		{name: "underscore becomes dash", input: "two_sum", want: "two-sum"},
		{name: "punctuation collapses", input: "Hello,  World!", want: "hello-world"},
		{name: "leading and trailing punctuation dropped", input: "--Two Sum--", want: "two-sum"},
		{name: "cjk only yields empty", input: "两数之和", want: ""},
		{name: "emoji only yields empty", input: "🎉🎊", want: ""},
		{name: "mixed keeps ascii runs", input: "3Sum 两数之和 v2", want: "3sum-v2"},
		{name: "tabs and newlines are separators", input: "\tTwo\nSum\n", want: "two-sum"},
		{name: "digits kept", input: "123", want: "123"},
		{name: "blank", input: "   ", want: ""},
		{name: "single letter", input: "A", want: "a"},
		{name: "double hyphens collapse", input: "two--sum", want: "two-sum"},
		{name: "cyrillic contributes nothing", input: "Привет", want: ""},
		{name: "arabic contributes nothing", input: "مرحبا", want: ""},
		{name: "plus signs become one dash", input: "C++", want: "c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := slugifyTag(tc.input); got != tc.want {
				t.Fatalf("slugifyTag(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestGenerateProblemSlug(t *testing.T) {
	suffixRE := regexp.MustCompile(`^[0-9a-f]{6}$`)
	cases := []struct {
		name       string
		title      string
		wantPrefix string
	}{
		{name: "ascii title slugifies", title: "Two Sum", wantPrefix: "two-sum-"},
		{name: "padded ascii title is trimmed", title: " Two Sum ", wantPrefix: "two-sum-"},
		{name: "punctuation collapses to dashes", title: "Hello, World!", wantPrefix: "hello-world-"},
		{name: "non-ascii title falls back to problem", title: "两数之和", wantPrefix: "problem-"},
		{name: "mixed ascii and non-ascii keeps the ascii base", title: "3Sum 两数之和", wantPrefix: "3sum-"},
		{name: "no alnum at all falls back", title: "!!!", wantPrefix: "problem-"},
		{name: "blank title falls back", title: "   ", wantPrefix: "problem-"},
		{name: "padded non-ascii still falls back", title: "  两数之和  ", wantPrefix: "problem-"},
		{name: "cyrillic falls back", title: "Привет", wantPrefix: "problem-"},
		{name: "arabic falls back", title: "مرحبا", wantPrefix: "problem-"},
		{name: "single letter", title: "A", wantPrefix: "a-"},
		{name: "digits only", title: "42", wantPrefix: "42-"},
		{name: "hyphenated ascii", title: "two--sum", wantPrefix: "two-sum-"},
		{name: "plus signs collapse", title: "C++", wantPrefix: "c-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slug, err := generateProblemSlug(tc.title)
			if err != nil {
				t.Fatalf("generateProblemSlug(%q) returned error: %v", tc.title, err)
			}
			if !validSlug(slug) {
				t.Fatalf("generateProblemSlug(%q) = %q, not a valid slug", tc.title, slug)
			}
			if !strings.HasPrefix(slug, tc.wantPrefix) {
				t.Fatalf("generateProblemSlug(%q) = %q, want prefix %q", tc.title, slug, tc.wantPrefix)
			}
			suffix := strings.TrimPrefix(slug, tc.wantPrefix)
			if !suffixRE.MatchString(suffix) {
				t.Fatalf("generateProblemSlug(%q) suffix = %q, want six lowercase hex chars", tc.title, suffix)
			}
		})
	}
}

func TestCreateProblemRetriesSlugConflict(t *testing.T) {
	repo := newFakeRepository()
	repo.slugConflicts = 2
	service := newProblemService(repo, &fakeStorage{})

	created, err := service.CreateProblem(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, CreateProblemInput{Title: "Two Sum"})
	if err != nil {
		t.Fatalf("CreateProblem returned error: %v", err)
	}
	if repo.slugConflicts != 0 || len(repo.problems) != 1 {
		t.Fatalf("expected a successful retry, conflicts=%d problems=%d", repo.slugConflicts, len(repo.problems))
	}
	if !validSlug(created.Slug) || !strings.HasPrefix(created.Slug, "two-sum-") {
		t.Fatalf("generated slug = %q, want slugified title plus suffix", created.Slug)
	}
}

// The conflict-retry path must also work for a title with no ASCII slug
// base: retries keep the "problem-" fallback prefix and still end in a valid
// slug once a free one is drawn.
func TestCreateProblemRetriesSlugConflictWithNonAsciiTitle(t *testing.T) {
	repo := newFakeRepository()
	repo.slugConflicts = 2
	service := newProblemService(repo, &fakeStorage{})

	created, err := service.CreateProblem(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, CreateProblemInput{Title: "两数之和"})
	if err != nil {
		t.Fatalf("CreateProblem returned error: %v", err)
	}
	if repo.slugConflicts != 0 || len(repo.problems) != 1 {
		t.Fatalf("expected a successful retry, conflicts=%d problems=%d", repo.slugConflicts, len(repo.problems))
	}
	if !validSlug(created.Slug) || !strings.HasPrefix(created.Slug, "problem-") {
		t.Fatalf("generated slug = %q, want problem- fallback prefix plus suffix", created.Slug)
	}
}

func TestCreateProblemGivesUpAfterSlugConflicts(t *testing.T) {
	repo := newFakeRepository()
	repo.slugConflicts = problemSlugAttempts + 1
	service := newProblemService(repo, &fakeStorage{})

	_, err := service.CreateProblem(t.Context(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, CreateProblemInput{Title: "Two Sum"})
	assertAppCode(t, err, "problem.slug_conflict")
	if len(repo.problems) != 0 {
		t.Fatalf("no problem should be persisted, got %+v", repo.problems)
	}
}

type fakeRepository struct {
	txMu                  sync.Mutex
	mu                    sync.RWMutex
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
	failCreateTestcaseSet bool
	slugConflicts         int
	lastListFilter        ListProblemsFilter
	tagBatchCalls         int
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

func (r *fakeRepository) WithProblemAuthoringTx(ctx context.Context, fn func(context.Context, problemAuthoringTx) error) error {
	r.txMu.Lock()
	defer r.txMu.Unlock()
	return fn(ctx, r)
}

func (r *fakeRepository) WithProblemCheckTx(ctx context.Context, fn func(context.Context, problemCheckTx) error) error {
	r.txMu.Lock()
	defer r.txMu.Unlock()
	return fn(ctx, r)
}

func (r *fakeRepository) CreateProblem(ctx context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.slugConflicts > 0 {
		r.slugConflicts--
		return ProblemRecord{}, apperror.Conflict("problem.slug_conflict", "problem slug already exists")
	}

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
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.problems[id]
	if !ok {
		return ProblemRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return p, nil
}

func (r *fakeRepository) ListProblems(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastListFilter = filter
	items := make([]ProblemRecord, 0)
	for _, problem := range r.problems {
		if filter.OwnerUserID > 0 && problem.OwnerUserID != filter.OwnerUserID {
			continue
		}
		items = append(items, problem)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func (r *fakeRepository) ListProblemsByCursor(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error) {
	return r.listProblemsByCursor(ctx, filter)
}

func (r *fakeRepository) listProblemsByCursor(ctx context.Context, filter ListProblemsFilter) ([]ProblemRecord, error) {
	items, err := r.ListProblems(ctx, filter)
	if err != nil {
		return nil, err
	}
	cursor := filter.Cursor
	if cursor == nil {
		cursor = &ProblemCursor{CreatedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC), ID: 1<<63 - 1}
	}
	items = items[:0]
	for _, problem := range r.problems {
		if filter.OwnerUserID > 0 && problem.OwnerUserID != filter.OwnerUserID {
			continue
		}
		if problem.CreatedAt.After(cursor.CreatedAt) || (problem.CreatedAt.Equal(cursor.CreatedAt) && problem.ID >= cursor.ID) {
			continue
		}
		items = append(items, problem)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if filter.Limit > 0 && len(items) > int(filter.Limit) {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (r *fakeRepository) CountProblems(ctx context.Context, filter ListProblemsFilter) (int64, error) {
	items, err := r.ListProblems(ctx, filter)
	return int64(len(items)), err
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems[id] = p
	return p, nil
}

func (r *fakeRepository) SetProblemStatus(ctx context.Context, id int64, status string) (ProblemRecord, error) {
	p, err := r.GetProblem(ctx, id)
	if err != nil {
		return ProblemRecord{}, err
	}
	p.Status = status
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems[id] = p
	return p, nil
}

func (r *fakeRepository) ArchiveProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.GetProblem(ctx, id)
	if err != nil {
		return ProblemRecord{}, err
	}
	p.Status = StatusArchived
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems[id] = p
	return p, nil
}

func (r *fakeRepository) RestoreProblem(ctx context.Context, id int64) (ProblemRecord, error) {
	p, err := r.GetProblem(ctx, id)
	if err != nil {
		return ProblemRecord{}, err
	}
	p.Status = StatusPublished
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems[id] = p
	return p, nil
}

func (r *fakeRepository) RecordAudit(context.Context, audit.Event) error {
	return nil
}

func (r *fakeRepository) LockProblemForUpdate(ctx context.Context, id int64) (ProblemRecord, error) {
	return r.GetProblem(ctx, id)
}

func (r *fakeRepository) NextProblemStatementVersion(ctx context.Context, problemID int64) (int32, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var max int32
	for _, statement := range r.statements {
		if statement.ProblemID == problemID && statement.Version > max {
			max = statement.Version
		}
	}
	return max + 1, nil
}

func (r *fakeRepository) ClearCurrentProblemStatement(ctx context.Context, problemID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

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
	r.mu.Lock()
	defer r.mu.Unlock()

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
	r.mu.RLock()
	defer r.mu.RUnlock()

	id := r.currentStatement[problemID]
	if id == 0 {
		return Statement{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return r.statements[id], nil
}

func (r *fakeRepository) ReplaceProblemTags(ctx context.Context, problemID int64, tags []TagInput) ([]Tag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	replaced := make([]Tag, 0, len(tags))
	for _, input := range tags {
		r.nextTagID++
		replaced = append(replaced, Tag{ID: r.nextTagID, Name: input.Name, Slug: input.Slug})
	}
	r.tags[problemID] = replaced
	return replaced, nil
}

func (r *fakeRepository) ListProblemTags(ctx context.Context, problemID int64) ([]Tag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return append([]Tag(nil), r.tags[problemID]...), nil
}

func (r *fakeRepository) ListProblemTagsByProblemIDs(ctx context.Context, problemIDs []int64) (map[int64][]Tag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tagBatchCalls++
	grouped := make(map[int64][]Tag, len(problemIDs))
	for _, problemID := range problemIDs {
		grouped[problemID] = append([]Tag(nil), r.tags[problemID]...)
	}
	return grouped, nil
}

func (r *fakeRepository) NextTestcaseSetVersion(ctx context.Context, problemID int64) (int32, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var max int32
	for _, set := range r.testcaseSets {
		if set.ProblemID == problemID && set.Version > max {
			max = set.Version
		}
	}
	return max + 1, nil
}

func (r *fakeRepository) ClearCurrentTestcaseSet(ctx context.Context, problemID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

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
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.failCreateTestcaseSet {
		return TestcaseSetRecord{}, errors.New("create testcase set failed")
	}
	r.nextTestcaseID++
	set := TestcaseSetRecord{ID: r.nextTestcaseID, ProblemID: problemID, Version: version, StorageKey: storageKey, ChecksumSHA256: checksum, SizeBytes: sizeBytes, CaseCount: caseCount, IsCurrent: true, CreatedBy: createdBy}
	r.testcaseSets[set.ID] = set
	r.currentTestcase[problemID] = set.ID
	p := r.problems[problemID]
	p.CurrentTestcaseSetID = set.ID
	r.problems[problemID] = p
	return set, nil
}

func (r *fakeRepository) GetCurrentTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id := r.currentTestcase[problemID]
	if id == 0 {
		return TestcaseSetRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return r.testcaseSets[id], nil
}

func (r *fakeRepository) CreateProblemCheckRun(ctx context.Context, input CreateProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextCheckRunID++
	now := time.Now()
	status := input.Status
	if status == "" {
		status = ProblemCheckStatusQueued
	}
	run := ProblemCheckRunRecord{
		ID:            r.nextCheckRunID,
		ProblemID:     input.ProblemID,
		StatementID:   input.StatementID,
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
	r.mu.RLock()
	defer r.mu.RUnlock()

	run, ok := r.checkRuns[id]
	if !ok {
		return ProblemCheckRunRecord{}, apperror.NotFound("problem.not_found", "problem not found")
	}
	return run, nil
}

func (r *fakeRepository) GetLatestCompletedProblemCheckRun(ctx context.Context, problemID, statementID, testcaseSetID int64) (ProblemCheckRunRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var latest ProblemCheckRunRecord
	for _, run := range r.checkRuns {
		if run.ProblemID != problemID || run.StatementID != statementID || run.TestcaseSetID != testcaseSetID || run.Status != ProblemCheckStatusCompleted {
			continue
		}
		if latest.ID == 0 || run.FinishedAt.After(latest.FinishedAt) || (run.FinishedAt.Equal(latest.FinishedAt) && run.ID > latest.ID) {
			latest = run
		}
	}
	if latest.ID == 0 {
		return ProblemCheckRunRecord{}, apperror.NotFound("problem_check.not_found", "problem check not found")
	}
	return latest, nil
}

func (r *fakeRepository) CompleteProblemCheckRun(ctx context.Context, input CompleteProblemCheckRunInput) (ProblemCheckRunRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

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

func (r *fakeRepository) CreateProblemCheckFindings(ctx context.Context, inputs []CreateProblemCheckFindingInput) ([]ProblemCheckFindingRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	records := make([]ProblemCheckFindingRecord, 0, len(inputs))
	for _, input := range inputs {
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
		records = append(records, finding)
	}
	return records, nil
}

func (r *fakeRepository) ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	findings := append([]ProblemCheckFindingRecord(nil), r.checkFindings[runID]...)
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].ID < findings[j].ID
	})
	return findings, nil
}

func (r *fakeRepository) GetProblemStats(ctx context.Context, problemID int64) (ProblemStats, error) {
	return ProblemStats{}, errors.New("not implemented")
}

func (r *fakeRepository) ListProblemSubmissionCounts(ctx context.Context, problemIDs []int64) (map[int64]ProblemSubmissionCounts, error) {
	return map[int64]ProblemSubmissionCounts{}, nil
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

type failingBodyReader struct{}

func (failingBodyReader) Read([]byte) (int, error) {
	return 0, errors.New("disk I/O error")
}

type applicationErrorBodyReader struct{}

func (applicationErrorBodyReader) Read([]byte) (int, error) {
	return 0, apperror.New("storage.read_failed", "disk I/O error", http.StatusInternalServerError)
}

type failingArchiveReader struct {
	body io.ReadCloser
}

func (r failingArchiveReader) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return r.body, storage.ObjectInfo{}, nil
}

func TestRunProblemCheckReportsFailedArchiveReadAsUnreadable(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[]`, zipArchive(t, map[string]string{
		"1.in":  "1 1\n",
		"1.ans": "2\n",
	}), 1)
	service := newProblemService(repo, store)
	service.checks.archives = failingArchiveReader{body: io.NopCloser(failingBodyReader{})}

	result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
	if err != nil {
		t.Fatalf("RunProblemCheck returned error: %v", err)
	}

	assertFindingCodes(t, result.Findings, []string{"testcase.storage_unreadable"})
	summary := result.Run.Summary
	if summary.StorageReadable || summary.ZipReadable || summary.Valid {
		t.Fatalf("summary = %+v, want storage_readable false, zip_readable false, valid false", summary)
	}
}

func TestRunProblemCheckDoesNotTreatOtherAppErrorsAsReadableStorage(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[]`, zipArchive(t, map[string]string{
		"1.in":  "1 1\n",
		"1.ans": "2\n",
	}), 1)
	service := newProblemService(repo, store)
	service.checks.archives = failingArchiveReader{body: io.NopCloser(applicationErrorBodyReader{})}

	result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
	if err != nil {
		t.Fatalf("RunProblemCheck returned error: %v", err)
	}
	assertFindingCodes(t, result.Findings, []string{"storage.read_failed"})
	if summary := result.Run.Summary; summary.StorageReadable || summary.ZipReadable || summary.Valid {
		t.Fatalf("summary = %+v, want storage_readable false, zip_readable false, valid false", summary)
	}
}

type countingZeroReader struct {
	remaining int64
	read      int64
}

func (r *countingZeroReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	clear(p[:n])
	r.remaining -= int64(n)
	r.read += int64(n)
	return n, nil
}

type streamingArchiveReader struct {
	body io.ReadCloser
}

func (r streamingArchiveReader) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return r.body, storage.ObjectInfo{}, nil
}

func TestRunProblemCheckReportsOversizedArchiveAsReadable(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[]`, nil, 1)
	body := &countingZeroReader{remaining: MaxTestcaseArchiveBytes + 1}
	service := newProblemService(repo, store)
	service.checks.archives = streamingArchiveReader{body: io.NopCloser(body)}

	result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
	if err != nil {
		t.Fatalf("RunProblemCheck returned error: %v", err)
	}
	assertFindingCodes(t, result.Findings, []string{codeArchiveTooLarge})
	if summary := result.Run.Summary; !summary.StorageReadable || summary.ZipReadable || summary.Valid {
		t.Fatalf("summary = %+v, want storage_readable true, zip_readable false, valid false", summary)
	}
	if body.read != MaxTestcaseArchiveBytes+1 {
		t.Fatalf("read %d bytes, want limit plus one byte (%d)", body.read, MaxTestcaseArchiveBytes+1)
	}
}

func TestRunProblemCheckReportsInvalidZipAsReadableStorage(t *testing.T) {
	repo := newFakeRepository()
	store := &fakeStorage{}
	seedProblemCheckData(t, repo, store, `[]`, []byte("not a zip"), 1)
	service := newProblemService(repo, store)

	result, err := service.RunProblemCheck(context.Background(), auth.Actor{UserID: 10, Roles: []auth.Role{auth.RoleAuthor}, Permissions: seededPermissions(auth.RoleAuthor)}, 1)
	if err != nil {
		t.Fatalf("RunProblemCheck returned error: %v", err)
	}
	assertFindingCodes(t, result.Findings, []string{codeZipInvalid})
	if summary := result.Run.Summary; !summary.StorageReadable || summary.ZipReadable || summary.Valid {
		t.Fatalf("summary = %+v, want storage_readable true, zip_readable false, valid false", summary)
	}
}
