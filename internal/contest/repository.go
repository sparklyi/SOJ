package contest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/postgres"
	"SOJ/internal/postgres/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, queries: db.New(pool)}
}

func (r *PostgresRepository) WithTx(ctx context.Context, fn func(context.Context, contestTransaction) error) error {
	return postgres.WithPoolTx(ctx, r.pool, func(tx pgx.Tx) error {
		return fn(ctx, &txRepository{queries: r.queries.WithTx(tx)})
	})
}

func (r *PostgresRepository) CreateContest(ctx context.Context, input ContestRecord) (ContestRecord, error) {
	return createContest(ctx, r.queries, input)
}

func (r *PostgresRepository) GetContest(ctx context.Context, id int64) (ContestRecord, error) {
	row, err := r.queries.GetContestByID(ctx, id)
	return contestFromDB(row), mapDBErr(err)
}

func (r *PostgresRepository) ListContests(ctx context.Context, filter ListContestFilter) ([]ContestRecord, int64, error) {
	return listContests(ctx, r.queries, filter)
}

func (r *PostgresRepository) ListContestsByCursor(ctx context.Context, filter ListContestFilter) ([]ContestRecord, error) {
	return listContestsByCursor(ctx, r.queries, filter)
}

func (r *PostgresRepository) UpdateContest(ctx context.Context, id int64, input ContestUpdateInput) (ContestRecord, error) {
	return updateContest(ctx, r.queries, id, input)
}

func (r *PostgresRepository) ArchiveContest(ctx context.Context, id int64) (ContestRecord, error) {
	row, err := r.queries.ArchiveContest(ctx, id)
	return contestFromDB(row), mapDBErr(err)
}

func (r *PostgresRepository) ReplaceContestProblems(ctx context.Context, contestID int64, problems []ContestProblem) error {
	return replaceContestProblems(ctx, r.queries, contestID, problems)
}

func (r *PostgresRepository) ListContestProblems(ctx context.Context, contestID int64) ([]ContestProblem, error) {
	return listContestProblems(ctx, r.queries, contestID)
}

func (r *PostgresRepository) CreateRegistration(ctx context.Context, input ContestRegistration) (ContestRegistration, error) {
	return createRegistration(ctx, r.queries, input)
}

func (r *PostgresRepository) GetRegistration(ctx context.Context, contestID, userID int64) (ContestRegistration, error) {
	row, err := r.queries.GetContestRegistration(ctx, db.GetContestRegistrationParams{ContestID: contestID, UserID: userID})
	return registrationFromDB(row), mapDBErr(err)
}

func (r *PostgresRepository) ListRegistrations(ctx context.Context, contestID int64) ([]ContestRegistration, error) {
	return listRegistrations(ctx, r.queries, contestID)
}

func (r *PostgresRepository) ListScoreboardRegistrations(ctx context.Context, contestID int64, query scoreboardRowsQuery) ([]ContestRegistration, error) {
	return listScoreboardRegistrations(ctx, r.queries, contestID, query)
}

func (r *PostgresRepository) ListProblemResults(ctx context.Context, contestID int64) ([]ContestProblemResult, error) {
	return listProblemResults(ctx, r.queries, contestID)
}

func (r *PostgresRepository) ListProblemResultsForUsers(ctx context.Context, contestID int64, userIDs []int64) ([]ContestProblemResult, error) {
	return listProblemResultsForUsers(ctx, r.queries, contestID, userIDs)
}

func (r *PostgresRepository) ListTerminalSubmissions(ctx context.Context, contestID int64) ([]ContestSubmissionResult, error) {
	return listTerminalSubmissions(ctx, r.queries, contestID)
}

func (r *PostgresRepository) UpsertProblemResult(ctx context.Context, result ContestProblemResult) (ContestProblemResult, error) {
	return upsertProblemResult(ctx, r.queries, result)
}

func (r *PostgresRepository) ListScoreSnapshotCandidates(ctx context.Context, now time.Time, limit int32) ([]ScoreSnapshotCandidate, error) {
	return listScoreSnapshotCandidates(ctx, r.queries, now, limit)
}

