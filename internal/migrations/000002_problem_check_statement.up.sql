ALTER TABLE problem_check_runs
    ADD COLUMN statement_id bigint REFERENCES problem_statements(id);

CREATE INDEX problem_check_runs_statement_id_idx ON problem_check_runs (statement_id);
CREATE INDEX problem_check_runs_publish_gate_idx
    ON problem_check_runs (problem_id, statement_id, testcase_set_id, finished_at DESC, id DESC)
    WHERE status = 'completed';
