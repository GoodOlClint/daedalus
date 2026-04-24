-- +goose Up
-- Track when a task last transitioned state (for hibernation TTL math) and
-- whether Minos has already reminded reviewers about a stale
-- awaiting-review task (so reminders don't spam the thread).
ALTER TABLE minos.tasks ADD COLUMN state_changed_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE minos.tasks ADD COLUMN reminded_at timestamptz NULL;

-- Backfill: existing rows (if any) get their state_changed_at set to the
-- newest meaningful timestamp we already have, in priority order:
--   finished_at > started_at > created_at.
UPDATE minos.tasks
   SET state_changed_at = COALESCE(finished_at, started_at, created_at)
 WHERE state_changed_at = now();

CREATE INDEX tasks_awaiting_review_idx ON minos.tasks(state_changed_at) WHERE state = 'awaiting-review';

-- +goose Down
DROP INDEX IF EXISTS minos.tasks_awaiting_review_idx;
ALTER TABLE minos.tasks DROP COLUMN IF EXISTS reminded_at;
ALTER TABLE minos.tasks DROP COLUMN IF EXISTS state_changed_at;
