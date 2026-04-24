-- +goose Up
-- Cerberus (Phase 1 library inside Minos; Phase 2 standalone broker).
-- Replay-ID store for webhook delivery dedup per security.md §2.
CREATE SCHEMA IF NOT EXISTS cerberus;

CREATE TABLE cerberus.webhook_deliveries (
    source       text NOT NULL,
    delivery_id  text NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, delivery_id)
);

CREATE INDEX webhook_deliveries_received_idx ON cerberus.webhook_deliveries(received_at);

-- +goose Down
DROP TABLE IF EXISTS cerberus.webhook_deliveries;
DROP SCHEMA IF EXISTS cerberus CASCADE;
