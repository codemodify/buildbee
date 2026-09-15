# Web

Vite + React + TypeScript + Tailwind UI for BuildBee. The Server serves the built UI from the same origin, so every API call is a relative `/v1/…` path.

## Develop

Start the Server (see the root README), then:

```bash
npm install
npm run dev      # http://127.0.0.1:5173, proxies /v1 and /healthz to :8080
npm run build    # type-check and build into dist/
```

`make run` from the repo root serves `web/dist` from disk; `make build` embeds it into the Server binary.

## What is there today

- Projects rail: a collapsible tree of Projects and their Channels; open two Projects side by side
- Channel chat with `@Bot` mentions that create a Task and hand it to that Bot
- Tasks on a Kanban board, each opening a Task page with Runs (live transcript), Artifacts and Pipelines
- Decisions: create, answer, and reuse remembered answers
- Bots with their Roles, Routines, Activity feed, Notifications and mute preferences
- Light theme using `--bb-*` tokens in `src/index.css`

The chat-first redesign (threads, direct messages, presence, one WebSocket feeding a client store) is Phase 2 of the [roadmap](../docs/roadmap.md).
