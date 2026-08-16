package contest

import (
	"context"
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
	return append([]auth.Role(nil), s.roles[[2]int64{contestID, userID}]...), nil
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
