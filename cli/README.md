# CLI

`buildbee` talks to the Server over HTTP. Module: `github.com/codemodify/buildbee/cli`.

```bash
export BUILDBEE_URL=http://127.0.0.1:8080   # default

go run ./cmd/buildbee version
go run ./cmd/buildbee project create --name Hive
go run ./cmd/buildbee task list --project "$PROJECT_ID"
go run ./cmd/buildbee handoff create --task "$TASK_ID" --from "$HUMAN_ID" --to "$BOT_ID" --note "please take this"
go run ./cmd/buildbee run start --task "$TASK_ID" --fake
go run ./cmd/buildbee routine list --project "$PROJECT_ID"
go run ./cmd/buildbee routine run --id "$ROUTINE_ID"
go run ./cmd/buildbee run start --task "$TASK_ID" --repo-url https://github.com/org/repo.git --cmd "echo hi"
```

`run start --fake` talks only to the Server (log Artifact + fake Repo PR). Without `--fake` it POSTs to `BUILDBEE_RUNTIME_URL` (default `:8090`) and falls back to fake if the runtime is down.

```bash
go test ./...
```
