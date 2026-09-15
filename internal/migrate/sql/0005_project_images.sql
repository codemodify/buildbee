-- A Project may name the image its agents run in (with its toolchain);
-- empty means the worker's default image.
ALTER TABLE projects ADD COLUMN agent_image TEXT NOT NULL DEFAULT '';
