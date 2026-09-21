package contest

import (
	"context"
	"sort"
	"testing"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/submission"
)

type contestRoleMemoryStore struct {
	roles map[[2]int64][]auth.Role
}

func (s *contestRoleMemoryStore) ListContestIDs(_ context.Context, userID int64) ([]int64, error) {
	ids := make([]int64, 0)
	for key := range s.roles {
		if key[1] == userID {
			ids = append(ids, key[0])
		}
	}
	return ids, nil
}

func (s *contestRoleMemoryStore) ListContestRoles(_ context.Context, contestID, userID int64) ([]auth.Role, error) {
	roles := append([]auth.Role(nil), s.roles[[2]int64{contestID, userID}]...)
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles, nil
}

func (s *contestRoleMemoryStore) ListContestRoleAssignments(_ context.Context, contestID int64) ([]ContestRoleAssignment, error) {
	assignments := make([]ContestRoleAssignment, 0)
	for key, roles := range s.roles {
		if key[0] != contestID {
			continue
		}
		sorted := append([]auth.Role(nil), roles...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		for _, role := range sorted {
			assignments = append(assignments, ContestRoleAssignment{
				ID:        1,
				ContestID: contestID,
				UserID:    key[1],
				Role:      role,
			})
		}
	}
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].UserID != assignments[j].UserID {
			return assignments[i].UserID < assignments[j].UserID
		}
		return assignments[i].Role < assignments[j].Role
	})
	return assignments, nil
}

func (s *contestRoleMemoryStore) GrantContestRole(context.Context, int64, int64, auth.Role, int64, string) (ContestRoleAssignment, error) {
	return ContestRoleAssignment{}, nil
}

func (s *contestRoleMemoryStore) RevokeContestRole(context.Context, int64, int64, auth.Role, int64, string) error {
	return nil
}

type privateContestReaderStore struct{}

func (privateContestReaderStore) GetContest(context.Context, int64) (ContestRecord, error) {
	return ContestRecord{ID: 9, OwnerUserID: 1, Visibility: VisibilityPrivate}, nil
}

func (privateContestReaderStore) ListContests(context.Context, ListContestFilter) ([]ContestRecord, int64, error) {
	return nil, 0, nil
}

func (privateContestReaderStore) ListContestsByCursor(context.Context, ListContestFilter) ([]ContestRecord, error) {
	return nil, nil
}

func (privateContestReaderStore) ListContestProblems(context.Context, int64) ([]ContestProblem, error) {
	return nil, nil
}

func (privateContestReaderStore) GetRegistration(context.Context, int64, int64) (ContestRegistration, error) {
	return ContestRegistration{}, nil
}

func (privateContestReaderStore) ListRegistrations(context.Context, int64) ([]ContestRegistration, error) {
	return nil, nil
}

type recordingContestReaderStore struct {
	contests []ContestRecord
	filter   ListContestFilter
}

func (s *recordingContestReaderStore) GetContest(context.Context, int64) (ContestRecord, error) {
	return ContestRecord{}, nil
}

func (s *recordingContestReaderStore) ListContests(_ context.Context, filter ListContestFilter) ([]ContestRecord, int64, error) {
	s.filter = filter
	return append([]ContestRecord(nil), s.contests...), int64(len(s.contests)), nil
}

func (s *recordingContestReaderStore) ListContestsByCursor(context.Context, ListContestFilter) ([]ContestRecord, error) {
	return nil, nil
}

func (s *recordingContestReaderStore) ListContestProblems(context.Context, int64) ([]ContestProblem, error) {
	return nil, nil
}

func (s *recordingContestReaderStore) GetRegistration(context.Context, int64, int64) (ContestRegistration, error) {
	return ContestRegistration{}, nil
}

func (s *recordingContestReaderStore) ListRegistrations(context.Context, int64) ([]ContestRegistration, error) {
	return nil, nil
}

func TestContestRoleMakesOnlyItsContestReadable(t *testing.T) {
	roles := &contestRoleMemoryStore{roles: map[[2]int64][]auth.Role{
		{9, 7}: {auth.RoleContestStaff},
	}}
	reader := NewContestReader(privateContestReaderStore{}, nil, roles)

	if _, err := reader.GetContest(t.Context(), auth.Actor{UserID: 7}, 9); err != nil {
		t.Fatalf("assigned staff cannot read contest: %v", err)
	}
	if _, err := reader.GetContest(t.Context(), auth.Actor{UserID: 8}, 9); err == nil {
		t.Fatal("unassigned user can read private contest")
	}
}

func TestContestRoleIsIncludedInListVisibilityFilter(t *testing.T) {
	store := &recordingContestReaderStore{contests: []ContestRecord{{ID: 9, Visibility: VisibilityPrivate}}}
	roles := &contestRoleMemoryStore{roles: map[[2]int64][]auth.Role{{9, 7}: {auth.RoleContestStaff}}}
	reader := NewContestReader(store, nil, roles)

	list, err := reader.ListContests(t.Context(), auth.Actor{UserID: 7}, ListContestFilter{PageSize: 20})
	if err != nil {
		t.Fatalf("ListContests returned error: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != 9 {
		t.Fatalf("list items = %+v, want contest 9", list.Items)
	}
	if !containsContestID(store.filter.VisibleToContestIDs, 9) {
		t.Fatalf("visible contest ids = %v, want contest 9", store.filter.VisibleToContestIDs)
	}
}

func TestContestManagerCanWriteWithoutGlobalAdmin(t *testing.T) {
	err := requireContestManager(auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleContestManager}}, ContestRecord{ID: 9, OwnerUserID: 1})
	if err != nil {
		t.Fatalf("contest manager write error = %v", err)
	}
	if err := requireContestManager(auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleContestStaff}}, ContestRecord{ID: 9, OwnerUserID: 1}); err == nil {
		t.Fatal("contest staff unexpectedly received manager permission")
	}
}

