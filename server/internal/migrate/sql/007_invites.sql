CREATE TABLE IF NOT EXISTS invites (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    email TEXT NOT NULL DEFAULT '',
    github_login TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL,
    token TEXT NOT NULL UNIQUE,
    invited_by_member_id UUID NOT NULL REFERENCES members (id),
    accepted_member_id UUID REFERENCES members (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS invites_project_id_idx ON invites (project_id);
CREATE UNIQUE INDEX IF NOT EXISTS invites_token_idx ON invites (token);
