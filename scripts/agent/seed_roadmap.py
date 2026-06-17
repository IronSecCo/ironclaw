#!/usr/bin/env python3
"""Seed the "Road to 1.0" roadmap into the registry + GitHub Issues.

Single source of truth for the post-backend roadmap (Waves 6-8): nanoclaw/openclaw
product-surface parity, open-source launch readiness, and the web UI. Mirrors the
agent-reconciler philosophy — registry is authoritative, issues are generated from it.

Usage:
  python3 scripts/agent/seed_roadmap.py --write-registry     # update .agents/task-registry.json
  python3 scripts/agent/seed_roadmap.py --create-issues      # create any missing [T-2xx] GitHub issues
  python3 scripts/agent/seed_roadmap.py --print              # dump the planned issues (dry run)

Idempotent: re-running --create-issues skips tasks whose [T-xxx] title already exists.
Requires: gh (authenticated) for --create-issues. base_sha is stamped by the caller.
"""
from __future__ import annotations
import argparse, json, os, subprocess, sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
REGISTRY = REPO_ROOT / ".agents" / "task-registry.json"

# Tasks T-001..T-120 are all landed (every GitHub issue closed). Flattened to `completed`.
DONE_IDS = [
    "T-086", "T-100", "T-101", "T-102", "T-103", "T-104", "T-105", "T-106",
    "T-107", "T-108", "T-109a", "T-109b", "T-110", "T-111", "T-112", "T-113",
    "T-114", "T-116", "T-118", "T-120",
]

CONTRACT = "internal/contract/**"

