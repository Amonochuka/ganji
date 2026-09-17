package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLocalSaveOpenRoundTrip(t *testing.T) {
	st, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	content := []byte("my delivered source code\n")

	if err := st.Save(ctx, "deals/deal-1/abc123.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("save: %v", err)
	}

	size, err := st.Size(ctx, "deals/deal-1/abc123.txt")
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}

	r, err := st.Open(ctx, "deals/deal-1/abc123.txt")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, content)
	}
}

func TestLocalDeleteRemovesObject(t *testing.T) {
	st, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	key := "deals/deal-1/abc123"

	if err := st.Save(ctx, key, strings.NewReader("data")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := st.Open(ctx, key); err != nil {
		t.Fatalf("object should exist: %v", err)
	}

	if err := st.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Deleting a missing object is not an error (best-effort cleanup).
	if err := st.Delete(ctx, key); err != nil {
		t.Fatalf("delete missing object: %v", err)
	}
	if _, err := st.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestLocalRejectsTraversal(t *testing.T) {
	st, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	// Absolute-looking and URL-encoded keys are neutralized by filepath.Join
	// and resolve inside the root, which is fine. Keys that truly escape it,
	// or are empty, must be rejected.
	for _, evil := range []string{
		"../../etc/passwd",
		"",
		"  ",
	} {
		if err := st.Save(ctx, evil, strings.NewReader("x")); err == nil {
			t.Errorf("expected save to reject key %q", evil)
		}
		if _, err := st.Open(ctx, evil); err == nil {
			t.Errorf("expected open to reject key %q", evil)
		}
	}
}

func TestLocalOpenMissingReturnsNotFound(t *testing.T) {
	st, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	if _, err := st.Open(context.Background(), "deals/deal-1/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNewLocalRejectsEmptyRoot(t *testing.T) {
	if _, err := NewLocal(""); err == nil {
		t.Fatal("expected error for empty root")
	}
}
