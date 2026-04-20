# Project Daedalus — Phase 1 Plan

*Version 0.1 — Draft*

---

## Purpose

This document decomposes `roadmap.md §Phase 1` into an ordered, slice-based build plan. It is the authoritative sequencing document for Phase 1 implementation. When `roadmap.md` changes Phase 1 scope, this document is updated to match; when implementation diverges from this plan, update the plan rather than letting it rot.

The planning method is slice-by-acceptance: work backward from the Phase 1 acceptance gate to find the minimum path, then build forward in increments that each prove one gate bullet.

---

## 1. Phase 1 Acceptance Gate

From `roadmap.md §Phase 1 acceptance`:

1. Operator posts a command on the configured surface; Minos commissions a pod; the agent works, opens a PR, signals awaiting-review; Minos hibernates; a review event respawns with Mnemosyne context; the task reaches a terminal state.
2. Iris answers "what's running?" and "start a task for X" on the same surface.
3. Run records persist across pod teardown; context injection from prior runs demonstrably primes a new run.

Every slice below closes one or more of these bullets.

---

## 2. Structural Decisions

### Language: Go

Daedalus is a systems/orchestration codebase — the architectural neighbors are Kubernetes, Vault, Consul, Nomad, Traefik, containerd. Go is the default language for Phase 1 for these maintainability reasons:

- **Static single-binary deploy.** One file per service, `systemctl restart` is the release; no per-service venv management, lockfile-per-service, or Python-version drift.
- **Compile-time safety over mutating shape.** Task envelope schema, scope tables, JWT claim shapes, and broker registry will all mutate across phases. Go's structural typing catches renames and shape changes at compile time; the single-operator homelab cannot rely on Python's runtime-caught shape errors as a safety net.
- **Concurrency maps to the actual problem.** k3s pod-phase watching, GitHub webhook dispatch, broker-subprocess supervision, Argus heartbeat ingest, Postgres connection pooling — goroutines + channels are the idiom.
- **Smaller operational surface.** Labeled binaries with legible RSS vs. Gunicorn/uvicorn/worker process trees.
- **Python-favorable carve-outs are thin in Phase 1.** Mnemosyne is Postgres writes, pgvector ORDER BY, and regex redaction. Embedding calls are HTTP to Athena. No in-process ML.
- **Polyglot doubles maintenance cost.** Two CI pipelines, two test frameworks, two release processes. One language, even if locally suboptimal for a narrow component, is cheaper to maintain at one-operator scale.

**Exceptions permitted:**
- Pod-image worker backends are language-agnostic (the plugin contract is HTTP/MCP + subprocess exit code). Claude Code is Node; any future plugin's host language is its author's choice.
- Scripts and one-off tooling in `scripts/` may be shell or Python.

**Revisit triggers for the Go default:**
- Phase 2 Apollo landing — reassess if the provider plugin ecosystem is Python-concentrated enough to warrant polyglot.
- Any component that requires in-process ML inference or embedding (none in Phase 1).

### Repository: monorepo

Single repository, structure per `architecture.md §15`. Maintainability reasons:

- **Cross-cutting code is load-bearing.** Task envelope schemas (`schemas/`), provider interface, JWT signing library, plugin interface contract, broker auth middleware — every service depends on these. Monorepo makes cross-cutting changes atomic and reviewable in one diff.
- **One operator, one deployment.** There is no independent release cadence or independent team to justify multi-repo coordination cost.
- **Go workspaces handle the "multi-module in one repo" shape** without splitting history.
- **CI blast-radius control** via per-module test isolation and `go build ./...` with build caches; no need for multi-repo to keep CI tractable.

**Revisit trigger:** if a broker (Hecate or Apollo most likely) acquires third-party consumers in Phase 2+, extract that broker to its own repo *then*, not preemptively.

### Build order and dependencies

```
A → (B ∥ D) → C → E
```

- **A** is the critical-path substrate; nothing else builds without it.
- **B** and **D** are parallel-safe once A is done — they touch different subsystems.
- **C** depends on B because the hibernate/respawn round-trip needs a review-event webhook to fire, which requires Cerberus.
- **E** depends on C because Iris's `memory.lookup` requires Mnemosyne.

---

## 3. Slice A — "a pod can do a task"

**Proves:** commission → pod work → PR. (Acceptance gate bullet 1, first half.)

