-- Retention needs two access paths, and both are the same concern: finding what a
-- self-run left behind.
--
-- 1. "Which finished runs are older than the window?" Without this it is a
--    sequential scan plus a sort of the whole runs table, every sweep, forever --
--    and the common answer is "none", so all of it is waste. Partial on purpose:
--    a queued or running run is never a retention candidate
--    (MarkStaleRunsSystemError terminates those), so the index has no reason to
--    carry them.
CREATE INDEX runs_retention_idx ON runs (created_at)
    WHERE status NOT IN ('queued', 'running');

-- 2. runs.source_artifact_id references artifacts, and PostgreSQL does not index
--    the referencing side of a foreign key. Two things need to look runs up by
--    artifact: deleting an artifact (the constraint check) and the orphan sweep,
--    which asks which run artifacts nothing points at. Without this index both
--    are a sequential scan of runs per row.
CREATE INDEX runs_source_artifact_idx ON runs (source_artifact_id)
    WHERE source_artifact_id IS NOT NULL;
