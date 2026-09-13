# Compose

Postgres 16 and the BuildBee **Server**. The Server retries the database, applies migrations, then serves `/v1`.

```bash
# from the repository root
docker compose -f deploy/compose/docker-compose.yml up --build
```

- Server: `http://localhost:8080/healthz`
- Postgres: `postgres://buildbee:buildbee@localhost:5432/buildbee`

Runtime supervisor (`--profile runtime`) listens on `:8090`. Set `BUILDBEE_FAKE_SANDBOX=0` for real Docker.

This compose file is the **self-host source of truth**. Railway uses the repo-root `Dockerfile` and `deploy/README.md` env list (`DATABASE_URL`, `GITHUB_*`, `SESSION_SECRET`).

MinIO (optional Artifact store) uses the `extras` profile:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile extras up
```
