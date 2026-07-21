package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestListCursorQueriesUseSeekPaginationWithoutCountOrOffset(t *testing.T) {
	tests := []struct {
		file string
		name string
		want []string
	}{
		{
			file: "users.sql",
			name: "ListUsersByCursor",
			want: []string{
				"(created_at, id) < (",
				"ORDER BY created_at DESC, id DESC",
				"LIMIT sqlc.arg('limit')",
			},
		},
		{
			file: "problems.sql",
			name: "ListProblemsByCursor",
			want: []string{
				"(p.created_at, p.id) < (",
				"ORDER BY p.created_at DESC, p.id DESC",
				"LIMIT sqlc.arg('limit')",
			},
		},
		{
			file: "contests.sql",
			name: "ListContestsByCursor",
			want: []string{
				"(c.start_at, c.id) < (",
				"ORDER BY c.start_at DESC, c.id DESC",
				"LIMIT sqlc.arg('limit')",
			},
		},
		{
			file: "submissions.sql",
			name: "ListSubmissionsByCursor",
			want: []string{
				"(submitted_at, id) < (",
				"ORDER BY submitted_at DESC, id DESC",
				"LIMIT sqlc.arg('limit')",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "queries", tt.file))
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			query := querySection(string(content), tt.name)
			for _, want := range tt.want {
				if !strings.Contains(query, want) {
					t.Fatalf("%s query missing %q:\n%s", tt.name, want, query)
				}
			}
			upperQuery := strings.ToUpper(query)
			for _, forbidden := range []string{"OFFSET", "COUNT("} {
				if strings.Contains(upperQuery, forbidden) {
					t.Fatalf("%s query must not contain %q:\n%s", tt.name, forbidden, query)
				}
			}
		})
	}
}

func querySection(source, name string) string {
	marker := "-- name: " + name
	start := strings.Index(source, marker)
	if start < 0 {
		return ""
	}
	section := source[start:]
	if end := strings.Index(section[1:], "-- name:"); end >= 0 {
		section = section[:end+1]
	}
	return section
}
