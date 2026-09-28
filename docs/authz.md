# Authorization Model

SOJ uses a flat permission directory plus ownership checks. Roles grant
permissions; some gates additionally require that the actor owns the resource.

## Source of truth

`internal/authz/permissions.go` is the single source of truth for

- the permission constants and the ordered directory (`AllPermissions` / `Catalog`),
- the named permission combinations shared with the web client (`Combos`).

The role-to-permission mapping is *not* in Go any more. Since phase 2 it lives
in the `role_permissions` table and is edited through the admin matrix; see
[Dynamic role permissions](#dynamic-role-permissions-phase-2) below. The
directory stays the only Go source of permission codes, so a code can never
appear in `role_permissions` without a matching `Catalog()` entry (the resolver
and the integration tests reject it).

`SOJ-web` mirrors the same directory in `lib/auth/gates.ts`. The two copies are
not generated from one another; each repository locks its own copy with a test
(`internal/authz/catalog_test.go` and `internal/authz/consumers_test.go`).

`Catalog()` returns a `PermissionSpec` per permission with its `Scope`, whether
it is `Delegable`, and the production `Consumer` that reads it. `Consumer` must
be non-empty; `consumers_test.go` fails the build if any permission constant is
not referenced by a non-test file under `internal/`.

## Roles

| Role | Seeded permissions (`role_permissions`) |
| --- | --- |
| `user` | none |
| `author` | `problem.create`, `problem.edit_own`, `problem.submit_review` |
| `reviewer` | `problem.review`, `problem.publish` |
| `operator` | `submission.rejudge`, `judge.inspect` |
| `contest_staff` | `contest.read` |
| `contest_manager` | `contest.read`, `contest.manage` |
| `contest_judge` | `contest.read`, `contest.judge` |
| `admin`, `root` | every permission (full access, synthesized) |

The table above is the `000010_role_permissions.up.sql` seed and can change at
runtime. `admin` and `root` are full-access roles defined by `fullAccessRoles`:
they intentionally have no `role_permissions` rows and the resolver expands
them to the whole directory. `root` is the break-glass account. Neither role has
an `Actor.Admin()` / `Actor.Root()` helper any more: every production gate
authorizes through a permission.

## Ownership axis

Ownership is separate from permissions and cannot be granted by a role:

- a problem owner may edit and submit their own problem (`problem.edit_own`,
  `problem.submit_review`),
- a submission owner may read their own submission and source,
- a run owner may read their own run,
- a contest owner may manage and read their own contest.

`problem.manage_all` bypasses problem ownership; `judge.inspect` bypasses
submission, source, run, and diagnostics ownership.

## Named combinations

`Combos()` exposes the combinations that are shared with the web client. They
are not permission points; each combination is an "any of" set unless its
consumer documents a stricter rule.

| Name | Permissions |
| --- | --- |
| `problem.authoring.access` | `problem.create`, `problem.review`, `problem.manage_all` |
| `problem.review.queue` | `problem.review`, `problem.manage_all` |
| `problem.review.decide` | `problem.review`, `problem.publish`, `problem.manage_all` |

`CanAccessAuthoring` and `CanViewReviewQueue` accept any member of their
combination. `CanDecideReview` is intentionally stricter than its combination:
`problem.manage_all` is a direct allow, an owner is rejected before any
permission check, and everyone else must hold both `problem.review` and
`problem.publish`.

## Exceptions and public capabilities

These surfaces succeed without a permission point and are therefore absent from
the directory:

- `submission.run`: playground runs only require an authenticated actor; see the
  comment on `RunService.CreateRun`.
- public problem reads: `published` + `public` problems are readable by anyone
  (`problem.canReadProblem`).
- public contest reads: `public` contests are readable by anyone
  (`contest.ContestReader`).
- `GET /languages`: the public language list (`LanguageService.ListPublicLanguages`).

## Rejudge paths

There are two authorized rejudge paths:

- Global problem rejudge: `problem.RBACProblemPolicy.CanRejudge` requires
  `submission.rejudge` (or full access), and the problem must be published.
- Contest rejudge: `contest.AuthorizeContestRejudge` requires `contest.judge`,
  or contest ownership / `contest.manage`, and the contest must be ended.

Both paths converge on `submission.RejudgeService`, whose batch list is
restricted to the actor's own batches unless they hold `submission.rejudge`.

## Permission → real gate

This table matches the `Consumer` field of `Catalog()`.

| Permission | Scope | Delegable | Real gate |
| --- | --- | --- | --- |
| `audit.read` | global | no | `audit.Service.ListEvents` |
| `contest.create` | global | yes | `contest.requireContestCreator` |
| `contest.judge` | contest | yes | `contest.AuthorizeContestRejudge` |
| `contest.manage` | contest | yes | `contest.canManageContest` |
| `contest.manage_all` | global | yes | `contest.requireContestCreator` / `canManageAllContests` |
| `contest.read` | contest | yes | `contest.ContestReader` |
| `judge.inspect` | global | yes | `submission.SubmissionReader` (any submission / diagnostics) + `submission.RunService.GetRun` |
| `problem.create` | global | yes | `problem.RBACProblemPolicy.CanCreate` |
| `problem.edit_own` | global | yes | `problem.RBACProblemPolicy.CanEdit` |
| `problem.manage_all` | global | yes | `problem.RBACProblemPolicy` (bypass) + `problem.canReadProblem` |
| `problem.publish` | global | yes | `problem.RBACProblemPolicy.CanDecideReview` |
| `problem.review` | global | yes | `problem.RBACProblemPolicy.CanViewReviewQueue` / `CanDecideReview` / `CanViewReviewEvents` |
| `problem.submit_review` | global | yes | `problem.RBACProblemPolicy.CanSubmitReview` |
| `role.grant` | global | no | `user.Service.GrantRole` |
| `role.permission.manage` | global | no | `user.Service.RolePermissionMatrix` / `ReplaceRolePermissions` |
| `role.revoke` | global | no | `user.Service.RevokeRole` |
| `submission.rejudge` | global | yes | `problem.RBACProblemPolicy.CanRejudge` + `submission.RejudgeService` |
| `system.manage` | global | no | `submission.LanguageService` (language administration) |
| `user.manage` | global | no | `user.Service.ListUsers` / `ListUsersByCursor` / `UpdateUser` |

`Delegable=false` means the permission may only be held through `admin`/`root`;
the phase-2 permission matrix must not assign it to a scoped role.

## Dynamic role permissions (phase 2)

The database is the runtime fact for the role→permission mapping. The admin
console edits it and the next request sees the new set; there is no snapshot,
no cache, and no fallback to a compile-time map.

### Data model and writes

- `role_permissions (role_code, permission_code)` has set semantics: one row per
grant. `role_code` references `roles(code)`; `permission_code` is validated by
the Go directory, not by a database foreign key (the directory stays the single
source of truth).
- Each write replaces a role's *whole* set: `DELETE` then `INSERT ... unnest`.
  A repeated write with the same normalized set is an idempotent no-op and does
  not append an audit event.
- History lives in `audit_events`, not in `role_permissions`. Every real change
  appends one `role.permissions.updated` event (`object_type = 'role'`,
  `object_id = NULL`, `metadata.role/before/after`). Concurrent writes to the
  same role are last-write-wins.

### Runtime read path

- The request middleware calls `ResolveActor`, which joins `users` →
  `user_role_assignments` → `role_permissions` in one query and attaches the
  resulting `Actor.Permissions`. Any `admin`/`root` role expands to
  `AllPermissions()`.
- `internal/authz.NewSubject` copies `Actor.Permissions`; it never derives
  permissions from roles. An actor with no resolved permissions has none
  (fail closed).
- Contest-scoped roles are resolved per contest by `ContestRoleStore` and merged
  into the actor the same way, so their `contest.*` permissions also come from
  `role_permissions`.
- `ListAccess` / `ListAccessForUsers` use the same join for `/me` and the admin
  user list.

### Matrix API

`GET /admin/roles` returns the permission directory plus every role's current
set. `PUT /admin/roles/:role/permissions` replaces one role's set and validates
(see the table below). Role scope is derived from the role code
(`auth.IsGlobalRole` / `auth.IsContestRole`), never from `roles.scope`.

| Rule | Error code |
| --- | --- |
| Unknown role | `role.not_found` |
| `admin` / `root` | `role.locked` |
| Unknown permission code | `role.permission_invalid` |
| `Delegable=false` permission | `role.permission_not_delegable` |
| Role scope ≠ permission scope | `role.permission_scope_mismatch` |
| Missing or >500-byte reason | `role.permission_reason_required` |

`permissions: null` (field missing) is a 400; `permissions: []` is legal and
clears the role. The request is gated by `role.permission.manage`, which only
full-access roles hold.

### Removing a permission from the directory

The directory is the source of truth, so a migration that removes a permission
constant must delete the matching `role_permissions` rows in the same migration
(otherwise the resolver would reject an unknown code). The integration test that
asserts `role_permissions.permission_code ⊆ Catalog()` is the tripwire for that
cleanup.

## Phase 1 cleanup

The first pass removed permissions that no production gate read
(`problem.read`, `problem.testcase.manage_own`, `problem.check_own`,
`submission.create`, `submission.read_own`, `contest.join`) and split the audit
read surface onto its own `audit.read` permission. `system.manage` now gates
only language administration.
