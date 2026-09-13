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
- Add **Tasks** (creates a **Handoff** to the seed Bot)
- **Decisions** inbox: create and answer

Vite proxies `/healthz` and `/v1` (including WebSocket) to `:8080`.

```bash
npm run build
```
