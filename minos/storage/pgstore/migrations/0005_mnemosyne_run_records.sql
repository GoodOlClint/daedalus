-- +goose Up
-- Mnemosyne run records per architecture.md §14. Phase 1 stores raw
-- run bodies (sanitized); Phase 2 adds fact-extraction tables and
-- pgvector embedding columns.
CREATE TABLE mnemosyne.run_records (
    id          uuid PRIMARY KEY,
    task_id     uuid NOT NULL,
    run_id      uuid NOT NULL,
    project_id  text NOT NULL,
    task_type   text NOT NULL,
    outcome     text NOT NULL,
    summary     text,
    body        jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (task_id, run_id)
);

CREATE INDEX run_records_project_idx ON mnemosyne.run_records(project_id, task_type, created_at DESC);
CREATE INDEX run_records_task_idx ON mnemosyne.run_records(task_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS mnemosyne.run_records;