# ── The Road to 1.0 ─────────────────────────────────────────────────────────
# wave 6 = public-launch readiness · wave 7 = product parity + web UI · wave 8 = trust & ecosystem
NEW_TASKS = [
    # ───────────────────────── Wave 6 — launch readiness ─────────────────────
    {
        "task_id": "T-200", "title": "SECURITY.md + Private Vulnerability Reporting",
        "priority": "P0", "wave": 6, "size": "S", "category": "security", "good_first_issue": True,
        "related_gaps": ["G-044"], "owned_paths": ["SECURITY.md"],
        "summary": "A security product with no disclosure policy is a trust smell — and both peers (nanoclaw, openclaw) fail GitHub's check here. Ship a SECURITY.md in a recognized location and enable native Private Vulnerability Reporting.",
        "acceptance_criteria": [
            "SECURITY.md at repo root: supported-versions table, security@ contact, instruction to use PVR as the primary channel",
            "Response-time commitment (initial response <= 14 days), coordinated-disclosure timeline, safe-harbor language, optional PGP key",
            "Private Vulnerability Reporting enabled in repo Settings -> Code security (manual; documented in the issue)",
        ],
        "validation_commands": ["test -f SECURITY.md"],
        "notes_for_worker": "Single most important file for a security product. Bug-report issue template (T-202) must redirect here.",
    },
    {
        "task_id": "T-201", "title": "CODE_OF_CONDUCT.md (Contributor Covenant)",
        "priority": "P0", "wave": 6, "size": "XS", "category": "docs", "good_first_issue": True,
        "related_gaps": ["G-044"], "owned_paths": ["CODE_OF_CONDUCT.md"],
        "summary": "Required community-health file; both peers ship one. Adopt Contributor Covenant 2.1 with a real enforcement contact.",
        "acceptance_criteria": [
            "CODE_OF_CONDUCT.md at root, Contributor Covenant 2.1, enforcement email filled in (not the placeholder)",
            "Green check on the repo community profile",
        ],
        "validation_commands": ["test -f CODE_OF_CONDUCT.md"],
        "notes_for_worker": "GitHub can add this via the web UI in two clicks; commit the result so it is version-controlled.",
    },
    {
        "task_id": "T-202", "title": "Issue templates (forms) + PR template",
        "priority": "P0", "wave": 6, "size": "S", "category": "docs", "good_first_issue": True,
        "related_gaps": ["G-044"], "owned_paths": [".github/ISSUE_TEMPLATE/**", ".github/pull_request_template.md"],
        "summary": "Reduce triage burden and route security reports correctly. Add bug-report + feature-request issue forms, a config.yml that disables blank issues and links Discussions/Discord/Security, and a PR template.",
        "acceptance_criteria": [
            "Bug Report + Feature Request as .yml issue forms with valid name/description keys",
            "config.yml: blank_issues_enabled false + contact links (Security policy, Discussions, Discord); bug form redirects security issues to SECURITY.md",
            "pull_request_template.md with a checklist (tests pass, docs updated, contract impact, AGENTS.md scope honored)",
        ],
        "validation_commands": ["test -d .github/ISSUE_TEMPLATE", "test -f .github/pull_request_template.md"],
        "notes_for_worker": "Templates must live in .github/ISSUE_TEMPLATE with exact key names or GitHub ignores them.",
    },
    {
        "task_id": "T-203", "title": "README overhaul: hero command, asciinema cast, security teaser",
        "priority": "P0", "wave": 6, "size": "M", "category": "docs",
        "related_gaps": ["G-045"], "owned_paths": ["README.md"],
        "summary": "The README is the whole first impression. Lead with logo + one-line pitch, a single hero install command and the exact next command/API URL it yields, a security-model teaser, and links to docs. Since there is no UI, substitute an asciinema cast (install -> first approval -> result) for the missing screenshot and frame CLI+API-first as a feature.",
        "acceptance_criteria": [
            "Hero section: logo, one-line pitch, <=4 meaningful badges (release, license, CI, Discord) — not a wall of shields",
            "One hero install command (the existing install.sh) immediately followed by the first command + API base URL, and a time-to-result phrase",
            "An asciinema cast or terminal GIF embedded; CLI+API-first explicitly framed as a feature; security-model teaser + docs links",
        ],
        "validation_commands": ["test -f README.md"],
        "notes_for_worker": "A static screenshot is the one near-universal element a UI-less product lacks; the asciinema recording is the idiomatic fix.",
    },
    {
        "task_id": "T-204", "title": "Repo metadata: description, topics, social-preview image",
        "priority": "P0", "wave": 6, "size": "XS", "category": "docs", "good_first_issue": True,
        "related_gaps": ["G-045"], "owned_paths": ["docs/assets/social-preview.png"],
        "summary": "Drive discovery and control how the repo looks when shared. Set a crisp description, 8-12 topics, and an Open-Graph social-preview image.",
        "acceptance_criteria": [
            "Description set; 8-12 topics (ai-agents, ai-assistant, self-hosted, security, sandbox, golang, claude, personal-assistant, mcp, agent-platform)",
            "1280x640 branded social-preview image committed to docs/assets/ and set in Settings -> Social preview",
        ],
        "validation_commands": [],
        "notes_for_worker": "Description/topics are GitHub repo settings (manual); commit the preview asset so it is reproducible.",
    },
    {
        "task_id": "T-205", "title": "docker-compose.yml + .env.example + published image",
        "priority": "P0", "wave": 6, "size": "M", "category": "infra",
        "related_gaps": ["G-046"], "owned_paths": ["docker-compose.yml", ".env.example", ".github/workflows/image.yml"],
        "locks_required": ["lock:ci", "lock:release"],
        "summary": "The most standardized onboarding pattern in the category is `cp .env.example .env && docker compose up -d`. Ship it, and publish the control-plane image to GHCR. The compose topology must reflect the real isolation posture (sandbox network=none), not flatten it.",
        "acceptance_criteria": [
            "`docker compose up -d` from a clone brings up the control-plane + gateway; .env.example documents every required var",
            "First run mints the admin/API token with a 'claim it / no recovery' warning",
            "A workflow publishes the control-plane image to GHCR on release; README documents the pull",
        ],
        "validation_commands": ["docker compose config -q"],
        "notes_for_worker": "Coordinate on .github/workflows with T-208/T-253/T-254 via lock:ci/lock:release. Do not weaken network=none in compose.",
    },
    {
        "task_id": "T-206", "title": "Guided onboarding wizard (ironctl init / onboard)",
        "priority": "P1", "wave": 6, "size": "L", "category": "feature",
        "related_gaps": ["G-038"], "owned_paths": ["cmd/ironctl/onboard.go", "internal/host/onboard/**"],
        "summary": "nanoclaw.sh and `openclaw onboard` get a user to a working assistant in minutes; IronClaw leaves env wiring, service install and channel auth as manual steps. Add an interactive `ironctl onboard` that detects the container runtime, mints the API token, registers the Anthropic credential host-side, builds/pulls the sandbox image, pairs a first channel, and verifies end to end.",
        "acceptance_criteria": [
            "`ironctl onboard` runs an idempotent, resumable wizard (detect runtime -> token -> model credential -> sandbox image -> pair one channel -> verify)",
            "Flags --yes (non-interactive) and --dry-run; refuses to overwrite existing config without --force",
            "On success prints the first message to send and the gateway/API URL",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./cmd/ironctl/...", "CGO_ENABLED=1 go build ./cmd/ironctl"],
        "notes_for_worker": "Pure host-side orchestration over existing APIs; do not touch the frozen contract.",
    },
    {
        "task_id": "T-207", "title": "5-minute Quickstart tutorial",
        "priority": "P1", "wave": 6, "size": "S", "category": "docs", "good_first_issue": True,
        "related_gaps": ["G-038"], "owned_paths": ["docs/quickstart.md"],
        "summary": "A copy-pasteable 'zero to first approved action in 5 minutes' guide using --dev mode (no gVisor), ending at a real approve + audit.",
        "acceptance_criteria": [
            "docs/quickstart.md: prerequisites, install, run --dev, submit a change, approve, read audit — each step copy-pasteable",
            "Linked from README and the docs site (T-250)",
        ],
        "validation_commands": ["test -f docs/quickstart.md"],
        "notes_for_worker": "Mirror the local-dev flow already in the inventory; keep it provider-key-light.",
    },
    {
        "task_id": "T-208", "title": "Homebrew tap + CHANGELOG + curated release notes",
        "priority": "P1", "wave": 6, "size": "S", "category": "infra",
        "related_gaps": ["G-052"], "owned_paths": ["CHANGELOG.md", "packaging/homebrew/**"],
        "locks_required": ["lock:release"],
        "summary": "IronClaw's Go binary + existing release pipeline is its distribution edge over the Node/Python peers. A Homebrew tap is the standard instant-install for a CLI; a CHANGELOG is an opensource.guide expectation and a Scorecard signal.",
        "acceptance_criteria": [
            "`brew install nivardsec/tap/ironclaw` installs ironctl + control-plane (formula generated from releases)",
            "CHANGELOG.md (Keep a Changelog format) and curated GitHub Release notes templated from commits",
        ],
        "validation_commands": ["test -f CHANGELOG.md"],
        "notes_for_worker": "The tap may live in a sibling repo (nivardsec/homebrew-tap); keep the formula source under packaging/homebrew. Coordinate on release.yml via lock:release (T-253).",
    },
    {
        "task_id": "T-209", "title": "Public-repo ruleset adapted to the push-to-main flow",
        "priority": "P0", "wave": 6, "size": "S", "category": "infra",
        "related_gaps": ["G-058"], "owned_paths": [".github/rulesets/main.json"],
        "locks_required": ["lock:ci"],
        "summary": "Public repos are expected to protect the default branch, but a hard PR-review gate would break IronClaw's authorized two-agent push-to-main build. Keep the trust signal via required status checks + signed commits + linear history instead of a review gate.",
        "acceptance_criteria": [
            "Ruleset on main: require CI + CodeQL green, block force-push, require linear history and signed commits — while still permitting direct pushes by authorized actors",
            "Ruleset exported to .github/rulesets/main.json (documented as the source of truth)",
        ],
        "validation_commands": [],
        "notes_for_worker": "Do NOT add required PR reviews — that contradicts AGENTS.md direct-push CAS. CAS preflight stays the guard.",
    },
    {
        "task_id": "T-210", "title": "Support path: Discussions + Discord + seeded good-first-issues",
        "priority": "P1", "wave": 6, "size": "S", "category": "docs",
        "related_gaps": ["G-053"], "owned_paths": [".github/DISCUSSIONS.md"],
        "summary": "A chat community is mandatory in this category (both peers run a Discord). Enable GitHub Discussions (Q&A + Announcements), stand up a Discord, and seed 5-10 scoped good-first-issues so the tracker does not look inactive.",
        "acceptance_criteria": [
            "Discussions enabled (Q&A + Announcements); Discord invite linked from README + issue-template config.yml",
            "5-10 real, well-described starter issues labeled 'good first issue' and pinned",
        ],
        "validation_commands": [],
        "notes_for_worker": "Mostly GitHub settings; commit a short DISCUSSIONS.md describing which channel is for what.",
    },

    # ───────────────────── Wave 7 — product parity + web UI ───────────────────
    {
        "task_id": "T-220", "title": "[spike] Web console architecture + scaffold",
        "priority": "P1", "wave": 7, "size": "spike", "category": "spike",
        "related_gaps": ["G-037"], "owned_paths": [".agents/spikes/web-console.md", "web/**"],
        "summary": "Every exemplar ships a web dashboard as its primary interface; IronClaw is CLI+API only — its single biggest structural gap. Decide the stack (recommended: an embedded Go-served SPA on a loopback port, like openclaw's Control UI) and land the scaffold that T-221..T-226 build on.",
        "acceptance_criteria": [
            "Spike doc: stack choice (SPA framework, embed vs separate, auth reuse of the API token, loopback-only binding), security review of serving a UI from the control-plane",
            "Scaffold: web/ app builds and is served by the control-plane at a loopback port behind the existing bearer-token auth; CI builds it",
        ],
        "validation_commands": ["test -f .agents/spikes/web-console.md"],
        "notes_for_worker": "Bind loopback-only by default (cf. openclaw 127.0.0.1:18789). The UI must never widen the network posture. Sole owner of the web/ scaffold; feature tasks own subtrees under it.",
    },
    {
        "task_id": "T-221", "title": "Web UI: Approvals inbox",
        "priority": "P1", "wave": 7, "size": "L", "category": "feature",
        "related_gaps": ["G-037"], "owned_paths": ["web/src/routes/approvals/**", "internal/host/api/ui_approvals.go"],
        "depends_on": ["T-220"],
        "summary": "The killer first UI feature for IronClaw: surface the human-approval gateway in a browser. List pending changes, show a readable diff of each requested mutation, and approve/reject with attribution.",
        "acceptance_criteria": [
            "Pending-changes list with per-change detail (kind, group, requester, payload) rendered readably",
            "Approve/reject actions call /v1/changes/{id}/decision; the list updates live",
            "Backed by a read-model endpoint; no new contract surface",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/api/..."],
        "notes_for_worker": "Reuses existing gateway endpoints. Disjoint route subtree + one backend file.",
    },
    {
        "task_id": "T-222", "title": "Web UI: Sessions browser",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-037"], "owned_paths": ["web/src/routes/sessions/**", "internal/host/api/ui_sessions.go"],
        "depends_on": ["T-220"],
        "summary": "List live sessions, inspect status/group/queue depth, and terminate a session — over the existing registry session endpoints.",
        "acceptance_criteria": [
            "Sessions list + detail view; terminate action wired to the host",
            "Read-model endpoint; disjoint route subtree",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/api/..."],
        "notes_for_worker": "Builds on /v1/registry/sessions.",
    },
    {
        "task_id": "T-223", "title": "Web UI: Channels & wiring management",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-037"], "owned_paths": ["web/src/routes/channels/**", "internal/host/api/ui_channels.go"],
        "depends_on": ["T-220"],
        "summary": "A visual surface to connect agent groups to messaging groups (wirings) and manage destinations — the registry CRUD that is CLI-only today.",
        "acceptance_criteria": [
            "Create/list wirings and destinations from the UI over existing registry endpoints",
            "Channel credential setup is guided (token fields with redaction)",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/api/..."],
        "notes_for_worker": "Mutations still flow through the gateway where applicable.",
    },
    {
        "task_id": "T-224", "title": "Web UI: Logs & audit viewer",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-037"], "owned_paths": ["web/src/routes/logs/**", "internal/host/api/ui_audit.go"],
        "depends_on": ["T-220"],
        "summary": "A real-time, filterable view over the append-only gateway audit log and structured logs (filter by level/module, search, export) — the observability surface every exemplar ships.",
        "acceptance_criteria": [
            "Audit log view with filter/search/export over /v1/audit",
            "Live tail of structured logs (read-only)",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/api/..."],
        "notes_for_worker": "Read-only; never expose secrets (reuse obs redaction).",
    },
    {
        "task_id": "T-225", "title": "Web UI: Config editor + web onboarding wizard",
        "priority": "P2", "wave": 7, "size": "L", "category": "feature",
        "related_gaps": ["G-037", "G-038"], "owned_paths": ["web/src/routes/setup/**", "internal/host/api/ui_config.go"],
        "depends_on": ["T-220"],
        "summary": "Browser counterpart to T-206: a first-run wizard (model credential, sandbox runtime, first channel) plus an in-UI config editor with validation — the 'almost everything is done from the UI' expectation (Home Assistant) without weakening the gateway.",
        "acceptance_criteria": [
            "First-run wizard mirrors `ironctl onboard` steps in the browser",
            "Config editor with validation; every write that mutates capabilities still routes through the gateway",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/api/..."],
        "notes_for_worker": "Depends on T-206 semantics; do not bypass the approval choke point.",
    },
    {
        "task_id": "T-226", "title": "Web UI: Chat playground",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-037"], "owned_paths": ["web/src/routes/chat/**", "internal/host/api/ui_chat.go"],
        "depends_on": ["T-220"],
        "summary": "A local test-conversation surface to message an agent group without wiring a real channel — the fastest path to 'see it work' after install.",
        "acceptance_criteria": [
            "Send/receive messages to a chosen agent group via an internal webchat destination",
            "Shows tool calls + token usage inline",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/api/..."],
        "notes_for_worker": "Implement as a first-class webchat channel adapter feeding the normal router/delivery path.",
    },
    {
        "task_id": "T-227", "title": "[spike] Host-side skills / extension system design",
        "priority": "P2", "wave": 7, "size": "spike", "category": "spike",
        "related_gaps": ["G-036"], "owned_paths": [".agents/spikes/skills-system.md"],
        "status": "needs-human", "requires_human_decision": True,
        "human_question": "Do we add a host-side, gateway-approved skills/extension mechanism (manifest + capability grant), given the 'sealed runtime / no in-sandbox install' pillar? If yes, what is the trust model for third-party skills?",
        "summary": "Skills are the headline extensibility of both peers (nanoclaw /add-* branch-copy model; openclaw SKILL.md + ClawHub, 13.7k+ skills). IronClaw deliberately forbids in-sandbox self-install. A host-side, gateway-approved capability-bundle mechanism could give parity without breaking the seal — but it is an architecture + threat-model decision, not a unilateral build.",
        "acceptance_criteria": [
            "Spike doc: a skill manifest format, where skills live (host-curated vs registry), how a skill maps to gateway-approved capability grants, and the supply-chain risk (cf. Koi Security's 341 malicious ClawHub skills)",
            "Recommendation: build / defer / integrate-existing, with a follow-up task breakdown if 'build'",
        ],
        "validation_commands": ["test -f .agents/spikes/skills-system.md"],
        "notes_for_worker": "Do not implement before the design is approved. The seal (no in-sandbox mutation) is non-negotiable; any skill capability must be a host-side, human-approved grant.",
    },
    {
        "task_id": "T-228", "title": "Channel adapter: WhatsApp",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature", "good_first_issue": True,
        "related_gaps": ["G-039"], "owned_paths": ["internal/host/channels/whatsapp.go", "internal/host/channels/whatsapp_test.go"],
        "forbidden_paths_extra": ["internal/host/channels/channels.go", "cmd/controlplane/main.go"],
        "summary": "WhatsApp is the flagship channel for both peers and the top parity ask. Add an adapter implementing channels.Adapter, following the existing telegram.go/slack.go/discord.go pattern.",
        "acceptance_criteria": [
            "Adapter implements channels.Adapter (send message + file) via a WhatsApp Business/Cloud API or bridge",
            "Token/secret redaction in errors; unit tests with a fake transport",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/channels/..."],
        "notes_for_worker": "Registration in main.go is wired by the daemon-wiring owner — do not edit main.go here. Disjoint file from other adapters.",
    },
    {
        "task_id": "T-229", "title": "Channel adapter: Email / Gmail",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-039"], "owned_paths": ["internal/host/channels/email.go", "internal/host/channels/email_test.go"],
        "forbidden_paths_extra": ["internal/host/channels/channels.go", "cmd/controlplane/main.go"],
        "summary": "Email/Gmail is a first-class channel on both peers (Resend outbound; Gmail Pub/Sub triggers). Add an SMTP/IMAP (or Gmail API) adapter so agents can send and receive mail.",
        "acceptance_criteria": [
            "Adapter sends via SMTP/Gmail API and (optionally) ingests via IMAP/Pub/Sub into the inbound path",
            "Credential redaction; unit tests",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/channels/..."],
        "notes_for_worker": "Inbound ingestion may need a small poller on the host side; keep it disjoint from delivery.",
    },
    {
        "task_id": "T-230", "title": "Channel adapter: Matrix",
        "priority": "P3", "wave": 7, "size": "M", "category": "feature", "good_first_issue": True,
        "related_gaps": ["G-039"], "owned_paths": ["internal/host/channels/matrix.go", "internal/host/channels/matrix_test.go"],
        "forbidden_paths_extra": ["internal/host/channels/channels.go", "cmd/controlplane/main.go"],
        "summary": "Matrix is a privacy-friendly, self-host-aligned channel both peers support. Add a client-server API adapter.",
        "acceptance_criteria": [
            "Adapter implements channels.Adapter against the Matrix client-server API",
            "Token redaction; unit tests",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/channels/..."],
        "notes_for_worker": "Good first issue — clone the telegram.go shape.",
    },
    {
        "task_id": "T-231", "title": "Channel adapter: Google Chat",
        "priority": "P3", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-039"], "owned_paths": ["internal/host/channels/googlechat.go", "internal/host/channels/googlechat_test.go"],
        "forbidden_paths_extra": ["internal/host/channels/channels.go", "cmd/controlplane/main.go"],
        "summary": "Google Chat is a common workplace channel on both peers. Add a webhook/bot adapter.",
        "acceptance_criteria": [
            "Adapter implements channels.Adapter for Google Chat spaces",
            "Credential redaction; unit tests",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/channels/..."],
        "notes_for_worker": "Disjoint file; registration via the daemon-wiring owner.",
    },
    {
        "task_id": "T-232", "title": "Additional channel adapters: Teams / iMessage / Signal (tracking)",
        "priority": "P3", "wave": 7, "size": "L", "category": "feature",
        "related_gaps": ["G-039"], "owned_paths": ["internal/host/channels/teams.go", "internal/host/channels/imessage.go", "internal/host/channels/signal.go"],
        "forbidden_paths_extra": ["internal/host/channels/channels.go", "cmd/controlplane/main.go"],
        "summary": "Tracking issue to close the long-tail channel gap vs openclaw's 23+ and nanoclaw's 13: Microsoft Teams, iMessage (macOS), Signal. Each is an independent adapter; split into sub-issues when claimed.",
        "acceptance_criteria": [
            "Teams adapter (bot framework)",
            "iMessage adapter (macOS host bridge)",
            "Signal adapter (signal-cli/bridge)",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/channels/..."],
        "notes_for_worker": "Umbrella — prefer one PR per adapter, disjoint files. iMessage is macOS-only.",
    },
    {
        "task_id": "T-233", "title": "Multi-provider model support (per-agent provider)",
        "priority": "P3", "wave": 7, "size": "L", "category": "feature",
        "related_gaps": ["G-040"], "owned_paths": ["internal/host/modelproxy/**", "internal/sandbox/provider/**"],
        "status": "needs-human", "requires_human_decision": True,
        "human_question": "Do we relax the Anthropic-via-proxy posture to allow per-agent providers (OpenAI/Codex, Ollama/local, OpenRouter)? What is the egress + audit story for each, and does the model proxy allowlist expand?",
        "summary": "Peers allow per-agent provider selection via drop-in modules (/add-codex, /add-ollama-provider, OpenCode). IronClaw is Anthropic-via-host-proxy by threat model. Supporting others means new egress destinations through the model proxy — a deliberate threat-model decision.",
        "acceptance_criteria": [
            "modelproxy allowlist + provider abstraction supports >=1 additional provider behind config, per agent group",
            "Egress stays gateway-governed; audit covers all providers; threat-model sign-off",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/modelproxy/...", "CGO_ENABLED=1 go test ./internal/sandbox/..."],
        "notes_for_worker": "Do not start before the network-posture decision is approved (related to the egress-broker work).",
    },
    {
        "task_id": "T-234", "title": "First-class persona / identity surface",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-042"], "owned_paths": ["internal/host/registry/persona.go", "internal/sandbox/tools/persona.go"],
        "summary": "Peers make the assistant feel personal via SOUL.md/CLAUDE.md persona files and a default name (@Andy). IronClaw has a persona change-kind and per-group workspace but no first-class identity surface. Add a per-group persona/identity (name + system persona) editable via the gateway and visible to the agent.",
        "acceptance_criteria": [
            "Per-group persona + agent name stored in the registry, set via a gateway-approved change",
            "The sandbox loop loads the persona into the system context; a tool can read (not self-edit) it",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/registry/...", "CGO_ENABLED=1 go test ./internal/sandbox/..."],
        "notes_for_worker": "Persona edits are gateway-approved (no in-sandbox self-edit). Confirm no frozen-contract change; if cross-seam, STOP + RFC.",
    },
    {
        "task_id": "T-235", "title": "In-product observability: ironctl status / doctor / usage",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-043"], "owned_paths": ["cmd/ironctl/status.go", "cmd/ironctl/doctor.go"],
        "summary": "Peers ship rich self-diagnostics (openclaw /status /trace /usage, `openclaw doctor`; nanoclaw chat-based debug). Add `ironctl status` (daemon/session health), `ironctl doctor` (preflight: runtime, gVisor, token, model reachability), and a token-usage report.",
        "acceptance_criteria": [
            "`ironctl status` summarizes daemon health, live sessions, pending approvals, last delivery",
            "`ironctl doctor` checks runtime/gVisor/token/model-proxy reachability and prints actionable fixes",
            "Token-usage report from model-proxy audit records",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./cmd/ironctl/...", "CGO_ENABLED=1 go build ./cmd/ironctl"],
        "notes_for_worker": "Read-only over existing endpoints/metrics. Disjoint files from onboard (T-206).",
    },

    # ─────────────────── Wave 8 — trust, supply-chain & ecosystem ─────────────
    {
        "task_id": "T-250", "title": "Documentation site (Mintlify/Fumadocs)",
        "priority": "P1", "wave": 8, "size": "L", "category": "docs",
        "related_gaps": ["G-047"], "owned_paths": ["docs/site/**", "docs/docs.json"],
        "summary": "7/7 exemplars and both peers run a dedicated docs site; a README alone reads as pre-1.0. Stand up a docs site (Mintlify or Fumadocs) with the consensus IA: Quickstart -> Concepts -> Guides -> Self-Hosting -> Configuration -> CLI + API Reference -> Architecture -> Security Model.",
        "acceptance_criteria": [
            "Docs site builds and deploys (CI + a docs.* domain or GitHub Pages)",
            "IA covers quickstart, concepts, self-hosting, config, CLI ref, API ref (T-251), architecture, security model (T-252)",
        ],
        "validation_commands": ["test -d docs/site"],
        "notes_for_worker": "Mintlify ships /llms.txt for free (useful for an AI project). Pull existing docs/*.md in as the starting content.",
    },
    {
        "task_id": "T-251", "title": "Publish & check in the OpenAPI spec",
        "priority": "P1", "wave": 8, "size": "S", "category": "docs",
        "related_gaps": ["G-048"], "owned_paths": ["api/openapi.yaml"],
        "summary": "For a UI-less product the OpenAPI spec IS the contract — it documents the API, future-proofs the dashboard (T-22x), and lets third parties build against you. IronClaw already has formal contract discipline; expose the control-plane API as a versioned, checked-in OpenAPI document.",
        "acceptance_criteria": [
            "Versioned api/openapi.yaml covering every /v1 route with auth/token scopes",
            "Rendered as the API reference in the docs site",
        ],
        "validation_commands": ["test -f api/openapi.yaml"],
        "notes_for_worker": "Document the gateway + registry endpoints from internal/host/api. Keep in sync as the API evolves.",
    },
    {
        "task_id": "T-252", "title": "Threat-model document expansion (STRIDE + data-flow)",
        "priority": "P1", "wave": 8, "size": "M", "category": "security",
        "related_gaps": ["G-049"], "owned_paths": ["docs/threat-model.md"],
        "summary": "Security is the battlefield for this category and IronClaw's natural high ground. Expand the threat model into a full document: assets, data-flow diagram, trust boundaries, adversaries, a STRIDE pass per boundary, mitigations, and explicit non-goals — also scoping what counts as a 'vulnerability' so disclosure stays sane.",
        "acceptance_criteria": [
            "Covers host<->sandbox, control-plane<->agent, agent<->agent, and egress-broker<->network boundaries with STRIDE",
            "Data-flow diagram + privilege matrix; versioned with the code; linked from README and SECURITY.md",
        ],
        "validation_commands": ["test -f docs/threat-model.md"],
        "notes_for_worker": "Do not edit docs/contract.md. Build on the existing threat-model.md.",
    },
    {
        "task_id": "T-253", "title": "Signed releases + SBOM + provenance (GoReleaser)",
        "priority": "P1", "wave": 8, "size": "M", "category": "security",
        "related_gaps": ["G-050"], "owned_paths": [".goreleaser.yaml", ".github/workflows/release.yml"],
        "locks_required": ["lock:release"],
        "summary": "A security tool that ships unsigned binaries undermines its own thesis. Neither peer offers SBOM/signing — a place IronClaw can decisively win, and the release pipeline already exists. Sign checksums + image (cosign, keyless), emit build provenance attestations, and attach an SBOM to every release.",
        "acceptance_criteria": [
            "GoReleaser builds binaries, generates SBOMs (syft, SPDX + CycloneDX), signs the checksum file keylessly (cosign), emits GitHub artifact attestations for binaries + image",
            "README documents verification (cosign verify / gh attestation verify)",
        ],
        "validation_commands": ["test -f .goreleaser.yaml"],
        "notes_for_worker": "Acquire lock:release; coordinate with T-205/T-208/T-256 on the release workflow. Mind CGO determinism.",
    },
    {
        "task_id": "T-254", "title": "Supply-chain hygiene (Dependabot / CodeQL / secret scanning / pinned actions)",
        "priority": "P1", "wave": 8, "size": "S", "category": "security",
        "related_gaps": ["G-051"], "owned_paths": [".github/dependabot.yml", ".github/workflows/codeql.yml"],
        "locks_required": ["lock:ci"],
        "summary": "A compromised dependency or leaked CI secret turns the repo into the attack vector against every downstream user — reputationally fatal for a security vendor. All are free for public repos and map to Scorecard checks; openclaw already runs CodeQL + Dependabot.",
        "acceptance_criteria": [
            "Dependabot alerts + version updates on; CodeQL running on push+PR for Go",
            "Secret scanning + push protection on; every third-party Action pinned to a full commit SHA; top-level permissions: contents: read with per-job elevation",
        ],
        "validation_commands": ["test -f .github/dependabot.yml"],
        "notes_for_worker": "Coordinate on .github/workflows via lock:ci. Some toggles are repo settings (document them).",
    },
    {
        "task_id": "T-255", "title": "OpenSSF Scorecard + Best Practices badges",
        "priority": "P2", "wave": 8, "size": "M", "category": "security",
        "related_gaps": ["G-054"], "owned_paths": [".github/workflows/scorecard.yml"],
        "locks_required": ["lock:ci"],
        "summary": "The two most recognized at-a-glance trust signals on a security repo, and Scorecard ratifies much of Wave 8. Few peers display either — a differentiator. Run the Scorecard Action (publish_results) and earn the Best Practices passing badge.",
        "acceptance_criteria": [
            "Auto-updating Scorecard badge in README with a strong score",
            "Best Practices passing criteria met (note the <=14-day vuln-response and static-analysis-before-release MUSTs)",
        ],
        "validation_commands": ["test -f .github/workflows/scorecard.yml"],
        "notes_for_worker": "Depends in spirit on T-200/T-253/T-254 being in place to score well.",
    },
    {
        "task_id": "T-256", "title": "SLSA L3 provenance + reproducible builds",
        "priority": "P2", "wave": 8, "size": "L", "category": "security",
        "related_gaps": ["G-054"], "owned_paths": [".github/workflows/slsa.yml", "docs/reproducible-builds.md"],
        "locks_required": ["lock:release"],
        "summary": "Reproducibility lets a third party rebuild from source and confirm the published binary — the strongest defense against a compromised build server, and the natural complement to signing. Neither peer claims either.",
        "acceptance_criteria": [
            "SLSA Build L3 provenance distributed + verifiable (slsa-github-generator)",
            "Deterministic flags (-trimpath, pinned toolchain, SOURCE_DATE_EPOCH) + a documented 'how to reproduce a release' guide (CGO determinism addressed)",
        ],
        "validation_commands": ["test -f docs/reproducible-builds.md"],
        "notes_for_worker": "CGO_ENABLED=1 adds C-toolchain determinism considerations. Coordinate on release workflow via lock:release.",
    },
    {
        "task_id": "T-257", "title": "Examples gallery + templates",
        "priority": "P2", "wave": 8, "size": "M", "category": "docs", "good_first_issue": True,
        "related_gaps": ["G-055"], "owned_paths": ["examples/**"],
        "summary": "Every exemplar ships an extensibility system + a gallery and treats a headline count as marketing. An examples/ dir of runnable templates (a scheduled briefing, a triage bot, a multi-agent setup) costs little and seeds adoption.",
        "acceptance_criteria": [
            "examples/ with >=3 runnable templates (config + README each): e.g. morning briefing, channel triage, multi-agent group",
            "Linked from README + docs",
        ],
        "validation_commands": ["test -d examples"],
        "notes_for_worker": "Use only in-threat-model capabilities (no browser/self-install).",
    },
    {
        "task_id": "T-258", "title": "Public roadmap board + demo media + comparison page",
        "priority": "P2", "wave": 8, "size": "M", "category": "docs",
        "related_gaps": ["G-056"], "owned_paths": ["docs/roadmap.md", "docs/assets/demo/**"],
        "summary": "Roadmaps are the weakest area across the field, so a simple public board exceeds it — and is exactly where 'Web UI' should be parked so the gap reads as planned, not missing. Add a public roadmap, demo media (asciinema/video), and a comparison page leaning on IronClaw's signing/SBOM/threat-model edge.",
        "acceptance_criteria": [
            "Public roadmap (GitHub Projects board or docs/roadmap.md) listing Waves 6-8 incl. Web UI",
            "Demo asciinema cast or short video in README/docs; a 'vs nanoclaw/openclaw' comparison page",
        ],
        "validation_commands": ["test -f docs/roadmap.md"],
        "notes_for_worker": "Comparison page should be honest: IronClaw wins on isolation/signing/threat-model; peers win on UI/skills/channel breadth (closing via Wave 7).",
    },
    {
        "task_id": "T-259", "title": "Third-party security audit engagement",
        "priority": "P2", "wave": 8, "size": "L", "category": "security",
        "related_gaps": ["G-057"], "owned_paths": ["docs/audits/**"],
        "status": "needs-human", "requires_human_decision": True,
        "human_question": "Approve engaging a third-party security firm (scope, budget, possibly via OSTIF)? Which boundaries are in scope first?",
        "summary": "External validation is the strongest trust signal a security vendor can offer and is often a procurement prerequisite. Neither peer has one — a strong differentiator. Do it after the hygiene items so the audit targets the product, not low-hanging gaps.",
        "acceptance_criteria": [
            "Engage a recognized firm (consider OSTIF funding) scoped to sandbox isolation, egress broker, RBAC/policy, a2a",
            "Publish the full report in docs/audits/; track remediation publicly; pair with a VDP",
        ],
        "validation_commands": [],
        "notes_for_worker": "Human/budget decision. Sequence after T-200/T-252/T-253/T-254.",
    },
    {
        "task_id": "T-260", "title": "End-user credential vault (OneCLI-style request-time injection)",
        "priority": "P3", "wave": 8, "size": "L", "category": "security",
        "related_gaps": ["G-041"], "owned_paths": ["internal/host/egress/vault.go", ".agents/spikes/credential-vault.md"],
        "status": "needs-human", "requires_human_decision": True,
        "human_question": "Do we add an end-user credential vault so agents can call arbitrary approved APIs without holding raw keys (nanoclaw's OneCLI pattern), building on the egress broker? Or integrate onecli rather than build?",
        "summary": "nanoclaw's headline secret story is OneCLI's Agent Vault: agents never hold raw keys; the gateway injects credentials at request time with per-agent policies and rate limits. IronClaw injects only the model credential and now has a gateway-approved egress broker — extending that into a general vault is the parity gap, and a threat-model decision (build vs integrate onecli).",
        "acceptance_criteria": [
            "Spike: build-vs-integrate decision; if build, request-time injection over the egress broker with per-agent policy + audit, no raw keys in the sandbox",
            "Threat-model sign-off; agents never see plaintext secrets",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/egress/..."],
        "notes_for_worker": "Builds on T-111 (egress broker, landed). Keep the no-secrets-in-sandbox invariant.",
    },

    # --- T-227 follow-ups: skills system BUILD (decision 2026-06-17; design .agents/spikes/skills-system.md). ---
    {
        "task_id": "T-227a", "title": "Skill manifest schema + loader",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-036"],
        "owned_paths": ["internal/host/skills/manifest.go", "internal/host/skills/manifest_test.go"],
        "blocks": ["T-227b", "T-227c"],
        "summary": "First build task for the skills system (spike .agents/spikes/skills-system.md §3). Parse + validate the skill manifest (skill.yaml): persona fragment, tools subset, egress hosts, read-only assets. A skill is host-side config, never code.",
        "acceptance_criteria": [
            "Loader parses skill.yaml into a typed Manifest and rejects unknown tool names (tools MUST be a subset of the compiled sandbox tool registry)",
            "Egress entries validated as bare hostnames (no wildcards in v1); assets recorded as read-only paths; a malformed manifest fails closed with a clear error",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/skills/..."],
        "notes_for_worker": "Design: .agents/spikes/skills-system.md. New host package, no contract change. The seal is non-negotiable: a skill enables already-compiled capabilities, it never introduces code.",
    },
    {
        "task_id": "T-227b", "title": "Skill signature verification + curated source",
        "priority": "P2", "wave": 7, "size": "M", "category": "security",
        "related_gaps": ["G-036"],
        "owned_paths": ["internal/host/skills/source.go", "internal/host/skills/source_test.go"],
        "depends_on": ["T-227a"],
        "summary": "Fetch skills from a host-configured curated source (pinned Git ref or OCI registry the operator controls), and verify the bundle signature against a host trust root BEFORE the manifest is ever shown for approval. Directly answers the ClawHub 341-malicious-skills failure mode (open + auto-install).",
        "acceptance_criteria": [
            "Skills resolve only from a host-configured source, never an agent-supplied URL",
            "Signature (cosign or minisign) verified against a configured trust root; an unverified or unsigned bundle is refused at fetch time (fail-closed)",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/skills/..."],
        "notes_for_worker": "Design: .agents/spikes/skills-system.md §5. Pick cosign vs minisign as part of this task; the gate itself is mandatory.",
    },
    {
        "task_id": "T-227c", "title": "Skill install -> gateway ChangeRequest mapping",
        "priority": "P2", "wave": 7, "size": "M", "category": "feature",
        "related_gaps": ["G-036"],
        "owned_paths": ["internal/host/skills/install.go", "internal/host/skills/install_test.go"],
        "forbidden_paths_extra": ["cmd/controlplane/main.go"],
        "depends_on": ["T-227a"], "blocks": ["T-227e", "T-227f"],
        "summary": "Synthesize ONE bundled gateway ChangeRequest (persona += / tools += / egress += / mount) from a validated manifest, and route it through the existing verifier chain + AlwaysRequireHuman floor. Install is a host action, never a sandbox action; the human sees exactly which capabilities a skill requests.",
        "acceptance_criteria": [
            "A validated manifest produces one ChangeRequest bundling its declared grants; apply touches config only (registry/allowlists/mounts), never the rootfs or an executable",
            "The change clears the gateway's human-approval floor like any other capability change (reuse internal/host/gateway)",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/skills/..."],
        "notes_for_worker": "Design: .agents/spikes/skills-system.md §4. Daemon wiring into cmd/controlplane/main.go is the main.go owner's job — do not edit main.go here.",
    },
    {
        "task_id": "T-227d", "title": "Read-only skill asset mount (/skills/<name>)",
        "priority": "P2", "wave": 7, "size": "S", "category": "feature",
        "related_gaps": ["G-036"],
        "owned_paths": ["internal/host/isolation/skillmount.go", "internal/host/isolation/skillmount_test.go"],
        "summary": "Bind a skill's bundled assets read-only at /skills/<name> (nosuid,nodev,noexec), outside the read-only rootfs, mirroring the existing /shared mount pattern (SandboxSpec.SharedReadOnlyPath). Assets are data, never executed.",
        "acceptance_criteria": [
            "An optional per-skill read-only mount is added to the OCI spec when set; default (unset) is unchanged",
            "The mount carries nosuid,nodev,noexec and is read-only; covered by an isolation test (mirror the egress-socket / durable-mount tests)",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/isolation/..."],
        "notes_for_worker": "Design: .agents/spikes/skills-system.md §2/§4. Mirror the /shared mount in oci.go; review against the BuildOCISpec hardening invariants.",
    },
    {
        "task_id": "T-227e", "title": "ironctl skill add/list/remove",
        "priority": "P2", "wave": 7, "size": "S", "category": "feature",
        "related_gaps": ["G-036"],
        "owned_paths": ["cmd/ironctl/skill.go", "cmd/ironctl/skill_test.go"],
        "depends_on": ["T-227c"],
        "summary": "Host-side CLI surface for skills: add (fetch+verify+propose install ChangeRequest), list, remove. This is the ONLY trigger for install — never a sandbox tool (an agent can at most ask; only a human grants).",
        "acceptance_criteria": [
            "ironctl skill add <name>@<ver> fetches+verifies and submits the install ChangeRequest; list and remove operate host-side",
            "No sandbox-side skill-install tool is added",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./cmd/ironctl/..."],
        "notes_for_worker": "Design: .agents/spikes/skills-system.md §4. Follow the existing ironctl subcommand structure (change|audit|registry|onboard).",
    },
    {
        "task_id": "T-227f", "title": "Threat-model addendum: skills boundary + sign-off",
        "priority": "P2", "wave": 7, "size": "S", "category": "security",
        "related_gaps": ["G-036"],
        "owned_paths": ["docs/threat-model.md"],
        "depends_on": ["T-227c", "T-227d"],
        "summary": "Document the skills boundary in the threat model (mirroring §7 egress / §10 multi-provider): a skill is a host-approved capability bundle, no in-sandbox install, no code execution; third-party skills untrusted; record the maintainer sign-off.",
        "acceptance_criteria": [
            "A threat-model section covers the skills boundary, the trust model for third-party skills, and the residual risk",
            "Maintainer sign-off recorded (mirror the §7/§10 sign-off lines)",
        ],
        "validation_commands": [],
        "notes_for_worker": "Design: .agents/spikes/skills-system.md §5. docs/threat-model.md is shared/append-only across tasks; rebase if it moved.",
    },

    # --- T-260 follow-ups: credential vault INTEGRATE OneCLI (decision 2026-06-17; design .agents/spikes/credential-vault.md). ---
    {
        "task_id": "T-260a", "title": "[spike] OneCLI vault integration design (close open questions)",
        "priority": "P3", "wave": 8, "size": "spike", "category": "spike",
        "related_gaps": ["G-041"],
        "owned_paths": [".agents/spikes/credential-vault.md"],
        "blocks": ["T-260b", "T-260c", "T-260d", "T-260e"],
        "summary": "Close the §6 open questions before any build: OneCLI's actual injection API + self-hostability, broker<->vault authentication, credential lifecycle (rotation/revocation), and license/supply-chain acceptability. Update the spike with the concrete broker->vault contract; if OneCLI is unacceptable, specify the minimal host-side injector fallback (a SEPARATE principal, never inside the broker).",
        "acceptance_criteria": [
            "Spike updated with the resolved broker->vault contract (API, authn, lifecycle) or the fallback decision",
            "Confirms the invariant: secrets never enter the sandbox and never enter the egress broker (B4-E preserved)",
        ],
        "validation_commands": ["test -f .agents/spikes/credential-vault.md"],
        "notes_for_worker": "Design: .agents/spikes/credential-vault.md §6. Gates the build tasks T-260b..e.",
    },
    {
        "task_id": "T-260b", "title": "Vault destination + vault:// addressing on the egress broker",
        "priority": "P3", "wave": 8, "size": "M", "category": "security",
        "related_gaps": ["G-041"],
        "owned_paths": ["internal/host/egress/vault.go", "internal/host/egress/vault_test.go"],
        "depends_on": ["T-260a"], "blocks": ["T-260f"],
        "summary": "Allowlist the host-local OneCLI vault endpoint and define the vault://<cred>/<path> addressing the broker forwards by name. The broker keeps forwarding the request's own bytes (no secret injection in the broker) — the vault is a separate host-side principal it forwards TO.",
        "acceptance_criteria": [
            "The broker forwards vault://<cred>/<path> requests to the configured host-local vault endpoint; non-vault egress is unchanged",
            "The egress broker injects NO secrets itself (B4-E intact); the vault destination is deny-by-default like any other host",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/egress/..."],
        "notes_for_worker": "Design: .agents/spikes/credential-vault.md §4. The broker must NOT become a secret sink.",
    },
    {
        "task_id": "T-260c", "title": "Per-group vault policy (gateway-gated)",
        "priority": "P3", "wave": 8, "size": "M", "category": "security",
        "related_gaps": ["G-041"],
        "owned_paths": ["internal/host/registry/vaultpolicy.go", "internal/host/registry/vaultpolicy_test.go"],
        "forbidden_paths_extra": ["cmd/controlplane/main.go"],
        "depends_on": ["T-260a"],
        "summary": "Model 'which agent group may use which credential against which host' as gateway-approved config, not sandbox-settable. Changing the policy is a capability change that flows through the gateway like any other.",
        "acceptance_criteria": [
            "A per-group vault policy (group -> credential -> host) is stored host-side and is read-only to the sandbox",
            "Policy changes flow through the gateway approval path; the sandbox cannot mutate policy",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/registry/..."],
        "notes_for_worker": "Design: .agents/spikes/credential-vault.md §5. Daemon wiring into main.go is the owner's job — do not edit main.go here.",
    },
    {
        "task_id": "T-260d", "title": "Vault audit correlation (broker <-> vault request id)",
        "priority": "P3", "wave": 8, "size": "S", "category": "security",
        "related_gaps": ["G-041"],
        "owned_paths": ["internal/host/egress/correlate.go", "internal/host/egress/correlate_test.go"],
        "depends_on": ["T-260a"],
        "summary": "Carry a shared request id across the broker's per-request audit and the vault's injection/policy audit so a credential use is traceable end-to-end (the §5 Repudiation control, extended across the two host-side principals).",
        "acceptance_criteria": [
            "Every broker->vault request carries a correlation id surfaced in the broker audit record",
            "The id is documented as the join key for end-to-end credential-use traceability",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/egress/..."],
        "notes_for_worker": "Design: .agents/spikes/credential-vault.md §5.",
    },
    {
        "task_id": "T-260e", "title": "Vault response redaction backstop (broker -> sandbox)",
        "priority": "P3", "wave": 8, "size": "S", "category": "security",
        "related_gaps": ["G-041"],
        "owned_paths": ["internal/host/egress/redact.go", "internal/host/egress/redact_test.go"],
        "depends_on": ["T-260a"],
        "summary": "Apply the T-107 model-proxy redaction pattern on the broker->sandbox response path so an injected credential can never echo back into the sandbox even if an upstream reflects it.",
        "acceptance_criteria": [
            "Configured secrets are scrubbed from forwarded response bodies/headers on the broker->sandbox path (mirror modelproxy WithRedactedSecrets)",
            "Streaming responses are not buffered unsafely (mirror the modelproxy maxRedactBody handling)",
        ],
        "validation_commands": ["CGO_ENABLED=1 go test ./internal/host/egress/..."],
        "notes_for_worker": "Design: .agents/spikes/credential-vault.md §5. Reuse the T-107 redaction approach.",
    },
    {
        "task_id": "T-260f", "title": "Threat-model addendum: vault-behind-broker + sign-off",
        "priority": "P3", "wave": 8, "size": "S", "category": "security",
        "related_gaps": ["G-041"],
        "owned_paths": ["docs/threat-model.md"],
        "depends_on": ["T-260b"],
        "summary": "Document the vault-behind-broker boundary (mirroring §7/§10): secrets live host-side in the vault, never in the sandbox or the broker; keep 'build our own vault' a non-goal; record the maintainer sign-off.",
        "acceptance_criteria": [
            "A threat-model section covers the vault principal, the broker's unchanged B4-E property, and the residual risk",
            "Maintainer sign-off recorded; the 'build our own vault' non-goal restated",
        ],
        "validation_commands": [],
        "notes_for_worker": "Design: .agents/spikes/credential-vault.md §5/§7. docs/threat-model.md is shared/append-only; rebase if it moved.",
    },
]


def issue_title(t): return f"[{t['task_id']}] {t['title']}"


def labels_for(t):
    labels = [
        f"category:{t['category']}",
        f"priority:{t['priority']}",
        f"size:{t['size']}",
        f"wave:{t['wave']}",
    ]
    labels.append("agent:needs-human" if t.get("requires_human_decision") else "agent:ready")
    labels += list(t.get("locks_required", []))
    if t.get("good_first_issue"):
        labels.append("good first issue")
    return labels


def body_for(t):
    status = t.get("status", "available")
    forbidden = ["internal/contract/**"] + t.get("forbidden_paths_extra", [])
    owned = "\n".join(f"- `{p}`" for p in t["owned_paths"])
    forb = "\n".join(f"- `{p}`" for p in forbidden)
    locks = ", ".join(t.get("locks_required", [])) or "none"
    deps = ", ".join(t.get("depends_on", [])) or "none"
    blocks = ", ".join(t.get("blocks", [])) or "none"
    crits = "\n".join(f"- [ ] {c}" for c in t["acceptance_criteria"])
    val = "\n".join(t.get("validation_commands", []) or ["(no automated check — reviewer-verified)"])
    gaps = ", ".join(t["related_gaps"])
    human = ""
    if t.get("requires_human_decision"):
        human = f"\n## Human decision required\n{t['human_question']}\n"
    return f"""## Task {t['task_id']}
status **{status}** · priority **{t['priority']}** · wave **{t['wave']}** · size **{t['size']}** · category **{t['category']}**

**Related gaps:** {gaps}

## Summary
{t['summary']}

## Owned paths
{owned}

## Forbidden paths
{forb}

## Locks required
- {locks}

## Dependencies
- depends_on: {deps}
- blocks: {blocks}

## Acceptance criteria
{crits}

## Validation
```bash
{val}
```
{human}
Claim with `/agent-claim` per [AGENTS.md](../blob/main/AGENTS.md). Authoritative spec: `.agents/task-registry.json`.
"""


def registry_task(t):
    """Project a task dict into the registry schema."""
    return {
        "task_id": t["task_id"], "title": t["title"],
        "status": t.get("status", "available"), "priority": t["priority"],
        "wave": t["wave"], "size": t["size"], "category": t["category"],
        "related_gaps": t["related_gaps"], "owned_paths": t["owned_paths"],
        "forbidden_paths": ["internal/contract/**"] + t.get("forbidden_paths_extra", []),
        "locks_required": t.get("locks_required", []),
        "depends_on": t.get("depends_on", []), "blocks": t.get("blocks", []),
        "acceptance_criteria": t["acceptance_criteria"],
        "validation_commands": t.get("validation_commands", []),
        "requires_human_decision": bool(t.get("requires_human_decision")),
        **({"human_question": t["human_question"]} if t.get("requires_human_decision") else {}),
        "good_first_issue": bool(t.get("good_first_issue")),
        "summary": t["summary"], "notes_for_worker": t["notes_for_worker"],
    }


def write_registry(base_sha):
    reg = json.loads(REGISTRY.read_text())
    done_titles = {x["task_id"]: x for x in reg.get("completed", [])}
    # Flatten any still-listed done tasks into `completed`.
    for task in reg.get("tasks", []):
        if task["task_id"] in DONE_IDS and task["task_id"] not in done_titles:
            reg["completed"].append({"task_id": task["task_id"], "title": task["title"], "status": "done"})
            done_titles[task["task_id"]] = True
    reg["base_sha"] = base_sha
    reg["phase"] = ("Backend roadmap COMPLETE (Waves 0-5, T-001..T-120 all landed). "
                    "Road to 1.0 = Wave 6 launch readiness + Wave 7 product parity & web UI + Wave 8 trust & ecosystem.")
    reg["tasks"] = [registry_task(t) for t in NEW_TASKS]
    REGISTRY.write_text(json.dumps(reg, indent=2) + "\n")
    print(f"registry updated: {len(reg['completed'])} completed, {len(reg['tasks'])} open tasks -> {REGISTRY}")


def existing_titles():
    out = subprocess.run(
        ["gh", "issue", "list", "--state", "all", "--limit", "300", "--json", "title", "--jq", ".[].title"],
        capture_output=True, text=True, check=True).stdout
    return set(line.strip() for line in out.splitlines())


def create_issues():
    have = existing_titles()
    created = []
    for t in NEW_TASKS:
        title = issue_title(t)
        if title in have:
            print(f"skip (exists): {title}")
            continue
        labels = labels_for(t)
        cmd = ["gh", "issue", "create", "--title", title, "--body", body_for(t)]
        for l in labels:
            cmd += ["--label", l]
        res = subprocess.run(cmd, capture_output=True, text=True)
        if res.returncode != 0:
            print(f"FAILED {title}: {res.stderr.strip()}", file=sys.stderr)
        else:
            url = res.stdout.strip()
            created.append(url)
            print(f"created: {title} -> {url}")
    print(f"\n{len(created)} issue(s) created.")


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--write-registry", action="store_true")
    ap.add_argument("--create-issues", action="store_true")
    ap.add_argument("--print", action="store_true")
    ap.add_argument("--base-sha", default=os.environ.get("BASE_SHA", "HEAD"))
    a = ap.parse_args()
    if a.print:
        for t in NEW_TASKS:
            print(f"{issue_title(t):72} {','.join(labels_for(t))}")
    if a.write_registry:
        write_registry(a.base_sha)
    if a.create_issues:
        create_issues()
    if not (a.print or a.write_registry or a.create_issues):
        ap.print_help()