func TestContestJudgeCanRejudgeButStaffCannot(t *testing.T) {
	contest := ContestRecord{ID: 9, OwnerUserID: 1}
	if err := requireContestJudge(auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleContestJudge}}, contest); err != nil {
		t.Fatalf("contest judge rejudge error = %v", err)
	}
	if err := requireContestJudge(auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleContestStaff}}, contest); err == nil {
		t.Fatal("contest staff unexpectedly received judge permission")
	}
}

func TestContestStaffCannotSeeFullSubmissionDiagnostics(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC)
	contest := ContestRecord{ID: 9, OwnerUserID: 1, StartAt: now.Add(-3 * time.Hour), FreezeAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)}
	judgedAt := now
	sub := submission.ContestSubmissionVisibility{SubmittedAt: now.Add(-2 * time.Hour), JudgedAt: &judgedAt}

	staff := submissionResultVisibility(contest, auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleContestStaff}}, sub, now)
	if staff.Visibility != "frozen" || staff.ShowAdminDiagnostics {
		t.Fatalf("staff visibility = %+v, want frozen without diagnostics", staff)
	}
	judge := submissionResultVisibility(contest, auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleContestJudge}}, sub, now)
	if judge.Visibility != "visible" || !judge.ShowAdminDiagnostics {
		t.Fatalf("judge visibility = %+v, want visible diagnostics", judge)
	}
}

type publicContestReaderStore struct{ privateContestReaderStore }

func (publicContestReaderStore) GetContest(context.Context, int64) (ContestRecord, error) {
	return ContestRecord{ID: 9, OwnerUserID: 1, Visibility: VisibilityPublic}, nil
}

func TestContestRecordExposesCurrentUserRoles(t *testing.T) {
	roles := &contestRoleMemoryStore{roles: map[[2]int64][]auth.Role{
		{9, 7}: {auth.RoleContestStaff, auth.RoleContestJudge},
	}}
	reader := NewContestReader(privateContestReaderStore{}, nil, roles)

	record, err := reader.GetContest(t.Context(), auth.Actor{UserID: 7}, 9)
	if err != nil {
		t.Fatalf("GetContest error = %v", err)
	}
	want := []auth.Role{auth.RoleContestJudge, auth.RoleContestStaff}
	if len(record.CurrentUserRoles) != len(want) {
		t.Fatalf("current user roles = %v, want %v", record.CurrentUserRoles, want)
	}
	for i := range want {
		if record.CurrentUserRoles[i] != want[i] {
			t.Fatalf("current user roles = %v, want %v", record.CurrentUserRoles, want)
		}
	}
}

func TestContestRecordExposesEmptyRolesForViewersWithoutRoles(t *testing.T) {
	reader := NewContestReader(publicContestReaderStore{}, nil, nil)

	record, err := reader.GetContest(t.Context(), auth.Actor{UserID: 42, Role: auth.RoleUser}, 9)
	if err != nil {
		t.Fatalf("GetContest error = %v", err)
	}
	if record.CurrentUserRoles == nil {
		t.Fatal("current user roles = nil, want empty array")
	}
	if len(record.CurrentUserRoles) != 0 {
		t.Fatalf("current user roles = %v, want empty array", record.CurrentUserRoles)
	}
}

func TestListContestRolesRequiresContestManager(t *testing.T) {
	repo := newMemoryRepository()
	repo.contests[9] = ContestRecord{ID: 9, OwnerUserID: 1, Visibility: VisibilityPrivate, Status: StatusPublished}
	roles := &contestRoleMemoryStore{roles: map[[2]int64][]auth.Role{
		{9, 7}: {auth.RoleContestManager},
		{9, 8}: {auth.RoleContestStaff},
	}}
	reader := NewContestReader(repo, nil, roles)
	service := NewService(
		reader,
		NewContestAuthoring(repo, reader),
		NewContestPolicy(reader, repo),
		NewScoreboardService(reader, repo),
		roles,
	)

	assignments, err := service.ListContestRoles(t.Context(), auth.Actor{UserID: 7}, 9)
	if err != nil {
		t.Fatalf("contest manager ListContestRoles error = %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("assignments = %v, want 2 entries", assignments)
	}

	if _, err := service.ListContestRoles(t.Context(), auth.Actor{UserID: 8}, 9); err == nil {
		t.Fatal("contest staff can list role assignments")
	}
	if _, err := service.ListContestRoles(t.Context(), auth.Actor{}, 9); err == nil {
		t.Fatal("anonymous actor can list role assignments")
	}
	admin := auth.Actor{UserID: 5, Roles: []auth.Role{auth.RoleAdmin}}
	if _, err := service.ListContestRoles(t.Context(), admin, 9); err != nil {
		t.Fatalf("admin ListContestRoles error = %v", err)
	}
}
