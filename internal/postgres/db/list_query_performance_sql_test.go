package db

import (
	"strings"
	"testing"
)

func TestInitialSchemaAddsIndexableSearchAndOrderingPaths(t *testing.T) {
	schema := readInitialSchema(t)

	for _, want := range []string{
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"CREATE INDEX problems_public_catalog_recent_idx\n    ON problems (created_at DESC, id DESC)\n    WHERE status = 'published' AND visibility = 'public'",
		"CREATE INDEX problems_owner_recent_idx\n    ON problems (owner_user_id, created_at DESC, id DESC)",
		"CREATE INDEX submissions_user_submitted_id_idx\n    ON submissions (user_id, submitted_at DESC, id DESC)",
		"CREATE INDEX submissions_problem_submitted_id_idx\n    ON submissions (problem_id, submitted_at DESC, id DESC)",
		"CREATE INDEX submissions_contest_submitted_id_idx\n    ON submissions (contest_id, submitted_at DESC, id DESC)",
		"CREATE INDEX contests_public_recent_idx\n    ON contests (start_at DESC, id DESC)\n    WHERE visibility = 'public'",
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
