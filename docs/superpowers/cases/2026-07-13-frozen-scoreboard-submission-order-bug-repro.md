# Frozen Scoreboard Submission Order Bug Reproduction

Issue: #29

## Bug Statement

The frozen scoreboard rebuild omits `output_limit` submissions and processes terminal submissions by judge completion time instead of ACM submission order. A wrong submission made before an accepted submission can therefore disappear from attempts and penalty calculations.

## Minimal Cases

### Reversed judge completion order

- Input: submission 1 is wrong at minute 10 and judged at minute 25; submission 2 is accepted at minute 20 and judged at minute 21.
- Before fix: the accepted submission is processed first, producing 0 wrong attempts and 20 penalty minutes.
- After fix: submissions are processed by `submitted_at, id`, producing 1 wrong attempt and 40 penalty minutes.

### Equal submission timestamps

- Input: submission 1 is wrong and submission 2 is accepted at minute 20; submission 2 finishes judging first.
- Before fix: judge completion order produces 0 wrong attempts and 20 penalty minutes.
- After fix: ID breaks the timestamp tie, producing 1 wrong attempt and 40 penalty minutes.

### Output limit terminal status

- Input: an `output_limit` submission at minute 10 followed by an accepted submission at minute 20.
- Before fix: `ListContestTerminalSubmissions` excludes the first submission, producing 0 wrong attempts and 20 penalty minutes in a database-backed rebuild.
- After fix: the query returns `output_limit`, producing 1 wrong attempt and 40 penalty minutes.

## Local Executors

- `go test ./internal/contest -run TestBuildBoardFromSubmissionsUsesACMSubmissionOrder`
- `go test ./internal/postgres/db -run TestListContestTerminalSubmissionsUsesCompleteStatusesAndSubmissionOrder`

Both executors are deterministic and require no database, network, or external service.

## Handoff

Change frozen scoreboard sorting to `submitted_at, id`, keep `judged_at` only for freeze visibility, and synchronize the source SQL with its generated Go query. Re-run both executors to prove pass-after-fix.