**Scope:** the minimum substrate that lets Minos commission a pod via CLI or HTTP and produce a pull request. No Discord, no hibernation, no Mnemosyne, no Argus enforcement.

### Tasks

1. **Infrastructure floor** (environment work, not Daedalus code)
   - Proxmox VMs: Minos VM, Postgres LXC, Labyrinth VM, Ariadne VM per `architecture.md §4 VM Inventory`
   - ZFS mirror and firewall rules per `architecture.md §4 Network`
   - Ariadne Vector + Loki stood up for log ingest (no query-side work yet; ingest is needed because pods ship logs)

2. **Postgres LXC + schemas**
   - Single Postgres instance with `minos`, `argus`, `mnemosyne`, `iris` schemas per `architecture.md §6 Recovery and Reconciliation`
   - pgvector extension installed (used in Slice C; install now so migrations are ordered correctly)
   - Migration tooling chosen (golang-migrate, goose, or atlas — decide at implementation)

3. **Shared Go modules in monorepo**
   - `pkg/envelope` — task envelope schema types + JSON Schema validation
   - `pkg/jwt` — Ed25519 signing/verification (Phase 2 consumer; Phase 1 uses HMAC bearer, but design the package so Phase 2 substitution is drop-in)
   - `pkg/provider` — secret-provider interface (`Resolve`, `Rotate`, `Revoke`, `AuditList`)
   - `pkg/audit` — structured audit emitter; writes to stdout in JSON for Vector to pick up

4. **Secret provider: file-backed reference**
   - Implements the `pkg/provider` interface reading from a YAML/JSON file under the Minos config directory
   - Phase 1 shipping default per `architecture.md §17` MVP Blockers — Secret provider
   - Infisical provider is a Slice A stretch goal; not a blocker for the acceptance checkpoint

5. **Minos core — minimum viable**
   - Service binary under `minos/core/`
   - Single hardcoded project config loader
   - Single hardcoded admin identity loader
   - Task registry: CRUD on `tasks` table
   - Dispatch: accept an HTTP `POST /tasks` from CLI, compose task envelope, spawn a k3s pod, insert task row in `running` state
   - State machine: `queued → running → completed | failed`  (no `awaiting-review` yet — that lands in Slice C)
   - Startup reconciliation per `architecture.md §6 Recovery and Reconciliation` (minimum: DB integrity + adopt/orphan pods)

6. **Labyrinth k3s**
   - Single-node k3s on Labyrinth VM, default flannel CNI
   - Host nftables rules per `architecture.md §11 Network Isolation`
   - Proxmox-vNIC firewall allowlist per `architecture.md §11 Egress Granularity`

7. **GitHub App deployment**
   - GitHub App created in the operator's GitHub account with permissions: repo contents rw, PRs rw, issues rw, metadata r
   - Private key stored via the secret provider
   - Installation-token minter in Minos per `architecture.md §6 Credential Handling`: 1-hour TTL, repo-scoped to single `repo_url` in task envelope
   - Branch protection verification at project-registration time per `security.md §5`

8. **Worker plugin interface + Claude Code plugin**
   - Plugin contract defined in `agents/plugin/` — entry-point binary receives task envelope on stdin or file mount, exits 0 on success
   - Claude Code plugin under `agents/claude-code/`: pod image with the `claude-code` binary, envelope-parsing entry, git clone, `claude-code` invocation with the brief, PR open via `gh` CLI or direct GitHub API
   - Memory-extraction hook at SIGTERM (Phase 1: stdout dump + workspace file list; Mnemosyne consumes this in Slice C)

9. **CLI dispatcher for bootstrapping**
   - `minosctl commission --project X --brief Y --repo Z --branch feature/...`
   - Short-circuits the Hermes/Discord path for Slice A testing

### Acceptance checkpoint for Slice A

- From Minos VM, run `minosctl commission ...` against a test repo
- Pod spawns in Labyrinth, clones repo, opens a PR, exits cleanly
- Task row transitions `queued → running → completed`
- Logs visible in Ariadne (Loki query)

---

## 4. Slice B — "operator loop closes"

**Proves:** operator commissions from Discord; PR-merge webhook drives task to terminal; summary posts to thread. (Acceptance gate bullet 1, hibernation deferred to Slice C.)

