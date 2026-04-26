// Package pgstore is the Postgres-backed identity Store. Targets the
// shared Postgres LXC; uses the minos.identities + minos.pairing_requests
// tables created by migration 0009.
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zakros-hq/zakros/minos/identity"
	idn "github.com/zakros-hq/zakros/pkg/identity"
)

// Store is the Postgres implementation of identity.Store.
type Store struct {
	pool *pgxpool.Pool
}

// New wraps an existing pool. Migration 0009 must have run.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) LookupBySurface(ctx context.Context, surface, surfaceID string) (*idn.Identity, error) {
	const q = `
SELECT id, surface, surface_id, role, status,
       capabilities_added, capabilities_removed,
       created_at, updated_at
FROM minos.identities
WHERE surface = $1 AND surface_id = $2`
	row := s.pool.QueryRow(ctx, q, surface, surfaceID)
	out, err := scanIdentity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, idn.ErrNotFound
	}
	return out, err
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*idn.Identity, error) {
	const q = `
SELECT id, surface, surface_id, role, status,
       capabilities_added, capabilities_removed,
       created_at, updated_at
FROM minos.identities
WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	out, err := scanIdentity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, idn.ErrNotFound
	}
	return out, err
}

func (s *Store) Insert(ctx context.Context, in *idn.Identity) error {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if !in.Role.IsValid() {
		return idn.ErrInvalidRole
	}
	addedJSON, err := json.Marshal(in.CapabilitiesAdded)
	if err != nil {
		return fmt.Errorf("identity pgstore: marshal added: %w", err)
	}
	removedJSON, err := json.Marshal(in.CapabilitiesRemoved)
	if err != nil {
		return fmt.Errorf("identity pgstore: marshal removed: %w", err)
	}
	const q = `
INSERT INTO minos.identities (
  id, surface, surface_id, role, status,
  capabilities_added, capabilities_removed
) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err = s.pool.Exec(ctx, q,
		in.ID, in.Surface, in.SurfaceID, string(in.Role), string(in.Status),
		addedJSON, removedJSON,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return idn.ErrAlreadyExists
		}
		return fmt.Errorf("identity pgstore: insert: %w", err)
	}
	return nil
}

