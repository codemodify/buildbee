# Web

Vite + React + TypeScript + Tailwind UI for a BuildBee **Project**.

## Run

Start the Server first (`go run` or Compose), then:

```bash
npm install
npm run dev
```

Open [http://localhost:5173](http://localhost:5173).

- Create or pick a **Project**
- Chat in a **Channel** (polls every 2s; Server also has `/v1/channels/{id}/ws`)
- See seeded **Bots + Roles** (Scout, Builder, Sentry, Pulse)
- Add **Tasks** (auto-Handoff to Scout or Builder); Task page Handoff picker lists Bots by Role
- Open a Task for **Runs**, **Artifacts**, and **Pipelines**
- **Sync Issues** (fake sample Issues→Tasks without `GITHUB_TOKEN`)
- **Routines** list + force Run
- Dev-auth banner, or **Sign in with GitHub** when OAuth is configured
- **Decisions** inbox: create and answer

Vite proxies `/healthz` and `/v1` (including WebSocket) to `:8080`.

```bash
npm run build
```
