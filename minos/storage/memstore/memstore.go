// Package memstore provides an in-memory storage.Store for tests and local
// development without a Postgres dependency. Not a production target.
package memstore

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/GoodOlClint/daedalus/minos/storage"
)

// Store is an in-memory implementation of storage.Store.
type Store struct {
	mu    sync.RWMutex
	tasks map[uuid.UUID]*storage.Task
	now   func() time.Time
}

// New returns a Store with an optional clock override for deterministic
// tests. If now is nil, time.Now().UTC() is used.
func New(now func() time.Time) *Store {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{
		tasks: make(map[uuid.UUID]*storage.Task),
		now:   now,
	}
}

// InsertTask implements storage.Store.
func (s *Store) InsertTask(_ context.Context, t *storage.Task) error {
	if t == nil {
		return fmt.Errorf("%w: nil task", storage.ErrConflict)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[t.ID]; exists {
		return fmt.Errorf("%w: task %s already exists", storage.ErrConflict, t.ID)
	}
	copy := *t
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = s.now()
	}
	if copy.State == "" {
		copy.State = storage.StateQueued
	}
	s.tasks[copy.ID] = &copy
	return nil
}

// GetTask implements storage.Store.
func (s *Store) GetTask(_ context.Context, id uuid.UUID) (*storage.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, id)
	}
	clone := *t
	return &clone, nil
}

// ListTasks implements storage.Store.
func (s *Store) ListTasks(_ context.Context, states []storage.State, limit int) ([]*storage.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	allowed := make(map[storage.State]struct{}, len(states))
	for _, st := range states {
		allowed[st] = struct{}{}
	}
	out := make([]*storage.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if len(allowed) > 0 {
			if _, ok := allowed[t.State]; !ok {
				continue
			}
		}
		clone := *t
		out = append(out, &clone)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// TransitionTask implements storage.Store.
func (s *Store) TransitionTask(_ context.Context, id uuid.UUID, to storage.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, id)
	}
	if !validTransition(t.State, to) {
		return fmt.Errorf("%w: %s → %s", storage.ErrConflict, t.State, to)
	}
	t.State = to
	now := s.now()
	switch to {
	case storage.StateRunning:
		t.StartedAt = &now
	case storage.StateCompleted, storage.StateFailed:
		t.FinishedAt = &now
	}
	return nil
}

// SetTaskRun implements storage.Store.
func (s *Store) SetTaskRun(_ context.Context, id, runID uuid.UUID, podName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, id)
	}
	t.RunID = &runID
	pn := podName
	t.PodName = &pn
	return nil
}

// validTransition encodes the Phase 1 state machine per storage.Store docs.
func validTransition(from, to storage.State) bool {
	switch from {
	case storage.StateQueued:
		return to == storage.StateRunning || to == storage.StateFailed
	case storage.StateRunning:
		return to == storage.StateCompleted || to == storage.StateFailed
	default:
		return false
	}
}
