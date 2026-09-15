CREATE TABLE IF NOT EXISTS identities (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL,
    display_name TEXT NOT NULL,
    github_login TEXT NOT NULL DEFAULT '',
    github_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS identities_github_login_lower_idx
    ON identities (lower(github_login))
    WHERE github_login <> '';

ALTER TABLE decisions ADD COLUMN IF NOT EXISTS assignee_member_id UUID REFERENCES members (id);

CREATE TABLE IF NOT EXISTS member_preferences (
    identity TEXT PRIMARY KEY,
    mute_mentions BOOLEAN NOT NULL DEFAULT false,
    mute_routines BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