func (r *PostgresRepository) CreateScoreSnapshot(ctx context.Context, snapshot ScoreboardSnapshot) (ScoreboardSnapshot, error) {
	var created ScoreboardSnapshot
	err := postgres.WithPoolTx(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		created, err = createScoreSnapshot(ctx, r.queries.WithTx(tx), snapshot)
		return err
	})
	return created, err
}

func (r *PostgresRepository) LatestScoreSnapshot(ctx context.Context, contestID int64, view ScoreboardView) (ScoreboardSnapshot, error) {
	return latestScoreSnapshot(ctx, r.queries, contestID, view)
}

func (r *PostgresRepository) ScoreSnapshotPage(ctx context.Context, contestID int64, view ScoreboardView, afterOrdinal, limit int32) (scoreSnapshotPageResult, error) {
	return loadScoreSnapshotPage(ctx, r.queries, contestID, view, afterOrdinal, limit)
}

type txRepository struct {
	queries *db.Queries
}

func (r *txRepository) WithTx(ctx context.Context, fn func(context.Context, contestTransaction) error) error {
	return fn(ctx, r)
}

func (r *txRepository) CreateContest(ctx context.Context, input ContestRecord) (ContestRecord, error) {
	return createContest(ctx, r.queries, input)
}

func (r *txRepository) GetContest(ctx context.Context, id int64) (ContestRecord, error) {
	row, err := r.queries.GetContestByID(ctx, id)
	return contestFromDB(row), mapDBErr(err)
}

func (r *txRepository) ListContests(ctx context.Context, filter ListContestFilter) ([]ContestRecord, int64, error) {
	return listContests(ctx, r.queries, filter)
}

func (r *txRepository) ListContestsByCursor(ctx context.Context, filter ListContestFilter) ([]ContestRecord, error) {
	return listContestsByCursor(ctx, r.queries, filter)
}

func (r *txRepository) UpdateContest(ctx context.Context, id int64, input ContestUpdateInput) (ContestRecord, error) {
	return updateContest(ctx, r.queries, id, input)
}

func (r *txRepository) ArchiveContest(ctx context.Context, id int64) (ContestRecord, error) {
	row, err := r.queries.ArchiveContest(ctx, id)
	return contestFromDB(row), mapDBErr(err)
}

func (r *txRepository) ReplaceContestProblems(ctx context.Context, contestID int64, problems []ContestProblem) error {
	return replaceContestProblems(ctx, r.queries, contestID, problems)
}

func (r *txRepository) ListContestProblems(ctx context.Context, contestID int64) ([]ContestProblem, error) {
	return listContestProblems(ctx, r.queries, contestID)
}

func (r *txRepository) CreateRegistration(ctx context.Context, input ContestRegistration) (ContestRegistration, error) {
	return createRegistration(ctx, r.queries, input)
}

func (r *txRepository) GetRegistration(ctx context.Context, contestID, userID int64) (ContestRegistration, error) {
	row, err := r.queries.GetContestRegistration(ctx, db.GetContestRegistrationParams{ContestID: contestID, UserID: userID})
	return registrationFromDB(row), mapDBErr(err)
}

func (r *txRepository) ListRegistrations(ctx context.Context, contestID int64) ([]ContestRegistration, error) {
	return listRegistrations(ctx, r.queries, contestID)
}

func (r *txRepository) ListScoreboardRegistrations(ctx context.Context, contestID int64, query scoreboardRowsQuery) ([]ContestRegistration, error) {
	return listScoreboardRegistrations(ctx, r.queries, contestID, query)
}

func (r *txRepository) ListProblemResults(ctx context.Context, contestID int64) ([]ContestProblemResult, error) {
	return listProblemResults(ctx, r.queries, contestID)
}

func (r *txRepository) ListProblemResultsForUsers(ctx context.Context, contestID int64, userIDs []int64) ([]ContestProblemResult, error) {
	return listProblemResultsForUsers(ctx, r.queries, contestID, userIDs)
}

