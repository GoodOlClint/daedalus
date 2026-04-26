package identity_test

import (
	"testing"

	"github.com/zakros-hq/zakros/pkg/identity"
)

func TestRoleIsValid(t *testing.T) {
	cases := map[identity.Role]bool{
		identity.RoleAdmin:        true,
		identity.RoleCommissioner: true,
		identity.RoleObserver:     true,
		identity.RoleSystem:       true,
		identity.Role("operator"): false,
		identity.Role(""):         false,
	}
	for r, want := range cases {
		if got := r.IsValid(); got != want {
			t.Errorf("Role(%q).IsValid() = %v, want %v", r, got, want)
		}
	}
}

func TestHasCapability_RoleBaseline(t *testing.T) {
	cases := []struct {
		role     identity.Role
		cap      identity.Capability
		expected bool
	}{
		{identity.RoleAdmin, identity.CapTaskCommissionCode, true},
		{identity.RoleAdmin, identity.CapIdentityManage, true},
		{identity.RoleCommissioner, identity.CapTaskCommissionCode, true},
		{identity.RoleCommissioner, identity.CapIdentityManage, false},
		{identity.RoleObserver, identity.CapTaskQueryState, true},
		{identity.RoleObserver, identity.CapTaskCommissionCode, false},
		{identity.RoleSystem, identity.CapTaskCommissionCode, true},
		// system identities cannot manage other identities by design
		{identity.RoleSystem, identity.CapIdentityManage, false},
	}
	for _, tc := range cases {
		i := &identity.Identity{Role: tc.role, Status: identity.StatusActive}
		if got := i.HasCapability(tc.cap); got != tc.expected {
			t.Errorf("role=%s cap=%s: HasCapability=%v want %v", tc.role, tc.cap, got, tc.expected)
		}
	}
}

func TestHasCapability_RevokedNeverAuthorizes(t *testing.T) {
	i := &identity.Identity{Role: identity.RoleAdmin, Status: identity.StatusRevoked}
	if i.HasCapability(identity.CapTaskCommissionCode) {
		t.Error("revoked admin must not have any capability")
	}
}

func TestHasCapability_AddedOverride(t *testing.T) {
	// Observer + per-identity-added task.commission.code → can commission.
	i := &identity.Identity{
		Role:              identity.RoleObserver,
		Status:            identity.StatusActive,
		CapabilitiesAdded: []identity.Capability{identity.CapTaskCommissionCode},
	}
	if !i.HasCapability(identity.CapTaskCommissionCode) {
		t.Error("expected per-identity-added capability to authorize")
	}
}

func TestHasCapability_RemovedOverride(t *testing.T) {
	// Commissioner with task.commission.code removed → cannot commission.
	i := &identity.Identity{
		Role:                identity.RoleCommissioner,
		Status:              identity.StatusActive,
		CapabilitiesRemoved: []identity.Capability{identity.CapTaskCommissionCode},
	}
	if i.HasCapability(identity.CapTaskCommissionCode) {
		t.Error("expected per-identity-removed capability to deny")
	}
}

func TestHasCapability_AddedOverridesRemoved(t *testing.T) {
	// Conflict resolution: an explicit add wins over an explicit remove
	// for the same capability. (Bootstrap rejects this conflict, but
	// the runtime check is permissive.)
	i := &identity.Identity{
		Role:                identity.RoleObserver,
		Status:              identity.StatusActive,
		CapabilitiesAdded:   []identity.Capability{identity.CapTaskCommissionCode},
		CapabilitiesRemoved: []identity.Capability{identity.CapTaskCommissionCode},
	}
	if !i.HasCapability(identity.CapTaskCommissionCode) {
		t.Error("explicit add should override explicit remove")
	}
}

func TestHasCapability_NilSafe(t *testing.T) {
	var i *identity.Identity
	if i.HasCapability(identity.CapTaskQueryState) {
		t.Error("nil identity must never authorize")
	}
}
