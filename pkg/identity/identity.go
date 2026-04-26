package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status is the identity's lifecycle state.
type Status string

const (
	// StatusActive is the steady state for human admins/commissioners/
	// observers and for system identities. Commands authorize against
	// active identities only.
	StatusActive Status = "active"
	// StatusRevoked identities cannot commission new work; in-flight
	// tasks are unaffected.
	StatusRevoked Status = "revoked"
	// StatusPending is reserved for the pairing flow if the operator
	// chooses to also persist the pending state in the identity table.
	// Slice G keeps pending state in pairing_requests instead, so this
	// value is currently unused but reserved for future consistency.
	StatusPending Status = "pending"
)

// Identity is one (surface, surface_id) tuple Minos knows about, plus
// the role and any per-identity capability overrides.
type Identity struct {
	ID                  uuid.UUID
	Surface             string
	SurfaceID           string
	Role                Role
	Status              Status
	CapabilitiesAdded   []Capability
	CapabilitiesRemoved []Capability
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// HasCapability resolves the role baseline + per-identity overrides
// and reports whether the identity may invoke c. Removed-overrides
// take precedence over the role baseline; added-overrides take
// precedence over removals only if both list the same capability
// (in practice the bootstrap rejects that conflict). Status: only
// active identities are authorized — revoked/pending always return
// false regardless of capability set.
func (i *Identity) HasCapability(c Capability) bool {
	if i == nil || i.Status != StatusActive {
		return false
	}
	for _, removed := range i.CapabilitiesRemoved {
		if removed == c {
			// An explicit add overrides the explicit remove.
			for _, added := range i.CapabilitiesAdded {
				if added == c {
					return true
				}
			}
			return false
		}
	}
	for _, added := range i.CapabilitiesAdded {
		if added == c {
			return true
		}
	}
	for _, base := range roleCapabilities(i.Role) {
		if base == c {
			return true
		}
	}
	return false
}

// PairingRequest is one pending /pair invocation. The token is the
// short-lived handle the requester quotes when an admin approves.
type PairingRequest struct {
	Token     string
	Surface   string
	SurfaceID string
	Note      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Errors callers can switch on.
var (
	// ErrNotFound — identity or pairing request lookup miss.
	ErrNotFound = errors.New("identity: not found")
	// ErrAlreadyExists — duplicate (surface, surface_id) on insert,
	// or duplicate token on pairing-request insert.
	ErrAlreadyExists = errors.New("identity: already exists")
	// ErrLastAdmin — refused revocation/role-change that would leave
	// zero active human admins. system identities are not protected.
	ErrLastAdmin = errors.New("identity: last admin protection")
	// ErrPendingExpired — pairing request lookup that finds an
	// expired-but-not-yet-swept row.
	ErrPendingExpired = errors.New("identity: pairing request expired")
	// ErrInvalidRole — bootstrap or approval supplied an unknown role.
	ErrInvalidRole = errors.New("identity: invalid role")
	// ErrSystemIdentityRule — rule violation specific to system
	// identities (no last-identity protection, identity.* capabilities
	// rejected on registration).
	ErrSystemIdentityRule = errors.New("identity: system identity rule violation")
)
