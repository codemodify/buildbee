package core

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
)

// A 1x1 PNG.
var png1 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func (f *fixture) upload(a Actor, channelID, name string, body []byte) *models.File {
	f.t.Helper()
	file, err := f.s.UploadFile(f.ctx, a, channelID, name, int64(len(body)), bytes.NewReader(body))
	f.must(err)
	return file
}

func read(t *testing.T, rc io.ReadCloser) string {
	t.Helper()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAttachments(t *testing.T) {
	f := newFixture(t)
	ada, bob, cy := f.person("Ada"), f.person("Bob"), f.person("Cy")
	p := f.project(ada, "Files")
	ch := p.Channels[0].ID
	img := f.upload(ada, ch, "../../shot.png", png1)
	page := f.upload(ada, ch, "evil.html", []byte("<html><script>alert(1)</script></html>"))
	if img.Name != "shot.png" || img.ContentType != "image/png" || !img.InlineImage() || img.SHA256 == "" {
		t.Fatalf("image: %+v", img)
	}
	if page.ContentType != "text/html; charset=utf-8" || page.InlineImage() {
		t.Fatalf("page: %+v", page)
	}
	// Not posted yet: only the uploader can open it, and only they attach it.
	if _, _, err := f.s.OpenFile(f.ctx, bob, img.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("bob opens an unposted upload: %v", err)
	}
	if _, err := f.s.PostMessage(f.ctx, bob, ch, "mine", img.ID); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("bob attaches ada's upload: %v", err)
	}
	posted, err := f.s.PostMessage(f.ctx, ada, ch, "", img.ID, page.ID) // files alone are a message
	f.must(err)
	if len(posted.Files) != 2 {
		t.Fatalf("posted: %+v", posted.Files)
	}
	if evs := f.pub.topic("channel:" + ch); len(evs[len(evs)-1].Data.(*Posted).Files) != 2 {
		t.Fatal("viewers get the files with the message")
	}
	if _, err := f.s.PostMessage(f.ctx, ada, ch, "again", img.ID); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a file attaches once: %v", err)
	}
	msgs, _, err := f.s.Messages(f.ctx, bob, ch, store.Page{})
	f.must(err)
	if len(msgs) != 1 || len(msgs[0].Files) != 2 {
		t.Fatalf("listed: %+v", msgs)
	}
	got, rc, err := f.s.OpenFile(f.ctx, cy, img.ID)
	f.must(err)
	if read(t, rc) != string(png1) || got.Name != "shot.png" {
		t.Fatal("anyone reads a file in an open channel")
	}
	// In a DM, only its people.
	dm, err := f.s.OpenDirect(f.ctx, ada, NewDirect{PersonIDs: []string{bob.PersonID}})
	f.must(err)
	secret := f.upload(ada, dm.ID, "secret.txt", []byte("the key"))
	reply, err := f.s.PostMessage(f.ctx, ada, dm.ID, "here", secret.ID)
	f.must(err)
	if secret.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("text: %+v", secret)
	}
	if _, _, err := f.s.OpenFile(f.ctx, cy, secret.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cy opens a DM file: %v", err)
	}
	if _, err := f.s.UploadFile(f.ctx, cy, dm.ID, "x", 1, strings.NewReader("x")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cy uploads to the DM: %v", err)
	}
	_, rc, err = f.s.OpenFile(f.ctx, bob, secret.ID)
	f.must(err)
	if read(t, rc) != "the key" {
		t.Fatal("bob reads it")
	}
	// Deleting the message deletes its files, bytes included.
	f.must(f.s.DeleteMessage(f.ctx, ada, reply.ID))
	if _, _, err := f.s.OpenFile(f.ctx, bob, secret.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("file outlives its message: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.blobs.Root, "files", secret.ID[:2], secret.ID)); !os.IsNotExist(err) {
		t.Fatalf("blob outlives its message: %v", err)
	}
	// Threads carry files too; sizes are checked.
	th, err := f.s.Reply(f.ctx, bob, posted.ID, "", f.upload(bob, ch, "more.png", png1).ID)
	f.must(err)
	thread, err := f.s.Thread(f.ctx, ada, th.ID, store.Page{})
	f.must(err)
	if len(thread.Root.Files) != 2 || len(thread.Replies) != 1 || len(thread.Replies[0].Files) != 1 {
		t.Fatalf("thread: %+v", thread)
	}
	if _, err := f.s.UploadFile(f.ctx, ada, ch, "big", 2<<20, bytes.NewReader(make([]byte, 2<<20))); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("too big: %v", err)
	}
	if _, err := f.s.PostMessage(f.ctx, ada, ch, ""); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("an empty message with no files: %v", err)
	}
}

