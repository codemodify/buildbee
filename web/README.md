# Web

Vite + React + TypeScript + Tailwind UI for a BuildBee **Project**.

## Run

Start the Server first (`go run` or Compose), then:

```bash
npm install
npm run dev
```

Open [http://localhost:5173](http://localhost:5173).

- Create a **Project** (empty home is name + create only). Once any Project exists, a left **Projects** rail lists them on home and inside Project views
- Click a rail row to open/focus that Project. **Alt-click or ⊕** opens a second Project beside it (two panes max; × closes a pane)
- Deep links: `#/projects/:id` focuses that Project; `#/projects/:id?beside=:id2` restores a split. Layout also persists in `localStorage`
- Chat in a **Channel** (polls every 2s; Server also has `/v1/channels/{id}/ws`)
- See seeded **Bots + Roles** (Scout, Builder, Sentry, Pulse)
- Add **Tasks** (auto-Handoff to Scout or Builder); Task page Handoff picker lists Bots by Role
- Open a Task for **Runs** (live transcript over WebSocket / poll), **Artifacts**, and **Pipelines**
- **Sync Issues** (fake sample Issues→Tasks without `GITHUB_TOKEN`)
- **Routines** list + force Run
- Dev-auth banner, or **Sign in with GitHub** when OAuth is configured
- **Decisions** inbox: create and answer
- **Notifications** bell (unread count, mark read / mark all)
- **Invites**: Project form + pending list + copy link; `#/invite/:token` (or `/invite/:token`) accept page

Vite proxies `/healthz` and `/v1` (including WebSocket) to `:8080`. In the browser, API paths stay relative `/v1` so the same UI works when the Server serves `dist` on the same origin (`base: "/"`). The Tauri desktop shell (see [`desktop/`](../desktop/)) prefixes `/v1` with a configurable Server URL (default `http://127.0.0.1:8080`) so the webview can reach a local or remote Server. Hash routes (`#/invite/:token`) are unchanged.

```bash
npm run build
# then from repo root:
#   make run          # BUILDBEE_WEB_DIR=web/dist
#   make build        # embed into the Server binary
```
