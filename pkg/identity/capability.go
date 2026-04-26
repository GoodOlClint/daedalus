// Package identity defines the capability set, role bundles, and value
// types for the Phase 2 identity registry per architecture.md §6
// Command Intake and Pairing.
//
// The package is intentionally storage-free — it's a leaf dependency
// every other component (Minos intake, audit emission, future Iris
// admin surface, future admin web UI) imports without dragging
// Postgres or any provider in.
package identity

// Capability is one discrete authorization atom. Identities carry a
// set of capabilities; commands and admin operations are gated by
// matching against required capabilities.
//
// Phase 2 Slice G ships only the capabilities the acceptance gate
// actually exercises plus the identity-management ones. Per-task-type
// commission capabilities for Phase 2 pod classes (review/docs/release/
// adr) land with their respective L2-L5 slices; Pythia (research) and
// Talos (test) capabilities land in Phase 3.
type Capability string

const (
	// Task commission — one capability per task-type. Phase 2 Slice G
	// ships `code` only; review/docs/release/adr land with L2-L5.
	CapTaskCommissionCode Capability = "task.commission.code"

	// Direct a running agent (PR review event, @mention respawn, surface
	// @mention to a running pod). Phase 1 had this implicitly via the
	// admin-only intake; Slice G makes it a discrete capability.
	CapTaskDirect Capability = "task.direct"

	// Read-only state queries — list active tasks, queue, recent.
	CapTaskQueryState Capability = "task.query_state"

	// Approve a pending pairing request and assign a role to it.
	CapIdentityApprovePairing Capability = "identity.approve_pairing"

	// Revoke an identity, change its role, add/remove per-identity
	// capability overrides.
	CapIdentityManage Capability = "identity.manage"
)

// Role is a preset capability bundle. Identities carry a role; any
// operation a role's bundle covers is permitted unless explicitly
// removed for that identity, and capabilities can be added per-identity
// beyond the bundle baseline.
type Role string

const (
	RoleAdmin        Role = "admin"
	RoleCommissioner Role = "commissioner"
	RoleObserver     Role = "observer"
	RoleSystem       Role = "system"
)

// All returns every defined role — useful for validation against the
// migration's CHECK constraint without round-tripping through the DB.
var allRoles = []Role{RoleAdmin, RoleCommissioner, RoleObserver, RoleSystem}

// IsValid reports whether r is one of the defined roles.
func (r Role) IsValid() bool {
	for _, v := range allRoles {
		if r == v {
			return true
		}
	}
	return false
}

// roleCapabilities returns the baseline capability set for a role.
// admin gets everything; commissioner gets task ops; observer is
// read-only; system mirrors commissioner but is provisioned at
// deploy time, not via /pair (handled in the bootstrap layer, not here).
//
// Per-identity additions/removals layer on top of these baselines —
// see Identity.HasCapability for the resolution order.
func roleCapabilities(r Role) []Capability {
	switch r {
	case RoleAdmin:
		return []Capability{
			CapTaskCommissionCode,
			CapTaskDirect,
			CapTaskQueryState,
			CapIdentityApprovePairing,
			CapIdentityManage,
		}
	case RoleCommissioner, RoleSystem:
		return []Capability{
			CapTaskCommissionCode,
			CapTaskDirect,
			CapTaskQueryState,
		}
	case RoleObserver:
		return []Capability{
			CapTaskQueryState,
		}
	default:
		return nil
	}
}