func TestLargeArtifactsGoToTheBlobStore(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Logs")
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "log", HandoffRole: "none"})
	f.must(err)
	small, err := f.s.CreateArtifact(f.ctx, ada, tc.ID, NewArtifact{Kind: "log", Name: "small.log", Body: "short"})
	f.must(err)
	body := strings.Repeat("line of agent output\n", 80000) // ~1.6 MiB
	big, err := f.s.CreateArtifact(f.ctx, ada, tc.ID, NewArtifact{Kind: "log", Name: "claude.log", Body: body})
	f.must(err)
	got, err := f.s.Artifact(f.ctx, big.ID)
	f.must(err)
	if !got.Truncated || len(got.Body) != artifactHead || got.Size != len(body) || got.RawURL == "" {
		t.Fatalf("head: truncated=%v body=%d size=%d raw=%q", got.Truncated, len(got.Body), got.Size, got.RawURL)
	}
	rc, err := f.s.ArtifactRaw(f.ctx, got)
	f.must(err)
	if read(t, rc) != body {
		t.Fatal("raw is the whole body")
	}
	if s, _ := f.s.Artifact(f.ctx, small.ID); s.Body != "short" || s.Truncated || s.RawURL != "" {
		t.Fatalf("small stays inline: %+v", s)
	}
	list, err := f.s.Artifacts(f.ctx, tc.ID)
	f.must(err)
	for _, a := range list {
		if a.ID == big.ID && a.Size != len(body) {
			t.Fatalf("listed size: %d", a.Size)
		}
	}
}

func TestUnpostedUploadsAreSweptAfterADay(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC()
	f.s.now = func() time.Time { return now }
	ada := f.person("Ada")
	p := f.project(ada, "Sweep")
	stale := f.upload(ada, p.Channels[0].ID, "stale.txt", []byte("never posted"))
	now = now.Add(25 * time.Hour)
	fresh := f.upload(ada, p.Channels[0].ID, "fresh.txt", []byte("about to post"))
	n, err := f.s.SweepUploads(f.ctx)
	f.must(err)
	if n != 1 {
		t.Fatalf("swept %d", n)
	}
	if _, _, err := f.s.OpenFile(f.ctx, ada, stale.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.blobs.Root, "files", stale.ID[:2], stale.ID)); !os.IsNotExist(err) {
		t.Fatalf("stale bytes: %v", err)
	}
	if _, rc, err := f.s.OpenFile(f.ctx, ada, fresh.ID); err != nil {
		t.Fatalf("fresh: %v", err)
	} else {
		rc.Close()
	}
}

func TestSweepTrimsOldEventStreamsAndReadNotifications(t *testing.T) {
	f := newFixture(t)
	f.s.keepDays = 30
	now := time.Now().UTC()
	f.s.now = func() time.Time { return now }
	ada, bob := f.person("Ada"), f.person("Bob")
	p := f.project(ada, "Old")
	_, err := f.s.Join(f.ctx, bob, p.ID)
	f.must(err)
	r := f.queue(ada, p.ID, "old work", NewRun{Agent: "fake"})
	c, who := f.claimFor(bot(p, models.RoleBuilder).ID, "fake")
	_, err = f.s.ReportRun(f.ctx, who, c.Run.ID, RunReport{Status: "succeeded", Summary: "did it"})
	f.must(err)
	_, err = f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "@Bob look")
	f.must(err)
	notes := f.inbox(bob)
	if len(notes) != 1 {
		t.Fatalf("inbox: %+v", notes)
	}
	_, err = f.s.MarkRead(f.ctx, bob, notes[0].ID)
	f.must(err)

	now = now.AddDate(0, 0, 31)
	got, err := f.s.Sweep(f.ctx)
	f.must(err)
	if got.RunEvents == 0 || got.Notifications != 1 {
		t.Fatalf("swept: %+v", got)
	}
	evs, _, err := f.s.RunEvents(f.ctx, r.ID, 0, 100)
	f.must(err)
	if len(evs) != 0 {
		t.Fatalf("events kept: %+v", evs)
	}
	run, err := f.s.Run(f.ctx, r.ID)
	f.must(err)
	if run.Summary != "did it" || run.Status != models.RunSucceeded {
		t.Fatalf("the Run itself stays: %+v", run)
	}
	if n := f.inbox(bob); len(n) != 0 {
		t.Fatalf("notifications kept: %+v", n)
	}
	msgs, _, _ := f.s.Messages(f.ctx, ada, p.Channels[0].ID, store.Page{})
	if len(msgs) == 0 {
		t.Fatal("messages are never swept")
	}
}
