# BuildBee

BuildBee is a **Project** workspace where humans and **Bots** cooperate on engineering work.

Product nouns are locked in the [glossary](docs/glossary.md). See [team workflow](docs/workflow.md) and [ADR 0001](docs/adr/0001-stack.md).

License: [Apache-2.0](LICENSE).

## Monorepo map

| Path | What it is |
| --- | --- |
| [`server/`](server/) | Go **Server**: Postgres (or memory), REST `/v1`, Channel WebSocket |
| [`web/`](web/) | Vite + React UI: Project, Channel chat, Tasks, Decisions |
| [`cli/`](cli/) | `buildbee` CLI (project / task / handoff) |
| [`runtime/`](runtime/) | Sandbox stub + `fake-run` that posts **Run** status to the Server |
| [`deploy/compose/`](deploy/compose/) | Postgres 16 + Server (migrates on start) |
| [`docs/`](docs/) | Glossary, workflow, ADRs |

Go modules: `github.com/codemodify/buildbee/{server,cli,runtime}` with a root [`go.work`](go.work).

## End-to-end (this slice)

Preferred: Docker Compose brings up Postgres and a Server that applies migrations.

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
# Server: http://localhost:8080/healthz
```

Without Docker, the Server uses an in-memory Store (lost on restart):

```bash
cd server && go run ./cmd/server
```

With a local Postgres:

```bash
export DATABASE_URL=postgres://buildbee:buildbee@127.0.0.1:5432/buildbee?sslmode=disable
cd server && go run ./cmd/server
```

Then in other terminals:

```bash
# Web (proxies /v1 to the Server)
cd web && npm install && npm run dev
# open http://localhost:5173

# Scripted path: Project → message → Task → Handoff → Decision answer → Run
./scripts/e2e.sh

# CLI
cd cli
go run ./cmd/buildbee project create --name Hive
go run ./cmd/buildbee task list --project "$PROJECT_ID"
go run ./cmd/buildbee handoff create --task "$TASK_ID" --from "$HUMAN_ID" --to "$BOT_ID"

# Runtime stub posts a successful Run
cd runtime
go run ./cmd/fake-run --task "$TASK_ID"
```

Create a Project in the web UI, send a Channel message, add a Task (auto-Handoff to the Bot), and answer a Decision. All of that is persisted when `DATABASE_URL` is set.

## Auth (stub)

Humans: GitHub OAuth later. Bots: server-issued Identities. The UI uses the seed human Member (`You`) to post messages (`X-Member-ID` or `member_id`).

## Tests

```bash
cd server && go test ./...
cd web && npm run build
```
