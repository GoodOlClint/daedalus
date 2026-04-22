package memstore

import (
	"time"

	"github.com/google/uuid"
)

// AgeTask rewinds StateChangedAt on an existing task. Intended for tests
// that need to simulate hibernation duration without waiting wall-clock
// time. Returns false when the task is not present. Lives in the main
// package (not an _test.go file) so cross-package tests can reach it.
func (s *Store) AgeTask(id uuid.UUID, newStateChangedAt time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	t.StateChangedAt = newStateChangedAt
	return true
}
