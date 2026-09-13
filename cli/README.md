# CLI

`buildbee` talks to the Server over HTTP. Module: `github.com/codemodify/buildbee/cli`.

```bash
export BUILDBEE_URL=http://127.0.0.1:8080   # default

go run ./cmd/buildbee version
go run ./cmd/buildbee project create --name Hive
go run ./cmd/buildbee task list --project "$PROJECT_ID"
go run ./cmd/buildbee handoff create --task "$TASK_ID" --from "$HUMAN_ID" --to "$BOT_ID" --note "please take this"
```

```bash
go test ./...
```
