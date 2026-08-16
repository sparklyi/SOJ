package user

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

type memoryRepo struct {
	users       map[int64]UserWithPassword
	refresh     map[string]RefreshToken
	createdHash string
	revokedHash string
	cursorCalls int
}

type memoryRoleStore struct {
	roles map[int64][]auth.Role
}

func (r *memoryRoleStore) ListUserRoles(_ context.Context, userID int64) ([]auth.Role, error) {
	return append([]auth.Role(nil), r.roles[userID]...), nil
}

func (r *memoryRoleStore) GrantRole(_ context.Context, userID int64, role auth.Role, grantedBy *int64, _ string) (RoleAssignment, error) {
	for _, current := range r.roles[userID] {
		if current == role {
			return RoleAssignment{}, ErrConflict
		}
	}
	r.roles[userID] = append(r.roles[userID], role)
	return RoleAssignment{ID: int64(len(r.roles[userID])), UserID: userID, Role: role, GrantedBy: grantedBy}, nil
}

func (r *memoryRoleStore) RevokeRole(_ context.Context, userID int64, role auth.Role, _ int64, _ string) error {
	if role == auth.RoleUser {
		return ErrProtectedRole
	}
	roles := r.roles[userID]
	for i, current := range roles {
		if current == role {
			r.roles[userID] = append(roles[:i], roles[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
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
			42: {User: User{ID: 42, Email: "user@example.com", Username: "user", Roles: []auth.Role{auth.RoleUser}, Status: StatusActive}},
		},
		refresh: map[string]RefreshToken{
			hash: {UserID: 42, TokenHash: hash, DeviceID: "device-1", ExpiresAt: now.Add(time.Hour)},
		},
	}
	service := NewService(repo, auth.NewJWTManager("secret", time.Minute), WithRoleStore(&memoryRoleStore{roles: map[int64][]auth.Role{42: {auth.RoleUser}}}), WithTokenTTLs(time.Minute, time.Hour), WithClock(func() time.Time {
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
		3: {User: User{ID: 3, Username: "third", Roles: []auth.Role{auth.RoleUser}, Status: StatusActive, CreatedAt: createdAt}},
		2: {User: User{ID: 2, Username: "second", Roles: []auth.Role{auth.RoleUser}, Status: StatusActive, CreatedAt: createdAt}},
		1: {User: User{ID: 1, Username: "first", Roles: []auth.Role{auth.RoleUser}, Status: StatusActive, CreatedAt: createdAt.Add(-time.Minute)}},
	}}
	service := NewService(repo, auth.NewJWTManager("secret", time.Minute))
	actor := auth.Actor{UserID: 99, Roles: []auth.Role{auth.RoleRoot}}

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

func TestServiceMeLoadsRolesAndPermissions(t *testing.T) {
	repo := &memoryRepo{users: map[int64]UserWithPassword{
		42: {User: User{ID: 42, Email: "user@example.com", Username: "user", Status: StatusActive}},
	}}
	roles := &memoryRoleStore{roles: map[int64][]auth.Role{
		42: {auth.RoleUser, auth.RoleAuthor},
	}}
	service := NewService(repo, auth.NewJWTManager("secret", time.Minute), WithRoleStore(roles))

	got, err := service.Me(context.Background(), auth.Actor{UserID: 42})
	if err != nil {
		t.Fatalf("Me() error = %v", err)
	}
	if len(got.Roles) != 2 || got.Roles[0] != auth.RoleUser || got.Roles[1] != auth.RoleAuthor {
		t.Fatalf("roles = %v, want [user author]", got.Roles)
	}
	if len(got.Permissions) == 0 {
		t.Fatal("permissions are empty")
	}
	if got.Permissions[0] != "contest.join" {
		t.Fatalf("permissions are not deterministic: %v", got.Permissions)
	}
}

func TestServiceGrantRoleRequiresRootAndWritesAssignment(t *testing.T) {
	roles := &memoryRoleStore{roles: map[int64][]auth.Role{42: {auth.RoleUser}}}
	service := NewService(&memoryRepo{}, auth.NewJWTManager("secret", time.Minute), WithRoleStore(roles))

	if _, err := service.GrantRole(context.Background(), auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleUser}}, 42, GrantRoleInput{Role: "author", Reason: "approved author"}); err == nil {
		t.Fatal("user GrantRole() error = nil, want forbidden")
	}
	assignment, err := service.GrantRole(context.Background(), auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleRoot}}, 42, GrantRoleInput{Role: "author", Reason: "approved author"})
	if err != nil {
		t.Fatalf("root GrantRole() error = %v", err)
	}
	if assignment.UserID != 42 || assignment.Role != auth.RoleAuthor {
		t.Fatalf("assignment = %+v, want user 42 author", assignment)
	}
}

func TestServiceRevokeRoleProtectsBaseUserRole(t *testing.T) {
	roles := &memoryRoleStore{roles: map[int64][]auth.Role{42: {auth.RoleUser, auth.RoleAuthor}}}
	service := NewService(&memoryRepo{}, auth.NewJWTManager("secret", time.Minute), WithRoleStore(roles))

	err := service.RevokeRole(context.Background(), auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleRoot}}, 42, "user", RevokeRoleInput{Reason: "remove access"})
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != "role.protected" {
		t.Fatalf("RevokeRole() error = %v, want role.protected", err)
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
