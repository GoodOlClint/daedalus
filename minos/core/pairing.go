package core

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	hermescore "github.com/zakros-hq/zakros/hermes/core"
	"github.com/zakros-hq/zakros/pkg/audit"
	idn "github.com/zakros-hq/zakros/pkg/identity"
)

// pairingTokenTTL bounds how long a /pair request stays approvable.
// Slice G default — 24 hours is the architecture.md §23 placeholder
// pending the open question on quorum vs single-admin approval.
const pairingTokenTTL = 24 * time.Hour

// pairingTokenLen is the token length in base32 chars (no padding).
// 16 chars of base32 → 80 bits of entropy, enough for a short-lived
// approval handle.
const pairingTokenLen = 16

// handlePair is open: any (surface, surface_id) tuple may request
// pairing. Persists a pending request and posts the operator-facing
// token for the requester to share with an admin out-of-band, plus
// notifies admins on their configured channel.
func (s *Server) handlePair(ctx context.Context, msg hermescore.InboundMessage) {
	note := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(msg.Content), "/pair"))

	// If this tuple already has an active identity, just say so.
	if existing, err := s.identities.LookupBySurface(ctx, msg.Surface, msg.SurfaceUserID); err == nil && existing != nil && existing.Status == idn.StatusActive {
		s.replyToThread(ctx, msg, fmt.Sprintf("you're already paired as role=%s", existing.Role))
		return
	}

	// Mint a token + persist the pending request.
	token, err := generatePairingToken()
	if err != nil {
		s.replyToThread(ctx, msg, "pair failed: token generation error")
		return
	}
	pr := &idn.PairingRequest{
		Token:     token,
		Surface:   msg.Surface,
		SurfaceID: msg.SurfaceUserID,
		Note:      note,
		ExpiresAt: s.now().Add(pairingTokenTTL),
	}
	if err := s.identities.PairingCreate(ctx, pr); err != nil {
		if errors.Is(err, idn.ErrAlreadyExists) {
			s.replyToThread(ctx, msg, "you already have a pending pairing request — wait for an admin to approve, or for it to expire")
			return
		}
		s.replyToThread(ctx, msg, fmt.Sprintf("pair failed: %v", err))
		return
	}
	s.audit.Emit(audit.Event{
		Category: "pairing",
		Outcome:  "requested",
		Fields: map[string]string{
			"surface":          msg.Surface,
			"origin.requester": msg.SurfaceUserID,
			"token":            token,
		},
	})

	s.replyToThread(ctx, msg, fmt.Sprintf(
		"pairing requested. share this token with an admin: %s\nadmin runs `/minos approve %s [role]`",
		token, token,
	))

	// Best-effort notify admins via their configured admin channel.
	// Slice G keeps this simple: post to the project's thread_parent
	// (the Minos-watched channel) since every admin has eyes on it.
	// Per-admin DMs land in a future iteration.
	if s.hermes != nil && s.cfg.Project.ThreadParent != "" {
		_ = s.hermes.PostToThread(ctx, msg.Surface, s.cfg.Project.ThreadParent, hermescore.Message{
			Kind: hermescore.KindStatus,
			Content: fmt.Sprintf("pairing request from %s/%s — approve with `/minos approve %s [role]` (note: %q)",
				msg.Surface, msg.SurfaceUserID, token, note),
		})
	}
}

