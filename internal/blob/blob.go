// Package blob stores the bytes BuildBee keeps outside Postgres: message
// attachments and large Artifacts (agent logs, diffs). A directory on the
// Server's disk is the default; any S3-compatible service (MinIO, Garage,
// SeaweedFS, AWS S3) can be used instead.
package blob

import (
	"context"
	"errors"
	"io"
	"regexp"
)

// ErrNotFound is a key with nothing stored under it.
var ErrNotFound = errors.New("blob not found")

// Store keeps bytes under keys. Keys are made by BuildBee (validKey), never
// taken from users.
type Store interface {
	// Put stores size bytes from r under key, replacing what was there.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Get returns the bytes under key and their size; the caller closes them.
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	// Delete removes key; deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// Name says where bytes go, for logs.
	Name() string
}

// keyPattern is segments of [a-z0-9._-] that start with a letter or digit,
// so no segment is "." or "..".
var keyPattern = regexp.MustCompile(`^[a-z0-9]+(/[a-z0-9][a-z0-9._-]*)+$`)

// validKey keeps keys to a shape that is a safe path and object name.
func validKey(key string) error {
	if !keyPattern.MatchString(key) || len(key) > 256 {
		return errors.New("blob: bad key " + key)
	}
	return nil
}
