CREATE TABLE IF NOT EXISTS artifacts (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    run_id UUID REFERENCES runs (id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS artifacts_task_id_idx ON artifacts (task_id);

CREATE TABLE IF NOT EXISTS pipelines (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    artifact_id UUID REFERENCES artifacts (id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    external_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS pipelines_task_id_idx ON pipelines (task_id);
