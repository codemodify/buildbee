# Compose

Postgres 16 and the BuildBee **Server**. The Server retries the database, applies migrations, then serves `/v1`.

```bash
# from the repository root
docker compose -f deploy/compose/docker-compose.yml up --build
```

- Server: `http://localhost:8080/healthz`
- Postgres: `postgres://buildbee:buildbee@localhost:5432/buildbee`

MinIO (optional Artifact store) uses the `extras` profile:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile extras up
```
