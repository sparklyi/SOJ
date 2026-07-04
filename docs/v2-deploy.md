# SOJ v2 Docker Deployment

The first supported deployment target is Docker Compose. The v2 runtime is built and verified with Go 1.24.

```bash
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

The Compose stack starts PostgreSQL, Redis, MinIO, a one-shot migration job, a local seed job, API, and worker. Local development uses `SOJ_JUDGE_ENDPOINT=fake://accepted` and seeds one enabled fake language so the submit/worker smoke flow can run without a privileged judge sandbox.

Production can switch `SOJ_JUDGE_ENDPOINT` to Judge0 after adding Judge0 services and credentials. Do not reuse the local seed job for production language data.

## Files

- `Dockerfile.v2`: multi-stage image for `soj-api`, `soj-worker`, and `soj-migrate`.
- `deploy/docker-compose.yaml`: local v2 stack.
- `deploy/env/api.env.example`: environment variable reference.
- `deploy/smoke.sh`: end-to-end local smoke test against the running stack.
- `deploy/config.example.yaml`: human-readable config reference. The current runtime reads `SOJ_*` environment variables.

## Required Secrets

Set these through environment variables in real deployments:

- `SOJ_DATABASE_DSN`
- `SOJ_JWT_SECRET`
- `SOJ_STORAGE_ACCESS_KEY`
- `SOJ_STORAGE_SECRET_KEY`

Do not commit production DSNs, JWT secrets, or object storage credentials.

## Health Checks

- API liveness: `GET /healthz`
- API readiness: `GET /readyz`
- Worker liveness: `GET /healthz`
- Worker readiness: `GET /readyz`

Current API readiness checks PostgreSQL. Worker readiness is process-level only; Redis/object storage/judge readiness is exercised by `deploy/smoke.sh` and worker logs. Add broader dependency probes before using these health checks as production traffic gates.

## Local Reset

```bash
docker compose -f deploy/docker-compose.yaml down -v --remove-orphans
docker compose -f deploy/docker-compose.yaml up --build -d
./deploy/smoke.sh
```

`down -v` deletes PostgreSQL, Redis, and MinIO volumes.
