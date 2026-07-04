package user

import (
	"context"
	"testing"
	"time"

	"SOJ/internal/auth"
)

type memoryRepo struct {
	users       map[int64]UserWithPassword
	refresh     map[string]RefreshToken
	createdHash string
	revokedHash string
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
