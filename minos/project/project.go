// Package project is Minos's persistence layer for the project
// registry per architecture.md §6 Project Registry. Store is
// implemented by memstore (tests/local-dev) and pgstore (production).
//
// Project value type lives in pkg/project so non-Minos consumers can
// import it without dragging the Postgres dependency.
package project

import (
	"context"

	prj "github.com/zakros-hq/zakros/pkg/project"
)

// Store is the project-registry persistence contract.
type Store interface {
	// Get returns the project by ID, or prj.ErrNotFound.
	Get(ctx context.Context, id string) (*prj.Project, error)

	// List returns every project, alphabetical by ID. Phase 2 single-
	// project = one entry; useful for the future admin UI.
	List(ctx context.Context) ([]*prj.Project, error)

	// Insert persists a new project. Returns prj.ErrAlreadyExists on
	// duplicate ID.
	Insert(ctx context.Context, p *prj.Project) error

	// Upsert is bootstrap-side helper: insert when absent, update when
	// present. Used at Server startup to keep the singleton project's
	// row in sync with the on-disk config without a manual migration
	// step on every config change.
	Upsert(ctx context.Context, p *prj.Project) error
}
