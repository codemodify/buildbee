# Changelog

BuildBee v0 on `dev` (PR #1). Apache-2.0.

## v0 — feature complete

A **Project** workspace where humans and **Bots** cooperate. One Server process can host `/v1`, `/healthz`, and the Vite SPA.

### Workspace
- Project, Channel (REST + WebSocket), Task, Handoff, Decision (+ fingerprint memory), Activity
- Seeded Bot Roles: Scout, Builder, Sentry, Pulse
- Member Invite (token link; owner/admin create/revoke; members list)
- Cross-Project human **Identity**: same GitHub login reuses one profile on every Invite accept

### Execution
- Run + Artifact + Pipelines webhook; FakeACP or Docker Sandbox
- CLI `buildbee run start --task --acp [--agent fake]`
- Issues→Task sync; Routines (`morning-digest`)

### Identity and inbox
- Dev auth (default) or GitHub OAuth
- Notifications: Decision (assignee or all humans), Handoff, @mention Task, Pipeline failure, Invite accept, Routine fire
- `GET/PATCH /v1/me/preferences` — mute mentions / Routine digests
- Decision `assignee_id` + `?mine=1` inbox

### Web
- Same-origin `/v1` (Vite `base: "/"`); hash routes including `#/invite/:token`
- Kanban Tasks, Decisions (assignee + mine), Invites, Activity, inbox bell, mute prefs
- Empty states and inline errors/toasts on Invite, Sync Issues, Routine run, Start Run

### Ship
- Compose (Postgres 16 + Server) and Railway (`Dockerfile`, `railway.toml`)
- `make build` / `scripts/build.sh` embed `web/dist`
- CI: Go tests, web build, e2e scripts against in-memory Server (no DinD)

### Deferred
Desktop, IDE, Nostr/NIP-01, GitHub App install UI, bidirectional ACP stream.
