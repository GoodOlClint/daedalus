-- +goose Up
-- Bind a task row to the PR the pod opened. Populated by the pod's
-- POST /tasks/{id}/pr callback (Slice B). The webhook handler looks up
-- the task row by this URL when a pull_request event arrives.
ALTER TABLE minos.tasks ADD COLUMN pr_url text NULL;
CREATE UNIQUE INDEX tasks_pr_url_idx ON minos.tasks(pr_url) WHERE pr_url IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS minos.tasks_pr_url_idx;
ALTER TABLE minos.tasks DROP COLUMN IF EXISTS pr_url;
