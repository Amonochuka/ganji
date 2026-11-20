package deals

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
	"github.com/Amonochuka/ganji-backend/internal/storage"
)

// newTestArtifactRouter mounts the artifact routes behind a stub auth context,
// simulating what middleware.AuthRequired sets for a real request.
func newTestArtifactRouter(t *testing.T, service *Service, userID, email string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()

	protected := router.Group("/")
	protected.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Set("email", email)
		c.Next()
	})
	RegisterArtifactRoutes(protected, NewHandler(service))
	return router
}

func uploadFixture(t *testing.T, service *Service, dealID, content string) *Artifact {
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
		t.Fatalf("upload fixture: %v", err)
	}
	return artifact
}

func TestCreateArtifactMultipartUpload(t *testing.T) {
	repo := newFakeDealRepo()
	_ = escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	root := t.TempDir()
	st, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	router := newTestArtifactRouter(t, service, "freelancer-1", "freelancer-1@example.com")

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("kind", string(ArtifactSourceCode)); err != nil {
		t.Fatalf("write kind: %v", err)
	}
	part, err := w.CreateFormFile("artifact", "patch.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("handler upload content")); err != nil {
		t.Fatalf("write content: %v", err)
	}
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/artifacts", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"storage_key":"deals/deal-1/`)) {
		t.Errorf("expected a generated storage key in the response, got %s", resp.Body.String())
	}

	if n := fileCount(root); n != 1 {
		t.Errorf("expected exactly 1 stored blob, found %d", n)
	}
}

func TestCreateArtifactRejectsMissingFile(t *testing.T) {
	repo := newFakeDealRepo()
	_ = escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	st, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	router := newTestArtifactRouter(t, service, "freelancer-1", "freelancer-1@example.com")

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("kind", string(ArtifactSourceCode)); err != nil {
		t.Fatalf("write kind: %v", err)
	}
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/artifacts", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreateArtifactStreamsAndRejectsOversizedFile(t *testing.T) {
	repo := newFakeDealRepo()
	_ = escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	root := t.TempDir()
	st, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	router := newTestArtifactRouter(t, service, "freelancer-1", "freelancer-1@example.com")

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("kind", string(ArtifactSourceCode)); err != nil {
		t.Fatalf("write kind: %v", err)
	}
	part, err := w.CreateFormFile("artifact", "huge.bin")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("z"), 4096)); err != nil {
		t.Fatalf("write content: %v", err)
	}
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/artifacts", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized file, got %d: %s", resp.Code, resp.Body.String())
	}
	if n := fileCount(root); n != 0 {
		t.Errorf("expected no stored blob for a rejected upload, found %d", n)
	}
}

func TestCreateArtifactRequiresKindBeforeFile(t *testing.T) {
	repo := newFakeDealRepo()
	_ = escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	st, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	router := newTestArtifactRouter(t, service, "freelancer-1", "freelancer-1@example.com")

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("artifact", "patch.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("x"))
	if err := w.WriteField("kind", string(ArtifactSourceCode)); err != nil {
		t.Fatalf("write kind: %v", err)
	}
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/artifacts", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when the file precedes kind, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestDownloadArtifactStreamsBlob(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	st, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	artifact := uploadFixture(t, service, deal.ID, "handler download content")

	router := newTestArtifactRouter(t, service, "freelancer-1", "freelancer-1@example.com")
	resp := doGet(t, router, "/deals/deal-1/artifacts/"+artifact.ID+"/download", nil)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if resp.Body.String() != "handler download content" {
		t.Errorf("unexpected body %q", resp.Body.String())
	}
	if cd := resp.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("expected attachment disposition, got %q", cd)
	}
	if ct := resp.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" && ct != "text/plain" {
		t.Logf("content type %q (extension-driven, ok if octet-stream)", ct)
	}
}

func TestDownloadArtifactHandlerRejectsStranger(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	st, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	artifact := uploadFixture(t, service, deal.ID, "secret work")

	router := newTestArtifactRouter(t, service, "stranger", "stranger@example.com")
	resp := doGet(t, router, "/deals/deal-1/artifacts/"+artifact.ID+"/download", nil)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.Code)
	}
}

func TestCreateArtifactNonPartyForbidden(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	st, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	router := newTestArtifactRouter(t, service, "stranger", "stranger@example.com")

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("kind", string(ArtifactSourceCode)); err != nil {
		t.Fatalf("write kind: %v", err)
	}
	part, err := w.CreateFormFile("artifact", "patch.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("x"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/artifacts", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.Code)
	}
}

func TestPublicArtifactRoutesUseShareToken(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	st, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	defer st.Close()

	service := NewService(repo, &lnbits.Client{}, WithStorage(st, 1024))
	artifact := uploadFixture(t, service, deal.ID, "public deliverable content")

	gin.SetMode(gin.TestMode)
	router := gin.New()
	public := router.Group("/public")
	RegisterPublicArtifactRoutes(public, NewHandler(service))

	list := doGet(t, router, "/public/deals/"+deal.ShareToken+"/artifacts", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), artifact.ID) {
		t.Errorf("expected artifact in list response, got %s", list.Body.String())
	}

	download := doGet(t, router, "/public/deals/"+deal.ShareToken+"/artifacts/"+artifact.ID+"/download", nil)
	if download.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", download.Code, download.Body.String())
	}
	if download.Body.String() != "public deliverable content" {
		t.Errorf("unexpected body %q", download.Body.String())
	}

	wrong := doGet(t, router, "/public/deals/wrong-token/artifacts", nil)
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong share token, got %d", wrong.Code)
	}
}

func doGet(t *testing.T, router http.Handler, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, body)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}
