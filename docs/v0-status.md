# v0 status

Honest checklist after Member Invite. v0 core (including multi-user join) is in place. Remaining work is polish and the deferred desktop/IDE/Nostr tracks.

## Shipped (v0 core)

- [x] Project workspace REST + Channel chat / WebSocket
- [x] Task, Handoff, Decision (+ memory / don’t-ask-twice), Activity
- [x] Bot Roles: Scout / Builder / Sentry / Pulse
- [x] Run + Artifact + Pipelines webhook; FakeACP / Docker Sandbox
- [x] GitHub OAuth / dev Identity, Issues→Task, Routines
- [x] Channel `@bot` mentions; webhook HMAC when secret set
- [x] Notifications: generate on Decision open, Handoff to a Member, @mention Task, Pipeline failure; `GET /v1/notifications`, mark read / read-all
- [x] Web: Channel, Kanban, Decisions, Bots, auto_run, Project switcher, Activity, inbox bell
- [x] One Server hosts API + UI (`Dockerfile` / `make build` / `BUILDBEE_WEB_DIR`)
- [x] CLI `buildbee`; compose + Railway
- [x] CI (memory store, FakeACP, no Docker-in-Docker)
- [x] Multi-user Member Invite (token link; owner/admin invite; accept binds session / GitHub Identity)

## Still open (not new subsystems)

- [ ] Bind GitHub Identity to an existing Member across Projects automatically (Invite accept does this per Project)
- [ ] Notification preferences / per-Role mute
- [ ] Empty-state and error UX beyond current pages (keep polishing in place)
- [ ] Persist Decision assignee (inbox is “all humans” for new Decisions)

## Deferred (out of v0)

- [ ] Desktop / tray app
- [ ] IDE extension
- [ ] Full Nostr / signed Activity (NIP-01 not adopted; see ADR 0001)
- [ ] Production GitHub App install flow beyond webhook HMAC + PAT
- [ ] Real ACP streaming (start / send / collect only)
