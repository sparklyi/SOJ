# List Query Performance Bug Reproduction

```yaml
analysis:
  bug_statement: "List endpoints perform substring searches and ordered page reads without indexes that match their current predicates and sort order."
  expected: "The schema provides trigram indexes for every current list-search column and high-volume users, problems, contests, and submissions lists can be read with stable seek cursors without offsets or separate count queries."
  actual_before_fix: "The initial schema lacks the matching list indexes, and the high-volume submission list path combines optional OR predicates, OFFSET, and COUNT(*)."
  affected_layers:
    - http
    - service
    - storage
  dependency_isolation: "The regression test reads the local initial schema; PostgreSQL syntax is validated against a disposable local PostgreSQL 16 container."
coverage_plan:
  obligations:
    - trigger_bug
    - prove_before_fail
    - prove_after_pass
    - cover_http_surface
    - cover_service_surface
  proof:
    - "Before the initial-schema change, TestInitialSchemaAddsIndexableSearchAndOrderingPaths fails because 000001_init.up.sql lacks the required indexes."
    - "After the initial-schema change and cursor query, the SQL invariant test passes and a clean PostgreSQL 16 database applies the schema successfully."
cases:
  - id: list-query-index-contract
    purpose: "Retain indexes for the current list-search and ordered list access paths."
    target_layer: storage
    trigger: "Run the focused SQL invariant test."
    before_assertion: "The initial schema lacks pg_trgm and the required list indexes."
    after_assertion: "The initial schema contains pg_trgm and the cursor query uses user_id plus a submitted_at/id seek predicate with no OR, OFFSET, or COUNT."
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
  - id: own-submission-keyset-service
    purpose: "Read a caller's submission history with a stable submitted_at/id cursor."
    target_layer: service
    trigger: "Request a two-item page containing two records with the same submitted_at value."
    before_assertion: "The cursor service method and repository query do not exist."
    after_assertion: "The first page returns IDs 4 and 3, the next cursor points to ID 3, and the second page returns ID 2 without calling the page/count repository method."
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_service_surface
  - id: own-submission-keyset-http
    purpose: "Expose the cursor path without changing the legacy page response contract."
    target_layer: http
    trigger: "GET /api/v1/submissions/mine?page_size=2 as user 5."
    before_assertion: "The request is routed to /api/v1/submissions/:id and returns invalid_submission_id."
    after_assertion: "The response returns items and next_cursor without total; following that cursor returns the remaining item."
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_http_surface
  - id: list-cursor-surfaces
    purpose: "Expose seek pagination for every high-volume list surface while retaining legacy page APIs."
    target_layer: http
    trigger: "Request the cursor routes for problems, contests, admin users, and submissions with page_size=2."
    before_assertion: "Only the submission-history cursor route exists; the other list queries use OFFSET and COUNT(*)."
    after_assertion: "Each cursor route returns items plus an optional next_cursor and its repository query contains the submitted_at/id, created_at/id, or start_at/id seek boundary without OFFSET or COUNT(*)."
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_http_surface
executors:
  focused_test:
    command: "go test ./internal/postgres/db -run 'Test(InitialSchemaAddsIndexableSearchAndOrderingPaths|OwnSubmissionCursorQueryUsesSeekPagination|ListCursorQueriesUseSeekPaginationWithoutCountOrOffset)' -count=1"
  service_and_http_test:
    command: "go test ./internal/user ./internal/problem ./internal/contest ./internal/submission -run 'Test(ListUsersByCursor|ListProblemsByCursor|ListContestsByCursor|ListSubmissionsByCursor|HandlerListUsersByCursor|HandlerListsOwnSubmissionsByCursor)' -count=1"
  database_validation:
    command: "Apply migrations 000001 through 000002 to a disposable PostgreSQL 16 database, then EXPLAIN the cursor and trigram queries."
handoff:
  implementation: "Because the project is not deployed, 000001 adds the tested indexes directly. Cursor routes for users, problems, contests, and submissions provide stable seek pagination without changing the legacy page response contracts."
```
