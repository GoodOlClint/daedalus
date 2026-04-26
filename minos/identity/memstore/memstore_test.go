package memstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zakros-hq/zakros/minos/identity/memstore"
	idn "github.com/zakros-hq/zakros/pkg/identity"
)

func newAdmin(surfaceID string) *idn.Identity {
	return &idn.Identity{
		Surface:   "discord",
		SurfaceID: surfaceID,
		Role:      idn.RoleAdmin,
		Status:    idn.StatusActive,
	}
}

func TestInsertAndLookup(t *testing.T) {
	s := memstore.New(nil)
	ctx := context.Background()
	if err := s.Insert(ctx, newAdmin("alice")); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := s.LookupBySurface(ctx, "discord", "alice")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Role != idn.RoleAdmin {
		t.Errorf("role: %s", got.Role)
	}
	if !got.HasCapability(idn.CapTaskCommissionCode) {
		t.Error("admin should have task.commission.code")
	}
}

func TestInsertDuplicate(t *testing.T) {
	s := memstore.New(nil)
	ctx := context.Background()
	_ = s.Insert(ctx, newAdmin("alice"))
	err := s.Insert(ctx, newAdmin("alice"))
	if !errors.Is(err, idn.ErrAlreadyExists) {
		t.Errorf("want ErrAlreadyExists, got %v", err)
	}
}

func TestRevokeLastAdminRefused(t *testing.T) {
	s := memstore.New(nil)
	ctx := context.Background()
	_ = s.Insert(ctx, newAdmin("alice"))
	alice, _ := s.LookupBySurface(ctx, "discord", "alice")

	err := s.Revoke(ctx, alice.ID)
	if !errors.Is(err, idn.ErrLastAdmin) {
		t.Errorf("want ErrLastAdmin, got %v", err)
	}
}

func TestRevokeWithSecondAdminAllowed(t *testing.T) {
	s := memstore.New(nil)
	ctx := context.Background()
	_ = s.Insert(ctx, newAdmin("alice"))
	_ = s.Insert(ctx, newAdmin("bob"))
	alice, _ := s.LookupBySurface(ctx, "discord", "alice")

	if err := s.Revoke(ctx, alice.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ := s.Get(ctx, alice.ID)
	if got.Status != idn.StatusRevoked {
		t.Errorf("status: %s", got.Status)
	}
	// After revocation, alice has no capabilities even though her role
	// is still admin.
	if got.HasCapability(idn.CapTaskCommissionCode) {
		t.Error("revoked admin must not authorize")
	}
}

func TestSystemRoleNotProtected(t *testing.T) {
	// system identities don't count toward last-admin protection — they
	// can be revoked freely (per architecture §6).
	s := memstore.New(nil)
	ctx := context.Background()
	themis := &idn.Identity{
		Surface: "pod-class", SurfaceID: "themis",
		Role: idn.RoleSystem, Status: idn.StatusActive,
	}
	_ = s.Insert(ctx, themis)
	got, _ := s.LookupBySurface(ctx, "pod-class", "themis")

	if err := s.Revoke(ctx, got.ID); err != nil {
		t.Errorf("system revoke should succeed: %v", err)
	}
}

func TestPerIdentityCapabilityAdd(t *testing.T) {
	s := memstore.New(nil)
	ctx := context.Background()
	observer := &idn.Identity{
		Surface: "discord", SurfaceID: "bob",
		Role: idn.RoleObserver, Status: idn.StatusActive,
	}
	_ = s.Insert(ctx, observer)
	got, _ := s.LookupBySurface(ctx, "discord", "bob")

	if got.HasCapability(idn.CapTaskCommissionCode) {
		t.Error("observer baseline should not include task.commission.code")
	}

	if err := s.AddCapability(ctx, got.ID, idn.CapTaskCommissionCode); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ = s.Get(ctx, got.ID)
	if !got.HasCapability(idn.CapTaskCommissionCode) {
		t.Error("expected observer + per-identity add to authorize")
	}
}

func TestPairingExpiry(t *testing.T) {
	now := time.Now().UTC()
	clock := now
	s := memstore.New(func() time.Time { return clock })
	ctx := context.Background()

	if err := s.PairingCreate(ctx, &idn.PairingRequest{
		Token: "TKN1", Surface: "discord", SurfaceID: "newcomer",
		ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Within window → ok.
	if _, err := s.PairingByToken(ctx, "TKN1"); err != nil {
		t.Errorf("in-window lookup: %v", err)
	}

	// Past expiry → ErrPendingExpired.
	clock = now.Add(10 * time.Minute)
	_, err := s.PairingByToken(ctx, "TKN1")
	if !errors.Is(err, idn.ErrPendingExpired) {
		t.Errorf("want ErrPendingExpired, got %v", err)
	}

	// Sweep removes the row.
	n, err := s.PairingSweep(ctx)
	if err != nil || n != 1 {
		t.Errorf("sweep: n=%d err=%v", n, err)
	}
	if _, err := s.PairingByToken(ctx, "TKN1"); !errors.Is(err, idn.ErrNotFound) {
		t.Errorf("post-sweep want ErrNotFound, got %v", err)
	}
}
