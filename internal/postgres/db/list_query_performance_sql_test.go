package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestInitialSchemaAddsIndexableSearchAndOrderingPaths(t *testing.T) {
	schema := readInitialSchema(t)

	for _, want := range []string{
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"CREATE INDEX problems_public_catalog_recent_idx\n    ON problems (created_at DESC, id DESC)\n    WHERE status = 'published' AND visibility = 'public'",
		"CREATE INDEX problems_recent_idx\n    ON problems (created_at DESC, id DESC)",
		"CREATE INDEX problems_owner_recent_idx\n    ON problems (owner_user_id, created_at DESC, id DESC)",
		"CREATE INDEX submissions_user_submitted_id_idx\n    ON submissions (user_id, submitted_at DESC, id DESC)",
		"CREATE INDEX submissions_problem_submitted_id_idx\n    ON submissions (problem_id, submitted_at DESC, id DESC)",
		"CREATE INDEX submissions_contest_submitted_id_idx\n    ON submissions (contest_id, submitted_at DESC, id DESC)",
		"CREATE INDEX submissions_recent_idx\n    ON submissions (submitted_at DESC, id DESC)",
		"CREATE INDEX contests_public_recent_idx\n    ON contests (start_at DESC, id DESC)\n    WHERE visibility = 'public'",
		"CREATE INDEX contests_recent_idx\n    ON contests (start_at DESC, id DESC)",
		"CREATE INDEX users_recent_idx\n    ON users (created_at DESC, id DESC)",
		"CREATE INDEX users_email_trgm_idx\n    ON users USING gin (email gin_trgm_ops)",
		"CREATE INDEX users_username_trgm_idx\n    ON users USING gin (username gin_trgm_ops)",
		"CREATE INDEX problems_title_trgm_idx\n    ON problems USING gin (title gin_trgm_ops)",
		"CREATE INDEX problems_slug_trgm_idx\n    ON problems USING gin (slug gin_trgm_ops)",
		"CREATE INDEX contests_title_trgm_idx\n    ON contests USING gin (title gin_trgm_ops)",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("initial schema missing %q", want)
		}
	}
}

func TestOwnSubmissionCursorQueryUsesSeekPagination(t *testing.T) {
	for _, want := range []string{
		"WHERE user_id = $1",
		"(submitted_at, id) < (\n      $2::timestamptz,\n      $3::bigint\n  )",
		"ORDER BY submitted_at DESC, id DESC",
		"LIMIT $4",
	} {
		if !strings.Contains(listSubmissionsByUserBefore, want) {
			t.Fatalf("ListSubmissionsByUserBefore missing %q:\n%s", want, listSubmissionsByUserBefore)
		}
	}
	upperQuery := strings.ToUpper(listSubmissionsByUserBefore)
	for _, forbidden := range []string{" OR ", "OFFSET", "COUNT("} {
		if strings.Contains(upperQuery, forbidden) {
			t.Fatalf("ListSubmissionsByUserBefore must not contain %q:\n%s", forbidden, listSubmissionsByUserBefore)
		}
	}
}

