// Package memstore is the in-memory identity Store for tests and
// local-dev. Concurrency-safe via a single mutex; not optimized for
// scale but the identity registry never gets large in any deployment
// shape Phase 2 targets.
package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/zakros-hq/zakros/minos/identity"
	idn "github.com/zakros-hq/zakros/pkg/identity"
)

// Store is the in-memory implementation.
type Store struct {
	mu         sync.Mutex
	identities map[uuid.UUID]*idn.Identity
	pairings   map[string]*idn.PairingRequest
	now        func() time.Time
}

// New returns an empty Store. nowFn defaults to time.Now (UTC) when nil.
func New(nowFn func() time.Time) *Store {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &Store{
		identities: map[uuid.UUID]*idn.Identity{},
		pairings:   map[string]*idn.PairingRequest{},
		now:        nowFn,
	}
}

func (s *Store) LookupBySurface(_ context.Context, surface, surfaceID string) (*idn.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, i := range s.identities {
		if i.Surface == surface && i.SurfaceID == surfaceID {
			return cloneIdentity(i), nil
		}
	}
	return nil, idn.ErrNotFound
}

func (s *Store) Get(_ context.Context, id uuid.UUID) (*idn.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.identities[id]; ok {
		return cloneIdentity(i), nil
	}
	return nil, idn.ErrNotFound
}

func (s *Store) Insert(_ context.Context, in *idn.Identity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, i := range s.identities {
		if i.Surface == in.Surface && i.SurfaceID == in.SurfaceID {
			return idn.ErrAlreadyExists
		}
	}
	cp := cloneIdentity(in)
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	now := s.now()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	s.identities[cp.ID] = cp
	return nil
}

func (s *Store) SetRole(_ context.Context, id uuid.UUID, role idn.Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.identities[id]
	if !ok {
		return idn.ErrNotFound
	}
	if !role.IsValid() {
		return idn.ErrInvalidRole
	}
	if cur.Role == idn.RoleAdmin && role != idn.RoleAdmin {
		// Demoting the last active human admin would lock the operator
		// out — refuse.
		if s.countActiveAdminsLocked()-1 < 1 {
			return idn.ErrLastAdmin
		}
	}
	cur.Role = role
	cur.UpdatedAt = s.now()
	return nil
}

func (s *Store) Revoke(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.identities[id]
	if !ok {
		return idn.ErrNotFound
	}
	if cur.Status == idn.StatusRevoked {
		return nil
	}
	if cur.Role == idn.RoleAdmin {
		if s.countActiveAdminsLocked()-1 < 1 {
			return idn.ErrLastAdmin
		}
	}
	cur.Status = idn.StatusRevoked
	cur.UpdatedAt = s.now()
	return nil
}

func (s *Store) AddCapability(_ context.Context, id uuid.UUID, c idn.Capability) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.identities[id]
	if !ok {
		return idn.ErrNotFound
	}
	for _, existing := range cur.CapabilitiesAdded {
		if existing == c {
			return nil
		}
	}
	// Drop from removed if present (operator un-doing a previous
	// removal).
	cur.CapabilitiesRemoved = filterOut(cur.CapabilitiesRemoved, c)
	cur.CapabilitiesAdded = append(cur.CapabilitiesAdded, c)
	cur.UpdatedAt = s.now()
	return nil
}

func (s *Store) RemoveCapability(_ context.Context, id uuid.UUID, c idn.Capability) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.identities[id]
	if !ok {
		return idn.ErrNotFound
	}
	for _, existing := range cur.CapabilitiesRemoved {
		if existing == c {
			return nil
		}
	}
	cur.CapabilitiesAdded = filterOut(cur.CapabilitiesAdded, c)
	cur.CapabilitiesRemoved = append(cur.CapabilitiesRemoved, c)
	cur.UpdatedAt = s.now()
	return nil
}

func (s *Store) CountActiveAdmins(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.countActiveAdminsLocked(), nil
}

// countActiveAdminsLocked counts only HUMAN admins (system role is
// excluded by design — system identities don't satisfy the operator-
// reachability guarantee last-admin protection exists for).
func (s *Store) countActiveAdminsLocked() int {
	n := 0
	for _, i := range s.identities {
		if i.Role == idn.RoleAdmin && i.Status == idn.StatusActive {
			n++
		}
	}
	return n
}

func (s *Store) PairingCreate(_ context.Context, p *idn.PairingRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.pairings {
		if existing.Surface == p.Surface && existing.SurfaceID == p.SurfaceID {
			return idn.ErrAlreadyExists
		}
	}
	cp := *p
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = s.now()
	}
	s.pairings[cp.Token] = &cp
	return nil
}

func (s *Store) PairingByToken(_ context.Context, token string) (*idn.PairingRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, ok := s.pairings[token]
	if !ok {
		return nil, idn.ErrNotFound
	}
	if !pr.ExpiresAt.IsZero() && pr.ExpiresAt.Before(s.now()) {
		return nil, idn.ErrPendingExpired
	}
	out := *pr
	return &out, nil
}

func (s *Store) PairingDelete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pairings, token)
	return nil
}

func (s *Store) PairingSweep(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	n := 0
	for tok, pr := range s.pairings {
		if !pr.ExpiresAt.IsZero() && pr.ExpiresAt.Before(now) {
			delete(s.pairings, tok)
			n++
		}
	}
	return n, nil
}

// _ assertion that Store satisfies identity.Store at compile time.
var _ identity.Store = (*Store)(nil)

// cloneIdentity returns a deep copy so callers can mutate freely
// without aliasing the in-memory map's value.
func cloneIdentity(src *idn.Identity) *idn.Identity {
	if src == nil {
		return nil
	}
	cp := *src
	if len(src.CapabilitiesAdded) > 0 {
		cp.CapabilitiesAdded = append([]idn.Capability(nil), src.CapabilitiesAdded...)
	}
	if len(src.CapabilitiesRemoved) > 0 {
		cp.CapabilitiesRemoved = append([]idn.Capability(nil), src.CapabilitiesRemoved...)
	}
	return &cp
}

func filterOut(list []idn.Capability, c idn.Capability) []idn.Capability {
	out := list[:0]
	for _, v := range list {
		if v != c {
			out = append(out, v)
		}
	}
	return out
}
