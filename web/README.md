# Web

Vite + React + TypeScript + Tailwind UI for a BuildBee **Project**.

## Run

```bash
npm install
npm run dev
```

Open [http://localhost:5173](http://localhost:5173). The homepage is a placeholder that says **BuildBee**.

Vite proxies `/healthz` and `/v1` to the Server on `:8080`. Start the Server first if you want those routes to resolve during `npm run dev`.

## Build

```bash
npm run build
npm run preview
```

## Later

This app will host Channel, Task, Decisions, and Activity views. Auth (GitHub OAuth for humans; server-issued Identities for Bots) is not wired yet.
