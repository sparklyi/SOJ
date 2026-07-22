package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

type ListUsersByCursorParams struct {
	Role            pgtype.Text        `db:"role" json:"role"`
	Status          pgtype.Text        `db:"status" json:"status"`
	Keyword         pgtype.Text        `db:"keyword" json:"keyword"`
	BeforeCreatedAt pgtype.Timestamptz `db:"before_created_at" json:"before_created_at"`
	BeforeID        int64              `db:"before_id" json:"before_id"`
	Limit           int32              `db:"limit" json:"limit"`
}

type ListProblemsByCursorParams struct {
	Difficulty      pgtype.Text        `db:"difficulty" json:"difficulty"`
	Status          pgtype.Text        `db:"status" json:"status"`
	Visibility      pgtype.Text        `db:"visibility" json:"visibility"`
	Tag             pgtype.Text        `db:"tag" json:"tag"`
	Keyword         pgtype.Text        `db:"keyword" json:"keyword"`
	OwnerUserID     int64              `db:"owner_user_id" json:"owner_user_id"`
	IncludeAll      bool               `db:"include_all" json:"include_all"`
	ViewerUserID    int64              `db:"viewer_user_id" json:"viewer_user_id"`
	BeforeCreatedAt pgtype.Timestamptz `db:"before_created_at" json:"before_created_at"`
	BeforeID        int64              `db:"before_id" json:"before_id"`
	Limit           int32              `db:"limit" json:"limit"`
}

type ListProblemsByCursorRow struct {
	ID                    int64              `db:"id" json:"id"`
	OwnerUserID           int64              `db:"owner_user_id" json:"owner_user_id"`
	Title                 string             `db:"title" json:"title"`
	Slug                  string             `db:"slug" json:"slug"`
	Difficulty            string             `db:"difficulty" json:"difficulty"`
	Visibility            string             `db:"visibility" json:"visibility"`
	Status                string             `db:"status" json:"status"`
	TimeLimitMs           int32              `db:"time_limit_ms" json:"time_limit_ms"`
	MemoryLimitKb         int32              `db:"memory_limit_kb" json:"memory_limit_kb"`
	CreatedAt             pgtype.Timestamptz `db:"created_at" json:"created_at"`
	UpdatedAt             pgtype.Timestamptz `db:"updated_at" json:"updated_at"`
	PublishedAt           pgtype.Timestamptz `db:"published_at" json:"published_at"`
	CurrentStatementID    int64              `db:"current_statement_id" json:"current_statement_id"`
	CurrentTestcaseSetID  int64              `db:"current_testcase_set_id" json:"current_testcase_set_id"`
	CurrentTestcaseStatus string             `db:"current_testcase_status" json:"current_testcase_status"`
}

type ListContestsByCursorParams struct {
	Status          pgtype.Text        `db:"status" json:"status"`
	Visibility      pgtype.Text        `db:"visibility" json:"visibility"`
	Keyword         pgtype.Text        `db:"keyword" json:"keyword"`
	IncludePrivate  bool               `db:"include_private" json:"include_private"`
	VisibleToUserID pgtype.Int8        `db:"visible_to_user_id" json:"visible_to_user_id"`
	BeforeStartAt   pgtype.Timestamptz `db:"before_start_at" json:"before_start_at"`
	BeforeID        int64              `db:"before_id" json:"before_id"`
	Limit           int32              `db:"limit" json:"limit"`
}

type ListSubmissionsByCursorParams struct {
	UserID            pgtype.Int8        `db:"user_id" json:"user_id"`
	ProblemID         pgtype.Int8        `db:"problem_id" json:"problem_id"`
	ContestID         pgtype.Int8        `db:"contest_id" json:"contest_id"`
	Status            pgtype.Text        `db:"status" json:"status"`
	BeforeSubmittedAt pgtype.Timestamptz `db:"before_submitted_at" json:"before_submitted_at"`
	BeforeID          int64              `db:"before_id" json:"before_id"`
	Limit             int32              `db:"limit" json:"limit"`
}

// Cursor queries omit inactive filters entirely so PostgreSQL can plan against
// concrete predicates instead of nullable OR guards.
type cursorQueryBuilder struct {
	clauses []string
	args    []any
}

func (b *cursorQueryBuilder) bind(value any, cast string) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d%s", len(b.args), cast)
}

func (b *cursorQueryBuilder) add(clause string) {
	b.clauses = append(b.clauses, clause)
}

func (b *cursorQueryBuilder) finish(selectSQL, orderBy string, limit int32) (string, []any) {
	limitArg := b.bind(limit, "")
	query := selectSQL + "\nWHERE " + strings.Join(b.clauses, "\n  AND ") +
		"\nORDER BY " + orderBy + "\nLIMIT " + limitArg
	return query, b.args
}

