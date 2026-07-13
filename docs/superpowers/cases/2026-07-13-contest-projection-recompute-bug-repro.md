# Contest Projection Recompute Bug Repro Cases

Source:
- GitHub issue #28
- `internal/submission/repository.go`
- `internal/contest/submission.go`

## Structured Output

```yaml
analysis:
  bug_class: state-consistency
  affected_layers:
    - service
    - storage
  root_cause_summary: >-
    Contest problem results are incrementally updated in judge completion order and
    short-circuit when the current projection is accepted or references the same
    submission. The projection therefore does not reflect submission order or a
    rejudged submission's current verdict.
  missing_information: []
  reproducibility:
    status: reproducible
    blockers: []

coverage_plan:
  obligations:
    - trigger_bug
    - prove_before_fail
    - prove_after_pass
    - cover_service_surface
  selected_cases:
    - out_of_order_completion
    - rejudge_replaces_current_verdict

cases:
  - case_id: out_of_order_completion
    purpose: trigger
    target_layer: service
    minimality_reason: >-
      Two submissions are the smallest sequence that proves judge completion order
      must not determine ACM attempts or penalty.
    input:
      http: {}
      service:
        function: buildContestProblemProjection
        args: >-
          Contest starts at 10:00. Submission 1 is wrong_answer at 10:10 and
          submission 2 is accepted at 10:20, supplied in completion order 2 then 1.
      fixtures:
        db: Two current terminal submission rows with their current attempt ids.
        es: not-needed
        redis: not-needed
        files: not-needed
        network: not-needed
    isolation:
      db: fixture
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: not-needed
      network: not-needed
    before_expectation:
      outcome: wrong-state
      assertion: "attempts == 1 and penalty_minutes == 20"
    after_expectation:
      outcome: pass
      assertion: "status == accepted and attempts == 2 and penalty_minutes == 40 and last_submission_id == 2"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_service_surface

  - case_id: rejudge_replaces_current_verdict
    purpose: guard
    target_layer: service
    minimality_reason: >-
      One submission whose current verdict changes is sufficient to prove the
      projection must be rebuilt instead of skipped by submission id.
    input:
      http: {}
      service:
        function: buildContestProblemProjection
        args: >-
          One submission currently has wrong_answer after rejudge and points to the
          new current attempt.
      fixtures:
        db: One current terminal submission row after accepted was rejudged to wrong_answer.
        es: not-needed
        redis: not-needed
        files: not-needed
        network: not-needed
    isolation:
      db: fixture
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: not-needed
      network: not-needed
    before_expectation:
      outcome: wrong-state
      assertion: "projection remains accepted because last_submission_id matches"
    after_expectation:
      outcome: pass
      assertion: "status == attempted and attempts == 1 and accepted_at is null and last_attempt_id is the rejudge attempt"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_service_surface

executors:
  http_reproducer:
    needed: false
    strategy: none
    target_files: []
    setup_notes:
      - The defect is in transaction-local projection calculation, below the HTTP boundary.
  service_reproducer:
    needed: true
    strategy: direct-call
    target_files:
      - internal/submission/contest_projection_test.go
      - internal/submission/repository.go
      - internal/postgres/queries/submissions.sql
    setup_notes:
      - Use a pure projection builder for deterministic fail-before and pass-after assertions.
      - Verify SQL separately keeps the rebuild inside the submission completion transaction.

minimization:
  core_case: out_of_order_completion
  removed_candidates:
    - case_id: accepted_then_later_wrong
      removal_reason: same-path
    - case_id: historical_wrong_rejudged_accepted
      removal_reason: same-obligation
  final_case_count: 2

handoff:
  artifact_path: docs/superpowers/cases/2026-07-13-contest-projection-recompute-bug-repro.md
  summary: >-
    Recompute each contest user/problem projection from current terminal submissions
    ordered by submission time, and serialize concurrent recomputations per projection.
  next_goal: >-
    Replace completion-order incremental updates with a transactionally locked rebuild
    that uses current submission_results attempt links and supports rejudge replacement.
  suggested_skills:
    - go-feature-change-review
    - git-commit-push-pr
  verification_focus:
    - out_of_order_completion
    - rejudge_replaces_current_verdict
    - concurrent completions cannot overwrite a projection with a partial view
```
