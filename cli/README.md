# CLI

Go CLI named `buildbee`. Module: `github.com/codemodify/buildbee/cli`.

The root [`go.work`](../go.work) includes this module so you can develop Server, CLI, and runtime together.

## Run

```bash
go run ./cmd/buildbee version
# buildbee 0.0.0

go test ./...
```

Install locally:

```bash
go install ./cmd/buildbee
buildbee version
```

## Later

Commands will talk to the Server (Project, Channel, Task, Handoff, Run) using a Member Identity. Not implemented in this scaffold.
