package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Dir stores blobs as files under Root.
type Dir struct{ Root string }

func (d Dir) Name() string { return "dir " + d.Root }

func (d Dir) path(key string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	return filepath.Join(d.Root, filepath.FromSlash(key)), nil
}

// Put writes to a temporary file and renames it into place, so a reader
// never sees half a blob.
func (d Dir) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(r, size+1))
	if err == nil && n != size {
		err = fmt.Errorf("blob: got %d bytes, want %d", n, size)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (d Dir) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (d Dir) Delete(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
