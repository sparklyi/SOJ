package user

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"SOJ/internal/auth"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type PostgresRepository struct {
	q db.Querier
}

func NewPostgresRepository(q db.Querier) *PostgresRepository {
	return &PostgresRepository{q: q}
}

func (r *PostgresRepository) CreateUser(ctx context.Context, email, passwordHash, username string) (User, error) {
	row, err := r.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
		Username:     username,
		Role:         string(auth.RoleUser),
		Status:       StatusActive,
	})
	if err != nil {
		return User{}, mapDBError(err)
	}
	return mapUser(row), nil
}

func (r *PostgresRepository) GetUserByEmail(ctx context.Context, email string) (UserWithPassword, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		return UserWithPassword{}, mapDBError(err)
	}
	return UserWithPassword{User: mapUser(row), PasswordHash: row.PasswordHash}, nil
}

func (r *PostgresRepository) GetUserByID(ctx context.Context, id int64) (User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if err != nil {
		return User{}, mapDBError(err)
	}
	return mapUser(row), nil
}

func (r *PostgresRepository) ListUsers(ctx context.Context, input ListUsersInput) ([]User, int64, error) {
	limit := input.PageSize
	offset := (input.Page - 1) * input.PageSize
	params := db.ListUsersParams{
		Role:    nullableText(input.Role),
		Status:  nullableText(input.Status),
		Keyword: nullableText(input.Keyword),
		Limit:   limit,
		Offset:  offset,
	}
	rows, err := r.q.ListUsers(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	total, err := r.q.CountUsers(ctx, db.CountUsersParams{
		Role:    params.Role,
		Status:  params.Status,
		Keyword: params.Keyword,
	})
	if err != nil {
		return nil, 0, err
	}
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		users = append(users, mapUser(row))
	}
	return users, total, nil
}

func (r *PostgresRepository) ListUsersByCursor(ctx context.Context, input ListUsersInput) ([]User, error) {
	cursor := input.Cursor
	if cursor == nil {
		cursor = &UserCursor{CreatedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC), ID: 1<<63 - 1}
	}
	rows, err := r.q.ListUsersByCursor(ctx, db.ListUsersByCursorParams{
		Role:            nullableText(input.Role),
		Status:          nullableText(input.Status),
		Keyword:         nullableText(input.Keyword),
		BeforeCreatedAt: pgtype.Timestamptz{Time: cursor.CreatedAt.UTC(), Valid: true},
		BeforeID:        cursor.ID,
		Limit:           input.PageSize,
	})
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		users = append(users, mapUser(row))
	}
	return users, nil
}

func (r *PostgresRepository) UpdateUser(ctx context.Context, id int64, input UpdateUserInput) (User, error) {
	row, err := r.q.UpdateUserAdminFields(ctx, db.UpdateUserAdminFieldsParams{
		ID:       id,
		Username: nullableTextPtr(input.Username),
		Bio:      nullableTextPtr(input.Bio),
		Role:     nullableTextPtr(input.Role),
		Status:   nullableTextPtr(input.Status),
	})
	if err != nil {
		return User{}, mapDBError(err)
	}
	return mapUser(row), nil
}

func (r *PostgresRepository) CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, meta TokenMetadata) error {
	var ip *netip.Addr
	if meta.IP != "" {
		if parsed, err := netip.ParseAddr(meta.IP); err == nil {
			ip = &parsed
		}
	}
	_, err := r.q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		DeviceID:  meta.DeviceID,
		UserAgent: nullableText(meta.UserAgent),
		Ip:        ip,
		ExpiresAt: pgtype.Timestamptz{Time: meta.ExpiresAt, Valid: true},
	})
	return mapDBError(err)
}

func (r *PostgresRepository) GetRefreshToken(ctx context.Context, tokenHash string) (RefreshToken, error) {
	row, err := r.q.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return RefreshToken{}, mapDBError(err)
	}
	var revokedAt *time.Time
	if row.RevokedAt.Valid {
		revokedAt = &row.RevokedAt.Time
	}
	return RefreshToken{
		UserID:    row.UserID,
		TokenHash: row.TokenHash,
		DeviceID:  row.DeviceID,
		ExpiresAt: row.ExpiresAt.Time,
		RevokedAt: revokedAt,
	}, nil
}

func (r *PostgresRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	return r.q.RevokeRefreshToken(ctx, tokenHash)
}

func (r *PostgresRepository) RevokeUserDeviceRefreshTokens(ctx context.Context, userID int64, deviceID string) error {
	return r.q.RevokeUserDeviceRefreshTokens(ctx, db.RevokeUserDeviceRefreshTokensParams{UserID: userID, DeviceID: deviceID})
}

func mapUser(row db.User) User {
	role, _ := auth.ParseRole(row.Role)
	return User{
		ID:        row.ID,
		Email:     row.Email,
		Username:  row.Username,
		AvatarURL: textValue(row.AvatarUrl),
		Bio:       textValue(row.Bio),
		Role:      role,
		Status:    row.Status,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func nullableText(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != ""}
}

func nullableTextPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func mapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
