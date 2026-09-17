package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Local is a Storage backend that writes blobs under a directory on disk.
// It is the default for dev and small deployments; keys are resolved against
// the root with traversal protection so a caller-supplied key can never reach
// a file outside the upload directory.
type Local struct {
	root string
}

// NewLocal resolves and creates the root directory.
func NewLocal(root string) (*Local, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("storage: empty root path")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: resolving root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage: creating root %s: %w", abs, err)
	}
	return &Local{root: abs}, nil
}

// resolve joins key onto root, then verifies the result is still inside root.
// Without this check a key like "../../etc/passwd" could redirect writes
// outside the upload directory.
func (l *Local) resolve(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", errors.New("storage: empty key")
	}
	joined := filepath.Join(l.root, key)
	rel, err := filepath.Rel(l.root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage: key %q escapes storage root", key)
	}
	return joined, nil
}

func (l *Local) Save(ctx context.Context, key string, r io.Reader) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("storage: creating directories: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("storage: creating %s: %w", key, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("storage: writing %s: %w", key, err)
	}
	return nil
}

func (l *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: opening %s: %w", key, err)
	}
	return f, nil
}

func (l *Local) Size(ctx context.Context, key string) (int64, error) {
	path, err := l.resolve(key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("storage: stat %s: %w", key, err)
	}
	return info.Size(), nil
}

func (l *Local) Delete(ctx context.Context, key string) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: deleting %s: %w", key, err)
	}
	return nil
}

func (l *Local) Close() error { return nil }
