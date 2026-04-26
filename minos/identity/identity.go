// Package identity is Minos's persistence + lookup layer for the
// identity registry per architecture.md §6 Command Intake and Pairing.
// The Store interface is implemented by memstore (tests, local dev)
// and pgstore (production, against the shared Postgres LXC).
//
// Capability resolution and the value types live in pkg/identity so
// non-Minos consumers (Iris, future admin web UI) can import them
// without dragging the Postgres dependency.
package identity

import (
	"context"

	"github.com/google/uuid"

	idn "github.com/zakros-hq/zakros/pkg/identity"
)

// Store is the identity-registry persistence contract. Lookups and
// mutations are atomic; last-admin protection is enforced at the Store
// boundary so all callers (admin chat commands, future admin UI,
// Iris's delegated revocation) get the same protection automatically.
type Store interface {
	// LookupBySurface returns the identity matching the (surface,
	// surface_id) tuple, or idn.ErrNotFound. Status is included so the
	// caller can distinguish revoked vs absent.
	LookupBySurface(ctx context.Context, surface, surfaceID string) (*idn.Identity, error)

	// Get returns an identity by its uuid id.
	Get(ctx context.Context, id uuid.UUID) (*idn.Identity, error)

	// Insert persists a new identity. Returns idn.ErrAlreadyExists if
	// (surface, surface_id) is taken.
	Insert(ctx context.Context, i *idn.Identity) error

	// SetRole updates the identity's role; intended for admin-driven
	// role changes via /minos. Returns idn.ErrLastAdmin if changing
	// the last active human admin to a non-admin role.
	SetRole(ctx context.Context, id uuid.UUID, role idn.Role) error

	// Revoke flips the identity to StatusRevoked. Returns
	// idn.ErrLastAdmin for human admins; system identities are
	// unprotected per architecture §6 (deployment artifacts, not
	// reachability guarantees).
	Revoke(ctx context.Context, id uuid.UUID) error

	// AddCapability records a per-identity capability addition.
	// Idempotent: re-adding the same capability is a no-op.
	AddCapability(ctx context.Context, id uuid.UUID, c idn.Capability) error

	// RemoveCapability records a per-identity capability removal.
	// Idempotent: re-removing the same capability is a no-op.
	// Returns idn.ErrLastAdmin if removing identity.manage from the
	// last active admin would leave a no-admin state.
	RemoveCapability(ctx context.Context, id uuid.UUID, c idn.Capability) error

	// CountActiveAdmins is the load-bearing helper for last-admin
	// protection — exposed so callers (e.g. /minos revoke chat handler)
	// can preview whether an action would be refused.
	CountActiveAdmins(ctx context.Context) (int, error)

	// PairingCreate persists a pending /pair request. Returns
	// idn.ErrAlreadyExists if the (surface, surface_id) tuple already
	// has a pending request (operator must wait for expiry or cancel
	// before re-requesting).
	PairingCreate(ctx context.Context, p *idn.PairingRequest) error

	// PairingByToken returns the pending request matching the operator-
	// presented token, or idn.ErrNotFound. Expired rows return
	// idn.ErrPendingExpired.
	PairingByToken(ctx context.Context, token string) (*idn.PairingRequest, error)

	// PairingDelete removes a pending request — called after approval
	// (which inserts an Identity) or after operator-cancellation.
	PairingDelete(ctx context.Context, token string) error

	// PairingSweep deletes all expired pending rows. Background
	// sweeper invokes this on a cadence; safe to call concurrently.
	// Returns the number of rows deleted.
	PairingSweep(ctx context.Context) (int, error)
}
