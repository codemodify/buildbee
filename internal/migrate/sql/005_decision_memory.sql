CREATE TABLE IF NOT EXISTS decision_memories (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    prompt TEXT NOT NULL,
    answer TEXT NOT NULL,
    decision_id UUID REFERENCES decisions (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS decision_memories_project_idx ON decision_memories (project_id, updated_at DESC);
