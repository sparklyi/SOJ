package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusDeleted  = "deleted"

	defaultAccessTTL  = 15 * time.Minute
	defaultRefreshTTL = 30 * 24 * time.Hour
)

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Bio       string    `json:"bio,omitempty"`
	Role      auth.Role `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type RegisterInput struct {
	Email    string `json:"email" binding:"required,email"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	DeviceID string `json:"device_id"`
}

type LoginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	DeviceID string `json:"device_id"`
}

type RefreshInput struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
	DeviceID     string `json:"device_id"`
}

type LogoutInput struct {
	RefreshToken string `json:"refresh_token"`
}

type ListUsersInput struct {
	Role     string
	Status   string
	Keyword  string
	Page     int32
	PageSize int32
	Cursor   *UserCursor
}

type UserCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

type UpdateUserInput struct {
	Username *string `json:"username"`
	Bio      *string `json:"bio"`
	Role     *string `json:"role"`
	Status   *string `json:"status"`
}

type AuthSession struct {
	User         User   `json:"user"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type UserList struct {
	Items    []User `json:"items"`
	Total    int64  `json:"total"`
	Page     int32  `json:"page"`
	PageSize int32  `json:"page_size"`
}

type UserCursorPage struct {
	Items      []User      `json:"items"`
	NextCursor *UserCursor `json:"next_cursor,omitempty"`
}

type TokenMetadata struct {
	DeviceID  string
	UserAgent string
	IP        string
	ExpiresAt time.Time
}

type RefreshToken struct {
	UserID    int64
	TokenHash string
	DeviceID  string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type Repository interface {
	CreateUser(ctx context.Context, email, passwordHash, username string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (UserWithPassword, error)
	GetUserByID(ctx context.Context, id int64) (User, error)
	ListUsers(ctx context.Context, input ListUsersInput) ([]User, int64, error)
	ListUsersByCursor(ctx context.Context, input ListUsersInput) ([]User, error)
	UpdateUser(ctx context.Context, id int64, input UpdateUserInput) (User, error)
	CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, meta TokenMetadata) error
	GetRefreshToken(ctx context.Context, tokenHash string) (RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	RevokeUserDeviceRefreshTokens(ctx context.Context, userID int64, deviceID string) error
}

type UserWithPassword struct {
	User
	PasswordHash string
}

type Service struct {
	repo       Repository
	jwt        *auth.JWTManager
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

func NewService(repo Repository, jwtManager *auth.JWTManager, opts ...Option) *Service {
	s := &Service{
		repo:       repo,
		jwt:        jwtManager,
		accessTTL:  defaultAccessTTL,
		refreshTTL: defaultRefreshTTL,
		now:        func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type Option func(*Service)

func WithTokenTTLs(accessTTL, refreshTTL time.Duration) Option {
	return func(s *Service) {
		if accessTTL > 0 {
			s.accessTTL = accessTTL
		}
		if refreshTTL > 0 {
			s.refreshTTL = refreshTTL
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func (s *Service) Register(ctx context.Context, actor auth.Actor, input RegisterInput) (AuthSession, error) {
	input.Email = normalizeEmail(input.Email)
	input.Username = strings.TrimSpace(input.Username)
	if input.Email == "" || input.Username == "" || input.Password == "" {
		return AuthSession{}, apperror.BadRequest("auth.invalid_register", "email, username and password are required")
	}
	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		return AuthSession{}, err
	}
	created, err := s.repo.CreateUser(ctx, input.Email, passwordHash, input.Username)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return AuthSession{}, apperror.Conflict("user.email_conflict", "email already exists")
		}
		return AuthSession{}, err
	}
	return s.issueSession(ctx, created, deviceID(input.DeviceID), "", "")
}

func (s *Service) Login(ctx context.Context, actor auth.Actor, input LoginInput) (AuthSession, error) {
	input.Email = normalizeEmail(input.Email)
	if input.Email == "" || input.Password == "" {
		return AuthSession{}, apperror.BadRequest("auth.invalid_login", "email and password are required")
	}
	user, err := s.repo.GetUserByEmail(ctx, input.Email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return AuthSession{}, invalidCredentials()
		}
		return AuthSession{}, err
	}
	if user.Status != StatusActive || !auth.VerifyPassword(user.PasswordHash, input.Password) {
		return AuthSession{}, invalidCredentials()
	}
	return s.issueSession(ctx, user.User, deviceID(input.DeviceID), "", "")
}

func (s *Service) Refresh(ctx context.Context, actor auth.Actor, input RefreshInput) (AuthSession, error) {
	if strings.TrimSpace(input.RefreshToken) == "" {
		return AuthSession{}, apperror.BadRequest("auth.refresh_token_required", "refresh token is required")
	}
	hash := auth.HashRefreshToken(input.RefreshToken)
	token, err := s.repo.GetRefreshToken(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return AuthSession{}, apperror.Unauthorized("auth.invalid_refresh_token", "invalid refresh token")
		}
		return AuthSession{}, err
	}
	if token.RevokedAt != nil || !s.now().Before(token.ExpiresAt) {
		return AuthSession{}, apperror.Unauthorized("auth.invalid_refresh_token", "invalid refresh token")
	}
	currentUser, err := s.repo.GetUserByID(ctx, token.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return AuthSession{}, apperror.Unauthorized("auth.invalid_refresh_token", "invalid refresh token")
		}
		return AuthSession{}, err
	}
	if currentUser.Status != StatusActive {
		return AuthSession{}, apperror.Unauthorized("auth.invalid_refresh_token", "invalid refresh token")
	}
	if err := s.repo.RevokeRefreshToken(ctx, hash); err != nil {
		return AuthSession{}, err
	}
	if input.DeviceID != "" {
		token.DeviceID = input.DeviceID
	}
	return s.issueSession(ctx, currentUser, token.DeviceID, "", "")
}

func (s *Service) Logout(ctx context.Context, actor auth.Actor, input LogoutInput) error {
	if input.RefreshToken != "" {
		return s.repo.RevokeRefreshToken(ctx, auth.HashRefreshToken(input.RefreshToken))
	}
	if actor.Authenticated() && actor.DeviceID != "" {
		return s.repo.RevokeUserDeviceRefreshTokens(ctx, actor.UserID, actor.DeviceID)
	}
	return apperror.Unauthorized("unauthorized", "unauthorized")
}

func (s *Service) Me(ctx context.Context, actor auth.Actor) (User, error) {
	if !actor.Authenticated() {
		return User{}, apperror.Unauthorized("unauthorized", "unauthorized")
	}
	user, err := s.repo.GetUserByID(ctx, actor.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, apperror.Unauthorized("unauthorized", "unauthorized")
		}
		return User{}, err
	}
	return user, nil
}

func (s *Service) ListUsers(ctx context.Context, actor auth.Actor, input ListUsersInput) (UserList, error) {
	if !actor.Root() {
		return UserList{}, apperror.Forbidden("forbidden", "root role required")
	}
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PageSize <= 0 || input.PageSize > 100 {
		input.PageSize = 20
	}
	users, total, err := s.repo.ListUsers(ctx, input)
	if err != nil {
		return UserList{}, err
	}
	return UserList{Items: users, Total: total, Page: input.Page, PageSize: input.PageSize}, nil
}

func (s *Service) ListUsersByCursor(ctx context.Context, actor auth.Actor, input ListUsersInput) (UserCursorPage, error) {
	if !actor.Root() {
		return UserCursorPage{}, apperror.Forbidden("forbidden", "root role required")
	}
	if input.PageSize <= 0 || input.PageSize > 100 {
		input.PageSize = 20
	}
	cursor := UserCursor{
		CreatedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC),
		ID:        1<<63 - 1,
	}
	if input.Cursor != nil {
		if input.Cursor.ID <= 0 || input.Cursor.CreatedAt.IsZero() {
			return UserCursorPage{}, apperror.BadRequest("invalid_cursor", "cursor is invalid")
		}
		cursor = UserCursor{CreatedAt: input.Cursor.CreatedAt.UTC(), ID: input.Cursor.ID}
	}
	input.Cursor = &cursor
	limit := input.PageSize
	input.PageSize++
	users, err := s.repo.ListUsersByCursor(ctx, input)
	if err != nil {
		return UserCursorPage{}, err
	}
	hasMore := len(users) > int(limit)
	if hasMore {
		users = users[:limit]
	}
	page := UserCursorPage{Items: users}
	if hasMore {
		last := users[len(users)-1]
		page.NextCursor = &UserCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func (s *Service) UpdateUser(ctx context.Context, actor auth.Actor, id int64, input UpdateUserInput) (User, error) {
	if !actor.Root() {
		return User{}, apperror.Forbidden("forbidden", "root role required")
	}
	current, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, apperror.NotFound("user.not_found", "user not found")
		}
		return User{}, err
	}
	if current.Role == auth.RoleRoot && actor.UserID != current.ID {
		return User{}, apperror.Forbidden("user.root_protected", "root user cannot be modified by another user")
	}
	if input.Role != nil {
		if _, err := auth.ParseRole(*input.Role); err != nil {
			return User{}, apperror.BadRequest("user.invalid_role", "invalid role")
		}
	}
	if input.Status != nil && !validStatus(*input.Status) {
		return User{}, apperror.BadRequest("user.invalid_status", "invalid status")
	}
	user, err := s.repo.UpdateUser(ctx, id, input)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, apperror.NotFound("user.not_found", "user not found")
		}
		return User{}, err
	}
	return user, nil
}

func (s *Service) issueSession(ctx context.Context, user User, deviceID, userAgent, ip string) (AuthSession, error) {
	token, refreshHash, err := auth.NewRefreshToken()
	if err != nil {
		return AuthSession{}, err
	}
	actor := auth.Actor{UserID: user.ID, Role: user.Role, DeviceID: deviceID}
	access, err := s.jwt.IssueAccessToken(actor)
	if err != nil {
		return AuthSession{}, err
	}
	if err := s.repo.CreateRefreshToken(ctx, user.ID, refreshHash, TokenMetadata{
		DeviceID:  deviceID,
		UserAgent: userAgent,
		IP:        ip,
		ExpiresAt: s.now().Add(s.refreshTTL),
	}); err != nil {
		return AuthSession{}, err
	}
	return AuthSession{
		User:         user,
		AccessToken:  access,
		RefreshToken: token,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
	}, nil
}

func invalidCredentials() error {
	return apperror.Unauthorized("auth.invalid_credentials", "invalid credentials")
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func deviceID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return value
}

func validStatus(status string) bool {
	switch status {
	case StatusActive, StatusDisabled, StatusDeleted:
		return true
	default:
		return false
	}
}
