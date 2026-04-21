// Package core defines the Mnemosyne memory-and-context service contract
// per architecture.md §14. Phase 1 ships with the pgstore backend against
// the shared Postgres LXC; memstore exists for tests and local dev.
//
// Scope of this package (Phase 1 Slice C minimum):
//   - Store interface for run-record persistence and context assembly
//   - Sanitization primitives callers apply before persistence
//   - Simple "concatenate last-N summaries" context assembly — Phase 2 refines
//     into fact extraction, pgvector semantic lookup, and trust-marker preservation
package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by lookups that resolve to no record.
var ErrNotFound = errors.New("mnemosyne: not found")

// Outcome records how a run terminated.
type Outcome string

const (
	OutcomeCompleted  Outcome = "completed"
	OutcomeFailed     Outcome = "failed"
	OutcomeTerminated Outcome = "terminated"
)

// RunRecord is the unit of memory — one pod execution. Body is the
// sanitized JSON blob the plugin dumped at teardown.
type RunRecord struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	RunID     uuid.UUID
	ProjectID string
	TaskType  string
	Outcome   Outcome
	Summary   string
	Body      json.RawMessage
	CreatedAt time.Time
}

// Context is a Mnemosyne-assembled blob Minos injects into a fresh task
// envelope so the new run starts with prior-run context. Phase 1 assembles
// by concatenating last-N summaries; Phase 2 uses pgvector semantic lookup
// and preserves trust markers.
type Context struct {
	// Ref is an opaque identifier the task envelope carries
	// (`envelope.ContextRef`). Not a UUID yet — can evolve.
	Ref string
	// Body is the assembled context as plain text the plugin reads.
	Body string
	// PriorRuns is how many prior runs contributed to this context.
	PriorRuns int
}

// Store is the persistence + lookup contract.
type Store interface {
	// StoreRun persists a run record. Callers MUST have already run
	// Sanitize on the body against their injected-credential set.
	StoreRun(ctx context.Context, rec *RunRecord) error

	// GetContext assembles the per-project prior-run context for a new
	// task. Returns (nil, nil) when there is no prior context to inject.
	GetContext(ctx context.Context, projectID, taskType string) (*Context, error)

	// GetRunsForTask returns every run record bound to a task, newest
	// first.
	GetRunsForTask(ctx context.Context, taskID uuid.UUID) ([]*RunRecord, error)
}
