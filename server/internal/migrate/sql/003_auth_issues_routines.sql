ALTER TABLE members ADD COLUMN IF NOT EXISTS github_login TEXT NOT NULL DEFAULT '';
ALTER TABLE members ADD COLUMN IF NOT EXISTS github_id TEXT NOT NULL DEFAULT '';

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS issue_number INT;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS issue_url TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS routines (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    bot_member_id UUID REFERENCES members (id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    schedule TEXT NOT NULL DEFAULT '24h',
    enabled BOOLEAN NOT NULL DEFAULT false,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS routines_project_id_idx ON routines (project_id);
