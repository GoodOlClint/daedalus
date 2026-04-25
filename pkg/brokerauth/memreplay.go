package brokerauth

import (
	"context"
	"sync"
	"time"
)

// MemReplayStore is an in-memory ReplayStore suitable for single-broker
// deployments. Phase 2 brokers running on Crete share Postgres, so a
// future PgReplayStore lands when cross-broker replay protection is
// needed. For now each broker tracks its own JTIs in-process.
type MemReplayStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

// NewMemReplayStore returns an empty store.
func NewMemReplayStore() *MemReplayStore {
	return &MemReplayStore{entries: map[string]time.Time{}}
}

// Seen reports whether jti was already recorded; on first sighting it
// records jti with the supplied expiry and a sweep of stale entries.
// The store grows bounded by the number of unique jtis whose tokens
// haven't yet expired.
func (s *MemReplayStore) Seen(_ context.Context, jti string, expires time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.entries {
		if exp.Before(now) {
			delete(s.entries, k)
		}
	}
	if _, exists := s.entries[jti]; exists {
		return true, nil
	}
	s.entries[jti] = expires
	return false, nil
}

// Size returns the current entry count — useful for diagnostics + tests.
func (s *MemReplayStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