func TestCursorQueryBuildersAppendOnlyActiveFilters(t *testing.T) {
	before := time.Date(2026, time.July, 22, 10, 30, 0, 0, time.UTC)

	t.Run("users", func(t *testing.T) {
		query, args := buildListUsersByCursorQuery(ListUsersByCursorParams{
			Role:            pgtype.Text{String: "admin", Valid: true},
			Status:          pgtype.Text{String: "active", Valid: true},
			Keyword:         pgtype.Text{String: "alice", Valid: true},
			BeforeCreatedAt: pgtype.Timestamptz{Time: before, Valid: true},
			BeforeID:        42,
			Limit:           21,
		})
		assertIndexableCursorQuery(t, query, []string{
			"role = $1::text",
			"status = $2::text",
			"email ILIKE '%' || $3::text || '%'",
			"username ILIKE '%' || $3::text || '%'",
			"(created_at, id) < ($4::timestamptz, $5::bigint)",
			"ORDER BY created_at DESC, id DESC",
			"LIMIT $6",
		})
		assertQueryArgs(t, args, []any{"admin", "active", "alice", before, int64(42), int32(21)})
	})

	t.Run("problems", func(t *testing.T) {
		query, args := buildListProblemsByCursorQuery(ListProblemsByCursorParams{
			Difficulty:      pgtype.Text{String: "hard", Valid: true},
			Status:          pgtype.Text{String: "draft", Valid: true},
			Visibility:      pgtype.Text{String: "private", Valid: true},
			Tag:             pgtype.Text{String: "graphs", Valid: true},
			Keyword:         pgtype.Text{String: "shortest", Valid: true},
			OwnerUserID:     7,
			ViewerUserID:    7,
			BeforeCreatedAt: pgtype.Timestamptz{Time: before, Valid: true},
			BeforeID:        84,
			Limit:           11,
		})
		assertIndexableCursorQuery(t, query, []string{
			"p.difficulty = $1::text",
			"p.status = $2::text",
			"p.visibility = $3::text",
			"pt.slug = $4::text",
			"p.title ILIKE '%' || $5::text || '%'",
			"p.slug ILIKE '%' || $5::text || '%'",
			"p.owner_user_id = $6::bigint",
			"(p.created_at, p.id) < ($7::timestamptz, $8::bigint)",
			"ORDER BY p.created_at DESC, p.id DESC",
			"LIMIT $9",
		})
		if strings.Contains(query, "p.status = 'published' AND p.visibility = 'public'") {
			t.Fatalf("owner-scoped problem query should not retain a redundant public visibility branch:\n%s", query)
		}
		assertQueryArgs(t, args, []any{"hard", "draft", "private", "graphs", "shortest", int64(7), before, int64(84), int32(11)})
	})

	t.Run("contests", func(t *testing.T) {
		query, args := buildListContestsByCursorQuery(ListContestsByCursorParams{
			Status:          pgtype.Text{String: "running", Valid: true},
			Visibility:      pgtype.Text{String: "private", Valid: true},
			Keyword:         pgtype.Text{String: "regional", Valid: true},
			VisibleToUserID: pgtype.Int8{Int64: 9, Valid: true},
			BeforeStartAt:   pgtype.Timestamptz{Time: before, Valid: true},
			BeforeID:        126,
			Limit:           31,
		})
		assertIndexableCursorQuery(t, query, []string{
			"c.status = $1::text",
			"c.visibility = $2::text",
			"c.title ILIKE '%' || $3::text || '%'",
			"c.owner_user_id = $4::bigint",
			"cr.user_id = $4::bigint",
			"(c.start_at, c.id) < ($5::timestamptz, $6::bigint)",
			"ORDER BY c.start_at DESC, c.id DESC",
			"LIMIT $7",
		})
		assertQueryArgs(t, args, []any{"running", "private", "regional", int64(9), before, int64(126), int32(31)})
	})

	t.Run("submissions", func(t *testing.T) {
		query, args := buildListSubmissionsByCursorQuery(ListSubmissionsByCursorParams{
			UserID:            pgtype.Int8{Int64: 10, Valid: true},
			ProblemID:         pgtype.Int8{Int64: 20, Valid: true},
			ContestID:         pgtype.Int8{Int64: 30, Valid: true},
			Status:            pgtype.Text{String: "accepted", Valid: true},
			BeforeSubmittedAt: pgtype.Timestamptz{Time: before, Valid: true},
			BeforeID:          168,
			Limit:             51,
		})
		assertIndexableCursorQuery(t, query, []string{
			"user_id = $1::bigint",
			"problem_id = $2::bigint",
			"contest_id = $3::bigint",
			"status = $4::text",
			"(submitted_at, id) < ($5::timestamptz, $6::bigint)",
			"ORDER BY submitted_at DESC, id DESC",
			"LIMIT $7",
		})
		assertQueryArgs(t, args, []any{int64(10), int64(20), int64(30), "accepted", before, int64(168), int32(51)})
	})
}

