# Daedalus Architecture Additions
**For:** Clio (Documentation Pod)
**Purpose:** Document new components and AI routing recommendations for integration into `architecture.md`

---

## New Components

The following components were identified as missing from the current architecture to complete the autonomous software development team capability.

### Momus — Code Review Pod
**Role:** Automated PR review covering style, correctness, logic, and architectural drift detection.

**Rationale:** Code review is a distinct function from QA (Talos) and requires a dedicated pod. Without it, review burden falls on human operators or gets conflated with test execution. Momus triages all PRs as a first pass before escalating flagged items. Named for the Greek god of criticism and fault-finding, whose mythological function was identifying flaws in the work of other gods.

---

### Hephaestus — Architectural Assistant
**Role:** Drafts Architecture Decision Records (ADRs), surfaces coupling and structural concerns, visualizes system topology, and presents tradeoffs for human decision. Does not make autonomous architectural decisions.

**Rationale:** A fully autonomous architecture pod poses unacceptable risk — wrong structural decisions compound across every other pod. Hephaestus operates as an assistant that informs human judgment rather than replacing it. Named for the master craftsman of the gods, who built the divine infrastructure and created autonomous constructs — Daedalus's divine counterpart.

---

### Clio — Documentation Pod
**Role:** Generates and maintains READMEs, API documentation, changelogs, and Architecture Decision Records. Consumes code review output and commit history as primary inputs.

**Rationale:** Documentation is consistently neglected in automated pipelines and left as human burden. A dedicated pod prevents the "working software, no docs" failure mode. Named for the Muse of history and record-keeping.

---

### Prometheus — DevOps / Release Pod
**Role:** Manages pipeline configuration, environment promotion, versioning, artifact publication, and release orchestration.

**Rationale:** Release engineering is distinct from development. Owning the *how it ships* layer separately from implementation prevents release logic from polluting application code and allows release processes to evolve independently. Named for the titan who brought capability into the world and enabled everything else to function.

---

### Themis — Project Management Pod
**Role:** Owns the backlog, decomposes epics into tasks, tracks work state across pods, and resolves cross-pod blockers. Acts as the primary orchestration layer and human-in-the-loop confirmation point.

**Rationale:** Without a PM pod, the other pods have no foreman — they can execute tasks but cannot determine priority, sequence, or whether a task is complete relative to the project goal. Themis is the load-bearing pod; it is the first pod that should be operational. Named for the goddess of divine order, proper procedure, and scheduling.

---

## AI Routing Recommendations

These recommendations apply to all pods, both existing and new. The core heuristic:

> **Structured artifact generation against a known schema → local model. Novel judgment, ambiguity resolution, or adversarial reasoning → Claude (Apollo).**

### Routing Table

| Component | Name | Recommended Route | Primary Model | Notes |
|---|---|---|---|---|
| Project Management | Themis | Local | qwen3.5:27b | Task decomposition against known schema is well within a 27B model. High call volume makes local routing important. |
| Research | Pythia | Hybrid | Local: qwen3.5:27b / Claude: Sonnet via Apollo | Retrieval, summarization, chunking → local + Qdrant. Cross-domain synthesis or ambiguous findings → Claude. |
| Development | Daedalus | Hybrid | Local: qwen2.5-coder:32b / Claude: Sonnet via Apollo | Boilerplate scaffolding → local. Non-trivial implementation → Claude. Quality of output matters too much to route entirely local. |
| Code Review | Momus | Hybrid | Local: qwen2.5-coder:32b (triage) / Claude: Sonnet via Apollo (escalation) | Two-stage: local model does full sweep, only items above confidence threshold escalate to Claude. Expected to reduce Claude calls on review by 60–70% per PR. |
| QA | Talos | Local | qwen2.5-coder:32b | Test scaffolding and coverage analysis are structured tasks. Complex edge case synthesis is the exception and may warrant escalation. |
| Documentation | Clio | Local | qwen3.5:27b | High volume, templated output, low reasoning ceiling. Docstring, README, and changelog generation are pattern-based. |
| DevOps / Release | Prometheus | Local | qwen3.5:27b | Config generation, version bumping, and YAML scaffolding are pattern-based tasks. |
| Architectural Assistant | Hephaestus | Claude | Sonnet via Apollo (Opus for complex decisions) | Low call frequency, high stakes. Worth the token cost. Sonnet as default; escalate to Opus for genuinely ambiguous structural decisions. |
| Red Team | Minotaur | Claude | Sonnet via Apollo (consider Opus) | Adversarial reasoning — finding non-obvious attack paths, chaining vulnerabilities, prompt injection against internal agents — requires genuine novel judgment. A local model running patterns is a second security scanner, not red teaming. |

### Model Assignments (Athena / Ollama)

| Model | Role |
|---|---|
| qwen3.5:27b | General reasoning workhorse: PM, Documentation, DevOps, Research (local tier) |
| qwen2.5-coder:32b | Code-specific tasks: Code Review triage, QA scaffolding, Development scaffolding |
| qwen3.5:35b-a3b | Fast triage / first-pass filter before escalation decisions |

### Claude Tier (via Apollo)

Default to `claude-sonnet-4`. Escalate to `claude-opus-4` only at specific call sites where Sonnet output consistently requires human correction. Expected Opus usage: Architecture (complex decisions), Minotaur (adversarial depth), and any pod where empirical observation shows Sonnet failing.

---

## Open Questions for Human Review

1. **Minotaur scope conflict:** Current `architecture.md` describes Minotaur as "sandbox test runner for recovery validation." Per Project Daedalus design sessions, Minotaur is the red team pod. This conflict needs resolution before `architecture.md` is updated.
2. **Hephaestus autonomy boundary:** The exact scope of what Hephaestus can propose vs. what requires human confirmation should be defined in its pod spec before implementation.
3. **Themis as Argus integration point:** Themis (PM) and Argus (guardrails monitor) likely need a defined interface — Themis is the pod that would receive Argus escalations and decide whether to halt, redirect, or escalate to human.
