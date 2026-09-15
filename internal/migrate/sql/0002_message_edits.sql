-- People edit and delete their own messages. A deleted message keeps its
-- row (and its thread) with an empty body.
ALTER TABLE messages ADD COLUMN edited_at TIMESTAMPTZ;
ALTER TABLE messages ADD COLUMN deleted_at TIMESTAMPTZ;
