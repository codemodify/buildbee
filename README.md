# BuildBee

BuildBee is a **Project** workspace where humans and **Bots** cooperate on engineering work.

Product nouns are locked in the [glossary](docs/glossary.md). See [team workflow](docs/workflow.md) and [ADR 0001](docs/adr/0001-stack.md).

License: [Apache-2.0](LICENSE).

## Monorepo map

| Path | What it is |
| --- | --- |
| [`server/`](server/) | Go **Server**: Postgres (or memory), REST `/v1`, Channel WebSocket |
| [`web/`](web/) | Vite + React UI: Project, Channel, Tasks, Decisions, Runs, Artifacts, Pipelines |
| [`cli/`](cli/) | `buildbee` CLI (project / task / handoff / run start) |
| [`runtime/`](runtime/) | Docker **Sandbox** supervisor for **Runs** (`fake-run` fallback) |
| [`deploy/compose/`](deploy/compose/) | Postgres 16 + Server; optional runtime profile |
| [`docs/`](docs/) | Glossary, workflow, ADRs |

Go modules: `github.com/codemodify/buildbee/{server,cli,runtime}` with a root [`go.work`](go.work).

## End-to-end

Preferred: Docker Compose brings up Postgres and a Server that applies migrations.

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
# Server: http://localhost:8080/healthz
```

Runtime (Docker Sandbox). Default `BUILDBEE_FAKE_SANDBOX=1` so it works without a socket:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile runtime up --build
# set BUILDBEE_FAKE_SANDBOX=0 and mount docker.sock for real containers
```

Without Docker, the Server uses an in-memory Store (lost on restart):

```bash
cd server && go run ./cmd/server
```

Then:

```bash
cd web && npm install && npm run dev   # http://localhost:5173

./scripts/e2e.sh       # Project → message → Task → Handoff → Decision → Run
./scripts/e2e-run.sh   # Run → log Artifact → fake Repo PR → Pipelines webhook

cd cli
go run ./cmd/buildbee run start --task "$TASK_ID" --fake
# real Sandbox (needs runtime on :8090 and Docker):
# go run ./cmd/buildbee run start --task "$TASK_ID" --repo-url https://github.com/org/repo.git --cmd "echo hi"
```

Open a Task in the web UI to see **Runs**, **Artifacts** (including PR URLs), and **Pipelines**.

### Repo (GitHub)

`GITHUB_TOKEN` + `GITHUB_REPO=owner/name` let `POST /v1/tasks/{id}/pr` open a draft PR (branch + file commit). Without a token, pass `{"fake":true}` (or omit the token) to store a fake PR URL Artifact.

### Pipelines

`POST /v1/pipelines/webhook` records check status on a Task. Simple JSON `{task_id,name,status,external_url}` or a GitHub Actions-shaped `check_run` object.

## Auth (stub)

Humans: GitHub OAuth later. Bots: server-issued Identities.

## Tests

```bash
cd server && go test ./...
cd runtime && go test ./...
cd web && npm run build
```
