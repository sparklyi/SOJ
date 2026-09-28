-- Phase-2 dynamic role permissions. The mapping from role to permission moves
-- out of Go and into the database so the admin console can edit it and the next
-- request sees the new set.
--
-- Design notes:
--  * This table has set semantics: one row per (role, permission). The admin
--    write path replaces a role's whole set ("DELETE then INSERT"), never
--    patches one row, so the resulting set is always well-defined.
--  * Nothing here is an event ledger. History lives in audit_events
--    (action 'role.permissions.updated'); this table only holds the current set.
--  * permission_code is a plain text column validated by the Go directory
--    (internal/authz.Catalog): there is deliberately no permissions table and no
--    foreign key. The directory stays the single source of truth, and a
--    permission removed from it must be cleaned out of this table by the same
--    migration that removes it.
CREATE TABLE role_permissions (
    role_code       text NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    permission_code text NOT NULL CHECK (permission_code <> ''),
    PRIMARY KEY (role_code, permission_code)
);

-- Seed the phase-1 default mapping. admin and root hold every permission at
-- runtime, so they intentionally have no rows: the resolver expands them
-- instead. user is intentionally empty.
INSERT INTO role_permissions (role_code, permission_code)
VALUES
    ('author', 'problem.create'),
    ('author', 'problem.edit_own'),
    ('author', 'problem.submit_review'),
    ('reviewer', 'problem.review'),
    ('reviewer', 'problem.publish'),
    ('operator', 'submission.rejudge'),
    ('operator', 'judge.inspect'),
    ('contest_staff', 'contest.read'),
    ('contest_manager', 'contest.read'),
    ('contest_manager', 'contest.manage'),
    ('contest_judge', 'contest.read'),
    ('contest_judge', 'contest.judge');

-- The audit ledger gains one action and one object type. The 000009 CHECK
-- constraints are inline and therefore unnamed; Postgres generated the default
-- names below. object_id becomes nullable because a role has no numeric id:
-- the role code travels in metadata, so role events record object_id = NULL.
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_action_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_action_check CHECK (action IN (
    'user.role.granted',
    'user.role.revoked',
    'user.disabled',
    'user.enabled',
    'user.deleted',
    'language.enabled',
    'language.disabled',
    'problem.archived',
    'problem.restored',
    'contest.archived',
    'role.permissions.updated'
));

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_object_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_object_type_check
    CHECK (object_type IN ('user', 'language', 'problem', 'contest', 'role'));

ALTER TABLE audit_events ALTER COLUMN object_id DROP NOT NULL;