// handleMinosCommand is the umbrella for admin-issued operations.
// Subcommands implemented in Slice G:
//   /minos approve <token> [role]      — identity.approve_pairing
//   /minos revoke  <surface_id>        — identity.manage
//   /minos grant   <surface_id> <cap>  — identity.manage
//   /minos role    <surface_id> <role> — identity.manage
//
// Anything else gets a usage reply.
func (s *Server) handleMinosCommand(ctx context.Context, msg hermescore.InboundMessage, caller *idn.Identity) {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(msg.Content), "/minos"))
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		s.replyToThread(ctx, msg, "usage: /minos {approve|revoke|grant|role} ...")
		return
	}
	switch parts[0] {
	case "approve":
		if !caller.HasCapability(idn.CapIdentityApprovePairing) {
			s.replyCapabilityRefused(ctx, msg, string(idn.CapIdentityApprovePairing))
			return
		}
		s.handleApprove(ctx, msg, parts[1:])
	case "revoke":
		if !caller.HasCapability(idn.CapIdentityManage) {
			s.replyCapabilityRefused(ctx, msg, string(idn.CapIdentityManage))
			return
		}
		s.handleRevoke(ctx, msg, parts[1:])
	case "grant":
		if !caller.HasCapability(idn.CapIdentityManage) {
			s.replyCapabilityRefused(ctx, msg, string(idn.CapIdentityManage))
			return
		}
		s.handleGrant(ctx, msg, parts[1:])
	case "role":
		if !caller.HasCapability(idn.CapIdentityManage) {
			s.replyCapabilityRefused(ctx, msg, string(idn.CapIdentityManage))
			return
		}
		s.handleRoleChange(ctx, msg, parts[1:])
	default:
		s.replyToThread(ctx, msg, fmt.Sprintf("unknown subcommand: %s", parts[0]))
	}
}

// handleApprove: /minos approve <token> [role]
func (s *Server) handleApprove(ctx context.Context, msg hermescore.InboundMessage, args []string) {
	if len(args) < 1 {
		s.replyToThread(ctx, msg, "usage: /minos approve <token> [role=commissioner|observer]")
		return
	}
	token := args[0]
	role := idn.RoleCommissioner
	if len(args) >= 2 {
		candidate := idn.Role(args[1])
		if candidate == idn.RoleSystem {
			s.replyToThread(ctx, msg, "system role is bootstrap-only — refused")
			return
		}
		if !candidate.IsValid() {
			s.replyToThread(ctx, msg, fmt.Sprintf("unknown role: %s", args[1]))
			return
		}
		role = candidate
	}

	pr, err := s.identities.PairingByToken(ctx, token)
	if err != nil {
		s.replyToThread(ctx, msg, fmt.Sprintf("approve failed: %v", err))
		return
	}
	id := &idn.Identity{
		ID:        uuid.New(),
		Surface:   pr.Surface,
		SurfaceID: pr.SurfaceID,
		Role:      role,
		Status:    idn.StatusActive,
	}
	if err := s.identities.Insert(ctx, id); err != nil {
		s.replyToThread(ctx, msg, fmt.Sprintf("approve failed: %v", err))
		return
	}
	_ = s.identities.PairingDelete(ctx, token)
	s.audit.Emit(audit.Event{
		Category: "pairing",
		Outcome:  "approved",
		Fields: map[string]string{
			"surface":     pr.Surface,
			"surface_id":  pr.SurfaceID,
			"role":        string(role),
			"approved_by": msg.SurfaceUserID,
		},
	})
	s.replyToThread(ctx, msg, fmt.Sprintf("approved %s/%s as %s", pr.Surface, pr.SurfaceID, role))
}

// handleRevoke: /minos revoke <surface_id>  (surface defaults to the
// caller's surface — operators rarely revoke cross-surface)
func (s *Server) handleRevoke(ctx context.Context, msg hermescore.InboundMessage, args []string) {
	if len(args) < 1 {
		s.replyToThread(ctx, msg, "usage: /minos revoke <surface_id>")
		return
	}
	target, err := s.identities.LookupBySurface(ctx, msg.Surface, args[0])
	if err != nil {
		s.replyToThread(ctx, msg, fmt.Sprintf("revoke failed: %v", err))
		return
	}
	if err := s.identities.Revoke(ctx, target.ID); err != nil {
		if errors.Is(err, idn.ErrLastAdmin) {
			s.replyToThread(ctx, msg, "refused: would leave zero active human admins")
			return
		}
		s.replyToThread(ctx, msg, fmt.Sprintf("revoke failed: %v", err))
		return
	}
	s.audit.Emit(audit.Event{
		Category: "identity",
		Outcome:  "revoked",
		Fields: map[string]string{
			"surface":    target.Surface,
			"surface_id": target.SurfaceID,
			"revoked_by": msg.SurfaceUserID,
		},
	})
	s.replyToThread(ctx, msg, fmt.Sprintf("revoked %s/%s", target.Surface, target.SurfaceID))
}

