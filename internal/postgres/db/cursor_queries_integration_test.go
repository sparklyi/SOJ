package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCursorQueriesExecuteAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SOJ_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SOJ_TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fixture := newCursorQueryIntegrationFixture()
	seedCursorQueryIntegrationData(t, ctx, tx, fixture)
	queries := New(tx)
	before := pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}

	users, err := queries.ListUsersByCursor(ctx, ListUsersByCursorParams{
		Keyword:         pgtype.Text{String: fixture.token, Valid: true},
		BeforeCreatedAt: before,
		BeforeID:        1<<63 - 1,
		Limit:           10,
	})
	if err != nil || len(users) != 2 {
		t.Fatalf("list users: len=%d err=%v", len(users), err)
	}

	problems, err := queries.ListProblemsByCursor(ctx, ListProblemsByCursorParams{
		Keyword:         pgtype.Text{String: fixture.token, Valid: true},
		BeforeCreatedAt: before,
		BeforeID:        1<<63 - 1,
		Limit:           10,
	})
	if err != nil || len(problems) != 1 || problems[0].ID != fixture.publicProblemID {
		t.Fatalf("list public problems: rows=%v err=%v", problems, err)
	}
	viewerProblems, err := queries.ListProblemsByCursor(ctx, ListProblemsByCursorParams{
		Keyword:         pgtype.Text{String: fixture.token, Valid: true},
		ViewerUserID:    fixture.userTwoID,
		BeforeCreatedAt: before,
		BeforeID:        1<<63 - 1,
		Limit:           10,
	})
	if err != nil || len(viewerProblems) != 2 {
		t.Fatalf("list viewer problems: len=%d err=%v", len(viewerProblems), err)
	}

	contests, err := queries.ListContestsByCursor(ctx, ListContestsByCursorParams{
		Keyword:         pgtype.Text{String: fixture.token, Valid: true},
		VisibleToUserID: pgtype.Int8{Int64: fixture.userOneID, Valid: true},
		BeforeStartAt:   before,
		BeforeID:        1<<63 - 1,
		Limit:           10,
	})
	if err != nil || len(contests) != 2 {
		t.Fatalf("list visible contests: len=%d err=%v", len(contests), err)
	}

	submissions, err := queries.ListSubmissionsByCursor(ctx, ListSubmissionsByCursorParams{
		UserID:            pgtype.Int8{Int64: fixture.userOneID, Valid: true},
		Status:            pgtype.Text{String: "accepted", Valid: true},
		BeforeSubmittedAt: before,
		BeforeID:          1<<63 - 1,
		Limit:             10,
	})
	if err != nil || len(submissions) != 1 || submissions[0].ID != fixture.submissionID {
		t.Fatalf("list submissions: rows=%v err=%v", submissions, err)
	}
}

type integrationExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type cursorQueryIntegrationFixture struct {
	token            string
	userOneID        int64
	userTwoID        int64
	publicProblemID  int64
	privateProblemID int64
	languageID       int64
	testcaseSetID    int64
	publicContestID  int64
	privateContestID int64
	submissionID     int64
}

func newCursorQueryIntegrationFixture() cursorQueryIntegrationFixture {
	base := time.Now().UnixNano() & ((1 << 62) - 1)
	return cursorQueryIntegrationFixture{
		token:            time.Now().UTC().Format("20060102T150405.000000000"),
		userOneID:        base,
		userTwoID:        base + 1,
		publicProblemID:  base + 2,
		privateProblemID: base + 3,
		languageID:       base + 4,
		testcaseSetID:    base + 5,
		publicContestID:  base + 6,
		privateContestID: base + 7,
		submissionID:     base + 8,
	}
}

func seedCursorQueryIntegrationData(t *testing.T, ctx context.Context, tx integrationExecer, fixture cursorQueryIntegrationFixture) {
	t.Helper()
	type statement struct {
		query string
		args  []any
	}
	statements := []statement{
		{
			query: `INSERT INTO users (id, email, password_hash, username, role, status) VALUES
				($1, $2, 'hash', $3, 'user', 'active'),
				($4, $5, 'hash', $6, 'user', 'active')`,
			args: []any{
				fixture.userOneID, fixture.token + "-one@example.test", fixture.token + "-one",
				fixture.userTwoID, fixture.token + "-two@example.test", fixture.token + "-two",
			},
		},
		{
			query: `INSERT INTO problems (id, owner_user_id, title, slug, difficulty, visibility, status, time_limit_ms, memory_limit_kb) VALUES
				($1, $2, $3, $4, 'easy', 'public', 'published', 1000, 262144),
				($5, $6, $7, $8, 'hard', 'private', 'draft', 1000, 262144)`,
			args: []any{
				fixture.publicProblemID, fixture.userOneID, fixture.token + " public", fixture.token + "-public",
				fixture.privateProblemID, fixture.userTwoID, fixture.token + " private", fixture.token + "-private",
			},
		},
		{
			query: `INSERT INTO languages (id, engine, engine_language_id, name, default_time_limit_ms, default_memory_limit_kb)
				VALUES ($1, 'local', $2, 'Go', 1000, 262144)`,
			args: []any{fixture.languageID, fixture.token + "-go"},
		},
		{
			query: `INSERT INTO testcase_sets (id, problem_id, version, storage_key, checksum_sha256, size_bytes, case_count, status, is_current, created_by)
				VALUES ($1, $2, 1, $3, repeat('a', 64), 1, 1, 'ready', true, $4)`,
			args: []any{fixture.testcaseSetID, fixture.publicProblemID, fixture.token + "/testcases.zip", fixture.userOneID},
		},
		{
			query: `INSERT INTO contests (id, owner_user_id, title, visibility, status, start_at, end_at, freeze_at) VALUES
				($1, $2, $3, 'public', 'published', now(), now() + interval '2 hours', now() + interval '1 hour'),
				($4, $5, $6, 'private', 'published', now(), now() + interval '2 hours', now() + interval '1 hour')`,
			args: []any{
				fixture.publicContestID, fixture.userOneID, fixture.token + " public contest",
				fixture.privateContestID, fixture.userTwoID, fixture.token + " private contest",
			},
		},
		{
			query: `INSERT INTO contest_registrations (contest_id, user_id, display_name, email, status)
				VALUES ($1, $2, $3, $4, 'active')`,
			args: []any{fixture.privateContestID, fixture.userOneID, fixture.token, fixture.token + "-one@example.test"},
		},
		{
			query: `INSERT INTO submissions (id, user_id, problem_id, language_id, testcase_set_id, status)
				VALUES ($1, $2, $3, $4, $5, 'accepted')`,
			args: []any{fixture.submissionID, fixture.userOneID, fixture.publicProblemID, fixture.languageID, fixture.testcaseSetID},
		},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed integration data: %v", err)
		}
	}
}
