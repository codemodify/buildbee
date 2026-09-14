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
- Persistent Projects rail + two side-by-side Project panes (Alt-click or ⊕ opens beside). Header Project `<select>` removed. `#/projects/:id?beside=` + localStorage restore
- Slack-style Projects tree: collapsible Project nodes, nested `#channel` navigator, Channel create under each Project; in-pane Channel list removed
- Light theme by default (`html[data-theme=light]` + `--bb-*` tokens). Amber accents kept. No theme switcher in v0.

### Ship
- Compose (Postgres 16 + Server) and Railway (`Dockerfile`, `railway.toml`)
- `make build` / `scripts/build.sh` embed `web/dist`
- Desktop MVP (`desktop/`): Tauri 2 window loads `web/`; Server URL persists locally; native Reload / Open Server URL / Quit. Server (and Postgres) are not bundled.
- Live ACP streaming: RunEvent (`token` / `tool_call` / `tool_result` / `status` / `log`), `GET /v1/runs/{id}/events?after=`, WebSocket `/v1/runs/{id}/ws`. FakeACP emits chunks over ~1–2s. Final Artifact remains `acp.log`. Web Task page shows a live transcript.
- CI: Go tests, web build, desktop `cargo check` on Ubuntu (no signed `.dmg`), e2e scripts against in-memory Server (no DinD) including `e2e-acp-stream.sh`

### Pinned later
IDE extension, Nostr/NIP-01 signed Activity, production GitHub App install UI.

### Deferred
Desktop tray / multi-window bench.
