package user

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"SOJ/internal/auth"
)

type memoryRepo struct {
	users       map[int64]UserWithPassword
	refresh     map[string]RefreshToken
	createdHash string
	revokedHash string
	cursorCalls int
}

func (r *memoryRepo) CreateUser(context.Context, string, string, string) (User, error) {
	return User{}, nil
}

func (r *memoryRepo) GetUserByEmail(context.Context, string) (UserWithPassword, error) {
	return UserWithPassword{}, ErrNotFound
}

func (r *memoryRepo) GetUserByID(_ context.Context, id int64) (User, error) {
	user, ok := r.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return user.User, nil
}

func (r *memoryRepo) ListUsers(context.Context, ListUsersInput) ([]User, int64, error) {
	return nil, 0, nil
}

func (r *memoryRepo) ListUsersByCursor(_ context.Context, input ListUsersInput) ([]User, error) {
	r.cursorCalls++
	cursor := input.Cursor
	if cursor == nil {
		cursor = &UserCursor{CreatedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC), ID: 1<<63 - 1}
	}
	keyword := strings.ToLower(strings.TrimSpace(input.Keyword))
	users := make([]User, 0, len(r.users))
	for _, user := range r.users {
		row := user.User
		if input.Role != "" && string(row.Role) != input.Role {
			continue
		}
		if input.Status != "" && row.Status != input.Status {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(row.Email), keyword) && !strings.Contains(strings.ToLower(row.Username), keyword) {
			continue
		}
		if row.CreatedAt.After(cursor.CreatedAt) || (row.CreatedAt.Equal(cursor.CreatedAt) && row.ID >= cursor.ID) {
			continue
		}
		users = append(users, row)
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].CreatedAt.Equal(users[j].CreatedAt) {
			return users[i].ID > users[j].ID
		}
		return users[i].CreatedAt.After(users[j].CreatedAt)
	})
	if input.PageSize > 0 && len(users) > int(input.PageSize) {
		users = users[:input.PageSize]
	}
	return users, nil
}

func (r *memoryRepo) UpdateUser(context.Context, int64, UpdateUserInput) (User, error) {
	return User{}, nil
}

func (r *memoryRepo) CreateRefreshToken(_ context.Context, userID int64, tokenHash string, meta TokenMetadata) error {
	r.createdHash = tokenHash
	r.refresh[tokenHash] = RefreshToken{
		UserID:    userID,
		TokenHash: tokenHash,
		DeviceID:  meta.DeviceID,
		ExpiresAt: meta.ExpiresAt,
	}
	return nil
}

func (r *memoryRepo) GetRefreshToken(_ context.Context, tokenHash string) (RefreshToken, error) {
	token, ok := r.refresh[tokenHash]
	if !ok {
		return RefreshToken{}, ErrNotFound
	}
	return token, nil
}

func (r *memoryRepo) RevokeRefreshToken(_ context.Context, tokenHash string) error {
	r.revokedHash = tokenHash
	token := r.refresh[tokenHash]
	now := time.Now().UTC()
	token.RevokedAt = &now
	r.refresh[tokenHash] = token
	return nil
}

func (r *memoryRepo) RevokeUserDeviceRefreshTokens(context.Context, int64, string) error {
	return nil
}

func TestServiceRefreshRotatesRefreshTokenByHash(t *testing.T) {
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	plain, hash, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	repo := &memoryRepo{
		users: map[int64]UserWithPassword{
			42: {User: User{ID: 42, Email: "user@example.com", Username: "user", Role: auth.RoleUser, Status: StatusActive}},
		},
		refresh: map[string]RefreshToken{
			hash: {UserID: 42, TokenHash: hash, DeviceID: "device-1", ExpiresAt: now.Add(time.Hour)},
		},
	}
	service := NewService(repo, auth.NewJWTManager("secret", time.Minute), WithTokenTTLs(time.Minute, time.Hour), WithClock(func() time.Time {
		return now
	}))

	session, err := service.Refresh(context.Background(), auth.Anonymous("req-1"), RefreshInput{RefreshToken: plain})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if repo.revokedHash != hash {
		t.Fatalf("revoked hash = %q, want %q", repo.revokedHash, hash)
	}
	if repo.createdHash == "" || repo.createdHash == hash || repo.createdHash == session.RefreshToken {
		t.Fatalf("created hash = %q, old hash = %q, plaintext = %q", repo.createdHash, hash, session.RefreshToken)
	}
	if got := auth.HashRefreshToken(session.RefreshToken); got != repo.createdHash {
		t.Fatalf("new refresh hash = %q, want %q", got, repo.createdHash)
	}
}

func TestListUsersByCursorUsesSeekPagination(t *testing.T) {
	createdAt := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	repo := &memoryRepo{users: map[int64]UserWithPassword{
		3: {User: User{ID: 3, Username: "third", Role: auth.RoleUser, Status: StatusActive, CreatedAt: createdAt}},
		2: {User: User{ID: 2, Username: "second", Role: auth.RoleUser, Status: StatusActive, CreatedAt: createdAt}},
		1: {User: User{ID: 1, Username: "first", Role: auth.RoleUser, Status: StatusActive, CreatedAt: createdAt.Add(-time.Minute)}},
	}}
	service := NewService(repo, auth.NewJWTManager("secret", time.Minute))
	actor := auth.Actor{UserID: 99, Role: auth.RoleRoot}

	first, err := service.ListUsersByCursor(t.Context(), actor, ListUsersInput{PageSize: 2})
	if err != nil {
		t.Fatalf("first cursor page: %v", err)
	}
	if got := []int64{first.Items[0].ID, first.Items[1].ID}; !equalInt64s(got, []int64{3, 2}) {
		t.Fatalf("first cursor IDs = %v, want [3 2]", got)
	}
	if first.NextCursor == nil || first.NextCursor.ID != 2 {
		t.Fatalf("first next cursor = %+v, want ID 2", first.NextCursor)
	}

	second, err := service.ListUsersByCursor(t.Context(), actor, ListUsersInput{PageSize: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("second cursor page: %v", err)
	}
	if got := []int64{second.Items[0].ID}; !equalInt64s(got, []int64{1}) {
		t.Fatalf("second cursor IDs = %v, want [1]", got)
	}
	if second.NextCursor != nil {
		t.Fatalf("second next cursor = %+v, want nil", second.NextCursor)
	}
	if repo.cursorCalls != 2 {
		t.Fatalf("cursor calls = %d, want 2", repo.cursorCalls)
	}
}

func equalInt64s(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