func buildListUsersByCursorQuery(arg ListUsersByCursorParams) (string, []any) {
	builder := cursorQueryBuilder{}
	if arg.Role.Valid {
		builder.add("role = " + builder.bind(arg.Role.String, "::text"))
	}
	if arg.Status.Valid {
		builder.add("status = " + builder.bind(arg.Status.String, "::text"))
	}
	if arg.Keyword.Valid {
		keywordArg := builder.bind(arg.Keyword.String, "::text")
		builder.add("(email ILIKE '%' || " + keywordArg + " || '%' OR username ILIKE '%' || " + keywordArg + " || '%')")
	}
	beforeArg := builder.bind(arg.BeforeCreatedAt.Time, "::timestamptz")
	beforeIDArg := builder.bind(arg.BeforeID, "::bigint")
	builder.add("(created_at, id) < (" + beforeArg + ", " + beforeIDArg + ")")

	return builder.finish(
		"SELECT id, email, password_hash, username, avatar_url, bio, role, status, created_at, updated_at\nFROM users",
		"created_at DESC, id DESC",
		arg.Limit,
	)
}

func buildListProblemsByCursorQuery(arg ListProblemsByCursorParams) (string, []any) {
	builder := cursorQueryBuilder{}
	if arg.Difficulty.Valid {
		builder.add("p.difficulty = " + builder.bind(arg.Difficulty.String, "::text"))
	}
	if arg.Status.Valid {
		builder.add("p.status = " + builder.bind(arg.Status.String, "::text"))
	}
	if arg.Visibility.Valid {
		builder.add("p.visibility = " + builder.bind(arg.Visibility.String, "::text"))
	}
	if arg.Tag.Valid {
		tagArg := builder.bind(arg.Tag.String, "::text")
		builder.add("EXISTS (SELECT 1 FROM problem_tag_links ptl JOIN problem_tags pt ON pt.id = ptl.tag_id WHERE ptl.problem_id = p.id AND pt.slug = " + tagArg + ")")
	}
	if arg.Keyword.Valid {
		keywordArg := builder.bind(arg.Keyword.String, "::text")
		builder.add("(p.title ILIKE '%' || " + keywordArg + " || '%' OR p.slug ILIKE '%' || " + keywordArg + " || '%')")
	}
	if arg.OwnerUserID > 0 {
		builder.add("p.owner_user_id = " + builder.bind(arg.OwnerUserID, "::bigint"))
	}
	if !arg.IncludeAll {
		switch {
		case arg.OwnerUserID > 0 && arg.OwnerUserID == arg.ViewerUserID:
			// The owner predicate already limits the result to rows visible to the viewer.
		case arg.ViewerUserID > 0:
			viewerArg := builder.bind(arg.ViewerUserID, "::bigint")
			builder.add("((p.status = 'published' AND p.visibility = 'public') OR p.owner_user_id = " + viewerArg + ")")
		default:
			builder.add("p.status = 'published'")
			builder.add("p.visibility = 'public'")
		}
	}
	beforeArg := builder.bind(arg.BeforeCreatedAt.Time, "::timestamptz")
	beforeIDArg := builder.bind(arg.BeforeID, "::bigint")
	builder.add("(p.created_at, p.id) < (" + beforeArg + ", " + beforeIDArg + ")")

	return builder.finish(
		"SELECT\n    p.id, p.owner_user_id, p.title, p.slug, p.difficulty, p.visibility, p.status, p.time_limit_ms, p.memory_limit_kb, p.created_at, p.updated_at, p.published_at,\n    coalesce(ps.id, 0)::bigint AS current_statement_id,\n    coalesce(ts.id, 0)::bigint AS current_testcase_set_id,\n    coalesce(ts.status, '')::text AS current_testcase_status\nFROM problems p\nLEFT JOIN problem_statements ps ON ps.problem_id = p.id AND ps.is_current = true\nLEFT JOIN testcase_sets ts ON ts.problem_id = p.id AND ts.is_current = true",
		"p.created_at DESC, p.id DESC",
		arg.Limit,
	)
}

