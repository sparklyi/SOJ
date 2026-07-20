# SOJ v2 API Guide

This guide summarizes the WP1 contract baseline in `api/openapi.yaml`.

## Ownership

- WP1 owns shared schemas, response envelopes, pagination, error responses, and this guide.
- WP2 owns auth paths and admin user paths.
- WP3 owns problem, statement, testcase-set, problem check, and problem stats paths.
- WP4 owns submission, run, and admin language paths.
- WP5 owns contest, registration, and scoreboard paths.

## Response Envelope

All successful `2xx` responses use:

```json
{
  "data": {},
  "error": null,
  "request_id": "req_..."
}
```

Error responses use:

```json
{
  "data": null,
  "error": {
    "code": "validation.failed",
    "message": "validation failed"
  },
  "request_id": "req_..."
}
```

Exceptions:

- `204` responses have an empty body.
- Explicitly empty `202` responses have an empty body. In the baseline this applies to `POST /api/v1/admin/languages/sync`.

## Pagination

List endpoints use `page` and `page_size`. The response `data` object contains:

```json
{
  "items": [],
  "page": 1,
  "page_size": 20,
  "total": 0
}
```

Submission list endpoints may later add cursor pagination, but the initial contract keeps `page` and `page_size`.

## Endpoint Groups

Auth:

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `GET /api/v1/me`

Problems:

- `GET /api/v1/problems`
- `POST /api/v1/problems`
- `GET /api/v1/problems/{id}`
- `GET /api/v1/problems/{id}/statement`
- `PATCH /api/v1/problems/{id}`
- `DELETE /api/v1/problems/{id}`
- `POST /api/v1/problems/{id}/statement`
- `POST /api/v1/problems/{id}/testcase-sets`
- `POST /api/v1/problems/{id}/checks`
- `GET /api/v1/problems/{id}/checks/{check_id}`
- `GET /api/v1/problems/{id}/authoring`
- `GET /api/v1/problems/{id}/stats`

Use `GET /api/v1/problems?mine=true` with an authenticated request to list only problems owned by the current user. This is the supported source for authoring consoles.

Problem checks:

- Testcase archive uploads allow at most 128 MiB of compressed ZIP data in a 129 MiB multipart request. Upload validation, problem checks, and worker parsing share limits of 16 MiB per file, 128 MiB total uncompressed data, 2048 files, and a 200:1 per-file compression ratio.
- `POST /api/v1/problems/{id}/checks` synchronously validates the current ready testcase set for the problem. The response is a `201` envelope whose `data` is a `ProblemCheckRun`.
- `GET /api/v1/problems/{id}/checks/{check_id}` returns the persisted check run and its findings in a `200` envelope.
- Check endpoints are owner/admin/root scoped. They use the same error envelope as the rest of the API.
- `ProblemCheckRun.summary` includes archive counts, finding counts by severity, storage/zip readability flags, and `valid`.
- `ProblemCheckRun.findings` contains stable finding `code`, `severity`, `message`, optional `case_index` and `testcase_key`, and structured `details`.
- `GET /api/v1/problems/{id}/authoring` returns the current statement, current ready testcase set, the latest completed check for that exact statement and testcase-set pair, and stable publish blockers.
- Publishing with `PATCH /api/v1/problems/{id}` and `status=published` requires a completed check whose summary is valid for the current statement and testcase set. Saving a replacement statement or testcase archive invalidates the previous check for publication.

Submissions and runs:

- `POST /api/v1/submissions`
- `GET /api/v1/submissions`
- `GET /api/v1/submissions/mine`
- `GET /api/v1/submissions/{id}`
- `POST /api/v1/runs`
- `GET /api/v1/runs/{id}`

`GET /api/v1/submissions/mine` uses opaque cursor pagination for a caller's own history. Send the optional `cursor` returned as `next_cursor` with a `page_size` of at most 100. Its response contains `items` and an optional `next_cursor`, rather than a page number or `total`; use it for long histories to avoid deep offsets and a separate count query.

Languages:

- `GET /api/v1/languages`

Submission result visibility:

- `result` is the current safe projection from `submission_results`; complete evidence stays in `judge_attempts` and `judge_case_results`.
- `cases` contains sanitized case summaries only. It does not expose testcase keys, artifact ids, storage keys, raw manifest, stdout, stderr, or full diffs.
- `admin_diagnostics` is present only for admin/root or contest owner contexts that are explicitly allowed by the service policy.
- ACM contest submissions after freeze return `visibility: "frozen"` and omit `result`, `cases`, and `admin_diagnostics` for contestants until final visibility opens.
- Admin/root and contest owner views may still inspect frozen contest submissions for operations and adjudication.

Rejudge batches:

- `POST /api/v1/rejudge-batches`
- `GET /api/v1/rejudge-batches`
- `GET /api/v1/rejudge-batches/{id}`
- `POST /api/v1/rejudge-batches/{id}/cancel`
- A create request must provide exactly one of `problem_id` or `contest_id`, plus a non-empty `reason`.
- Problem batches select terminal non-contest submissions only. Contest batches select terminal submissions from an ended contest.
- Batch membership is fixed at creation. Each item records its submission, reused judge task, new attempt, status, and timestamps.
- While a submission is queued or running for rejudge, the API omits its previous result projection even though historical attempts remain stored.
- Cancellation restores undispatched submissions to their previous projected result. Attempts already running may finish, and completed results are not rolled back.

Contests:

- `GET /api/v1/contests`
- `POST /api/v1/contests`
- `GET /api/v1/contests/{id}`
- `PATCH /api/v1/contests/{id}`
- `DELETE /api/v1/contests/{id}`
- `POST /api/v1/contests/{id}/registrations`
- `GET /api/v1/contests/{id}/scoreboard`

Contest create/update notes:

- `problems` is an ordered array of `{ "problem_id": 1, "alias": "A" }`; `sort_order` is generated from array order and returned in responses.
- Contest responses include `scoring_mode`, currently `acm`, `registered` for the current authenticated user, and enriched `problems[].title` values when the linked problem is available.
- `invite_code` is required when creating a private contest or switching a contest to private without an existing invite code.
- Private contest list/detail access is limited to owner, admin/root, or active registrants.

Admin:

- `GET /api/v1/admin/users`
- `PATCH /api/v1/admin/users/{id}`
- `GET /api/v1/admin/languages`
- `POST /api/v1/admin/languages/sync`
- `PATCH /api/v1/admin/languages/{id}`

## Status And Error Baseline

Common HTTP statuses:

- `200`, `201`, `202`, `204`
- `400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`

Core error codes are enumerated in `components.schemas.Error`.
