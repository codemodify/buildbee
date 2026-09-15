-- Projects choose whether agents act freely (auto) or ask before acting
-- (ask): each permission request becomes a Decision tied to its Run.
ALTER TABLE projects ADD COLUMN agent_permissions TEXT NOT NULL DEFAULT 'auto'
    CHECK (agent_permissions IN ('auto', 'ask'));
ALTER TABLE decisions DROP CONSTRAINT decisions_action_check;
ALTER TABLE decisions ADD CONSTRAINT decisions_action_check CHECK (action IN ('', 'merge', 'permission'));
ALTER TABLE decisions ADD COLUMN run_id UUID REFERENCES runs (id) ON DELETE SET NULL;
CREATE INDEX decisions_open_run_idx ON decisions (run_id) WHERE answer IS NULL AND run_id IS NOT NULL;
