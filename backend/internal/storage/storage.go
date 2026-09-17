package storage

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned when the referenced object does not exist.
var ErrNotFound = errors.New("storage: object not found")

// Storage persists artifact blobs and hands them back for download. Keys are
// opaque strings chosen by the caller (e.g. "deals/<dealID>/<random>") — the
// backend resolves them to its own layout, so callers must not assume anything
// about the physical path. S3 and other backends can implement this interface
// without touching callers.
type Storage interface {
	// Save writes the stream to key, replacing any previous content.
	// The key must be non-empty and must not escape the backend's root.
	Save(ctx context.Context, key string, r io.Reader) error
	// Open returns a reader for the saved object, or ErrNotFound.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Size returns the stored object's size in bytes.
	Size(ctx context.Context, key string) (int64, error)
	// Delete removes the object; a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// Close releases any underlying resources.
	Close() error
}
