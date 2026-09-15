-- Message search: words and prefixes, no stemming ('simple' suits code
-- and names as well as prose).
ALTER TABLE messages ADD COLUMN search tsvector GENERATED ALWAYS AS (to_tsvector('simple', body)) STORED;
CREATE INDEX messages_search_idx ON messages USING GIN (search);
