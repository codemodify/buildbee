package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/jackc/pgx/v5"
)

// paged runs base (a SELECT ending in a WHERE clause with len(args)
// placeholders) over one Page of seq. Chat reads oldest-to-newest
// (newestFirst=false); feeds read newest-first. hasMore reports whether
// another page exists in the direction the Page was reading.
func paged[T any](ctx context.Context, q querier, base string, args []any, p Page, newestFirst bool,
	scan func(pgx.Rows) (T, error)) (items []T, hasMore bool, err error) {
	limit := p.limit()
	n := len(args)
	var sql string
	var desc bool
	switch {
	case p.After > 0:
		sql = fmt.Sprintf(`%s AND seq > $%d ORDER BY seq ASC LIMIT $%d`, base, n+1, n+2)
		args = append(args, p.After, limit+1)
	case p.Before > 0:
		sql = fmt.Sprintf(`%s AND seq < $%d ORDER BY seq DESC LIMIT $%d`, base, n+1, n+2)
		args = append(args, p.Before, limit+1)
		desc = true
	default:
		sql = fmt.Sprintf(`%s ORDER BY seq DESC LIMIT $%d`, base, n+1)
		args = append(args, limit+1)
		desc = true
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, false, mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(items) > limit {
		items, hasMore = items[:limit], true
	}
	if desc != newestFirst {
		slices.Reverse(items)
	}
	if items == nil {
		items = []T{}
	}
	return items, hasMore, nil
}

// --- messages ---

const messageCols = `id, seq, channel_id, project_id, member_id, body, COALESCE(thread_id::text, ''), reply_count, last_reply_at,
	COALESCE(task_id::text, ''), created_at`

func scanMessage(row pgx.Rows) (models.Message, error) {
	var m models.Message
	err := row.Scan(&m.ID, &m.Seq, &m.ChannelID, &m.ProjectID, &m.MemberID, &m.Body, &m.ThreadID, &m.ReplyCount, &m.LastReplyAt,
		&m.TaskID, &m.CreatedAt)
	return m, err
}

// InsertMessage stores m and returns it with its seq. A reply also counts
// on its root.
func (s *Store) InsertMessage(ctx context.Context, m models.Message) (*models.Message, error) {
	err := s.q.QueryRow(ctx, `INSERT INTO messages (id, channel_id, project_id, member_id, body, thread_id, task_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING seq`,
		m.ID, m.ChannelID, m.ProjectID, m.MemberID, m.Body, nullID(m.ThreadID), nullID(m.TaskID), m.CreatedAt).Scan(&m.Seq)
	if err != nil {
		return nil, mapErr(err)
	}
	if m.ThreadID != "" {
		if err := one(s.q.Exec(ctx, `UPDATE messages SET reply_count = reply_count + 1, last_reply_at = $2 WHERE id = $1`,
			m.ThreadID, m.CreatedAt)); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// GetMessage returns one message.
func (s *Store) GetMessage(ctx context.Context, id string) (*models.Message, error) {
	rows, err := s.q.Query(ctx, `SELECT `+messageCols+` FROM messages WHERE id=$1`, id)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, mapErr(err)
		}
		return nil, ErrNotFound
	}
	m, err := scanMessage(rows)
	return &m, mapErr(err)
}

// SetMessageTask marks a root message as the thread of a Task.
func (s *Store) SetMessageTask(ctx context.Context, messageID, taskID string) error {
	return one(s.q.Exec(ctx, `UPDATE messages SET task_id=$2 WHERE id=$1`, messageID, taskID))
}

// ListMessages returns one page of a Channel's root messages, oldest first.
func (s *Store) ListMessages(ctx context.Context, channelID string, p Page) ([]models.Message, bool, error) {
	return paged(ctx, s.q, `SELECT `+messageCols+` FROM messages WHERE channel_id=$1 AND thread_id IS NULL`,
		[]any{channelID}, p, false, scanMessage)
}

// ListChannelSince returns a Channel's messages, replies included, after
// seq: what a WebSocket subscriber missed.
func (s *Store) ListChannelSince(ctx context.Context, channelID string, after int64, limit int) ([]models.Message, bool, error) {
	return paged(ctx, s.q, `SELECT `+messageCols+` FROM messages WHERE channel_id=$1`,
		[]any{channelID}, Page{After: after, Limit: limit}, false, scanMessage)
}

// ListReplies returns one page of a thread's replies, oldest first.
func (s *Store) ListReplies(ctx context.Context, rootID string, p Page) ([]models.Message, bool, error) {
	return paged(ctx, s.q, `SELECT `+messageCols+` FROM messages WHERE thread_id=$1`, []any{rootID}, p, false, scanMessage)
}

// MarkRead moves a Person's read marker in a Channel forward to seq.
func (s *Store) MarkRead(ctx context.Context, personID, channelID string, seq int64) error {
	_, err := s.q.Exec(ctx, `INSERT INTO read_markers (person_id, channel_id, last_seq) VALUES ($1, $2, $3)
		ON CONFLICT (person_id, channel_id) DO UPDATE SET last_seq = GREATEST(read_markers.last_seq, EXCLUDED.last_seq)`,
		personID, channelID, seq)
	return mapErr(err)
}

// Unread counts, for each Channel of a Project the Person can see, the
// messages others wrote after the Person's read marker.
func (s *Store) Unread(ctx context.Context, personID, projectID string) ([]models.Unread, error) {
	rows, err := s.q.Query(ctx, `SELECT c.id,
			count(m.id) FILTER (WHERE m.seq > COALESCE(rm.last_seq, 0) AND (me.id IS NULL OR m.member_id <> me.id)),
			COALESCE(max(m.seq), 0)
		FROM channels c
		LEFT JOIN members me ON me.project_id = c.project_id AND me.person_id = $1
		LEFT JOIN read_markers rm ON rm.channel_id = c.id AND rm.person_id = $1
		LEFT JOIN messages m ON m.channel_id = c.id
		WHERE c.project_id = $2 AND c.archived_at IS NULL
			AND (c.kind = 'channel' OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.member_id = me.id))
		GROUP BY c.id ORDER BY c.id`, personID, projectID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Unread{}
	for rows.Next() {
		var u models.Unread
		if err := rows.Scan(&u.ChannelID, &u.Unread, &u.LastSeq); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// --- activity ---

const activityCols = `seq, project_id, COALESCE(actor_member_id::text, ''), actor, type, action, COALESCE(subject_id::text, ''), payload, created_at`

func scanActivity(row pgx.Rows) (models.Activity, error) {
	var a models.Activity
	var raw []byte
	if err := row.Scan(&a.Seq, &a.ProjectID, &a.ActorMemberID, &a.Actor, &a.Type, &a.Action, &a.SubjectID, &raw, &a.CreatedAt); err != nil {
		return a, err
	}
	_ = json.Unmarshal(raw, &a.Payload)
	if a.Payload == nil {
		a.Payload = map[string]any{}
	}
	return a, nil
}

// InsertActivity appends to a Project's log and returns the entry with its seq.
func (s *Store) InsertActivity(ctx context.Context, a models.Activity) (*models.Activity, error) {
	if a.Payload == nil {
		a.Payload = map[string]any{}
	}
	raw, err := json.Marshal(a.Payload)
	if err != nil {
		return nil, err
	}
	err = s.q.QueryRow(ctx, `INSERT INTO activity (project_id, actor_member_id, actor, type, action, subject_id, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING seq`,
		a.ProjectID, nullID(a.ActorMemberID), a.Actor, a.Type, a.Action, nullID(a.SubjectID), raw, a.CreatedAt).Scan(&a.Seq)
	return &a, mapErr(err)
}

// ListActivity returns one page of a Project's log, newest first.
func (s *Store) ListActivity(ctx context.Context, projectID, typ string, p Page) ([]models.Activity, bool, error) {
	return paged(ctx, s.q, `SELECT `+activityCols+` FROM activity WHERE project_id=$1 AND ($2 = '' OR type = $2)`,
		[]any{projectID, typ}, p, true, scanActivity)
}

// --- notifications ---

const notificationCols = `id, seq, person_id, project_id, kind, title, body, href, read_at, created_at`

func scanNotification(row pgx.Rows) (models.Notification, error) {
	var n models.Notification
	err := row.Scan(&n.ID, &n.Seq, &n.PersonID, &n.ProjectID, &n.Kind, &n.Title, &n.Body, &n.Href, &n.ReadAt, &n.CreatedAt)
	return n, err
}

// InsertNotification stores n and returns it with its seq.
func (s *Store) InsertNotification(ctx context.Context, n models.Notification) (*models.Notification, error) {
	err := s.q.QueryRow(ctx, `INSERT INTO notifications (id, person_id, project_id, kind, title, body, href, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING seq`,
		n.ID, n.PersonID, n.ProjectID, n.Kind, n.Title, n.Body, n.Href, n.CreatedAt).Scan(&n.Seq)
	return &n, mapErr(err)
}

// ListNotifications returns one page of a Person's inbox, newest first.
func (s *Store) ListNotifications(ctx context.Context, personID string, unreadOnly bool, p Page) ([]models.Notification, bool, error) {
	return paged(ctx, s.q, `SELECT `+notificationCols+` FROM notifications WHERE person_id=$1 AND (NOT $2 OR read_at IS NULL)`,
		[]any{personID, unreadOnly}, p, true, scanNotification)
}

// CountUnread counts a Person's unread notifications.
func (s *Store) CountUnread(ctx context.Context, personID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE person_id=$1 AND read_at IS NULL`, personID).Scan(&n)
	return n, mapErr(err)
}

// MarkNotificationRead marks one of the Person's notifications read.
func (s *Store) MarkNotificationRead(ctx context.Context, id, personID string, at time.Time) (*models.Notification, error) {
	rows, err := s.q.Query(ctx, `UPDATE notifications SET read_at = COALESCE(read_at, $3)
		WHERE id=$1 AND person_id=$2 RETURNING `+notificationCols, id, personID, at)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, mapErr(err)
		}
		return nil, ErrNotFound
	}
	n, err := scanNotification(rows)
	return &n, err
}

// MarkAllNotificationsRead marks every unread notification of a Person read.
func (s *Store) MarkAllNotificationsRead(ctx context.Context, personID string, at time.Time) (int, error) {
	tag, err := s.q.Exec(ctx, `UPDATE notifications SET read_at=$2 WHERE person_id=$1 AND read_at IS NULL`, personID, at)
	if err != nil {
		return 0, mapErr(err)
	}
	return int(tag.RowsAffected()), nil
}
