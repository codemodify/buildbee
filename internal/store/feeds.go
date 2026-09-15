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

const messageCols = `id, seq, channel_id, project_id, member_id, body, created_at`

func scanMessage(row pgx.Rows) (models.Message, error) {
	var m models.Message
	err := row.Scan(&m.ID, &m.Seq, &m.ChannelID, &m.ProjectID, &m.MemberID, &m.Body, &m.CreatedAt)
	return m, err
}

// InsertMessage stores m and returns it with its seq.
func (s *Store) InsertMessage(ctx context.Context, m models.Message) (*models.Message, error) {
	err := s.q.QueryRow(ctx, `INSERT INTO messages (id, channel_id, project_id, member_id, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING seq`,
		m.ID, m.ChannelID, m.ProjectID, m.MemberID, m.Body, m.CreatedAt).Scan(&m.Seq)
	return &m, mapErr(err)
}

// ListMessages returns one page of a Channel, oldest first.
func (s *Store) ListMessages(ctx context.Context, channelID string, p Page) ([]models.Message, bool, error) {
	return paged(ctx, s.q, `SELECT `+messageCols+` FROM messages WHERE channel_id=$1`,
		[]any{channelID}, p, false, scanMessage)
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
