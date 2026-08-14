package contest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"SOJ/internal/apperror"
)

const (
	defaultScoreboardPageSize int32 = 20
	maxScoreboardPageSize     int32 = 100
)

type ScoreboardQuery struct {
	View     ScoreboardView
	PageSize int32
	Cursor   string
}

type scoreboardCursor struct {
	ContestID          int64          `json:"contest_id"`
	View               ScoreboardView `json:"view"`
	SnapshotID         int64          `json:"snapshot_id,omitempty"`
	AfterOrdinal       int32          `json:"after_ordinal,omitempty"`
	AfterAcceptedCount int32          `json:"after_accepted_count,omitempty"`
	AfterPenalty       int32          `json:"after_penalty,omitempty"`
	AfterDisplayName   string         `json:"after_display_name,omitempty"`
	AfterUserID        int64          `json:"after_user_id,omitempty"`
	RowsSeen           int32          `json:"rows_seen,omitempty"`
	LastRank           int32          `json:"last_rank,omitempty"`
}

type scoreboardRowsQuery struct {
	PageSize           int32
	HasCursor          bool
	AfterAcceptedCount int32
	AfterPenalty       int32
	AfterDisplayName   string
	AfterUserID        int64
}

type scoreSnapshotPageResult struct {
	Snapshot ScoreboardSnapshot
	Rows     []ScoreboardRow
	HasMore  bool
}

func normalizeScoreboardPageSize(size int32) (int32, error) {
	if size < 1 || size > maxScoreboardPageSize {
		return 0, apperror.BadRequest("invalid_argument", fmt.Sprintf("scoreboard page_size must be between 1 and %d", maxScoreboardPageSize))
	}
	return size, nil
}

func encodeScoreboardCursor(cursor scoreboardCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeScoreboardCursor(raw string) (scoreboardCursor, error) {
	if raw == "" {
		return scoreboardCursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return scoreboardCursor{}, apperror.BadRequest("invalid_cursor", "scoreboard cursor is invalid")
	}
	var cursor scoreboardCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.ContestID <= 0 || cursor.View == "" {
		return scoreboardCursor{}, apperror.BadRequest("invalid_cursor", "scoreboard cursor is invalid")
	}
	return cursor, nil
}

func validateScoreboardCursor(cursor scoreboardCursor, contestID int64, view ScoreboardView) error {
	if cursor.ContestID == 0 {
		return nil
	}
	if cursor.ContestID != contestID || cursor.View != view {
		return apperror.BadRequest("invalid_cursor", "scoreboard cursor does not belong to this contest view")
	}
	if cursor.SnapshotID != 0 && cursor.AfterOrdinal < 0 {
		return apperror.BadRequest("invalid_cursor", "scoreboard cursor is invalid")
	}
	return nil
}
