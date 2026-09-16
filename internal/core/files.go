package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codemodify/buildbee/internal/blob"
	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// ErrNoStorage means the Server has no blob store for files.
var ErrNoStorage = fmt.Errorf("%w: this Server stores no files (BUILDBEE_BLOB_DIR / BUILDBEE_S3_ENDPOINT)", store.ErrConflict)

// UploadFile stores a file sent to a Channel by the acting Person, to be
// attached to their next message there. Its type is sniffed from its bytes,
// never taken from the sender.
func (s *Service) UploadFile(ctx context.Context, a Actor, channelID, fileName string, size int64, r io.Reader) (*models.File, error) {
	if s.blobs == nil {
		return nil, ErrNoStorage
	}
	if !a.IsPerson() {
		return nil, ErrNoActor
	}
	if size <= 0 || size > s.maxFile {
		return nil, invalid("a file is 1 byte to %d bytes", s.maxFile)
	}
	name := cleanFileName(fileName)
	ch, err := s.writableChannel(ctx, a, channelID)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	head = head[:n]
	f := models.File{ID: uuid.NewString(), ChannelID: ch.ID, UploaderPersonID: a.PersonID, Name: name,
		ContentType: sniff(head), Size: size, CreatedAt: s.now()}
	f.BlobKey = "files/" + f.ID[:2] + "/" + f.ID
	sum := sha256.New()
	body := io.TeeReader(io.MultiReader(bytes.NewReader(head), r), sum)
	if err := s.blobs.Put(ctx, f.BlobKey, body, size, f.ContentType); err != nil {
		return nil, fmt.Errorf("storing %s: %w", name, err)
	}
	f.SHA256 = hex.EncodeToString(sum.Sum(nil))
	if err := s.st.InsertFile(ctx, f); err != nil {
		s.dropBlobs([]string{f.BlobKey})
		return nil, err
	}
	f.URL = "/v1/files/" + f.ID
	return &f, nil
}

// Swept is what one round of housekeeping removed.
type Swept struct {
	Uploads       int `json:"uploads"`
	RunEvents     int `json:"run_events"`
	Notifications int `json:"notifications"`
}

// Sweep is the Server's housekeeping: uploads nobody posted, and, with
// RetentionDays set, the event streams of long-finished Runs and read
// notifications. It removes at most a few thousand rows per round, so a
// first sweep of an old database is spread over several.
func (s *Service) Sweep(ctx context.Context) (Swept, error) {
	var out Swept
	var err error
	if out.Uploads, err = s.SweepUploads(ctx); err != nil {
		return out, err
	}
	if s.keepDays <= 0 {
		return out, nil
	}
	const batch = 5000
	before := s.now().AddDate(0, 0, -s.keepDays)
	if out.RunEvents, err = s.st.DeleteOldRunEvents(ctx, before, batch); err != nil {
		return out, err
	}
	out.Notifications, err = s.st.DeleteOldNotifications(ctx, before, batch)
	return out, err
}

// SweepUploads deletes uploads nobody posted within a day, bytes included.
func (s *Service) SweepUploads(ctx context.Context) (int, error) {
	keys, err := s.st.DeleteUnattached(ctx, s.now().Add(-24*time.Hour))
	if err != nil {
		return 0, err
	}
	s.dropBlobs(keys)
	return len(keys), nil
}

// OpenFile returns a file the acting Person may read, and its bytes: one
// attached to a message they can read, or their own upload.
func (s *Service) OpenFile(ctx context.Context, a Actor, id string) (*models.File, io.ReadCloser, error) {
	if s.blobs == nil {
		return nil, nil, ErrNoStorage
	}
	f, err := s.st.GetFile(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if f.MessageID == "" {
		if !a.IsPerson() || a.PersonID != f.UploaderPersonID {
			return nil, nil, store.ErrNotFound
		}
	} else {
		ch, err := s.st.GetChannel(ctx, f.ChannelID)
		if err != nil {
			return nil, nil, err
		}
		if err := s.canSee(ctx, a, ch); err != nil {
			return nil, nil, err
		}
	}
	rc, _, err := s.blobs.Get(ctx, f.BlobKey)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, nil, store.ErrNotFound
	}
	return f, rc, err
}

// maxTaskFiles is how many attachments one Run is given.
const maxTaskFiles = 20

// OpenRunFile returns a file attached to a running Run's Task, for the
// worker running it.
func (s *Service) OpenRunFile(ctx context.Context, a Actor, runID, fileID string) (*models.File, io.ReadCloser, error) {
	if s.blobs == nil {
		return nil, nil, ErrNoStorage
	}
	if a.Worker == "" {
		return nil, nil, invalid("only the worker running the Run reads its files")
	}
	r, err := s.st.GetRun(ctx, runID, false)
	if err != nil {
		return nil, nil, err
	}
	if r.Worker != a.Worker {
		return nil, nil, fmt.Errorf("%w: run is not on worker %s", store.ErrConflict, a.Worker)
	}
	t, err := s.st.GetTask(ctx, r.TaskID, false)
	if err != nil {
		return nil, nil, err
	}
	files, err := s.st.TaskFiles(ctx, t.ID, t.ThreadID, maxTaskFiles)
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(files, func(f models.File) bool { return f.ID == fileID })
	if i < 0 {
		return nil, nil, store.ErrNotFound
	}
	rc, _, err := s.blobs.Get(ctx, files[i].BlobKey)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, nil, store.ErrNotFound
	}
	return &files[i], rc, err
}

// writableChannel is a Channel the acting Person can post to now.
func (s *Service) writableChannel(ctx context.Context, a Actor, channelID string) (*models.Channel, error) {
	ch, err := s.st.GetChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if ch.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: channel #%s is archived", store.ErrConflict, ch.Name)
	}
	p, err := s.st.GetProject(ctx, ch.ProjectID)
	if err != nil {
		return nil, err
	}
	if p.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: project %q is archived", store.ErrConflict, p.Name)
	}
	if err := s.canSee(ctx, a, ch); err != nil {
		return nil, err
	}
	return ch, nil
}

// withFiles fills in the files attached to msgs.
func (s *Service) withFiles(ctx context.Context, msgs ...*models.Message) error {
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	files, err := s.st.FilesOf(ctx, ids)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		m.Files = files[m.ID]
	}
	return nil
}

// dropBlobs deletes blobs whose rows are gone. A failure leaves an orphan
// behind, which costs space but nothing else.
func (s *Service) dropBlobs(keys []string) {
	if s.blobs == nil {
		return
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		if err := s.blobs.Delete(context.Background(), k); err != nil {
			s.log.Warn("blob not deleted", "key", k, "err", err)
		}
	}
}

// cleanFileName keeps the base name, printable, at most 200 characters.
func cleanFileName(n string) string {
	n = path.Base(strings.ReplaceAll(strings.TrimSpace(n), "\\", "/"))
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, n)
	for utf8.RuneCountInString(n) > 200 {
		_, size := utf8.DecodeLastRuneInString(n)
		n = n[:len(n)-size]
	}
	if n == "" || n == "." || n == "/" {
		return "file"
	}
	return n
}

// sniff names a file's type from its first bytes; text files keep a
// charset, everything unknown is octet-stream.
func sniff(head []byte) string {
	t := http.DetectContentType(head)
	if t == "application/octet-stream" && utf8.Valid(head) && !bytes.ContainsRune(head, 0) && len(head) > 0 {
		return "text/plain; charset=utf-8"
	}
	return t
}