func buildListContestsByCursorQuery(arg ListContestsByCursorParams) (string, []any) {
	builder := cursorQueryBuilder{}
	if arg.Status.Valid {
		builder.add("c.status = " + builder.bind(arg.Status.String, "::text"))
	}
	if arg.Visibility.Valid {
		builder.add("c.visibility = " + builder.bind(arg.Visibility.String, "::text"))
	}
	if arg.Keyword.Valid {
		keywordArg := builder.bind(arg.Keyword.String, "::text")
		builder.add("c.title ILIKE '%' || " + keywordArg + " || '%'")
	}
	if !arg.IncludePrivate {
		if arg.VisibleToUserID.Valid {
			viewerArg := builder.bind(arg.VisibleToUserID.Int64, "::bigint")
			builder.add("(c.visibility = 'public' OR c.owner_user_id = " + viewerArg + " OR EXISTS (SELECT 1 FROM contest_registrations cr WHERE cr.contest_id = c.id AND cr.user_id = " + viewerArg + " AND cr.status = 'active'))")
		} else {
			builder.add("c.visibility = 'public'")
		}
	}
	beforeArg := builder.bind(arg.BeforeStartAt.Time, "::timestamptz")
	beforeIDArg := builder.bind(arg.BeforeID, "::bigint")
	builder.add("(c.start_at, c.id) < (" + beforeArg + ", " + beforeIDArg + ")")

	return builder.finish(
		"SELECT c.id, c.owner_user_id, c.title, c.description, c.visibility, c.status, c.start_at, c.end_at, c.freeze_at, c.invite_code_hash, c.created_at, c.updated_at\nFROM contests c",
		"c.start_at DESC, c.id DESC",
		arg.Limit,
	)
}

func buildListSubmissionsByCursorQuery(arg ListSubmissionsByCursorParams) (string, []any) {
	builder := cursorQueryBuilder{}
	if arg.UserID.Valid {
		builder.add("user_id = " + builder.bind(arg.UserID.Int64, "::bigint"))
	}
	if arg.ProblemID.Valid {
		builder.add("problem_id = " + builder.bind(arg.ProblemID.Int64, "::bigint"))
	}
	if arg.ContestID.Valid {
		builder.add("contest_id = " + builder.bind(arg.ContestID.Int64, "::bigint"))
	}
	if arg.Status.Valid {
		builder.add("status = " + builder.bind(arg.Status.String, "::text"))
	}
	beforeArg := builder.bind(arg.BeforeSubmittedAt.Time, "::timestamptz")
	beforeIDArg := builder.bind(arg.BeforeID, "::bigint")
	builder.add("(submitted_at, id) < (" + beforeArg + ", " + beforeIDArg + ")")

	return builder.finish(
		"SELECT id, user_id, problem_id, contest_id, language_id, testcase_set_id, status, source_artifact_id, time_ms, memory_kb, score, error_message, submitted_at, judged_at, updated_at\nFROM submissions",
		"submitted_at DESC, id DESC",
		arg.Limit,
	)
}

func (q *Queries) ListUsersByCursor(ctx context.Context, arg ListUsersByCursorParams) ([]User, error) {
	query, args := buildListUsersByCursorQuery(arg)
	rows, err := q.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []User
	for rows.Next() {
		var item User
		if err := rows.Scan(
			&item.ID,
			&item.Email,
			&item.PasswordHash,
			&item.Username,
			&item.AvatarUrl,
			&item.Bio,
			&item.Role,
			&item.Status,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *Queries) ListProblemsByCursor(ctx context.Context, arg ListProblemsByCursorParams) ([]ListProblemsByCursorRow, error) {
	query, args := buildListProblemsByCursorQuery(arg)
	rows, err := q.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ListProblemsByCursorRow
	for rows.Next() {
		var item ListProblemsByCursorRow
		if err := rows.Scan(
			&item.ID,
			&item.OwnerUserID,
			&item.Title,
			&item.Slug,
			&item.Difficulty,
			&item.Visibility,
			&item.Status,
			&item.TimeLimitMs,
			&item.MemoryLimitKb,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.PublishedAt,
			&item.CurrentStatementID,
			&item.CurrentTestcaseSetID,
			&item.CurrentTestcaseStatus,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *Queries) ListContestsByCursor(ctx context.Context, arg ListContestsByCursorParams) ([]Contest, error) {
	query, args := buildListContestsByCursorQuery(arg)
	rows, err := q.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Contest
	for rows.Next() {
		var item Contest
		if err := rows.Scan(
			&item.ID,
			&item.OwnerUserID,
			&item.Title,
			&item.Description,
			&item.Visibility,
			&item.Status,
			&item.StartAt,
			&item.EndAt,
			&item.FreezeAt,
			&item.InviteCodeHash,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *Queries) ListSubmissionsByCursor(ctx context.Context, arg ListSubmissionsByCursorParams) ([]Submission, error) {
	query, args := buildListSubmissionsByCursorQuery(arg)
	rows, err := q.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Submission
	for rows.Next() {
		var item Submission
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ProblemID,
			&item.ContestID,
			&item.LanguageID,
			&item.TestcaseSetID,
			&item.Status,
			&item.SourceArtifactID,
			&item.TimeMs,
			&item.MemoryKb,
			&item.Score,
			&item.ErrorMessage,
			&item.SubmittedAt,
			&item.JudgedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
