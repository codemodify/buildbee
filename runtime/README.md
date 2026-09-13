# Runtime

Go supervisor + Docker **Sandbox** for **Runs**. Module: `github.com/codemodify/buildbee/runtime`

## Sandbox lifecycle

```
POST /v1/tasks/{taskID}/runs     → pending
        │
PATCH … status=running
        │
docker run --rm -w /work <image> sh -c '
  optional git clone $REPO
  $CMD
'
        │
POST /v1/tasks/{id}/artifacts    → sandbox.log (Artifact)
POST /v1/tasks/{id}/pr           → fake or real draft PR Artifact
PATCH … succeeded | failed
```

## Run locally

Server must be up. Fake engine (no Docker):

```bash
BUILDBEE_FAKE_SANDBOX=1 go run ./cmd/runtime
# POST http://127.0.0.1:8090/runs  {"task_id":"...","fake":true}
```

Real Docker:

```bash
go run ./cmd/runtime
# needs `docker` on PATH
```

CLI:

```bash
buildbee run start --task "$TASK_ID" --fake
buildbee run start --task "$TASK_ID" --repo-url https://github.com/org/repo.git --cmd "ls"
```

`fake-run` still exists:

```bash
go run ./cmd/fake-run --task "$TASK_ID"
```

Compose: `--profile runtime` (see `deploy/compose`). Default `BUILDBEE_FAKE_SANDBOX=1`.

## Packages

| Path | Role |
| --- | --- |
| `.` | Supervisor |
| [`sandbox/`](sandbox/) | `Engine` interface, `DockerEngine`, `FakeEngine` |
| [`notify/`](notify/) | Server HTTP client |
| [`cmd/runtime/`](cmd/runtime/) | HTTP supervisor |
| [`cmd/fake-run/`](cmd/fake-run/) | Fake success helper |

```bash
go test ./...
```
