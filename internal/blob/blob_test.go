package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// exercise puts, gets, replaces and deletes through st.
func exercise(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	key := "files/ab/cd-1.txt"
	if _, _, err := st.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	for _, body := range []string{"hello", "hello again, longer"} {
		if err := st.Put(ctx, key, strings.NewReader(body), int64(len(body)), "text/plain"); err != nil {
			t.Fatal(err)
		}
		r, n, err := st.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(r)
		r.Close()
		if string(got) != body || n != int64(len(body)) {
			t.Fatalf("got %q (%d)", got, n)
		}
	}
	big := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
	if err := st.Put(ctx, "logs/r1/claude.log", bytes.NewReader(big), int64(len(big)), ""); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.Get(ctx, "logs/r1/claude.log")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, big) {
		t.Fatal("big blob differs")
	}
	if err := st.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(ctx, key); err != nil {
		t.Fatalf("deleting twice: %v", err)
	}
	if _, _, err := st.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
	for _, bad := range []string{"../etc/passwd", "files/../../x", "Files/a", "/abs/key", "files", "files/a b"} {
		if err := st.Put(ctx, bad, strings.NewReader("x"), 1, ""); err == nil {
			t.Errorf("key %q accepted", bad)
		}
	}
}

func TestDir(t *testing.T) { exercise(t, Dir{Root: t.TempDir()}) }

func TestDirRejectsShortWrites(t *testing.T) {
	if err := (Dir{Root: t.TempDir()}).Put(context.Background(), "files/a/b", strings.NewReader("abc"), 5, ""); err == nil {
		t.Fatal("a short body is not stored")
	}
}

// TestS3 runs against MinIO in a throwaway container when Docker has the
// quay.io/minio/minio image; it is skipped otherwise.
func TestS3(t *testing.T) {
	if exec.Command("docker", "image", "inspect", "quay.io/minio/minio").Run() != nil {
		t.Skip("no quay.io/minio/minio image")
	}
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::9000",
		"-e", "MINIO_ROOT_USER=bbtest", "-e", "MINIO_ROOT_PASSWORD=bbtest-secret",
		"quay.io/minio/minio", "server", "/data").Output()
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	port, err := exec.Command("docker", "port", id, "9000/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + strings.TrimSpace(strings.Split(string(port), "\n")[0])
	for i := 0; ; i++ {
		res, err := http.Get(endpoint + "/minio/health/ready")
		if err == nil && res.StatusCode == 200 {
			res.Body.Close()
			break
		}
		if i > 100 {
			t.Fatalf("minio did not start: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	st := &S3{Endpoint: endpoint, Bucket: "buildbee", AccessKey: "bbtest", SecretKey: "bbtest-secret"}
	ctx := context.Background()
	if err := st.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureBucket(ctx); err != nil {
		t.Fatalf("again: %v", err)
	}
	exercise(t, st)
	wrong := &S3{Endpoint: endpoint, Bucket: "buildbee", AccessKey: "bbtest", SecretKey: "nope"}
	if err := wrong.Put(ctx, "files/x/y", strings.NewReader("x"), 1, ""); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("a bad signature is refused: %v", err)
	}
	fmt.Println("s3 ok against", endpoint)
}
