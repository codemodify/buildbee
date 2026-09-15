-- Attachments: the bytes live in the blob store (BUILDBEE_BLOB_DIR or S3);
-- a row says what a file is and where it belongs. A file is uploaded to a
-- channel, then attached to one message when that is posted.
CREATE TABLE files (
    id UUID PRIMARY KEY,
    channel_id UUID NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    message_id UUID REFERENCES messages (id) ON DELETE CASCADE,
    uploader_person_id UUID REFERENCES people (id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (name <> ''),
    content_type TEXT NOT NULL,
    size BIGINT NOT NULL CHECK (size >= 0),
    sha256 TEXT NOT NULL,
    blob_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX files_message_idx ON files (message_id);
CREATE INDEX files_unattached_idx ON files (created_at) WHERE message_id IS NULL;

-- Large Artifact bodies (agent logs, diffs) move to the blob store too.
ALTER TABLE artifacts ADD COLUMN blob_key TEXT NOT NULL DEFAULT '';
ALTER TABLE artifacts ADD COLUMN blob_size BIGINT NOT NULL DEFAULT 0;
