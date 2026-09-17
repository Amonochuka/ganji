package deals

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
	"github.com/Amonochuka/ganji-backend/internal/storage"
)

func newTestServiceWithStorage(t *testing.T, repo DealRepository, maxBytes int64) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	st, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, maxBytes))
	return service, root
}

func TestUploadArtifactStreamsToStorage(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	st, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))

	artifact, err := service.UploadArtifact(
		context.Background(),
		"freelancer-1",
		deal.ID,
		ArtifactSourceCode,
		"patch.txt",
		strings.NewReader("my delivered patch\n"),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if artifact.StorageKey == "" {
		t.Fatal("expected a storage key to be generated")
	}
	if !strings.HasPrefix(artifact.StorageKey, "deals/deal-1/") {
		t.Errorf("expected deal-scoped key, got %q", artifact.StorageKey)
	}
	if !strings.HasSuffix(artifact.StorageKey, ".txt") {
		t.Errorf("expected sanitized extension in key, got %q", artifact.StorageKey)
	}

	r, err := st.Open(context.Background(), artifact.StorageKey)
	if err != nil {
		t.Fatalf("stored blob should exist: %v", err)
	}
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(got) != "my delivered patch\n" {
		t.Errorf("unexpected blob content: %q", got)
	}

	if len(repo.artifacts[deal.ID]) != 1 {
		t.Fatalf("expected the artifact to be recorded, got %d", len(repo.artifacts[deal.ID]))
	}
}

func TestUploadArtifactRejectsOversizeAndLeavesNoBlob(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusAwaitingPayment

	root := t.TempDir()
	st, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 10))

	_, err = service.UploadArtifact(
		context.Background(),
		"freelancer-1",
		deal.ID,
		ArtifactSourceFile,
		"big.zip",
		strings.NewReader(strings.Repeat("x", 100)),
	)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for oversized upload, got %v", err)
	}

	if n := fileCount(root); n != 0 {
		t.Errorf("expected no leftover blob after an oversized upload, found %d file(s)", n)
	}
}

func TestUploadArtifactRejectsNonOwner(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service, _ := newTestServiceWithStorage(t, repo, 1024)

	_, err := service.UploadArtifact(context.Background(), "someone-else", "deal-1", ArtifactSourceCode, "a.txt", strings.NewReader("x"))
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestUploadArtifactRejectsAfterSubmission(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReleased

	service, _ := newTestServiceWithStorage(t, repo, 1024)

	_, err := service.UploadArtifact(context.Background(), "freelancer-1", "deal-1", ArtifactSourceCode, "a.txt", strings.NewReader("x"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for closed deal, got %v", err)
	}
}

func TestUploadArtifactRejectsInvalidKind(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service, _ := newTestServiceWithStorage(t, repo, 1024)

	_, err := service.UploadArtifact(context.Background(), "freelancer-1", "deal-1", "video", "a.txt", strings.NewReader("x"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for bad kind, got %v", err)
	}
}

func TestUploadArtifactWithoutStorage(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := NewService(repo, &lnbits.Client{})

	_, err := service.UploadArtifact(context.Background(), "freelancer-1", "deal-1", ArtifactSourceCode, "a.txt", strings.NewReader("x"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput when storage is missing, got %v", err)
	}
}

func uploadHelper(t *testing.T, service *Service, dealID string, content string) *Artifact {
	t.Helper()
	artifact, err := service.UploadArtifact(
		context.Background(),
		"freelancer-1",
		dealID,
		ArtifactSourceCode,
		"patch.txt",
		strings.NewReader(content),
	)
	if err != nil {
		t.Fatalf("upload helper: %v", err)
	}
	return artifact
}

func TestDownloadArtifactAsFreelancer(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service, _ := newTestServiceWithStorage(t, repo, 1024)
	artifact := uploadHelper(t, service, deal.ID, "work content")

	_, out, size, err := service.DownloadArtifact(context.Background(), "freelancer-1", "", deal.ID, artifact.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer out.Close()

	body, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "work content" {
		t.Errorf("unexpected body %q", body)
	}
	if size != int64(len("work content")) {
		t.Errorf("expected size %d, got %d", len("work content"), size)
	}
}

func TestDownloadArtifactAsClient(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service, _ := newTestServiceWithStorage(t, repo, 1024)
	artifact := uploadHelper(t, service, deal.ID, "work content")

	// The client ("visitor-id") is not the freelancer, but matches client_email.
	_, out, _, err := service.DownloadArtifact(context.Background(), "visitor-id", "CLIENT@example.com", deal.ID, artifact.ID)
	if err != nil {
		t.Fatalf("expected client download, got %v", err)
	}
	out.Close()
}

func TestDownloadArtifactRejectsStranger(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service, _ := newTestServiceWithStorage(t, repo, 1024)
	artifact := uploadHelper(t, service, deal.ID, "work content")

	_, _, _, err := service.DownloadArtifact(context.Background(), "stranger", "stranger@example.com", deal.ID, artifact.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestDownloadArtifactUnknownOrMismatched(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service, _ := newTestServiceWithStorage(t, repo, 1024)

	if _, _, _, err := service.DownloadArtifact(context.Background(), "freelancer-1", "", "deal-1", "nope"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound for unknown artifact, got %v", err)
	}

	repo.artifacts["deal-1"] = []Artifact{{ID: "a1", DealID: "deal-1", StorageKey: "deals/deal-1/missing"}}
	if _, _, _, err := service.DownloadArtifact(context.Background(), "freelancer-1", "", "deal-2", "a1"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound for deal mismatch, got %v", err)
	}
	if _, _, _, err := service.DownloadArtifact(context.Background(), "freelancer-1", "", "deal-1", "a1"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound for missing blob, got %v", err)
	}
}

// fileCount returns the number of regular files under root (storage dirs and
// subdirectories don't count).
func fileCount(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return nil
	})
	return count
}