func (r *txRepository) ListTerminalSubmissions(ctx context.Context, contestID int64) ([]ContestSubmissionResult, error) {
	return listTerminalSubmissions(ctx, r.queries, contestID)
}

func (r *txRepository) UpsertProblemResult(ctx context.Context, result ContestProblemResult) (ContestProblemResult, error) {
	return upsertProblemResult(ctx, r.queries, result)
}

func (r *txRepository) ListScoreSnapshotCandidates(ctx context.Context, now time.Time, limit int32) ([]ScoreSnapshotCandidate, error) {
	return listScoreSnapshotCandidates(ctx, r.queries, now, limit)
}

func (r *txRepository) CreateScoreSnapshot(ctx context.Context, snapshot ScoreboardSnapshot) (ScoreboardSnapshot, error) {
	return createScoreSnapshot(ctx, r.queries, snapshot)
}

func (r *txRepository) LatestScoreSnapshot(ctx context.Context, contestID int64, view ScoreboardView) (ScoreboardSnapshot, error) {
	return latestScoreSnapshot(ctx, r.queries, contestID, view)
}

func (r *txRepository) ScoreSnapshotPage(ctx context.Context, contestID int64, view ScoreboardView, afterOrdinal, limit int32) (scoreSnapshotPageResult, error) {
	return loadScoreSnapshotPage(ctx, r.queries, contestID, view, afterOrdinal, limit)
}

func createContest(ctx context.Context, q *db.Queries, input ContestRecord) (ContestRecord, error) {
	row, err := q.CreateContest(ctx, db.CreateContestParams{
		OwnerUserID:    input.OwnerUserID,
		Title:          input.Title,
		Description:    textPtr(input.Description),
		Visibility:     input.Visibility,
		Status:         input.Status,
		StartAt:        timestamptz(input.StartAt),
		EndAt:          timestamptz(input.EndAt),
		FreezeAt:       timestamptz(input.FreezeAt),
		InviteCodeHash: textValuePtr(input.InviteCodeHash),
	})
	return contestFromDB(row), mapDBErr(err)
}

func listContests(ctx context.Context, q *db.Queries, filter ListContestFilter) ([]ContestRecord, int64, error) {
	rows, err := q.ListContests(ctx, db.ListContestsParams{
		Status:              textValuePtr(filter.Status),
		Visibility:          textValuePtr(filter.Visibility),
		Keyword:             textValuePtr(filter.Keyword),
		IncludePrivate:      filter.IncludePrivate,
		VisibleToUserID:     int8ValuePtr(filter.VisibleToUserID),
		VisibleToContestIds: filter.VisibleToContestIDs,
		Offset:              filter.Offset,
		Limit:               filter.Limit,
	})
	if err != nil {
		return nil, 0, mapDBErr(err)
	}
	total, err := q.CountContests(ctx, db.CountContestsParams{
		Status:              textValuePtr(filter.Status),
		Visibility:          textValuePtr(filter.Visibility),
		Keyword:             textValuePtr(filter.Keyword),
		IncludePrivate:      filter.IncludePrivate,
		VisibleToUserID:     int8ValuePtr(filter.VisibleToUserID),
		VisibleToContestIds: filter.VisibleToContestIDs,
	})
	if err != nil {
		return nil, 0, mapDBErr(err)
	}
	out := make([]ContestRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, contestFromDB(row))
	}
	return out, total, nil
}

func listContestsByCursor(ctx context.Context, q *db.Queries, filter ListContestFilter) ([]ContestRecord, error) {
	cursor := filter.Cursor
	if cursor == nil {
		cursor = &ContestCursor{StartAt: time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC), ID: 1<<63 - 1}
	}
	rows, err := q.ListContestsByCursor(ctx, db.ListContestsByCursorParams{
		Status:              textValuePtr(filter.Status),
		Visibility:          textValuePtr(filter.Visibility),
		Keyword:             textValuePtr(filter.Keyword),
		IncludePrivate:      filter.IncludePrivate,
		VisibleToUserID:     int8ValuePtr(filter.VisibleToUserID),
		VisibleToContestIDs: filter.VisibleToContestIDs,
		BeforeStartAt:       timestamptz(cursor.StartAt.UTC()),
		BeforeID:            cursor.ID,
		Limit:               filter.Limit,
	})
	if err != nil {
		return nil, mapDBErr(err)
	}
	out := make([]ContestRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, contestFromDB(row))
	}
	return out, nil
}

