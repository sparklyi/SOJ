# Redis Stream Retention Bug Repro Cases

Source:
- GitHub issue #41
- `internal/queue/redis_stream.go`

## Structured Output

```yaml
analysis:
  bug_class: boundary
  affected_layers:
    - service
  root_cause_summary: "RedisStreamQueue publishes request, result, and dead-letter records with XADD but no retention option, so acknowledged history can grow without a bound."
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
    - publish-retention-command
    - dead-letter-retention-command
    - reject-unbounded-retention-config

cases:
  - case_id: publish-retention-command
    purpose: trigger
    target_layer: service
    minimality_reason: "A recording Redis client isolates the single Publish call and captures the command that controls stream growth."
    input:
      service:
        function: "RedisStreamQueue.Publish"
        args: "taskID=42, payload=\"{}\", MaxLen=50"
      fixtures:
        db: not-needed
        es: not-needed
        redis: "recording go-redis hook"
        files: not-needed
        network: not-needed
    isolation:
      db: not-needed
      es: not-needed
      doris: not-needed
      redis: mock
      file_io: not-needed
      network: mock
    before_expectation:
      outcome: wrong-state
      assertion: "captured XADD arguments omit the exact prefix [xadd tasks maxlen ~ 50 *]"
    after_expectation:
      outcome: pass
      assertion: "captured XADD arguments prefix equals [xadd tasks maxlen ~ 50 *]"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass
      - cover_boundary_class

  - case_id: dead-letter-retention-command
    purpose: guard
    target_layer: service
    minimality_reason: "DeadLetter has a separate XADD path and requires an independently configured cap."
    input:
      service:
        function: "RedisStreamQueue.DeadLetter"
        args: "messageID=1-0, DeadMaxLen=7"
      fixtures:
        db: not-needed
        es: not-needed
        redis: "recording go-redis hook"
        files: not-needed
        network: not-needed
    isolation:
      db: not-needed
      es: not-needed
      doris: not-needed
      redis: mock
      file_io: not-needed
      network: mock
    before_expectation:
      outcome: wrong-state
      assertion: "captured dead-letter XADD arguments omit the exact prefix [xadd tasks:dead maxlen ~ 7 *]"
    after_expectation:
      outcome: pass
      assertion: "captured dead-letter XADD arguments prefix equals [xadd tasks:dead maxlen ~ 7 *]"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass

  - case_id: reject-unbounded-retention-config
    purpose: guard
    target_layer: service
    minimality_reason: "Rejecting zero prevents an environment override from silently disabling the required retention policy."
    input:
      service:
        function: "config.Load"
        args: "SOJ_REDIS_STREAM_MAX_LEN=0"
      fixtures:
        db: not-needed
        es: not-needed
        redis: not-needed
        files: not-needed
        network: not-needed
    isolation:
      db: not-needed
      es: not-needed
      doris: not-needed
      redis: not-needed
      file_io: not-needed
      network: not-needed
    before_expectation:
      outcome: wrong-state
      assertion: "config.Load returns nil error when SOJ_REDIS_STREAM_MAX_LEN equals 0"
    after_expectation:
      outcome: pass
      assertion: "config.Load returns an error whose prefix is SOJ_REDIS_STREAM_MAX_LEN when the value equals 0"
    obligations_covered:
      - trigger_bug
      - prove_before_fail
      - prove_after_pass

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
      - internal/queue/redis_stream_test.go
      - internal/config/config_test.go
    setup_notes:
      - "Use a go-redis hook that records commands without connecting to Redis."
      - "Use t.Setenv for configuration parsing cases."

minimization:
  core_case: publish-retention-command
  removed_candidates:
    - case_id: stream-length-integration
      removal_reason: same-obligation
  final_case_count: 3

handoff:
  artifact_path: docs/superpowers/cases/2026-07-16-redis-stream-retention-bug-repro.md
  summary: "Retain both normal and dead-letter Redis Stream history with independent approximate MAXLEN values and reject non-positive overrides."
  next_goal: "Add validated stream retention configuration and apply it to every RedisStreamQueue XADD path."
  suggested_skills:
    - go-feature-change-review
    - pr-creator
  verification_focus:
    - "Publish emits XADD MAXLEN ~ with the normal stream limit."
    - "DeadLetter emits XADD MAXLEN ~ with the dead-letter limit."
    - "Zero or negative retention overrides fail configuration loading."
```
