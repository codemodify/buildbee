# v0 status

**v0 is feature-complete for merge** (Server + web + CLI + runtime loop + desktop MVP). Remaining items are explicitly deferred (IDE, Nostr, GitHub App install, ACP stream).

## Shipped (v0 complete)

- [x] Project workspace REST + Channel chat / WebSocket
- [x] Task, Handoff, Decision (+ memory / don’t-ask-twice + optional assignee), Activity
- [x] Bot Roles: Scout / Builder / Sentry / Pulse
- [x] Run + Artifact + Pipelines webhook; FakeACP / Docker Sandbox
- [x] GitHub OAuth / dev Identity, Issues→Task, Routines
- [x] Channel `@bot` mentions; webhook HMAC when secret set
- [x] Notifications inbox; mute mentions / Routine digests (`GET/PATCH /v1/me/preferences`)
- [x] Cross-Project human Identity: same GitHub login reuses one Identity on every Invite accept
- [x] Multi-user Member Invite (token link; owner/admin)
- [x] Web: Channel, Kanban, Decisions (assignee + mine), Bots, Invites, Activity, inbox, prefs
- [x] One Server hosts API + UI (`Dockerfile` / `make build` / `BUILDBEE_WEB_DIR`)
- [x] CLI `buildbee`; compose + Railway
- [x] CI (memory store, FakeACP, no Docker-in-Docker)
- [x] Desktop MVP (Tauri 2 wraps `web/`; Server URL setting; hash Invite routes)

## Deferred (out of v0)

- [ ] Desktop tray / multi-window agent bench (beyond the v1 shell)
- [ ] IDE extension
- [ ] Full Nostr / signed Activity (NIP-01 not adopted; see ADR 0001)
- [ ] Production GitHub App install flow beyond webhook HMAC + PAT
- [ ] Real ACP streaming (start / send / collect only)