**Scope:** Hermes + Discord plugin + thread sidecar + Cerberus-in-Minos with Cloudflare Tunnel. Still no hibernation, no Mnemosyne, no Iris.

### Tasks

1. **Cerberus as a library inside Minos**
   - `cerberus/core/` — route table (Postgres-backed) and delivery-ID replay store
   - `cerberus/ingress/cloudflare/` — `cloudflared` configuration reference + plaintext forward to the in-Minos Cerberus handler
   - `cerberus/verification/github/` — HMAC verification with `X-GitHub-Delivery` replay protection

2. **Cloudflare Tunnel setup**
   - `cloudflared` service on Minos VM
   - Public hostname → `localhost:<cerberus port>` forward
   - GitHub App webhook URL pointed at the public hostname

3. **Hermes core (in-process)**
   - `hermes/core/` — minimal broker: maintains surface-plugin registry, `thread_surface` → plugin routing, cross-thread posting enforcement (task_id → thread_ref lookup) per `architecture.md §6 Communication Surfaces`
   - Runs in the Minos process in Phase 1 per `roadmap.md §Phase 1` services list

4. **Discord plugin**
   - `hermes/plugins/discord/` — bot connection via `bwmarrin/discordgo`, inbound message stream, outbound thread posts
   - Bot token resolved from secret provider
   - Outbound-only gateway (no webhook from Discord side needed in Phase 1; uses Discord's bot gateway)

5. **Thread sidecar**
   - `agents/sidecar/thread/` — MCP server running inside each pod as a sidecar container
   - Exposes `post_status`, `post_thinking`, `post_code_block`, `request_human_input`
   - Proxies to Hermes on Minos VM over bearer-token HTTP

6. **Minos — command intake**
   - Parse Discord messages matching the single-admin identity
   - `/commission` or natural-language intake (simple regex sufficient for Phase 1; Iris NL parsing is Slice E)
   - Dispatch path reuses Slice A's `POST /tasks` handler

7. **Minos — webhook handler**
   - `POST /webhooks/github` in Minos (verified by Cerberus library)
   - Handle `pull_request.closed` with `merged: true` → task finalization
   - Handle `pull_request.closed` with `merged: false` → task closed
   - Handle `pull_request_review` and `issue_comment` events → no-op in Slice B (respawn logic lands in Slice C)

8. **Summary posting**
   - On terminal transition, Minos composes summary and posts to the task thread via Hermes

### Acceptance checkpoint for Slice B

- Operator posts `/commission fix bug 123` in Discord
- Pod spawns, opens PR, thread sidecar posts progress to the same Discord thread
- Operator merges the PR on GitHub
- Minos receives the webhook, transitions task to `completed`, posts summary to thread

---

## 5. Slice D — "guardrails on" (parallel with B)

**Proves:** Argus wall-clock cap and stall detection terminate misbehaving pods; task threads get warning/escalation/termination posts.

**Scope:** Argus logic bundled into Minos per `roadmap.md §Phase 1 Services on the Minos VM`; Argus-sidecar container in each pod; no push-event ingest yet (Phase 2).

Landing D in parallel with B ensures every later slice runs under real guardrails. Both slices touch `Minos` but in non-overlapping packages (`minos/argus/` vs. `cerberus/` + `hermes/`); they can land in either order.

### Tasks

1. **Argus-sidecar container**
   - `agents/sidecar/argus/` — minimal Go binary that emits heartbeat POST to Minos's Argus ingest endpoint on a configurable interval (default: 30s)
   - Runs in every Daedalus pod alongside the worker backend and thread sidecar

2. **Argus logic in Minos**
   - `minos/argus/` package — per-agent state table in Postgres (`started_at`, `last_heartbeat_at`, `token_count_self_reported`, `mcp_call_count`, `phase`)
   - Rules engine: wall-clock cap, heartbeat-silence threshold, warning/escalation/termination tiers per `architecture.md §7 Guardrails`
   - k3s watcher: poll pod phase on short cadence

3. **Heartbeat ingest endpoint**
   - `POST /argus/heartbeat` on Minos
   - Bearer-token check (Phase 1 posture; JWT is Phase 2)

4. **Tiered response**
   - Warning → post to task thread
   - Escalation → ping admin on configured surface (Hermes), pause agent (Phase 1: best-effort — no pod-pause primitive; escalation defaults to termination if not human-acknowledged within timeout)
   - Termination → `kubectl delete pod` with 30s grace, post incident to thread

5. **Startup reconciliation and grace period**
   - Per `architecture.md §7 State Persistence and Recovery` — recovery grace period on Minos restart to suppress false-positive stall alerts

### Acceptance checkpoint for Slice D

- A test pod that sleeps past its wall-clock cap is terminated with a thread post
- A test pod whose Argus sidecar is killed is detected as stalled and terminated
- Termination event visible in Ariadne

---

## 6. Slice C — "memory persists across runs"

**Proves:** run records persist; `awaiting-review` hibernation + respawn with Mnemosyne context drives the task to terminal. (Acceptance gate bullets 1 full, 3.)

**Scope:** Mnemosyne core + `awaiting-review` state + respawn logic. Untrusted-source tagging is Phase 2 per `architecture.md §14 Secret Sanitization` — not in scope here.

### Tasks

1. **Mnemosyne — pgvector backend**
   - `mnemosyne/postgres/` — schema with `run_records`, `learned_facts`, `project_contexts` tables; vector columns for embeddings
   - Migration landed in Slice A's pgvector setup; schema DDL lands here
   - SQLite reference implementation under `mnemosyne/sqlite/` for local-dev and plugin-interface testing (not a deployment target per roadmap)

2. **Mnemosyne service**
   - `mnemosyne/core/` — service with two surfaces:
     - Internal API for Minos: `memory.store_run(run_record)`, `memory.get_context(project_id, task_type)`
     - MCP broker for pods: `memory.lookup(query, scope)`
   - Sanitization pass mandatory before persistence per `architecture.md §14 Secret Sanitization`
   - Fact-extraction pipeline: simple for Phase 1, refine in Phase 2

3. **Worker plugin — memory extraction at SIGTERM**
   - Claude Code plugin writes run record (conversation log, scratchpad summary, artifact list) to a shared volume on SIGTERM
   - 30s grace window before SIGKILL
   - Minos picks up the blob and forwards to `memory.store_run`

4. **Minos — hibernation on `awaiting-review`**
   - New state `awaiting-review` in the task state machine
   - Trigger: agent signals PR opened via worker interface
   - Action: call `memory.store_run`, `kubectl delete pod`, record `context_ref` for respawn

5. **Minos — respawn on qualifying review events**
   - Webhook handler (from Slice B) extends to handle `pull_request_review` with `state: changes_requested` and `issue_comment` with `@mention` of the agent
   - Respawn flow: new `run_id`, same `task_id`, resolve `context_ref` via `memory.get_context`, spawn fresh pod with injected context

6. **Hibernation TTLs**
   - Reminder threshold, abandonment threshold — defaults tracked in `architecture.md §18 Open Questions`; pick concrete values during Slice C and document in config

7. **Context injection verification**
   - Integration test: two-run task where run 2's log demonstrably references a decision from run 1

### Acceptance checkpoint for Slice C

- Operator commissions a task from Discord
- Agent opens PR, task hibernates (pod deleted)
- Operator requests changes on the PR
- Minos respawns a fresh pod; new pod's run log shows prior-run context primed its work
- PR eventually merges, task finalizes; both run records visible in Postgres

---

## 7. Slice E — "Iris talks"

**Proves:** Iris answers "what's running?" and "start a task for X" on the same surface. (Acceptance gate bullet 2.)

**Scope:** Iris long-running pod on Athena-hosted Ollama backend.

### Tasks

1. **Athena Ollama reachability**
   - Iris pod's network-policy allowlist (Phase 1: firewall rules) includes Athena's Ollama port (11434)
   - Specific model chosen, pulled to Athena in advance; model name configured for Iris

2. **Iris pod image**
   - Long-running pod spec with `daedalus.project/pod-class: iris` label
   - Backend: Ollama HTTP client, conversation state persisted to Postgres `iris.conversations` schema (keyed by surface + thread + user identity per `architecture.md §10 Conversation State`)

3. **Minos state API**
   - `GET /state/tasks`, `GET /state/queue`, `GET /state/recent` — read-only endpoints Iris consumes
   - Bearer-token auth; Iris holds a dedicated token

4. **Iris capabilities wired up**
   - `mnemosyne.memory.lookup` client — semantic search over project memory
   - `hermes.events.next` — long-poll inbound message delivery
   - `hermes.post_as_iris` — scoped reply posting

5. **Command intake**
   - `@iris` mention, DM, or `/iris` slash command trigger
   - Two primary intents for Phase 1: state query (answer from Minos state API) and commission (translate to structured commission + confirm + forward with admin identity)
   - Iris never manufactures identity — passes through the Hermes-delivered `(surface, surface_id)`

6. **Safeguards**
   - Admin-only in Phase 1 (hardcoded admin config check)
   - Argus sidecar deferred to Phase 2 per `architecture.md §10 Pod Configuration` — document the stall-detection gap
   - Trust-boundary framing best-effort in Phase 1 per `architecture.md §10 Pod Configuration`

### Acceptance checkpoint for Slice E

- Operator asks Iris "what's running?" in Discord → Iris replies with current task list
- Operator asks Iris "start a task to fix bug 456" → Iris confirms, commissions, and the rest of the Slice A-D path executes
- Run record for the commissioned task exists; Iris's own conversation state persists across Iris pod replacement

---

## 8. Cross-Cutting Concerns

### Testing strategy per slice

- **Unit tests** on every Go package touched.
- **Integration tests** per slice: a scripted end-to-end run that exercises the slice's acceptance checkpoint against a dev Postgres and a kind cluster (or dev k3s).
- **Manual smoke test** on the real Crete deployment at each slice's acceptance checkpoint before declaring the slice done.

### Observability baseline

- All services emit structured JSON logs to stdout (`pkg/audit`) picked up by Vector on each VM.
- Vector ships to Loki on Ariadne; manual LogQL queries suffice for Phase 1 debugging. An Ariadne MCP query surface is a Phase 1+ stretch goal (candidate: `grafana/mcp-grafana` per `build-vs-adopt.md`).

### CI

- One GitHub Actions workflow for the monorepo with per-module test invocation.
- Required checks: `go vet`, `go test ./...`, `golangci-lint`, `go build ./...`.
- Per-module Dockerfile builds for pod-image components (Claude Code plugin, Iris pod, sidecars).

### Config and secrets

- Minos config: YAML file under `/etc/minos/config.yaml`, pointed to by a systemd `Environment=` directive.
- Secrets never in config; config holds `credentials_ref` names that resolve through the configured provider.
- Project config (single project in Phase 1) lives alongside Minos config.

### Documentation updates during build

- Whenever an implementation decision clarifies or contradicts `architecture.md`, update that doc rather than letting implementation drift.
- Open Questions in `architecture.md §18` get resolved or re-scoped during the slice that forces the decision; track resolutions in commit messages and the affected doc.

---

## 9. Risks and Open Questions

### Risks

- **Go MCP SDK maturity.** The community `mark3labs/mcp-go` and `modelcontextprotocol/go-sdk` are production-used (github/github-mcp-server, grafana/mcp-grafana, hashicorp/vault-mcp-server are all vendor-official Go MCP servers). Low-probability risk; evaluate during Slice A's plugin-interface design.
- **Postgres LXC single-point-of-failure.** Accepted per `roadmap.md §Scope Anchors`; fail-silent posture documented. Operational risk sits with Proxmox-level monitoring until Asclepius lands in Phase 3.
- **Cloudflare Tunnel as trusted intermediary.** Phase 1 accepts TLS termination at Cloudflare's edge per `security.md §2`. Deployment prerequisite: the operator is willing to accept this exposure.
- **Shared `claude-code` credential.** Blast radius bounded by Anthropic workspace spend cap per `security.md §3 Phase 1 exception`. Spend cap configuration is a Phase 1 deployment prerequisite.

### Open questions (to resolve during the slice that forces them)

- **Slice A:** migration tooling choice (golang-migrate / goose / atlas)
- **Slice B:** specific Discord slash-command shape vs. plain-text intake
- **Slice C:** hibernation reminder/abandonment TTL defaults per `architecture.md §18`
- **Slice C:** `context_ref` payload shape — inline in envelope vs. shared-volume reference, threshold for switching
- **Slice D:** concrete budget defaults (token cap, wall-clock cap) per `architecture.md §18`
- **Slice E:** specific Ollama model chosen for Iris; footprint constraints on Athena

---

*This plan is authoritative for Phase 1 sequencing. Update it when scope changes in `roadmap.md §Phase 1`, when a slice completes, or when an implementation decision clarifies an open question.*