// handleGrant: /minos grant <surface_id> <capability>
func (s *Server) handleGrant(ctx context.Context, msg hermescore.InboundMessage, args []string) {
	if len(args) < 2 {
		s.replyToThread(ctx, msg, "usage: /minos grant <surface_id> <capability>")
		return
	}
	target, err := s.identities.LookupBySurface(ctx, msg.Surface, args[0])
	if err != nil {
		s.replyToThread(ctx, msg, fmt.Sprintf("grant failed: %v", err))
		return
	}
	if err := s.identities.AddCapability(ctx, target.ID, idn.Capability(args[1])); err != nil {
		s.replyToThread(ctx, msg, fmt.Sprintf("grant failed: %v", err))
		return
	}
	s.audit.Emit(audit.Event{
		Category: "identity",
		Outcome:  "capability-granted",
		Fields: map[string]string{
			"surface":    target.Surface,
			"surface_id": target.SurfaceID,
			"capability": args[1],
			"granted_by": msg.SurfaceUserID,
		},
	})
	s.replyToThread(ctx, msg, fmt.Sprintf("granted %s to %s/%s", args[1], target.Surface, target.SurfaceID))
}

// handleRoleChange: /minos role <surface_id> <role>
func (s *Server) handleRoleChange(ctx context.Context, msg hermescore.InboundMessage, args []string) {
	if len(args) < 2 {
		s.replyToThread(ctx, msg, "usage: /minos role <surface_id> <admin|commissioner|observer>")
		return
	}
	target, err := s.identities.LookupBySurface(ctx, msg.Surface, args[0])
	if err != nil {
		s.replyToThread(ctx, msg, fmt.Sprintf("role failed: %v", err))
		return
	}
	role := idn.Role(args[1])
	if role == idn.RoleSystem {
		s.replyToThread(ctx, msg, "system role is bootstrap-only — refused")
		return
	}
	if err := s.identities.SetRole(ctx, target.ID, role); err != nil {
		if errors.Is(err, idn.ErrLastAdmin) {
			s.replyToThread(ctx, msg, "refused: would leave zero active human admins")
			return
		}
		if errors.Is(err, idn.ErrInvalidRole) {
			s.replyToThread(ctx, msg, fmt.Sprintf("unknown role: %s", args[1]))
			return
		}
		s.replyToThread(ctx, msg, fmt.Sprintf("role failed: %v", err))
		return
	}
	s.audit.Emit(audit.Event{
		Category: "identity",
		Outcome:  "role-changed",
		Fields: map[string]string{
			"surface":    target.Surface,
			"surface_id": target.SurfaceID,
			"role":       string(role),
			"changed_by": msg.SurfaceUserID,
		},
	})
	s.replyToThread(ctx, msg, fmt.Sprintf("set %s/%s role=%s", target.Surface, target.SurfaceID, role))
}

// generatePairingToken returns a base32-encoded random token. Length is
// pairingTokenLen chars after stripping padding (10 random bytes →
// 16 base32 chars).
func generatePairingToken() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return enc.EncodeToString(buf)[:pairingTokenLen], nil
}

// replyToThread is a thin wrapper around hermes PostToThread used by
// pairing/admin handlers — keeps the call sites compact.
func (s *Server) replyToThread(ctx context.Context, msg hermescore.InboundMessage, content string) {
	if s.hermes == nil || msg.ThreadRef == "" {
		return
	}
	_ = s.hermes.PostToThread(ctx, msg.Surface, msg.ThreadRef, hermescore.Message{
		Kind:    hermescore.KindStatus,
		Content: content,
	})
}