func (s *Store) SetRole(ctx context.Context, id uuid.UUID, role idn.Role) error {
	if !role.IsValid() {
		return idn.ErrInvalidRole
	}
	// Last-admin protection has to run inside a transaction so the
	// count + update see a consistent snapshot.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var curRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM minos.identities WHERE id = $1`, id).Scan(&curRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return idn.ErrNotFound
		}
		return err
	}
	if curRole == string(idn.RoleAdmin) && role != idn.RoleAdmin {
		n, err := countActiveAdminsTx(ctx, tx)
		if err != nil {
			return err
		}
		if n-1 < 1 {
			return idn.ErrLastAdmin
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE minos.identities SET role = $2, updated_at = now() WHERE id = $1`,
		id, string(role),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Revoke(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var role, status string
	err = tx.QueryRow(ctx,
		`SELECT role, status FROM minos.identities WHERE id = $1`, id,
	).Scan(&role, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return idn.ErrNotFound
		}
		return err
	}
	if status == string(idn.StatusRevoked) {
		return nil
	}
	if role == string(idn.RoleAdmin) {
		n, err := countActiveAdminsTx(ctx, tx)
		if err != nil {
			return err
		}
		if n-1 < 1 {
			return idn.ErrLastAdmin
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE minos.identities SET status = 'revoked', updated_at = now() WHERE id = $1`, id,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AddCapability(ctx context.Context, id uuid.UUID, c idn.Capability) error {
	return s.mutateCaps(ctx, id, func(added, removed []idn.Capability) ([]idn.Capability, []idn.Capability, error) {
		for _, v := range added {
			if v == c {
				return added, removed, nil
			}
		}
		removed = filterOut(removed, c)
		added = append(added, c)
		return added, removed, nil
	})
}

func (s *Store) RemoveCapability(ctx context.Context, id uuid.UUID, c idn.Capability) error {
	return s.mutateCaps(ctx, id, func(added, removed []idn.Capability) ([]idn.Capability, []idn.Capability, error) {
		for _, v := range removed {
			if v == c {
				return added, removed, nil
			}
		}
		added = filterOut(added, c)
		removed = append(removed, c)
		return added, removed, nil
	})
}

// mutateCaps loads the current cap arrays in a tx, applies the
// caller's transform, and writes back. Transactional so concurrent
// mutations don't lose updates.
func (s *Store) mutateCaps(ctx context.Context, id uuid.UUID, fn func(added, removed []idn.Capability) ([]idn.Capability, []idn.Capability, error)) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var addedJSON, removedJSON []byte
	err = tx.QueryRow(ctx,
		`SELECT capabilities_added, capabilities_removed FROM minos.identities WHERE id = $1`,
		id,
	).Scan(&addedJSON, &removedJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return idn.ErrNotFound
		}
		return err
	}
	var added, removed []idn.Capability
	if len(addedJSON) > 0 {
		_ = json.Unmarshal(addedJSON, &added)
	}
	if len(removedJSON) > 0 {
		_ = json.Unmarshal(removedJSON, &removed)
	}
	added, removed, err = fn(added, removed)
	if err != nil {
		return err
	}
	addedOut, _ := json.Marshal(added)
	removedOut, _ := json.Marshal(removed)
	if _, err := tx.Exec(ctx,
		`UPDATE minos.identities SET capabilities_added = $2, capabilities_removed = $3, updated_at = now() WHERE id = $1`,
		id, addedOut, removedOut,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM minos.identities WHERE role = 'admin' AND status = 'active'`,
	).Scan(&n)
	return n, err
}

func (s *Store) PairingCreate(ctx context.Context, p *idn.PairingRequest) error {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	const q = `
INSERT INTO minos.pairing_requests (token, surface, surface_id, note, expires_at)
VALUES ($1, $2, $3, $4, $5)`
	_, err := s.pool.Exec(ctx, q, p.Token, p.Surface, p.SurfaceID, p.Note, p.ExpiresAt)
	if err != nil {
		if isUniqueViolation(err) {
			return idn.ErrAlreadyExists
		}
		return fmt.Errorf("identity pgstore: pairing insert: %w", err)
	}
	return nil
}

func (s *Store) PairingByToken(ctx context.Context, token string) (*idn.PairingRequest, error) {
	const q = `
SELECT token, surface, surface_id, note, created_at, expires_at
FROM minos.pairing_requests
WHERE token = $1`
	row := s.pool.QueryRow(ctx, q, token)
	var pr idn.PairingRequest
	if err := row.Scan(&pr.Token, &pr.Surface, &pr.SurfaceID, &pr.Note, &pr.CreatedAt, &pr.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, idn.ErrNotFound
		}
		return nil, err
	}
	if !pr.ExpiresAt.IsZero() && pr.ExpiresAt.Before(time.Now().UTC()) {
		return nil, idn.ErrPendingExpired
	}
	return &pr, nil
}

func (s *Store) PairingDelete(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM minos.pairing_requests WHERE token = $1`, token)
	return err
}

func (s *Store) PairingSweep(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM minos.pairing_requests WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// scanIdentity reads one row into an Identity. Common to LookupBySurface,
// Get, and any future paginated list.
func scanIdentity(row pgx.Row) (*idn.Identity, error) {
	var i idn.Identity
	var role, status string
	var addedJSON, removedJSON []byte
	if err := row.Scan(
		&i.ID, &i.Surface, &i.SurfaceID, &role, &status,
		&addedJSON, &removedJSON,
		&i.CreatedAt, &i.UpdatedAt,
	); err != nil {
		return nil, err
	}
	i.Role = idn.Role(role)
	i.Status = idn.Status(status)
	if len(addedJSON) > 0 {
		_ = json.Unmarshal(addedJSON, &i.CapabilitiesAdded)
	}
	if len(removedJSON) > 0 {
		_ = json.Unmarshal(removedJSON, &i.CapabilitiesRemoved)
	}
	return &i, nil
}

func countActiveAdminsTx(ctx context.Context, tx pgx.Tx) (int, error) {
	var n int
	err := tx.QueryRow(ctx,
		`SELECT count(*) FROM minos.identities WHERE role = 'admin' AND status = 'active'`,
	).Scan(&n)
	return n, err
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

// isUniqueViolation mirrors the helper in minos/storage/pgstore.
// Duplicated here to keep this package free of import-cycle headaches
// against the storage package.
func isUniqueViolation(err error) bool {
	type pgCoder interface{ SQLState() string }
	var pce pgCoder
	if errors.As(err, &pce) {
		return pce.SQLState() == "23505"
	}
	return false
}

// Compile-time assertion.
var _ identity.Store = (*Store)(nil)
