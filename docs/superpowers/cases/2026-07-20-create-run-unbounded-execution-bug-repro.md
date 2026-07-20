# CreateRun Unbounded Execution Bug Repro Cases

Source:
- GitHub issue #42
- `internal/submission/service.go`
- `internal/submission/handler.go`

## Structured Output

```yaml
analysis:
  bug_class: concurrency
  affected_layers:
    - http
    - service
    - storage
  root_cause_summary: "CreateRun starts a new background judge goroutine for every request after storing a run, without any capacity boundary."
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
    - run-execution-capacity-service
    - run-execution-capacity-http

cases:
  - case_id: run-execution-capacity-service
    purpose: trigger
    target_layer: service
    minimality_reason: "One blocked self-test and one follow-up request prove whether the API starts an unbounded second judge execution."
    input:
      http: {}
      service:
        function: "submission.Service.CreateRun"
        args: "two authenticated requests using one blocking judge"
      fixtures:
        db: "in-memory repository with one enabled language"
        es: not-needed
        redis: not-needed
        files: memory source store
        network: not-needed
    isolation:
      db: memory
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: memory
      network: not-needed
    before_expectation:
      outcome: wrong-state
      assertion: "the second request succeeds and starts another background judge execution"
    after_expectation:
      outcome: pass
      assertion: "the second request returns service unavailable before creating another run or artifact"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_service_surface
      - cover_boundary_class
  - case_id: run-execution-capacity-http
    purpose: guard
    target_layer: http
    minimality_reason: "The same saturated service verifies that the public POST route exposes the backpressure response."
    input:
      http:
        method: POST
        path: /api/v1/runs
        headers: "X-User-ID=5, X-User-Role=user"
      service: {}
      fixtures:
        db: "same blocked self-test fixture"
        es: not-needed
        redis: not-needed
        files: memory source store
        network: httptest
    isolation:
      db: memory
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: memory
      network: httptest
    before_expectation:
      outcome: wrong-http-status
      assertion: "the route accepts another run while the first judge execution is blocked"
    after_expectation:
      outcome: pass
      assertion: "the route returns HTTP 503 and does not persist a second run or artifact"
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
      - "A blocking in-memory judge holds the only execution slot."
  service_reproducer:
    needed: true
    strategy: direct-call
    target_files:
      - internal/submission/service_test.go
      - internal/submission/memory_repo_test.go
    setup_notes:
      - "The repository maps prove rejected work has no persisted run or artifact."

minimization:
  core_case: run-execution-capacity-service
  removed_candidates:
    - case_id: multiple-overflow-requests
      removal_reason: "one follow-up request proves the unbounded admission branch"
  final_case_count: 2

handoff:
  artifact_path: docs/superpowers/cases/2026-07-20-create-run-unbounded-execution-bug-repro.md
  summary: "Bound API self-test execution before run persistence, and preserve short-wait results for admitted work."
  next_goal: "Introduce a lifecycle-owned bounded execution limiter and wire it to API shutdown."
  suggested_skills:
    - go-feature-change-review
    - pr-creator
  verification_focus:
    - "No rejected request creates an execution goroutine or persisted run."
    - "An admitted run completes after its execution slot is released."
    - "The HTTP route maps saturation to a stable service-unavailable response."
```
