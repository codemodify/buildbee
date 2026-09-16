package store

import (
	"context"
	"fmt"
	"time"

	"github.com/codemodify/buildbee/internal/models"
)

const fileCols = `id, channel_id, COALESCE(message_id::text, ''), COALESCE(uploader_person_id::text, ''), name, content_type, size,
	sha256, blob_key, created_at`

// fileColsF are fileCols for a query that names files f.
const fileColsF = `f.id, f.channel_id, COALESCE(f.message_id::text, ''), COALESCE(f.uploader_person_id::text, ''), f.name,
	f.content_type, f.size, f.sha256, f.blob_key, f.created_at`

func scanFile(row interface{ Scan(...any) error }, f *models.File) error {
	err := row.Scan(&f.ID, &f.ChannelID, &f.MessageID, &f.UploaderPersonID, &f.Name, &f.ContentType, &f.Size, &f.SHA256, &f.BlobKey, &f.CreatedAt)
	f.URL = "/v1/files/" + f.ID
	return err
}

// InsertFile records an uploaded file, not yet attached to a message.
func (s *Store) InsertFile(ctx context.Context, f models.File) error {
	_, err := s.q.Exec(ctx, `INSERT INTO files (id, channel_id, uploader_person_id, name, content_type, size, sha256, blob_key, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		f.ID, f.ChannelID, nullID(f.UploaderPersonID), f.Name, f.ContentType, f.Size, f.SHA256, f.BlobKey, f.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetFile(ctx context.Context, id string) (*models.File, error) {
	var f models.File
	err := scanFile(s.q.QueryRow(ctx, `SELECT `+fileCols+` FROM files WHERE id=$1`, id), &f)
	return &f, mapErr(err)
}

// AttachFiles attaches a Person's unattached uploads in a Channel to a
// message. ErrInvalid unless every one qualifies.
func (s *Store) AttachFiles(ctx context.Context, ids []string, messageID, personID, channelID string) ([]models.File, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.q.Query(ctx, `UPDATE files SET message_id=$1
		WHERE id = ANY($2::uuid[]) AND uploader_person_id=$3 AND channel_id=$4 AND message_id IS NULL
		RETURNING `+fileCols, messageID, ids, personID, channelID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []models.File
	for rows.Next() {
		var f models.File
		if err := scanFile(rows, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(ids) {
		return nil, fmt.Errorf("%w: file_ids must be your uploads to this channel, not yet posted", ErrInvalid)
	}
	return out, nil
}

// FilesOf returns the files attached to each of the messages.
func (s *Store) FilesOf(ctx context.Context, messageIDs []string) (map[string][]models.File, error) {
	out := map[string][]models.File{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.Query(ctx, `SELECT `+fileCols+` FROM files WHERE message_id = ANY($1::uuid[]) ORDER BY created_at, id`, messageIDs)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var f models.File
		if err := scanFile(rows, &f); err != nil {
			return nil, err
		}
		out[f.MessageID] = append(out[f.MessageID], f)
	}
	return out, rows.Err()
}

// TaskFiles returns the files people attached to a Task: in the message
// that opened it and anywhere in its thread, oldest first.
func (s *Store) TaskFiles(ctx context.Context, taskID, threadID string, limit int) ([]models.File, error) {
	rows, err := s.q.Query(ctx, `SELECT `+fileColsF+` FROM files f
		JOIN messages m ON m.id = f.message_id AND m.deleted_at IS NULL
		WHERE m.task_id = $1 OR ($2 <> '' AND (m.id::text = $2 OR m.thread_id::text = $2))
		ORDER BY f.created_at, f.id LIMIT $3`, taskID, threadID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.File{}
	for rows.Next() {
		var f models.File
		if err := scanFile(rows, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DeleteFilesOf deletes a message's files, returning their blob keys.
func (s *Store) DeleteFilesOf(ctx context.Context, messageID string) ([]string, error) {
	return s.keys(ctx, `DELETE FROM files WHERE message_id=$1 RETURNING blob_key`, messageID)
}

// ChannelBlobKeys returns the blob keys of every file in a Channel.
func (s *Store) ChannelBlobKeys(ctx context.Context, channelID string) ([]string, error) {
	return s.keys(ctx, `SELECT blob_key FROM files WHERE channel_id=$1`, channelID)
}

// DeleteUnattached deletes uploads never posted since before, returning
// their blob keys.
func (s *Store) DeleteUnattached(ctx context.Context, before time.Time) ([]string, error) {
	return s.keys(ctx, `DELETE FROM files WHERE message_id IS NULL AND created_at < $1 RETURNING blob_key`, before)
}

func (s *Store) keys(ctx context.Context, sql string, args ...any) ([]string, error) {
	rows, err := s.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
