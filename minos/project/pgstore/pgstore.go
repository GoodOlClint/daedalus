// Package pgstore is the Postgres-backed project Store. Targets the
// shared Postgres LXC; uses minos.projects from migration 0009.
//
// Schema-per-project (phase-2-plan §2 D2) is structurally decided but
// not yet implemented — Slice G keeps every project row in the
// singleton minos schema until multi-project demand materializes.
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zakros-hq/zakros/minos/project"
	"github.com/zakros-hq/zakros/pkg/envelope"
	prj "github.com/zakros-hq/zakros/pkg/project"
)

// Store is the Postgres implementation.
type Store struct {
	pool *pgxpool.Pool
}

// New wraps an existing pool. Migration 0009 must have run.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Get(ctx context.Context, id string) (*prj.Project, error) {
	const q = `
SELECT id, name, backend, plugin_image, argus_sidecar_image,
       agent_mention_handle, default_workspace_size, default_base_branch,
       default_repo_url, branch_protection_required, mnemosyne_retention_days,
       default_budget, communication, thread_parent, capabilities
FROM minos.projects WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	out, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, prj.ErrNotFound
	}
	return out, err
}

func (s *Store) List(ctx context.Context) ([]*prj.Project, error) {
	const q = `
SELECT id, name, backend, plugin_image, argus_sidecar_image,
       agent_mention_handle, default_workspace_size, default_base_branch,
       default_repo_url, branch_protection_required, mnemosyne_retention_days,
       default_budget, communication, thread_parent, capabilities
FROM minos.projects ORDER BY id`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("project pgstore: list: %w", err)
	}
	defer rows.Close()
	var out []*prj.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Insert(ctx context.Context, p *prj.Project) error {
	cols, args, err := projectColumns(p)
	if err != nil {
		return err
	}
	const q = `
INSERT INTO minos.projects (
  id, name, backend, plugin_image, argus_sidecar_image,
  agent_mention_handle, default_workspace_size, default_base_branch,
  default_repo_url, branch_protection_required, mnemosyne_retention_days,
  default_budget, communication, thread_parent, capabilities
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`
	_ = cols
	if _, err := s.pool.Exec(ctx, q, args...); err != nil {
		if isUniqueViolation(err) {
			return prj.ErrAlreadyExists
		}
		return fmt.Errorf("project pgstore: insert: %w", err)
	}
	return nil
}

func (s *Store) Upsert(ctx context.Context, p *prj.Project) error {
	_, args, err := projectColumns(p)
	if err != nil {
		return err
	}
	const q = `
INSERT INTO minos.projects (
  id, name, backend, plugin_image, argus_sidecar_image,
  agent_mention_handle, default_workspace_size, default_base_branch,
  default_repo_url, branch_protection_required, mnemosyne_retention_days,
  default_budget, communication, thread_parent, capabilities
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  backend = EXCLUDED.backend,
  plugin_image = EXCLUDED.plugin_image,
  argus_sidecar_image = EXCLUDED.argus_sidecar_image,
  agent_mention_handle = EXCLUDED.agent_mention_handle,
  default_workspace_size = EXCLUDED.default_workspace_size,
  default_base_branch = EXCLUDED.default_base_branch,
  default_repo_url = EXCLUDED.default_repo_url,
  branch_protection_required = EXCLUDED.branch_protection_required,
  mnemosyne_retention_days = EXCLUDED.mnemosyne_retention_days,
  default_budget = EXCLUDED.default_budget,
  communication = EXCLUDED.communication,
  thread_parent = EXCLUDED.thread_parent,
  capabilities = EXCLUDED.capabilities,
  updated_at = now()`
	if _, err := s.pool.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("project pgstore: upsert: %w", err)
	}
	return nil
}

// projectColumns returns the column-value list in canonical order.
func projectColumns(p *prj.Project) ([]string, []any, error) {
	if p == nil || p.ID == "" {
		return nil, nil, errors.New("project pgstore: id required")
	}
	budgetJSON, err := json.Marshal(p.DefaultBudget)
	if err != nil {
		return nil, nil, err
	}
	commJSON, err := json.Marshal(p.Communication)
	if err != nil {
		return nil, nil, err
	}
	capsJSON, err := json.Marshal(p.Capabilities)
	if err != nil {
		return nil, nil, err
	}
	cols := []string{
		"id", "name", "backend", "plugin_image", "argus_sidecar_image",
		"agent_mention_handle", "default_workspace_size", "default_base_branch",
		"default_repo_url", "branch_protection_required", "mnemosyne_retention_days",
		"default_budget", "communication", "thread_parent", "capabilities",
	}
	args := []any{
		p.ID, p.Name, p.Backend, p.PluginImage, p.ArgusSidecarImage,
		p.AgentMentionHandle, string(p.DefaultWorkspaceSize), p.DefaultBaseBranch,
		p.DefaultRepoURL, p.BranchProtectionRequired, p.MnemosyneRetentionDays,
		budgetJSON, commJSON, p.ThreadParent, capsJSON,
	}
	return cols, args, nil
}

// scanProject reads one row into a Project. Common to Get + List.
func scanProject(row pgx.Row) (*prj.Project, error) {
	var p prj.Project
	var workspace string
	var budgetJSON, commJSON, capsJSON []byte
	if err := row.Scan(
		&p.ID, &p.Name, &p.Backend, &p.PluginImage, &p.ArgusSidecarImage,
		&p.AgentMentionHandle, &workspace, &p.DefaultBaseBranch,
		&p.DefaultRepoURL, &p.BranchProtectionRequired, &p.MnemosyneRetentionDays,
		&budgetJSON, &commJSON, &p.ThreadParent, &capsJSON,
	); err != nil {
		return nil, err
	}
	p.DefaultWorkspaceSize = envelope.WorkspaceSize(workspace)
	if len(budgetJSON) > 0 {
		_ = json.Unmarshal(budgetJSON, &p.DefaultBudget)
	}
	if len(commJSON) > 0 {
		_ = json.Unmarshal(commJSON, &p.Communication)
	}
	if len(capsJSON) > 0 {
		_ = json.Unmarshal(capsJSON, &p.Capabilities)
	}
	return &p, nil
}

func isUniqueViolation(err error) bool {
	type pgCoder interface{ SQLState() string }
	var pce pgCoder
	if errors.As(err, &pce) {
		return pce.SQLState() == "23505"
	}
	return false
}

var _ project.Store = (*Store)(nil)
