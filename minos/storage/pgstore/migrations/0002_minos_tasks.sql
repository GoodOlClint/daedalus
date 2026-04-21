-- +goose Up
CREATE TABLE minos.tasks (
    id            uuid PRIMARY KEY,
    parent_id     uuid NULL,
    project_id    text NOT NULL,
    task_type     text NOT NULL,
    backend       text NOT NULL,
    state         text NOT NULL,
    priority      smallint NOT NULL DEFAULT 0,
    envelope      jsonb NOT NULL,
    run_id        uuid NULL,
    pod_name      text NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    started_at    timestamptz NULL,
    finished_at   timestamptz NULL
);

CREATE INDEX tasks_state_idx ON minos.tasks(state);
CREATE INDEX tasks_project_idx ON minos.tasks(project_id);
CREATE INDEX tasks_created_idx ON minos.tasks(created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS minos.tasks;
