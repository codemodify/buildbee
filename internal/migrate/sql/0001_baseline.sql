-- BuildBee baseline schema.
--
-- Pre-release: this file is edited in place until the first release, so a
-- database created by an earlier build must be recreated (docker compose
-- down -v). After the first release every change is a new numbered file.
--
-- Foreign keys to members are DEFERRABLE INITIALLY DEFERRED: Postgres checks
-- them at commit, so deleting a Project can cascade through its members and
-- the rows that reference them in one statement.
--
-- seq columns are BIGSERIAL cursors for paging and WebSocket replay.

CREATE TABLE people (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX people_name_idx ON people (lower(name));

CREATE TABLE person_preferences (
    person_id UUID PRIMARY KEY REFERENCES people (id) ON DELETE CASCADE,
    mute_mentions BOOLEAN NOT NULL DEFAULT false,
    mute_routines BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    auto_run BOOLEAN NOT NULL DEFAULT false,
    repo_url TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT '',
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE members (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    person_id UUID REFERENCES people (id),
    kind TEXT NOT NULL CHECK (kind IN ('human', 'bot')),
    display_name TEXT NOT NULL,
    role TEXT NOT NULL,
    instructions TEXT NOT NULL DEFAULT '',
    agent TEXT NOT NULL DEFAULT '', -- the agent CLI a Bot runs as (claude, grok, codex, ...)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- every human Member is a Person; a Bot never is
    CHECK ((kind = 'human') = (person_id IS NOT NULL))
);
CREATE INDEX members_project_id_idx ON members (project_id, created_at);
CREATE UNIQUE INDEX members_project_person_idx ON members (project_id, person_id) WHERE person_id IS NOT NULL;

CREATE TABLE channels (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (name <> ''),
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX channels_project_id_idx ON channels (project_id, created_at);

CREATE TABLE messages (
    id UUID PRIMARY KEY,
    seq BIGSERIAL NOT NULL UNIQUE,
    channel_id UUID NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    member_id UUID NOT NULL REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX messages_channel_seq_idx ON messages (channel_id, seq);

CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (title <> ''),
    body TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'done', 'canceled')),
    assignee_member_id UUID REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    created_by_member_id UUID REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    issue_number INT CHECK (issue_number > 0),
    issue_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tasks_project_id_idx ON tasks (project_id, created_at DESC);
CREATE UNIQUE INDEX tasks_project_issue_idx ON tasks (project_id, issue_number) WHERE issue_number IS NOT NULL;

CREATE TABLE handoffs (
    id UUID PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    from_member_id UUID NOT NULL REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    to_member_id UUID NOT NULL REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    note TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('open', 'complete')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX handoffs_task_id_idx ON handoffs (task_id, created_at);
CREATE INDEX handoffs_to_open_idx ON handoffs (to_member_id) WHERE status = 'open';

CREATE TABLE decisions (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    task_id UUID REFERENCES tasks (id) ON DELETE SET NULL,
    prompt TEXT NOT NULL CHECK (prompt <> ''),
    options JSONB NOT NULL DEFAULT '[]',
    recommendation TEXT NOT NULL DEFAULT '',
    answer TEXT,
    answered_by_member_id UUID REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    reused BOOLEAN NOT NULL DEFAULT false,
    fingerprint TEXT NOT NULL DEFAULT '',
    assignee_member_id UUID REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    answered_at TIMESTAMPTZ
);
CREATE INDEX decisions_project_id_idx ON decisions (project_id, created_at DESC);

CREATE TABLE decision_memories (
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

CREATE TABLE activity (
    seq BIGSERIAL PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    actor_member_id UUID REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    actor TEXT NOT NULL,
    type TEXT NOT NULL,
    action TEXT NOT NULL,
    subject_id UUID,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX activity_project_seq_idx ON activity (project_id, seq DESC);
CREATE INDEX activity_project_type_idx ON activity (project_id, type, seq DESC);

CREATE TABLE runs (
    id UUID PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    bot_member_id UUID REFERENCES members (id) DEFERRABLE INITIALLY DEFERRED,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'canceled')),
    detail TEXT NOT NULL DEFAULT '',
    agent TEXT NOT NULL DEFAULT '',   -- '' = any agent the claiming worker offers
    prompt TEXT NOT NULL DEFAULT '',  -- what the agent is asked to do
    worker TEXT NOT NULL DEFAULT '',  -- the worker that claimed the Run
    lease_until TIMESTAMPTZ,          -- claim expires unless the worker heartbeats
    attempts INT NOT NULL DEFAULT 0,
    next_seq INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE INDEX runs_task_id_idx ON runs (task_id, created_at DESC);
CREATE INDEX runs_queue_idx ON runs (created_at) WHERE status = 'pending';
CREATE INDEX runs_leased_idx ON runs (lease_until) WHERE status = 'running' AND lease_until IS NOT NULL;

CREATE TABLE run_events (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('token', 'tool_call', 'tool_result', 'status', 'log')),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, seq)
);

CREATE TABLE artifacts (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    run_id UUID REFERENCES runs (id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL CHECK (name <> ''),
    body TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX artifacts_task_id_idx ON artifacts (task_id, created_at DESC);

CREATE TABLE pipelines (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    artifact_id UUID REFERENCES artifacts (id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'success', 'failure')),
    external_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX pipelines_task_id_idx ON pipelines (task_id, created_at DESC);

CREATE TABLE routines (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    bot_member_id UUID REFERENCES members (id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (name <> ''),
    schedule TEXT NOT NULL DEFAULT '24h',
    enabled BOOLEAN NOT NULL DEFAULT false,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX routines_project_id_idx ON routines (project_id);
CREATE INDEX routines_enabled_idx ON routines (enabled) WHERE enabled;

CREATE TABLE notifications (
    id UUID PRIMARY KEY,
    seq BIGSERIAL NOT NULL UNIQUE,
    person_id UUID NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    href TEXT NOT NULL DEFAULT '',
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notifications_person_seq_idx ON notifications (person_id, seq DESC);
CREATE INDEX notifications_person_unread_idx ON notifications (person_id) WHERE read_at IS NULL;
