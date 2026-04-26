package core_test

import (
	"context"
	"strings"
	"testing"
	"time"

	hermescore "github.com/zakros-hq/zakros/hermes/core"
	"github.com/zakros-hq/zakros/minos/storage"
	idn "github.com/zakros-hq/zakros/pkg/identity"
)

// Slice G acceptance gate integration test — the bullets from
// docs/phase-2-plan.md §6, exercised against the in-memory rig.
//
//   * second identity pairs via /pair, admin approves as observer
//   * observer can /status but cannot /commission
//   * admin grants task.commission.code per-identity → commission works
//   * admin revokes → new commissions refused
//   * revoke-last-admin refused
func TestSliceG_AcceptanceGate(t *testing.T) {
	kit, plug := newTestServerWithHermes(t)
	ctx := context.Background()

	// fakeplugin only accepts posts to threads it has created. Seed
	// two: opsThread plays the admin channel where /pair notifications
	// land; convoThread is where individual deliveries happen so the
	// per-message replies have somewhere to go.
	opsThread, err := plug.CreateThread(ctx, hermescore.CreateThreadRequest{
		Parent: "channel-ops", Title: "ops", Opener: "ops",
	})
	if err != nil {
		t.Fatalf("seed ops thread: %v", err)
	}
	convoThread, err := plug.CreateThread(ctx, hermescore.CreateThreadRequest{
		Parent: "channel-ops", Title: "test-convo", Opener: "test-convo",
	})
	if err != nil {
		t.Fatalf("seed convo thread: %v", err)
	}

	deliver := func(surfaceUserID, content string) {
		if err := plug.Deliver(ctx, hermescore.InboundMessage{
			Surface:       "discord",
			SurfaceUserID: surfaceUserID,
			ThreadRef:     convoThread,
			Content:       content,
			Timestamp:     time.Now().UTC(),
		}); err != nil {
			t.Fatalf("deliver: %v", err)
		}
	}
	_ = opsThread // referenced indirectly via project.ThreadParent

	postsContaining := func(substr string) []hermescore.Message {
		var out []hermescore.Message
		for _, th := range plug.Threads() {
			for _, p := range th.Posts {
				if strings.Contains(p.Content, substr) {
					out = append(out, p)
				}
			}
		}
		return out
	}

	// 1. Bob runs /pair from a previously-unknown discord ID.
	deliver("bob-discord", "/pair please give me access")
	tokens := postsContaining("pairing requested. share this token")
	if len(tokens) == 0 {
		t.Fatal("expected /pair to post a token; got no matching posts")
	}
	// Extract the token from the bot's reply: "share this token with an admin: <TOKEN>\n..."
	var token string
	for _, line := range strings.Split(tokens[len(tokens)-1].Content, "\n") {
		if i := strings.Index(line, "share this token with an admin: "); i >= 0 {
			token = strings.TrimSpace(line[i+len("share this token with an admin: "):])
		}
	}
	if token == "" {
		t.Fatalf("could not extract token from reply: %q", tokens[len(tokens)-1].Content)
	}

	// 2. Admin approves bob as observer.
	deliver("admin-id", "/minos approve "+token+" observer")
	approved := postsContaining("approved discord/bob-discord as observer")
	if len(approved) == 0 {
		t.Fatal("expected approval confirmation post")
	}

	// 3. Bob /status — should work (observer has task.query_state).
	deliver("bob-discord", "/status")
	statusReplies := postsContaining("no active tasks")
	if len(statusReplies) == 0 {
		t.Fatal("expected /status to succeed for observer")
	}

	// 4. Bob /commission — should be refused (observer lacks task.commission.code).
	deliver("bob-discord", "/commission repo=https://github.com/x/y branch=fix/a do something")
	refused := postsContaining("missing capability task.commission.code")
	if len(refused) == 0 {
		t.Fatal("expected commission to be refused for observer")
	}

	// 5. Admin grants task.commission.code to bob per-identity.
	deliver("admin-id", "/minos grant bob-discord task.commission.code")
	granted := postsContaining("granted task.commission.code to discord/bob-discord")
	if len(granted) == 0 {
		t.Fatal("expected grant confirmation")
	}

	// 6. Bob /commission — should now succeed.
	beforeCommissions, _ := kit.store.ListTasks(ctx, []storage.State{storage.StateRunning, storage.StateQueued, storage.StateCompleted}, 0)
	deliver("bob-discord", "/commission repo=https://github.com/x/y branch=fix/b do another")
	afterCommissions, _ := kit.store.ListTasks(ctx, []storage.State{storage.StateRunning, storage.StateQueued, storage.StateCompleted}, 0)
	if len(afterCommissions) <= len(beforeCommissions) {
		t.Fatalf("expected new task to be commissioned: before=%d after=%d", len(beforeCommissions), len(afterCommissions))
	}
	// The new task's envelope.Origin.Requester_Role should be "observer"
	// (the role at commission time — preserved on the audit line per
	// architecture.md §6 Audit).
	newest := afterCommissions[0]
	if got := newest.Envelope.Origin.RequesterRole; got != string(idn.RoleObserver) {
		t.Errorf("envelope.origin.requester_role = %q, want %q", got, idn.RoleObserver)
	}

	// 7. Admin revokes bob.
	deliver("admin-id", "/minos revoke bob-discord")
	revokedConfirm := postsContaining("revoked discord/bob-discord")
	if len(revokedConfirm) == 0 {
		t.Fatal("expected revoke confirmation")
	}

	// 8. Bob's next /commission is refused — revoked identities have no
	// capabilities. (handleInbound silently drops since LookupBySurface
	// returns the revoked row whose HasCapability returns false; we
	// observe via the absence of new tasks.)
	beforeRefused, _ := kit.store.ListTasks(ctx, nil, 0)
	deliver("bob-discord", "/commission repo=https://github.com/x/y branch=fix/c third")
	afterRefused, _ := kit.store.ListTasks(ctx, nil, 0)
	if len(afterRefused) > len(beforeRefused) {
		t.Errorf("expected revoked identity's commission to be refused; got new task")
	}

	// 9. Admin tries to revoke themselves — refused (last-admin protection).
	deliver("admin-id", "/minos revoke admin-id")
	lastAdmin := postsContaining("would leave zero active human admins")
	if len(lastAdmin) == 0 {
		t.Fatal("expected last-admin protection to refuse")
	}
}
