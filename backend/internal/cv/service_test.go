package cv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/pkg/hash"
)

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

	service := NewService(repo)
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
		t.Errorf("expected sha256 anchor over the storage key, got %s", got)
	}
	if len(profile.Entries) != 1 {
		t.Fatalf("expected 1 entry in the profile, got %d", len(profile.Entries))
	}
}

func TestGetProfileSkipsTrustScoreWhenUnchanged(t *testing.T) {
	repo := newFakeCVRepo()
	repo.profile = &profileRow{ID: "u1", DisplayName: "Ada", Slug: "ada", TrustScore: 125} // already correct for 1 deal
	repo.releasedCount = 1

	service := NewService(repo)
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
	service := NewService(newFakeCVRepo())

	_, err := service.GetProfile(context.Background(), "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetProfileRejectsEmptySlug(t *testing.T) {
	service := NewService(newFakeCVRepo())

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

	service := NewService(repo)
	if err := service.AnchorReleasedDeal(context.Background(), "u1", "d1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := repo.inserted["a1"]; got != hash.SumSHA256([]byte("s3://ganji/work/v1")) {
		t.Errorf("expected sha256 anchor over the storage key, got %s", got)
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

	service := NewService(repo)
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
		StorageKey: "s3://ganji/work/tampered", // storage reference changed
		DealTitle:  "Build a site",
	}

	service := NewService(repo)
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

	service := NewService(repo)
	_, err := service.VerifyEntry(context.Background(), "someone-else", "e1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a slug/entry mismatch, got %v", err)
	}
}

func TestVerifyEntryRejectsEmptyInput(t *testing.T) {
	service := NewService(newFakeCVRepo())

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
	ginTestRouter(NewService(repo)).ServeHTTP(w, req)

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
	ginTestRouter(NewService(newFakeCVRepo())).ServeHTTP(w, req)

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
	ginTestRouter(NewService(repo)).ServeHTTP(w, req)

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
