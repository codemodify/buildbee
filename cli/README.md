# CLI

`buildbee` talks to the Server over HTTP. Module: `github.com/codemodify/buildbee/cli`.

```bash
export BUILDBEE_URL=http://127.0.0.1:8080   # default

go run ./cmd/buildbee version
go run ./cmd/buildbee project create --name Hive
go run ./cmd/buildbee task list --project "$PROJECT_ID"
go run ./cmd/buildbee handoff create --task "$TASK_ID" --from "$HUMAN_ID" --to-role builder --note "please take this" --autorun
go run ./cmd/buildbee run start --task "$TASK_ID" --fake
go run ./cmd/buildbee run start --task "$TASK_ID" --acp --agent fake
go run ./cmd/buildbee routine list --project "$PROJECT_ID"
go run ./cmd/buildbee routine run --id "$ROUTINE_ID"
go run ./cmd/buildbee run start --task "$TASK_ID" --repo-url https://github.com/org/repo.git --cmd "echo hi"
```

`run start --fake` talks only to the Server (log Artifact + fake Repo PR). `--acp` starts an ACP session (`--agent claude|codex|opencode|goose|fake`); missing binaries use FakeACP and write Artifact `acp.log`. Without `--fake`/`--acp --agent fake` it POSTs to `BUILDBEE_RUNTIME_URL` (default `:8090`) and falls back if the runtime is down.

```bash
go test ./...
```
