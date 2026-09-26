package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetProblemStatsScopesStatusAggregationToRequestedProblem(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "queries", "problems.sql"))
	if err != nil {
		t.Fatalf("read problems queries: %v", err)
	}

	for _, test := range []struct {
		name  string
		query string
	}{
		{name: "source", query: namedQuery(t, string(source), "-- name: GetProblemStats :one")},
		{name: "generated", query: getProblemStats},
	} {
		for _, want := range []string{
			"SELECT s.status, count(*)::bigint AS count",
			"FROM submissions s",
			"WHERE s.problem_id = $1",
			"GROUP BY s.status",
			"coalesce(sum(status_counts.count), 0)::bigint AS total_submissions",
			"coalesce(sum(status_counts.count) FILTER (WHERE status_counts.status = 'accepted'), 0)::bigint AS accepted_submissions",
			"jsonb_object_agg(status_counts.status, status_counts.count)",
			") status_counts ON true",
		} {
			if !strings.Contains(test.query, want) {
				t.Fatalf("%s GetProblemStats missing %q:\n%s", test.name, want, test.query)
			}
		}
		if strings.Contains(test.query, "GROUP BY problem_id, status") {
			t.Fatalf("%s GetProblemStats aggregates every problem:\n%s", test.name, test.query)
		}
	}
}

func TestListProblemTagsByProblemIDsBatchesLookups(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "queries", "problems.sql"))
	if err != nil {
		t.Fatalf("read problems queries: %v", err)
	}

	for _, test := range []struct {
		name  string
		query string
	}{
		{name: "source", query: namedQuery(t, string(source), "-- name: ListProblemTagsByProblemIDs :many")},
		{name: "generated", query: listProblemTagsByProblemIDs},
	} {
		for _, want := range []string{
			"JOIN problem_tags pt ON pt.id = ptl.tag_id",
			"ORDER BY ptl.problem_id, pt.name",
		} {
			if !strings.Contains(test.query, want) {
				t.Fatalf("%s ListProblemTagsByProblemIDs missing %q:\n%s", test.name, want, test.query)
			}
		}
		anyArg := "ptl.problem_id = ANY($1::bigint[])"
		if test.name == "source" {
			anyArg = "ptl.problem_id = ANY(sqlc.arg('problem_ids')::bigint[])"
		}
		if !strings.Contains(test.query, anyArg) {
			t.Fatalf("%s ListProblemTagsByProblemIDs missing %q:\n%s", test.name, anyArg, test.query)
		}
		if strings.Contains(test.query, "LIMIT 1") || strings.Contains(test.query, "= $1 AND ptl.problem_id") {
			t.Fatalf("%s ListProblemTagsByProblemIDs looks per-problem:\n%s", test.name, test.query)
		}
	}
}

func TestTestcaseSetsDropStatusColumn(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "queries", "problems.sql"))
	if err != nil {
		t.Fatalf("read problems queries: %v", err)
	}
	generated := getCurrentTestcaseSet + createTestcaseSet
	for _, want := range []string{
		"WHERE problem_id = $1\n  AND is_current = true",
	} {
		if !strings.Contains(getCurrentTestcaseSet, want) {
			t.Fatalf("GetCurrentTestcaseSet missing %q:\n%s", want, getCurrentTestcaseSet)
		}
	}
	for name, query := range map[string]string{
		"source":    namedQuery(t, string(source), "-- name: GetCurrentTestcaseSet :one"),
		"generated": getCurrentTestcaseSet,
	} {
		if strings.Contains(query, "status") {
			t.Fatalf("%s GetCurrentTestcaseSet still filters on status:\n%s", name, query)
		}
	}
	if strings.Contains(generated, "status") {
		t.Fatalf("testcase set queries still reference the dropped status column:\n%s", generated)
	}

	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000008_drop_testcase_status.up.sql"))
	if err != nil {
		t.Fatalf("read drop-testcase-status migration: %v", err)
	}
	if !strings.Contains(string(migration), "ALTER TABLE testcase_sets DROP COLUMN status") {
		t.Fatalf("migration does not drop testcase_sets.status:\n%s", migration)
	}
}

func namedQuery(t *testing.T, source, name string) string {
	t.Helper()

	_, query, found := strings.Cut(source, name)
	if !found {
		t.Fatalf("query %q not found", name)
	}
	query, _, found = strings.Cut(query, ";")
	if !found {
		t.Fatalf("query %q has no terminating semicolon", name)
	}
	return name + query
}