func updateContest(ctx context.Context, q *db.Queries, id int64, input ContestUpdateInput) (ContestRecord, error) {
	inviteHash := pgtype.Text{}
	if input.InviteCode != nil {
		inviteHash = textValuePtr(hashInviteCode(*input.InviteCode))
	}
	row, err := q.UpdateContest(ctx, db.UpdateContestParams{
		ID:             id,
		Title:          textPtr(input.Title),
		Description:    textPtr(input.Description),
		Visibility:     textPtr(input.Visibility),
		Status:         textPtr(input.Status),
		StartAt:        timePtr(input.StartAt),
		EndAt:          timePtr(input.EndAt),
		FreezeAt:       timePtr(input.FreezeAt),
		InviteCodeHash: inviteHash,
	})
	return contestFromDB(row), mapDBErr(err)
}

func replaceContestProblems(ctx context.Context, q *db.Queries, contestID int64, problems []ContestProblem) error {
	if err := q.DeleteContestProblems(ctx, contestID); err != nil {
		return mapDBErr(err)
	}
	for _, problem := range problems {
		_, err := q.AddContestProblem(ctx, db.AddContestProblemParams{
			ContestID: contestID,
			ProblemID: problem.ProblemID,
			Alias:     problem.Alias,
			SortOrder: problem.SortOrder,
		})
		if err != nil {
			return mapDBErr(err)
		}
	}
	return nil
}

func listContestProblems(ctx context.Context, q *db.Queries, contestID int64) ([]ContestProblem, error) {
	rows, err := q.ListContestProblems(ctx, contestID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	out := make([]ContestProblem, 0, len(rows))
	for _, row := range rows {
		out = append(out, ContestProblem{ContestID: row.ContestID, ProblemID: row.ProblemID, Alias: row.Alias, SortOrder: row.SortOrder, Title: row.Title})
	}
	return out, nil
}

func createRegistration(ctx context.Context, q *db.Queries, input ContestRegistration) (ContestRegistration, error) {
	row, err := q.CreateContestRegistration(ctx, db.CreateContestRegistrationParams{
		ContestID:   input.ContestID,
		UserID:      input.UserID,
		DisplayName: input.DisplayName,
		Email:       input.Email,
	})
	return registrationFromDB(row), mapDBErr(err)
}

func listRegistrations(ctx context.Context, q *db.Queries, contestID int64) ([]ContestRegistration, error) {
	const pageSize int32 = 1000
	out := make([]ContestRegistration, 0, pageSize)
	for offset := int32(0); ; offset += pageSize {
		rows, err := q.ListContestRegistrations(ctx, db.ListContestRegistrationsParams{
			ContestID: contestID,
			Limit:     pageSize,
			Offset:    offset,
		})
		if err != nil {
			return nil, mapDBErr(err)
		}
		for _, row := range rows {
			out = append(out, registrationFromDB(row))
		}
		if len(rows) < int(pageSize) {
			break
		}
	}
	return out, nil
}

func listScoreboardRegistrations(ctx context.Context, q *db.Queries, contestID int64, query scoreboardRowsQuery) ([]ContestRegistration, error) {
	rows, err := q.ListScoreboardRegistrations(ctx, db.ListScoreboardRegistrationsParams{
		ContestID:           contestID,
		HasCursor:           query.HasCursor,
		AfterAcceptedCount:  query.AfterAcceptedCount,
		AfterPenaltyMinutes: query.AfterPenalty,
		AfterDisplayName:    query.AfterDisplayName,
		AfterUserID:         query.AfterUserID,
		Limit:               query.PageSize,
	})
	if err != nil {
		return nil, mapDBErr(err)
	}
	out := make([]ContestRegistration, 0, len(rows))
	for _, row := range rows {
		out = append(out, registrationFromDB(row))
	}
	return out, nil
}

func listProblemResults(ctx context.Context, q *db.Queries, contestID int64) ([]ContestProblemResult, error) {
	rows, err := q.ListContestProblemResults(ctx, contestID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	out := make([]ContestProblemResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, resultFromDB(row))
	}
	return out, nil
}

func listProblemResultsForUsers(ctx context.Context, q *db.Queries, contestID int64, userIDs []int64) ([]ContestProblemResult, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := q.ListContestProblemResultsForUsers(ctx, db.ListContestProblemResultsForUsersParams{ContestID: contestID, UserIds: userIDs})
	if err != nil {
		return nil, mapDBErr(err)
	}
	out := make([]ContestProblemResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, resultFromDB(row))
	}
	return out, nil
}