func TestCursorQueryBuildersUseExplicitDefaultVisibility(t *testing.T) {
	before := time.Date(2026, time.July, 22, 10, 30, 0, 0, time.UTC)

	problemQuery, problemArgs := buildListProblemsByCursorQuery(ListProblemsByCursorParams{
		BeforeCreatedAt: pgtype.Timestamptz{Time: before, Valid: true},
		BeforeID:        42,
		Limit:           21,
	})
	assertIndexableCursorQuery(t, problemQuery, []string{
		"p.status = 'published'",
		"p.visibility = 'public'",
		"(p.created_at, p.id) < ($1::timestamptz, $2::bigint)",
		"LIMIT $3",
	})
	assertQueryArgs(t, problemArgs, []any{before, int64(42), int32(21)})

	contestQuery, contestArgs := buildListContestsByCursorQuery(ListContestsByCursorParams{
		BeforeStartAt: pgtype.Timestamptz{Time: before, Valid: true},
		BeforeID:      84,
		Limit:         11,
	})
	assertIndexableCursorQuery(t, contestQuery, []string{
		"c.visibility = 'public'",
		"(c.start_at, c.id) < ($1::timestamptz, $2::bigint)",
		"LIMIT $3",
	})
	assertQueryArgs(t, contestArgs, []any{before, int64(84), int32(11)})
}

func TestCursorQueryBuildersPreservePrivilegedAndViewerAccess(t *testing.T) {
	before := time.Date(2026, time.July, 22, 10, 30, 0, 0, time.UTC)

	adminProblemQuery, adminProblemArgs := buildListProblemsByCursorQuery(ListProblemsByCursorParams{
		IncludeAll:      true,
		BeforeCreatedAt: pgtype.Timestamptz{Time: before, Valid: true},
		BeforeID:        42,
		Limit:           21,
	})
	assertIndexableCursorQuery(t, adminProblemQuery, []string{
		"(p.created_at, p.id) < ($1::timestamptz, $2::bigint)",
		"LIMIT $3",
	})
	for _, forbidden := range []string{"p.status = 'published'", "p.visibility = 'public'", "p.owner_user_id ="} {
		if strings.Contains(adminProblemQuery, forbidden) {
			t.Fatalf("privileged problem query must not contain %q:\n%s", forbidden, adminProblemQuery)
		}
	}
	assertQueryArgs(t, adminProblemArgs, []any{before, int64(42), int32(21)})

	viewerProblemQuery, viewerProblemArgs := buildListProblemsByCursorQuery(ListProblemsByCursorParams{
		ViewerUserID:    7,
		BeforeCreatedAt: pgtype.Timestamptz{Time: before, Valid: true},
		BeforeID:        84,
		Limit:           11,
	})
	assertIndexableCursorQuery(t, viewerProblemQuery, []string{
		"(p.status = 'published' AND p.visibility = 'public') OR p.owner_user_id = $1::bigint",
		"(p.created_at, p.id) < ($2::timestamptz, $3::bigint)",
		"LIMIT $4",
	})
	assertQueryArgs(t, viewerProblemArgs, []any{int64(7), before, int64(84), int32(11)})

	adminContestQuery, adminContestArgs := buildListContestsByCursorQuery(ListContestsByCursorParams{
		IncludePrivate: true,
		BeforeStartAt:  pgtype.Timestamptz{Time: before, Valid: true},
		BeforeID:       126,
		Limit:          31,
	})
	assertIndexableCursorQuery(t, adminContestQuery, []string{
		"(c.start_at, c.id) < ($1::timestamptz, $2::bigint)",
		"LIMIT $3",
	})
	if strings.Contains(adminContestQuery, "c.visibility = 'public'") {
		t.Fatalf("privileged contest query must not restrict visibility:\n%s", adminContestQuery)
	}
	assertQueryArgs(t, adminContestArgs, []any{before, int64(126), int32(31)})
}

func assertIndexableCursorQuery(t *testing.T, query string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	upperQuery := strings.ToUpper(query)
	for _, forbidden := range []string{"IS NULL OR", "OFFSET", "COUNT("} {
		if strings.Contains(upperQuery, forbidden) {
			t.Fatalf("cursor query must not contain %q:\n%s", forbidden, query)
		}
	}
}

func assertQueryArgs(t *testing.T, got, want []any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query args = %#v, want %#v", got, want)
	}
}
