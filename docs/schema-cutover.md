# PostgreSQL schema baseline

Fresh installations run `soj-migrate up` as usual. The canonical schema is
`internal/migrations/000001_baseline.up.sql`. An initialized database without
the baseline marker is rejected; the application does not automatically reset
or upgrade an existing database to this baseline.

## Existing installations

1. Upgrade to the previous release's complete migration chain through
   `000010_role_permissions`. Take and verify a restorable PostgreSQL backup.
   Rehearse the cutover against a copy and keep the matching old binaries.
2. Schedule downtime. Stop API, workers, judge agents, and any other database
   writers. Record in-flight work and drain normal judge delivery where possible.
3. Run the one-time cutover against the intended database:
   `psql "$SOJ_DATABASE_DSN" -X -v ON_ERROR_STOP=1 -f scripts/cutover-schema-baseline.sql`.
   The script checks the expected migration history, locks the affected tables,
   migrates effective result pointers and contest audit events, and commits the
   new schema and baseline marker atomically. It aborts if removed placeholder
   fields contain unexpected data or the legacy result contradicts its attempt.
4. Run the new `soj-migrate up`, start the new services, and verify representative
   submission results, rejudges, contest scoreboards, and role audit events.
   If you must revert after the cutover commits, restore the pre-cutover backup
   and start the previous binaries. Do not start old binaries on the new schema.

Old per-attempt and per-case history is retained. Superseded scoreboard history
is not pruned by this cutover. The temporary old-schema upgrade script is not
part of the application migration chain.