func listTerminalSubmissions(ctx context.Context, q *db.Queries, contestID int64) ([]ContestSubmissionResult, error) {
	rows, err := q.ListContestTerminalSubmissions(ctx, contestID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	out := make([]ContestSubmissionResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, ContestSubmissionResult{
			ID:            row.ID,
			ContestID:     row.ContestID.Int64,
			UserID:        row.UserID,
			ProblemID:     row.ProblemID,
			Status:        row.Status,
			SubmittedAt:   row.SubmittedAt.Time,
			JudgedAt:      row.JudgedAt.Time,
			FirstJudgedAt: timeFromDB(row.FirstJudgedAt),
		})
	}
	return out, nil
}

func upsertProblemResult(ctx context.Context, q *db.Queries, result ContestProblemResult) (ContestProblemResult, error) {
	row, err := q.UpsertContestProblemResult(ctx, db.UpsertContestProblemResultParams{
		ContestID:        result.ContestID,
		UserID:           result.UserID,
		ProblemID:        result.ProblemID,
		Status:           result.Status,
		Attempts:         result.Attempts,
		AcceptedAt:       timePtr(result.AcceptedAt),
		PenaltyMinutes:   result.PenaltyMinutes,
		LastSubmissionID: int8Ptr(result.LastSubmissionID),
	})
	return resultFromDB(row), mapDBErr(err)
}

func listScoreSnapshotCandidates(ctx context.Context, q *db.Queries, now time.Time, limit int32) ([]ScoreSnapshotCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := q.ListContestScoreSnapshotCandidates(ctx, db.ListContestScoreSnapshotCandidatesParams{
		FreezeAt: timestamptz(now),
		Limit:    limit,
	})
	if err != nil {
		return nil, mapDBErr(err)
	}
	out := make([]ScoreSnapshotCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, ScoreSnapshotCandidate{
			Contest: contestFromSnapshotCandidateDB(row),
			View:    ScoreboardView(row.SnapshotKind),
		})
	}
	return out, nil
}

