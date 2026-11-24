package submission

import (
	"context"
	"testing"

	"SOJ/internal/auth"
)

// recordingRefresher counts how many times the site-stats aggregate cache was
// asked to refresh. Language edits that cannot change the enabled-language
// count must not trigger it.
type recordingRefresher struct {
	calls int
}

func (r *recordingRefresher) Refresh(context.Context) { r.calls++ }

// updateLanguageStoreStub is a configurable languageStore for UpdateLanguage
// tests: it returns a fixed record/error and captures the last input.
type updateLanguageStoreStub struct {
	record   LanguageRecord
	err      error
	lastIn   UpdateLanguageInput
	lastID   int64
	lastUser int64
}

func (s *updateLanguageStoreStub) ListLanguages(context.Context, ListLanguagesInput) ([]LanguageRecord, int64, error) {
	return nil, 0, nil
}

func (s *updateLanguageStoreStub) UpdateLanguage(_ context.Context, id int64, input UpdateLanguageInput, userID int64) (LanguageRecord, error) {
	s.lastID = id
	s.lastIn = input
	s.lastUser = userID
	return s.record, s.err
}

func adminActor() auth.Actor {
	return auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleAdmin}, Permissions: seededPermissions(auth.RoleAdmin)}
}

func TestUpdateLanguageRefreshesStatsOnlyWhenEnabledProvided(t *testing.T) {
	trueVal := true
	falseVal := false
	timeLimit := int32(2000)

	cases := []struct {
		name      string
		input     UpdateLanguageInput
		storeErr  error
		wantCalls int
	}{
		{
			name:      "limit-only edit skips refresh",
			input:     UpdateLanguageInput{DefaultTimeLimitMS: &timeLimit},
			wantCalls: 0,
		},
		{
			name:      "explicit enabled true refreshes once",
			input:     UpdateLanguageInput{Enabled: &trueVal},
			wantCalls: 1,
		},
		{
			name:      "explicit enabled false refreshes once",
			input:     UpdateLanguageInput{Enabled: &falseVal},
			wantCalls: 1,
		},
		{
			name:      "failed save never refreshes",
			input:     UpdateLanguageInput{Enabled: &trueVal},
			storeErr:  context.DeadlineExceeded,
			wantCalls: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &updateLanguageStoreStub{record: LanguageRecord{ID: 71, Name: "Go", Enabled: true}, err: tc.storeErr}
			refresher := &recordingRefresher{}
			service := NewLanguageService(store, refresher)

			record, err := service.UpdateLanguage(t.Context(), adminActor(), 71, tc.input)
			if tc.storeErr != nil {
				if err == nil {
					t.Fatalf("UpdateLanguage() error = nil, want %v", tc.storeErr)
				}
			} else {
				if err != nil {
					t.Fatalf("UpdateLanguage() error = %v", err)
				}
				if record.ID != 71 {
					t.Fatalf("UpdateLanguage() record = %+v, want ID 71", record)
				}
			}

			// The updated limits must still be saved and returned regardless of
			// whether a refresh happened.
			if store.lastID != 71 {
				t.Fatalf("store saw id = %d, want 71", store.lastID)
			}
			if refresher.calls != tc.wantCalls {
				t.Fatalf("refresher calls = %d, want %d", refresher.calls, tc.wantCalls)
			}
		})
	}
}