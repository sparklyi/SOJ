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
