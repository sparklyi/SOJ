# Submission List N+1 Bug Repro Cases

Source:
- GitHub issue #37
- `internal/submission/service.go`
- `internal/submission/repository.go`

## Structured Output

```yaml
analysis:
  bug_class: boundary
  affected_layers:
    - http
    - service
    - storage
  root_cause_summary: "ListSubmissions invokes detail lookups for every terminal submission and includes every case result in the list response."
  missing_information: []
  reproducibility:
    status: reproducible
    blockers: []

coverage_plan:
  obligations:
    - trigger_bug
    - prove_before_fail
    - prove_after_pass
    - cover_http_surface
    - cover_service_surface
    - cover_boundary_class
  selected_cases:
    - batched-submission-list-summary
    - submission-list-http-summary

cases:
  - case_id: batched-submission-list-summary
    purpose: trigger
    target_layer: service
    minimality_reason: "Two terminal contest submissions show that fan-out scales with page items while still fitting a deterministic in-memory fixture."
    input:
      http: {}
      service:
        function: "submission.Service.ListSubmissions"
        args: "actor=admin 99, limit=50"
      fixtures:
        db: "two terminal submissions with result, latest attempt, and one case each"
        es: not-needed
        redis: not-needed
        files: not-needed
        network: not-needed
    isolation:
      db: memory
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: not-needed
      network: not-needed
    before_expectation:
      outcome: wrong-state
      assertion: "per-item result, attempt, and case read counters equal 2 while the batched summary loader count equals 0"
    after_expectation:
      outcome: pass
      assertion: "batched summary loader count equals 1, per-item detail read counters equal 0, and each view has a result with no cases"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_service_surface
      - cover_boundary_class
  - case_id: submission-list-http-summary
    purpose: guard
    target_layer: http
    minimality_reason: "The same two-submission fixture verifies the public list endpoint does not return case-level payloads."
    input:
      http:
        method: GET
        path: /api/v1/submissions
        headers: "X-User-ID=5, X-User-Role=user"
      service: {}
      fixtures:
        db: "same two terminal submissions as the service case"
        es: not-needed
        redis: not-needed
        files: not-needed
        network: not-needed
    isolation:
      db: memory
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: not-needed
      network: httptest
    before_expectation:
      outcome: wrong-response-body
      assertion: "each list item includes a non-empty cases array"
    after_expectation:
      outcome: pass
      assertion: "each list item includes result and omits cases"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_http_surface

executors:
  http_reproducer:
    needed: true
    strategy: existing-router
    target_files:
      - internal/submission/service_test.go
    setup_notes:
      - "The handler uses the in-memory repository and a deterministic contest visibility policy."
  service_reproducer:
    needed: true
    strategy: direct-call
    target_files:
      - internal/submission/service_test.go
      - internal/submission/memory_repo_test.go
    setup_notes:
      - "Read counters distinguish one batch loader from per-submission detail lookups."

minimization:
  core_case: batched-submission-list-summary
  removed_candidates:
    - case_id: single-submission-list-summary
      removal_reason: same-obligation
  final_case_count: 2

handoff:
  artifact_path: docs/superpowers/cases/2026-07-18-submission-list-n-plus-one-bug-repro.md
  summary: "Batch summary projections and contest visibility for list pages, and reserve case-level data for submission detail."
  next_goal: "Replace per-record list detail reads with bounded bulk queries while preserving result visibility and admin diagnostics."
  suggested_skills:
    - go-feature-change-review
    - pr-creator
  verification_focus:
    - "The service uses a constant number of detail queries for a page."
    - "List responses omit cases but detail responses retain them."
    - "Contest result visibility remains equivalent for each submission."
```