func createScoreSnapshot(ctx context.Context, q *db.Queries, snapshot ScoreboardSnapshot) (ScoreboardSnapshot, error) {
	problems := snapshot.Problems
	rows := snapshot.Rows
	problemPayload, err := json.Marshal(problems)
	if err != nil {
		return ScoreboardSnapshot{}, err
	}
	row, err := q.CreateContestScoreSnapshot(ctx, db.CreateContestScoreSnapshotParams{
		ContestID:      snapshot.ContestID,
		Kind:           string(snapshot.View),
		Problems:       problemPayload,
		SourceRevision: snapshot.SourceRevision,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, lookupErr := q.GetContestScoreSnapshotByKey(ctx, db.GetContestScoreSnapshotByKeyParams{
			ContestID: snapshot.ContestID, Kind: string(snapshot.View), SourceRevision: snapshot.SourceRevision,
		})
		if lookupErr != nil {
			return ScoreboardSnapshot{}, mapDBErr(lookupErr)
		}
		result, loadErr := loadScoreSnapshot(ctx, q, existing)
		if loadErr != nil {
			return ScoreboardSnapshot{}, loadErr
		}
		result.Created = false
		return result, nil
	}
	if err != nil {
		return ScoreboardSnapshot{}, mapDBErr(err)
	}
	snapshot.ID = row.ID
	snapshot.ContestID = row.ContestID
	snapshot.View = ScoreboardView(row.Kind)
	snapshot.Problems = problems
	snapshot.Rows = rows
	snapshot.SourceRevision = row.SourceRevision
	snapshot.GeneratedAt = row.GeneratedAt.Time
	snapshot.Created = true
	for i, boardRow := range rows {
		cells, err := json.Marshal(boardRow.Cells)
		if err != nil {
			return ScoreboardSnapshot{}, err
		}
		if _, err := q.CreateContestScoreSnapshotRow(ctx, db.CreateContestScoreSnapshotRowParams{
			SnapshotID:     row.ID,
			Ordinal:        int32(i + 1),
			Rank:           boardRow.Rank,
			UserID:         boardRow.UserID,
			DisplayName:    boardRow.DisplayName,
			AcceptedCount:  boardRow.AcceptedCount,
			PenaltyMinutes: boardRow.PenaltyMinutes,
			Cells:          cells,
		}); err != nil {
			return ScoreboardSnapshot{}, err
		}
	}
	return snapshot, nil
}

func latestScoreSnapshot(ctx context.Context, q *db.Queries, contestID int64, view ScoreboardView) (ScoreboardSnapshot, error) {
	row, err := q.GetLatestContestScoreSnapshot(ctx, db.GetLatestContestScoreSnapshotParams{ContestID: contestID, Kind: string(view)})
	if err != nil {
		return ScoreboardSnapshot{}, mapDBErr(err)
	}
	return loadScoreSnapshot(ctx, q, row)
}

func loadScoreSnapshot(ctx context.Context, q *db.Queries, row db.ContestScoreSnapshot) (ScoreboardSnapshot, error) {
	var problems []ContestProblem
	if err := json.Unmarshal(row.Problems, &problems); err != nil {
		return ScoreboardSnapshot{}, err
	}
	rows, err := q.ListContestScoreSnapshotRows(ctx, db.ListContestScoreSnapshotRowsParams{SnapshotID: row.ID, Limit: 1 << 30})
	if err != nil {
		return ScoreboardSnapshot{}, err
	}
	boardRows, err := snapshotRowsFromDB(rows)
	if err != nil {
		return ScoreboardSnapshot{}, err
	}
	board := ScoreboardResponse{ContestID: row.ContestID, View: ScoreboardView(row.Kind), GeneratedAt: row.GeneratedAt.Time, Problems: problems, Rows: boardRows}
	return ScoreboardSnapshot{ID: row.ID, ContestID: row.ContestID, View: ScoreboardView(row.Kind), Board: board, Problems: problems, Rows: boardRows, SourceRevision: row.SourceRevision, GeneratedAt: row.GeneratedAt.Time}, nil
}

func loadScoreSnapshotPage(ctx context.Context, q *db.Queries, contestID int64, view ScoreboardView, afterOrdinal, limit int32) (scoreSnapshotPageResult, error) {
	header, err := q.GetLatestContestScoreSnapshot(ctx, db.GetLatestContestScoreSnapshotParams{ContestID: contestID, Kind: string(view)})
	if err != nil {
		return scoreSnapshotPageResult{}, mapDBErr(err)
	}
	var problems []ContestProblem
	if err := json.Unmarshal(header.Problems, &problems); err != nil {
		return scoreSnapshotPageResult{}, err
	}
	rows, err := q.ListContestScoreSnapshotRows(ctx, db.ListContestScoreSnapshotRowsParams{SnapshotID: header.ID, AfterOrdinal: afterOrdinal, Limit: limit})
	if err != nil {
		return scoreSnapshotPageResult{}, mapDBErr(err)
	}
	boardRows, err := snapshotRowsFromDB(rows)
	if err != nil {
		return scoreSnapshotPageResult{}, err
	}
	return scoreSnapshotPageResult{
		Snapshot: ScoreboardSnapshot{ID: header.ID, ContestID: header.ContestID, View: ScoreboardView(header.Kind), Problems: problems, Rows: boardRows, SourceRevision: header.SourceRevision, GeneratedAt: header.GeneratedAt.Time},
		Rows:     boardRows,
		HasMore:  len(boardRows) == int(limit),
	}, nil
}

func snapshotRowsFromDB(rows []db.ContestScoreSnapshotRow) ([]ScoreboardRow, error) {
	out := make([]ScoreboardRow, 0, len(rows))
	for _, row := range rows {
		var cells []ScoreboardCell
		if err := json.Unmarshal(row.Cells, &cells); err != nil {
			return nil, err
		}
		out = append(out, ScoreboardRow{Rank: row.Rank, UserID: row.UserID, DisplayName: row.DisplayName, AcceptedCount: row.AcceptedCount, PenaltyMinutes: row.PenaltyMinutes, Cells: cells})
	}
	return out, nil
}

func contestFromSnapshotCandidateDB(row db.ListContestScoreSnapshotCandidatesRow) ContestRecord {
	return ContestRecord{
		ID:             row.ID,
		OwnerUserID:    row.OwnerUserID,
		Title:          row.Title,
		Description:    textFromDB(row.Description),
		Visibility:     row.Visibility,
		Status:         row.Status,
		StartAt:        row.StartAt.Time,
		EndAt:          row.EndAt.Time,
		FreezeAt:       row.FreezeAt.Time,
		InviteCodeHash: row.InviteCodeHash.String,
		ScoreRevision:  row.ScoreRevision,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

func contestFromDB(row db.Contest) ContestRecord {
	return ContestRecord{
		ID:             row.ID,
		OwnerUserID:    row.OwnerUserID,
		Title:          row.Title,
		Description:    textFromDB(row.Description),
		Visibility:     row.Visibility,
		Status:         row.Status,
		StartAt:        row.StartAt.Time,
		EndAt:          row.EndAt.Time,
		FreezeAt:       row.FreezeAt.Time,
		InviteCodeHash: row.InviteCodeHash.String,
		ScoreRevision:  row.ScoreRevision,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

func registrationFromDB(row db.ContestRegistration) ContestRegistration {
	return ContestRegistration{
		ID:             row.ID,
		ContestID:      row.ContestID,
		UserID:         row.UserID,
		DisplayName:    row.DisplayName,
		Email:          row.Email,
		Status:         row.Status,
		RegisteredAt:   row.RegisteredAt.Time,
		AcceptedCount:  row.AcceptedCount,
		PenaltyMinutes: row.PenaltyMinutes,
	}
}

func resultFromDB(row db.ContestProblemResult) ContestProblemResult {
	return ContestProblemResult{
		ContestID:        row.ContestID,
		UserID:           row.UserID,
		ProblemID:        row.ProblemID,
		Status:           row.Status,
		Attempts:         row.Attempts,
		AcceptedAt:       timeFromDB(row.AcceptedAt),
		PenaltyMinutes:   row.PenaltyMinutes,
		LastSubmissionID: int8FromDB(row.LastSubmissionID),
		UpdatedAt:        row.UpdatedAt.Time,
	}
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("contest.not_found", "contest not found")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if pgErr.ConstraintName == "contest_registrations_contest_user_uidx" {
				return apperror.Conflict("contest.registration_exists", "contest registration already exists")
			}
			if pgErr.ConstraintName == "contest_problems_alias_uidx" {
				return apperror.Conflict("contest.problem_alias_conflict", "contest problem alias must be unique")
			}
			return apperror.Conflict("contest.conflict", "contest conflict")
		case "23503":
			return apperror.NotFound("contest.reference_not_found", "contest reference not found")
		}
	}
	return err
}

func textPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func textValuePtr(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func textFromDB(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timePtr(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return timestamptz(*value)
}

func timeFromDB(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func int8Ptr(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func int8ValuePtr(value int64) pgtype.Int8 {
	if value <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: value, Valid: true}
}

func int8FromDB(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}
