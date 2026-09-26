package problem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests exercise the authoring queries that were rewritten to drop the
// testcase status column and to batch findings/tags. They require a disposable
// Postgres database seeded by the repository migrations:
//
//	SOJ_TEST_DATABASE_DSN=postgres://... go test ./internal/problem/...
func newIntegrationRepository(t *testing.T) (*PostgresRepository, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("SOJ_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SOJ_TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return NewPostgresRepository(pool), pool
}

func integrationBaseID() int64 {
	return (time.Now().UnixNano() & ((1 << 50) - 1)) * 16
}

func TestCreateProblemCheckFindingsInsertsBatch(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	ctx := context.Background()
	base := integrationBaseID()
	userID, problemID, runID := base+1, base+2, base+3

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, username, status)
		VALUES ($1, $2, 'hash', $3, 'active')`,
		userID, fmt.Sprintf("it-findings-%d@example.test", userID), fmt.Sprintf("it-findings-%d", userID)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO problems (id, owner_user_id, title, slug, difficulty, visibility, status, time_limit_ms, memory_limit_kb)
		VALUES ($1, $2, 'Integration', $3, 'easy', 'private', 'draft', 1000, 262144)`,
		problemID, userID, fmt.Sprintf("it-findings-%d", problemID)); err != nil {
		t.Fatalf("seed problem: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO problem_check_runs (id, problem_id, requested_by, status, summary)
		VALUES ($1, $2, $3, 'running', '{}'::jsonb)`, runID, problemID, userID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	records, err := repository.CreateProblemCheckFindings(ctx, []CreateProblemCheckFindingInput{
		{
			RunID:    runID,
			Severity: ProblemCheckSeverityError,
			Code:     codeZipInvalid,
			Message:  "bad zip",
		},
		{
			RunID:       runID,
			Severity:    ProblemCheckSeverityError,
			Code:        codeOutputMissing,
			Message:     "missing output",
			CaseIndex:   2,
			TestcaseKey: "2.in",
			Details:     json.RawMessage(`{"storage_key":"k"}`),
		},
		{
			RunID:       runID,
			Severity:    ProblemCheckSeverityWarning,
			Code:        codeFileIgnored,
			Message:     "ignored",
			TestcaseKey: ".DS_Store",
		},
	})
	if err != nil {
		t.Fatalf("CreateProblemCheckFindings returned error: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}
	for index, record := range records {
		if record.ID == 0 || record.RunID != runID {
			t.Fatalf("record %d = %+v", index, record)
		}
		if index > 0 && record.ID <= records[index-1].ID {
			t.Fatalf("records are not id-ordered: %+v", records)
		}
	}
	if records[0].CaseIndex != 0 || records[0].TestcaseKey != "" || string(records[0].Details) != "{}" {
		t.Fatalf("first record = %+v", records[0])
	}
	if records[1].CaseIndex != 2 || records[1].TestcaseKey != "2.in" || string(records[1].Details) != `{"storage_key": "k"}` {
		t.Fatalf("second record = %+v", records[1])
	}
	if records[2].Severity != ProblemCheckSeverityWarning || records[2].TestcaseKey != ".DS_Store" {
		t.Fatalf("third record = %+v", records[2])
	}
}

func TestListProblemTagsByProblemIDsGroupsAndOrdersTags(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	ctx := context.Background()
	base := integrationBaseID()
	userID, problemID, otherProblemID := base+1, base+2, base+3
	alphaTagID, zetaTagID := base+4, base+5

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, username, status)
		VALUES ($1, $2, 'hash', $3, 'active')`,
		userID, fmt.Sprintf("it-tags-%d@example.test", userID), fmt.Sprintf("it-tags-%d", userID)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	for _, seed := range []struct {
		id   int64
		slug string
	}{
		{problemID, fmt.Sprintf("it-tags-a-%d", problemID)},
		{otherProblemID, fmt.Sprintf("it-tags-b-%d", otherProblemID)},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO problems (id, owner_user_id, title, slug, difficulty, visibility, status, time_limit_ms, memory_limit_kb)
			VALUES ($1, $2, 'Integration', $3, 'easy', 'private', 'draft', 1000, 262144)`,
			seed.id, userID, seed.slug); err != nil {
			t.Fatalf("seed problem: %v", err)
		}
	}
	for _, tag := range []struct {
		id   int64
		name string
		slug string
	}{
		{zetaTagID, "zeta", fmt.Sprintf("it-zeta-%d", zetaTagID)},
		{alphaTagID, "Alpha", fmt.Sprintf("it-alpha-%d", alphaTagID)},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO problem_tags (id, name, slug) VALUES ($1, $2, $3)`,
			tag.id, tag.name, tag.slug); err != nil {
			t.Fatalf("seed tag: %v", err)
		}
	}
	for _, tagID := range []int64{zetaTagID, alphaTagID} {
		if _, err := pool.Exec(ctx, `INSERT INTO problem_tag_links (problem_id, tag_id) VALUES ($1, $2)`,
			problemID, tagID); err != nil {
			t.Fatalf("seed tag link: %v", err)
		}
	}

	grouped, err := repository.ListProblemTagsByProblemIDs(ctx, []int64{problemID, otherProblemID})
	if err != nil {
		t.Fatalf("ListProblemTagsByProblemIDs returned error: %v", err)
	}
	tags := grouped[problemID]
	if len(tags) != 2 || tags[0].Name != "Alpha" || tags[1].Name != "zeta" {
		t.Fatalf("tags = %+v, want Alpha then zeta", tags)
	}
	if len(grouped[otherProblemID]) != 0 {
		t.Fatalf("untagged problem tags = %+v, want none", grouped[otherProblemID])
	}
}

func TestGetCurrentTestcaseSetIgnoresDroppedStatus(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	ctx := context.Background()
	base := integrationBaseID()
	userID, problemID, currentSetID, oldSetID := base+1, base+2, base+3, base+4

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, username, status)
		VALUES ($1, $2, 'hash', $3, 'active')`,
		userID, fmt.Sprintf("it-sets-%d@example.test", userID), fmt.Sprintf("it-sets-%d", userID)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO problems (id, owner_user_id, title, slug, difficulty, visibility, status, time_limit_ms, memory_limit_kb)
		VALUES ($1, $2, 'Integration', $3, 'easy', 'private', 'draft', 1000, 262144)`,
		problemID, userID, fmt.Sprintf("it-sets-%d", problemID)); err != nil {
		t.Fatalf("seed problem: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO testcase_sets (id, problem_id, version, storage_key, checksum_sha256, size_bytes, case_count, is_current, created_by)
		VALUES ($1, $2, 1, $3, repeat('a', 64), 2, 1, false, $4),
		       ($5, $2, 2, $6, repeat('b', 64), 4, 2, true, $4)`,
		oldSetID, problemID, fmt.Sprintf("it-sets-old-%d.zip", oldSetID), userID,
		currentSetID, fmt.Sprintf("it-sets-current-%d.zip", currentSetID)); err != nil {
		t.Fatalf("seed testcase sets: %v", err)
	}

	set, err := repository.GetCurrentTestcaseSet(ctx, problemID)
	if err != nil {
		t.Fatalf("GetCurrentTestcaseSet returned error: %v", err)
	}
	if set.ID != currentSetID || !set.IsCurrent || set.CaseCount != 2 {
		t.Fatalf("current set = %+v, want %d", set, currentSetID)
	}
}
