package cv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/ots"
	"github.com/Amonochuka/ganji-backend/internal/storage"
	"github.com/Amonochuka/ganji-backend/pkg/hash"
)

// fakeStorage is a minimal in-memory Storage for tests.
type fakeStorage struct {
	blobs map[string][]byte
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{blobs: map[string][]byte{}}
}

func (f *fakeStorage) Save(ctx context.Context, key string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.blobs[key] = data
	return nil
}

func (f *fakeStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	data, ok := f.blobs[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeStorage) Size(ctx context.Context, key string) (int64, error) {
	data, ok := f.blobs[key]
	if !ok {
		return 0, storage.ErrNotFound
	}
	return int64(len(data)), nil
}

func (f *fakeStorage) Delete(ctx context.Context, key string) error {
	delete(f.blobs, key)
	return nil
}

func (f *fakeStorage) Close() error {
	return nil
}

// fakeCVRepo is a minimal in-memory CVRepository for service tests.
type fakeCVRepo struct {
	profile         *profileRow
	entries         []Entry
	candidates      []AnchorCandidate
	dealCandidates  []AnchorCandidate
	verifyRec       *entryRecord
	releasedCount   int
	inserted        map[string]string
	updatedScore    float64
	updateScoreCall bool
}

func newFakeCVRepo() *fakeCVRepo {
	return &fakeCVRepo{inserted: map[string]string{}}
}

func (f *fakeCVRepo) GetProfile(ctx context.Context, slug string) (*profileRow, error) {
	if f.profile == nil {
		return nil, ErrNotFound
	}
	return f.profile, nil
}

func (f *fakeCVRepo) ListEntries(ctx context.Context, slug string) ([]Entry, error) {
	return f.entries, nil
}

func (f *fakeCVRepo) ListUnanchoredReleasedArtifacts(ctx context.Context, freelancerID string) ([]AnchorCandidate, error) {
	return f.candidates, nil
}

func (f *fakeCVRepo) ListUnanchoredDealArtifacts(ctx context.Context, dealID, freelancerID string) ([]AnchorCandidate, error) {
	return f.dealCandidates, nil
}

func (f *fakeCVRepo) InsertAnchor(ctx context.Context, artifactID, hash string) error {
	f.inserted[artifactID] = hash
	return nil
}

func (f *fakeCVRepo) CountReleasedDeals(ctx context.Context, freelancerID string) (int, error) {
	return f.releasedCount, nil
}

func (f *fakeCVRepo) UpdateTrustScore(ctx context.Context, accountID string, score float64) error {
	f.updatedScore = score
	f.updateScoreCall = true
	return nil
}

func (f *fakeCVRepo) GetEntryForVerify(ctx context.Context, entryID, slug string) (*entryRecord, error) {
	if f.verifyRec == nil {
		return nil, ErrNotFound
	}
	return f.verifyRec, nil
}

func (f *fakeCVRepo) ListPendingOTSAnchors(ctx context.Context) ([]OTSPendingAnchor, error) {
	return nil, nil
}

func (f *fakeCVRepo) UpdateAnchorOTS(ctx context.Context, artifactID string, proof []byte, submittedAt, confirmedAt *time.Time) error {
	return nil
}

// fakeOTSClient is a no-op OTS client for tests
type fakeOTSClient struct{}

func (f *fakeOTSClient) Submit(ctx context.Context, hash []byte) ([]byte, error) {
	return nil, nil
}

func (f *fakeOTSClient) Upgrade(ctx context.Context, otsProof []byte) ([]byte, error) {
	return nil, nil
}

func setupTestService(repo *fakeCVRepo, storageKeys ...string) *Service {
	store := newFakeStorage()
	for _, key := range storageKeys {
		store.blobs[key] = []byte(key)
	}
	return NewService(repo, store, &fakeOTSClient{})
}

func TestGetProfileAnchorsMissingReleaseWork(t *testing.T) {
	repo := newFakeCVRepo()
	repo.profile = &profileRow{ID: "u1", DisplayName: "Ada", Slug: "ada", TrustScore: 100}
	repo.candidates = []AnchorCandidate{
		{ArtifactID: "a1", StorageKey: "s3://ganji/work/v1", DealID: "d1"},
	}
	repo.releasedCount = 2
	repo.entries = []Entry{{
		ID: "e1", DealTitle: "Build a site", AmountSats: 5000,
		SourcePlatform: "telegram", ArtifactKind: "source_code",
		Hash:       hash.SumSHA256([]byte("s3://ganji/work/v1")),
		VerifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}

	service := setupTestService(repo, "s3://ganji/work/v1")
	profile, err := service.GetProfile(context.Background(), "ada")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if profile.TrustScore != 150 { // 100 + 2*25
		t.Errorf("expected trust score 150 after 2 released deals, got %v", profile.TrustScore)
	}
	if !repo.updateScoreCall || repo.updatedScore != 150 {
		t.Errorf("expected updated score persisted, got call=%v score=%v", repo.updateScoreCall, repo.updatedScore)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 anchor written, got %d", len(repo.inserted))
	}
	if got := repo.inserted["a1"]; got != hash.SumSHA256([]byte("s3://ganji/work/v1")) {
		t.Errorf("expected sha256 anchor over file content, got %s", got)
	}
	if len(profile.Entries) != 1 {
		t.Fatalf("expected 1 entry in the profile, got %d", len(profile.Entries))
	}
}

func TestGetProfileSkipsTrustScoreWhenUnchanged(t *testing.T) {
	repo := newFakeCVRepo()
	repo.profile = &profileRow{ID: "u1", DisplayName: "Ada", Slug: "ada", TrustScore: 125} // already correct for 1 deal
	repo.releasedCount = 1

	service := setupTestService(repo)
	profile, err := service.GetProfile(context.Background(), "ada")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if repo.updateScoreCall {
		t.Error("expected no trust-score write when the value did not change")
	}
	if profile.TrustScore != 125 {
		t.Errorf("expected score 125, got %v", profile.TrustScore)
	}
}

func TestGetProfileNotFound(t *testing.T) {
	service := setupTestService(newFakeCVRepo())

	_, err := service.GetProfile(context.Background(), "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetProfileRejectsEmptySlug(t *testing.T) {
	service := setupTestService(newFakeCVRepo())

	_, err := service.GetProfile(context.Background(), "   ")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestAnchorReleasedDealWritesHashes(t *testing.T) {
	repo := newFakeCVRepo()
	repo.dealCandidates = []AnchorCandidate{
		{ArtifactID: "a1", StorageKey: "s3://ganji/work/v1", DealID: "d1"},
	}

	service := setupTestService(repo, "s3://ganji/work/v1")
	if err := service.AnchorReleasedDeal(context.Background(), "u1", "d1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := repo.inserted["a1"]; got != hash.SumSHA256([]byte("s3://ganji/work/v1")) {
		t.Errorf("expected sha256 anchor over file content, got %s", got)
	}
}

func TestVerifyEntryMatchesHash(t *testing.T) {
	repo := newFakeCVRepo()
	repo.verifyRec = &entryRecord{
		ID:         "e1",
		Hash:       hash.SumSHA256([]byte("s3://ganji/work/v1")),
		Algorithm:  "sha256",
		StorageKey: "s3://ganji/work/v1",
		DealTitle:  "Build a site",
		VerifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	service := setupTestService(repo, "s3://ganji/work/v1")
	result, err := service.VerifyEntry(context.Background(), "ada", "e1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !result.Valid {
		t.Error("expected the entry to verify")
	}
	if !result.MatchesCurrent {
		t.Error("expected stored hash to match the current artifact reference")
	}
	if result.Algorithm != "sha256" {
		t.Errorf("expected algorithm sha256, got %s", result.Algorithm)
	}
}

func TestVerifyEntryDetectsTamperedAnchor(t *testing.T) {
	repo := newFakeCVRepo()
	repo.verifyRec = &entryRecord{
		ID:         "e1",
		Hash:       hash.SumSHA256([]byte("s3://ganji/work/v1")),
		Algorithm:  "sha256",
		StorageKey: "s3://ganji/work/tampered", // file content changed
		DealTitle:  "Build a site",
	}

	service := setupTestService(repo, "s3://ganji/work/tampered")
	result, err := service.VerifyEntry(context.Background(), "ada", "e1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Valid {
		t.Error("expected the entry to fail verification when the hash does not match")
	}
	if result.MatchesCurrent {
		t.Error("expected stored hash to differ from the current artifact reference")
	}
}

func TestVerifyEntryHidesForeignSlug(t *testing.T) {
	repo := newFakeCVRepo()

	service := setupTestService(repo)
	_, err := service.VerifyEntry(context.Background(), "someone-else", "e1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a slug/entry mismatch, got %v", err)
	}
}

func TestVerifyEntryRejectsEmptyInput(t *testing.T) {
	service := setupTestService(newFakeCVRepo())

	if _, err := service.VerifyEntry(context.Background(), "", "e1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty slug, got %v", err)
	}
	if _, err := service.VerifyEntry(context.Background(), "ada", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty entry id, got %v", err)
	}
}

func ginTestRouter(service *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, NewHandler(service))
	return router
}

func TestGetProfileHandler(t *testing.T) {
	repo := newFakeCVRepo()
	repo.profile = &profileRow{ID: "u1", DisplayName: "Ada", Slug: "ada", TrustScore: 125}
	repo.releasedCount = 1
	repo.entries = []Entry{{
		ID: "e1", DealTitle: "Build a site", AmountSats: 5000,
		SourcePlatform: "telegram", ArtifactKind: "source_code",
		Hash:       hash.SumSHA256([]byte("s3://ganji/work/v1")),
		VerifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}

	req := httptest.NewRequest(http.MethodGet, "/cv/ada", nil)
	w := httptest.NewRecorder()
	ginTestRouter(setupTestService(repo, "s3://ganji/work/v1")).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Profile struct {
			DisplayName string `json:"display_name"`
			Slug        string `json:"slug"`
			TrustScore  int    `json:"trust_score"`
			Entries     []any  `json:"entries"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Profile.DisplayName != "Ada" || body.Profile.Slug != "ada" {
		t.Errorf("unexpected identity in response: %+v", body.Profile)
	}
	if body.Profile.TrustScore != 125 {
		t.Errorf("expected trust score 125, got %d", body.Profile.TrustScore)
	}
	if len(body.Profile.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(body.Profile.Entries))
	}
}

func TestGetProfileHandlerNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/cv/nobody", nil)
	w := httptest.NewRecorder()
	ginTestRouter(setupTestService(newFakeCVRepo())).ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestVerifyEntryHandler(t *testing.T) {
	repo := newFakeCVRepo()
	repo.verifyRec = &entryRecord{
		ID:         "e1",
		Hash:       hash.SumSHA256([]byte("s3://ganji/work/v1")),
		Algorithm:  "sha256",
		StorageKey: "s3://ganji/work/v1",
		DealTitle:  "Build a site",
		VerifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	req := httptest.NewRequest(http.MethodGet, "/cv/ada/verify/e1", nil)
	w := httptest.NewRecorder()
	ginTestRouter(setupTestService(repo, "s3://ganji/work/v1")).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Verification struct {
			Valid     bool   `json:"valid"`
			EntryID   string `json:"entry_id"`
			DealTitle string `json:"deal_title"`
		} `json:"verification"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Verification.Valid {
		t.Error("expected verification to pass")
	}
	if body.Verification.EntryID != "e1" {
		t.Errorf("expected entry id e1, got %s", body.Verification.EntryID)
	}
}

// fixtureBlockHeight is the Bitcoin block the shared OTS fixture proof is
// attested at — see internal/ots/testdata.
const fixtureBlockHeight = 891686

// otsFixture returns the confirmed .ots proof, the artifact bytes it anchors,
// and the artifact's content hash, using the real OTS test proof committed in
// internal/ots/testdata (a proof produced by the OpenTimestamps project).
func otsFixture(t *testing.T) (proof []byte, artifact []byte, artifactHash string) {
	t.Helper()
	base := filepath.Join("..", "ots", "testdata")
	proof, err := os.ReadFile(filepath.Join(base, "flatearthers-united.txt.ots"))
	if err != nil {
		t.Fatalf("read fixture proof: %v", err)
	}
	artifact, err = os.ReadFile(filepath.Join(base, "flatearthers-united.txt"))
	if err != nil {
		t.Fatalf("read fixture artifact: %v", err)
	}
	return proof, artifact, hash.SumSHA256(artifact)
}

func TestVerifyEntryWithOTSProof(t *testing.T) {
	proof, artifact, artifactHash := otsFixture(t)
	confirmedAt := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	repo := newFakeCVRepo()
	repo.verifyRec = &entryRecord{
		ID:             "e1",
		Hash:           artifactHash,
		Algorithm:      "sha256",
		StorageKey:     "s3://ganji/work/v1",
		DealTitle:      "Build a site",
		VerifiedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		OTSProof:       proof,
		OTSConfirmedAt: &confirmedAt,
	}

	store := newFakeStorage()
	store.blobs["s3://ganji/work/v1"] = artifact
	service := NewService(repo, store, &fakeOTSClient{}, WithVerifier(ots.NewVerifier()))

	result, err := service.VerifyEntry(context.Background(), "ada", "e1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Valid {
		t.Error("expected the entry to verify")
	}
	if !result.OTSVerified {
		t.Error("expected the OTS proof to verify")
	}
	if result.OTSBlockHeight != fixtureBlockHeight {
		t.Errorf("expected OTS block height %d, got %d", fixtureBlockHeight, result.OTSBlockHeight)
	}
	// Offline verification cannot derive a block time, so the confirmed-at
	// timestamp recorded when the upgrade worker stored the proof is kept.
	if result.OTSConfirmedAt == nil || !result.OTSConfirmedAt.Equal(confirmedAt) {
		t.Errorf("expected OTSConfirmedAt %v, got %v", confirmedAt, result.OTSConfirmedAt)
	}
}

func TestVerifyEntryWithoutOTSProof(t *testing.T) {
	_, artifact, artifactHash := otsFixture(t)

	repo := newFakeCVRepo()
	repo.verifyRec = &entryRecord{
		ID:         "e1",
		Hash:       artifactHash,
		Algorithm:  "sha256",
		StorageKey: "s3://ganji/work/v1",
		DealTitle:  "Build a site",
	}

	store := newFakeStorage()
	store.blobs["s3://ganji/work/v1"] = artifact
	service := NewService(repo, store, &fakeOTSClient{}, WithVerifier(ots.NewVerifier()))

	result, err := service.VerifyEntry(context.Background(), "ada", "e1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Valid {
		t.Error("expected the entry to verify")
	}
	if result.OTSVerified {
		t.Error("expected OTSVerified false when no proof is stored")
	}
}

func TestVerifyEntryWithMismatchedOTSProof(t *testing.T) {
	proof, _, _ := otsFixture(t) // proof anchors a different artifact
	confirmedAt := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	content := []byte("some other artifact content")
	repo := newFakeCVRepo()
	repo.verifyRec = &entryRecord{
		ID:             "e1",
		Hash:           hash.SumSHA256(content),
		Algorithm:      "sha256",
		StorageKey:     "s3://ganji/work/v1",
		DealTitle:      "Build a site",
		OTSProof:       proof,
		OTSConfirmedAt: &confirmedAt,
	}

	store := newFakeStorage()
	store.blobs["s3://ganji/work/v1"] = content
	service := NewService(repo, store, &fakeOTSClient{}, WithVerifier(ots.NewVerifier()))

	result, err := service.VerifyEntry(context.Background(), "ada", "e1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Valid {
		t.Error("expected the hash verification to pass")
	}
	if result.OTSVerified {
		t.Error("expected OTSVerified false when the proof commits to a different digest")
	}
}
