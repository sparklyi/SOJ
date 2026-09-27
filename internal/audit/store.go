package audit

import (
	"context"
	"encoding/json"
	"time"

	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5/pgtype"
)

type ListFilter struct {
	ObjectType ObjectType
	ObjectID   int64
	ActorID    int64
	Action     Action
	Page       int32
	PageSize   int32
}

// Record is one audit event as the admin console reads it back.
type Record struct {
	ID            int64           `json:"id"`
	ActorUserID   *int64          `json:"actor_user_id,omitempty"`
	ActorUsername string          `json:"actor_username,omitempty"`
	Action        Action          `json:"action"`
	ObjectType    ObjectType      `json:"object_type"`
	ObjectID      int64           `json:"object_id"`
	Reason        string          `json:"reason,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type Store interface {
	ListEvents(ctx context.Context, filter ListFilter) ([]Record, int64, error)
}

type PostgresStore struct {
	q *db.Queries
}

func NewPostgresStore(q *db.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

func (s *PostgresStore) ListEvents(ctx context.Context, filter ListFilter) ([]Record, int64, error) {
	params := db.ListAuditEventsParams{
		ObjectType: nullableObjectType(filter.ObjectType),
		ObjectID:   nullableID(filter.ObjectID),
		ActorID:    nullableID(filter.ActorID),
		Action:     nullableAction(filter.Action),
		Offset:     (filter.Page - 1) * filter.PageSize,
		Limit:      filter.PageSize,
	}
	rows, err := s.q.ListAuditEvents(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.q.CountAuditEvents(ctx, db.CountAuditEventsParams{
		ObjectType: params.ObjectType,
		ObjectID:   params.ObjectID,
		ActorID:    params.ActorID,
		Action:     params.Action,
	})
	if err != nil {
		return nil, 0, err
	}
	records := make([]Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, recordFromDB(row))
	}
	return records, total, nil
}

func recordFromDB(row db.ListAuditEventsRow) Record {
	record := Record{
		ID:            row.ID,
		Action:        Action(row.Action),
		ObjectType:    ObjectType(row.ObjectType),
		ObjectID:      row.ObjectID,
		Reason:        row.Reason,
		Metadata:      row.Metadata,
		CreatedAt:     row.CreatedAt.Time.UTC(),
		ActorUsername: row.ActorUsername.String,
	}
	if row.ActorUserID.Valid {
		actorID := row.ActorUserID.Int64
		record.ActorUserID = &actorID
	}
	return record
}

func nullableObjectType(value ObjectType) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(value), Valid: true}
}

func nullableAction(value Action) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(value), Valid: true}
}

func nullableID(value int64) pgtype.Int8 {
	if value <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: value, Valid: true}
}
