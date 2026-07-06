# Runner Capacity Report - 2026-07-06

## Summary

The local runsc capacity test could not run because Docker did not have the `runsc` runtime registered. A default Docker runtime capacity run completed as a non-production comparison.

This report is not a production capacity claim. Production capacity still requires a judge node with Docker plus gVisor/runsc installed and validated.

## Environment

| Item | Value |
| --- | --- |
| Branch | `feat/judge-runtime-readiness` |
| Base commit | `afa2105` |
| OS | Darwin 25.5.0 arm64 |
| CPU | Apple M5, 10 cores |
| Memory | 24 GiB |
| Docker context | `orbstack` |
| Docker version | client/server `29.4.0` |
| Docker runtimes | `runc`; `runsc` unavailable |
| Runner workdir | `/tmp/soj-runner-work` |
| Go runner image | `ghcr.io/sparklyi/soj-runner-go:main@sha256:148de7dcab3eada409f7a590a998d2b3123cd955a59029b2dadcdce238902e11` |
| C++17 runner image | `ghcr.io/sparklyi/soj-runner-cpp17:main@sha256:60025cca9d106bc45b7c02cdb899b56a2a5561be58497746471f9f0b0f786c31` |

Both runner images expose `linux/amd64` and `linux/arm64` manifests.

## Commands

General checks:

```bash
go test ./...
go vet ./...
docker compose -f deploy/docker-compose.yaml config
RUNNER_IMAGES_PREPARE=pull make smoke-real-docker
```

runsc capacity attempt:

```bash
SOJ_DOCKER_RUNNER_RUNTIME=runsc RUNNER_IMAGES_PREPARE=skip make smoke-runner-capacity
```

Result:

```text
docker runtime runsc is not registered
make: *** [smoke-runner-capacity] Error 1
```

Default Docker comparison:

```bash
RUNNER_IMAGES_PREPARE=skip SOJ_CAPACITY_SKIP_BUILD=1 make smoke-runner-capacity
```

## Verification Results

| Check | Result |
| --- | --- |
| `go test ./...` | Passed |
| `go vet ./...` | Passed |
| `docker compose -f deploy/docker-compose.yaml config` | Passed |
| `RUNNER_IMAGES_PREPARE=pull make smoke-real-docker` | Passed: `smoke ok: problem=1 submission=1 contest=1 contest_submission=3 judge_results=3 async_rows=2 case_results=2` |
| `SOJ_DOCKER_RUNNER_RUNTIME=runsc ... make smoke-runner-capacity` | Blocked: Docker runtime `runsc` is not registered |
| Default Docker `make smoke-runner-capacity` | Passed |

## Default Docker Capacity Data

| Slots | Submissions | Accepted | Submissions/min | P95 latency ms | P99 latency ms | P95 attempt ms | P99 attempt ms | Startup P95 ms | Startup P99 ms | Agent memory peak MiB | Queue oldest pending max s | Sandbox errors | Cleanup failures |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 4 | 4 | 30.14 | 7677.46 | 7899.57 | 7608.32 | 7830.12 | 378 | 378 | 30.38 | 7.08 | 0 | 0 |
| 2 | 8 | 8 | 43.97 | 10885.40 | 10892.83 | 10471.48 | 10477.51 | 245 | 245 | 25.61 | 9.82 | 0 | 0 |
| 4 | 16 | 16 | 66.02 | 14472.62 | 14504.51 | 14311.45 | 14341.04 | 206 | 206 | 44.32 | 12.88 | 0 | 0 |
| 8 | 32 | 32 | 62.87 | 30420.76 | 30485.04 | 28250.97 | 28283.77 | 242 | 242 | 80.70 | 28.16 | 0 | 0 |
| 16 | 64 | 64 | 70.45 | 54114.53 | 54269.97 | 49977.44 | 50077.20 | 216 | 216 | 159.70 | 52.94 | 0 | 0 |

## Findings

- The local Docker comparison is stable functionally: all submitted jobs reached `accepted`, and sandbox backend/cleanup error deltas stayed at zero.
- Throughput plateaus after 4 slots in this environment, while queue age and p95 latency grow sharply. This points to local Docker/OrbStack runner startup or host scheduling limits, not an application correctness failure.
- No runsc production conclusion can be drawn from this machine because `runsc` is not registered.

## Next Actions

- Run the same `SOJ_DOCKER_RUNNER_RUNTIME=runsc make smoke-runner-capacity` command on a Linux judge node with gVisor installed.
- Capture the same table for runsc and compare against the default Docker baseline above.
- If runsc also plateaus below target, profile container startup cost and consider a warmed runner pool or per-attempt case runner reuse.
