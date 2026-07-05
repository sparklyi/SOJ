# SOJ v2 API Guide

This guide summarizes the WP1 contract baseline in `api/openapi.yaml`.

## Ownership

- WP1 owns shared schemas, response envelopes, pagination, error responses, and this guide.
- WP2 owns auth paths and admin user paths.
- WP3 owns problem, statement, testcase-set, and problem stats paths.
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
- `GET /api/v1/problems/{id}/stats`

Submissions and runs:

- `POST /api/v1/submissions`
- `GET /api/v1/submissions`
- `GET /api/v1/submissions/{id}`
- `POST /api/v1/runs`
- `GET /api/v1/runs/{id}`

Submission result visibility:

- `result` is the current safe projection from `submission_results`; complete evidence stays in `judge_attempts` and `judge_case_results`.
- `cases` contains sanitized case summaries only. It does not expose testcase keys, artifact ids, storage keys, raw manifest, stdout, stderr, or full diffs.
- `admin_diagnostics` is present only for admin/root or contest owner contexts that are explicitly allowed by the service policy.
- ACM contest submissions after freeze return `visibility: "frozen"` and omit `result`, `cases`, and `admin_diagnostics` for contestants until final visibility opens.
- Admin/root and contest owner views may still inspect frozen contest submissions for operations and adjudication.

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
