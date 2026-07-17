# Problem Stats Query Scope Bug Repro Cases

Source:
- GitHub issue #40
- `internal/postgres/queries/problems.sql`
- `internal/postgres/db/problems.sql.go`

## Structured Output

```yaml
analysis:
  bug_class: boundary
  affected_layers:
    - storage
  root_cause_summary: "GetProblemStats filters the outer problem row but aggregates submission status counts for every problem before joining the requested row."
  missing_information: []
  reproducibility:
    status: reproducible
    blockers: []

coverage_plan:
  obligations:
    - trigger_bug
    - prove_before_fail
    - prove_after_pass
    - cover_boundary_class
  selected_cases:
    - scoped-status-aggregation

cases:
  - case_id: scoped-status-aggregation
    purpose: trigger
    target_layer: service
    minimality_reason: "One query shape is sufficient to prove whether the status aggregation can scan submissions outside the requested problem."
    input:
      http: {}
      service:
        function: "db.Queries.GetProblemStats"
        args: "problemID=42"
      fixtures:
        db: "generated SQL constant and source query file"
        es: not-needed
        redis: not-needed
        files: "queries/problems.sql"
        network: not-needed
    isolation:
      db: fixture
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: mock
      network: not-needed
    before_expectation:
      outcome: wrong-state
      assertion: "status aggregation contains GROUP BY problem_id, status without WHERE s.problem_id = $1"
    after_expectation:
      outcome: pass
      assertion: "source and generated GetProblemStats SQL contain WHERE s.problem_id = $1 and GROUP BY s.status"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_boundary_class

executors:
  http_reproducer:
    needed: false
    strategy: none
    target_files: []
    setup_notes: []
  service_reproducer:
    needed: true
    strategy: direct-call
    target_files:
      - internal/postgres/db/problems_sql_test.go
      - internal/postgres/queries/problems.sql
      - internal/postgres/db/problems.sql.go
    setup_notes:
      - "The static query test verifies the source and generated sqlc query remain synchronized."
      - "The existing submissions_problem_status_idx supports the filtered aggregation."

minimization:
  core_case: scoped-status-aggregation
  removed_candidates:
    - case_id: multi-problem-explain
      removal_reason: same-obligation
  final_case_count: 1

handoff:
  artifact_path: docs/superpowers/cases/2026-07-17-problem-stats-query-scope-bug-repro.md
  summary: "Push the requested problem id into the status-count aggregation and calculate all statistics from that scoped result."
  next_goal: "Rewrite GetProblemStats so its only submissions aggregation is constrained by the requested problem id."
  suggested_skills:
    - go-feature-change-review
    - pr-creator
  verification_focus:
    - "The source and generated SQL both scope the aggregation with problem_id = $1."
    - "Totals, accepted count, and status JSON are derived from the scoped aggregation."
```
