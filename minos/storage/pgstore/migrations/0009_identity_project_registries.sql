-- +goose Up
-- Slice G — identity registry, pairing requests, project registry.
-- Per docs/phase-2-plan.md §6 + architecture.md §6 Command Intake and
-- Pairing / Project Registry. Greenfield: Phase 1's AdminIdentity scalar
-- and ProjectConfig singleton are replaced wholesale; bootstrap on first
-- start populates the tables from /etc/minos/config.json so the operator
-- still gets a functioning single-admin / single-project deployment.
--
-- Schema-per-project (§2 D2) is structurally decided but not implemented
-- here; deferred until multi-project demand materializes. All tables in
-- this migration live in the singleton minos schema.

-- One row per (surface, surface_id) tuple. Status starts active for
-- bootstrap-seeded identities; active|revoked|pending for paired ones.
-- Capability overrides are JSONB arrays (added/removed beyond the
-- role's baseline).
CREATE TABLE minos.identities (
    id                  uuid        PRIMARY KEY,
    surface             text        NOT NULL,
    surface_id          text        NOT NULL,
    role                text        NOT NULL,
    status              text        NOT NULL,
    capabilities_added  jsonb       NOT NULL DEFAULT '[]'::jsonb,
    capabilities_removed jsonb      NOT NULL DEFAULT '[]'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (surface, surface_id),
    CHECK (role IN ('admin', 'commissioner', 'observer', 'system')),
    CHECK (status IN ('active', 'revoked', 'pending'))
);

CREATE INDEX identities_status_idx ON minos.identities(status);
CREATE INDEX identities_role_idx ON minos.identities(role);

-- Pending-pairing state. Token is the operator-facing handle the
-- requester quotes when an admin approves; it is short-lived.
CREATE TABLE minos.pairing_requests (
    token         text        PRIMARY KEY,
    surface       text        NOT NULL,
    surface_id    text        NOT NULL,
    note          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    UNIQUE (surface, surface_id)
);

CREATE INDEX pairing_requests_expires_idx ON minos.pairing_requests(expires_at);

-- Project registry. Phase 2 single-project = one row; Phase 3 multi-
-- project adds rows + per-project egress + per-project Hermes plugin
-- selection. JSONB blobs capture the full Phase 1 ProjectConfig shape
-- so we don't need a column-per-field migration when fields evolve.
CREATE TABLE minos.projects (
    id                            text        PRIMARY KEY,
    name                          text        NOT NULL,
    backend                       text        NOT NULL,
    plugin_image                  text        NOT NULL,
    argus_sidecar_image           text        NOT NULL DEFAULT '',
    agent_mention_handle          text        NOT NULL DEFAULT '',
    default_workspace_size        text        NOT NULL,
    default_base_branch           text        NOT NULL,
    default_repo_url              text        NOT NULL DEFAULT '',
    branch_protection_required    boolean     NOT NULL DEFAULT false,
    mnemosyne_retention_days      integer     NOT NULL DEFAULT 0,
    default_budget                jsonb       NOT NULL DEFAULT '{}'::jsonb,
    communication                 jsonb       NOT NULL DEFAULT '{}'::jsonb,
    thread_parent                 text        NOT NULL DEFAULT '',
    capabilities                  jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at                    timestamptz NOT NULL DEFAULT now(),
    updated_at                    timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS minos.projects;
DROP TABLE IF EXISTS minos.pairing_requests;
DROP TABLE IF EXISTS minos.identities;
