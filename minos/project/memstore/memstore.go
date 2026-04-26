// Package memstore is the in-memory project Store for tests/local-dev.
package memstore

import (
	"context"
	"sort"
	"sync"

	"github.com/zakros-hq/zakros/minos/project"
	prj "github.com/zakros-hq/zakros/pkg/project"
)

// Store is the in-memory implementation.
type Store struct {
	mu       sync.Mutex
	projects map[string]*prj.Project
}

// New returns an empty Store.
func New() *Store {
	return &Store{projects: map[string]*prj.Project{}}
}

func (s *Store) Get(_ context.Context, id string) (*prj.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.projects[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, prj.ErrNotFound
}

func (s *Store) List(_ context.Context) ([]*prj.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.projects))
	for id := range s.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*prj.Project, 0, len(ids))
	for _, id := range ids {
		cp := *s.projects[id]
		out = append(out, &cp)
	}
	return out, nil
}

func (s *Store) Insert(_ context.Context, p *prj.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.projects[p.ID]; exists {
		return prj.ErrAlreadyExists
	}
	cp := *p
	s.projects[p.ID] = &cp
	return nil
}

func (s *Store) Upsert(_ context.Context, p *prj.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *p
	s.projects[p.ID] = &cp
	return nil
}

var _ project.Store = (*Store)(nil)
