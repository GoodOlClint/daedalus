// Package replay provides ReplayStore implementations that Cerberus
// verification plugins use to dedupe webhook deliveries per security.md §2.
//
// Phase 1 ships two: MemStore for tests and local dev, PGStore backed by
// the shared Postgres LXC's cerberus schema (migration 0003).
package replay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MemStore is an in-memory replay store. Bounded by nothing — tests and
// short-lived dev only.
type MemStore struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	window  time.Duration
	nowFunc func() time.Time
}

// NewMemStore returns a MemStore with the given retention window; deliveries
// older than window are garbage-collected on each Seen call. Zero window
// disables GC.
func NewMemStore(window time.Duration) *MemStore {
	return &MemStore{
		seen:    map[string]time.Time{},
		window:  window,
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

// Seen implements the ReplayStore contract for github.Verifier et al.
// The key is "<source>|<delivery>" so the same delivery ID from different
// sources doesn't collide.
func (s *MemStore) Seen(_ context.Context, key string, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(at)
	if _, ok := s.seen[key]; ok {
		return true, nil
	}
	s.seen[key] = at
	return false, nil
}

func (s *MemStore) gcLocked(now time.Time) {
	if s.window <= 0 {
		return
	}
	cutoff := now.Add(-s.window)
	for k, t := range s.seen {
		if t.Before(cutoff) {
			delete(s.seen, k)
		}
	}
}

// PGStore is the Postgres-backed replay store. Keyed by (source, delivery)
// matching the cerberus.webhook_deliveries schema.
type PGStore struct {
	pool   *pgxpool.Pool
	source string
	window time.Duration
}

// NewPGStore wraps a pool with the source tag every Seen call will use.
// window is used by Purge for periodic housekeeping; Seen itself always
// inserts and relies on the primary-key conflict to detect duplicates.
func NewPGStore(pool *pgxpool.Pool, source string, window time.Duration) *PGStore {
	return &PGStore{pool: pool, source: source, window: window}
}

// Seen inserts (source, key, now); returns true if the row already exists.
func (s *PGStore) Seen(ctx context.Context, key string, at time.Time) (bool, error) {
	const q = `
INSERT INTO cerberus.webhook_deliveries (source, delivery_id, received_at)
VALUES ($1, $2, $3)
ON CONFLICT (source, delivery_id) DO NOTHING`
	ct, err := s.pool.Exec(ctx, q, s.source, key, at)
	if err != nil {
		return false, fmt.Errorf("cerberus replay: insert: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return true, nil
	}
	return false, nil
}

// Purge deletes delivery rows older than window. Operators or a background
// worker should call it periodically.
func (s *PGStore) Purge(ctx context.Context, at time.Time) error {
	if s.window <= 0 {
		return nil
	}
	cutoff := at.Add(-s.window)
	_, err := s.pool.Exec(ctx, `DELETE FROM cerberus.webhook_deliveries WHERE received_at < $1`, cutoff)
	if err != nil {
		return fmt.Errorf("cerberus replay: purge: %w", err)
	}
	return nil
}

