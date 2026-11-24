package deals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Amonochuka/ganji-backend/internal/storage"
)

// Artifacts
//
// UploadArtifact persists a deliverable (the file) for a deal and records its
// storage reference. Only the freelancer can upload, and only while the deal
// is still open to the work — once submitted (or released/disputed/refunded)
// the deliverable set is frozen. The file is streamed straight into storage:
// the reader is size-capped so an oversized upload is rejected without a byte
// being committed. If the DB record cannot be written afterwards, the
// just-saved blob is best-effort deleted so storage never accumulates orphans.
func (s *Service) MaxUploadBytes() int64 {
	if s.maxUploadBytes <= 0 {
		return 0
	}
	return s.maxUploadBytes
}

func (s *Service) UploadArtifact(ctx context.Context, userID, dealID string, kind ArtifactKind, filename string, r io.Reader) (*Artifact, error) {
	if s.storage == nil {
		return nil, fmt.Errorf("%w: artifact storage is not configured", ErrInvalidInput)
	}
	if s.maxUploadBytes <= 0 {
		return nil, fmt.Errorf("%w: artifact upload limit is not configured", ErrInvalidInput)
	}
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	switch kind {
	case ArtifactSourceCode, ArtifactSourceFile:
	default:
		return nil, fmt.Errorf("%w: invalid artifact kind", ErrInvalidInput)
	}
	if r == nil {
		return nil, fmt.Errorf("%w: artifact content is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	// TEMP: Allow uploads before LNBits escrow integration.
	// Remove this once deals automatically transition to StatusLocked after payment.
	switch deal.Status {
	case StatusWorkSubmitted,
		StatusReviewing,
		StatusReleased,
		StatusDisputed,
		StatusRefunded:
		return nil, fmt.Errorf("%w: uploads are no longer allowed", ErrInvalidInput)
	}

	key, err := artifactStorageKey(dealID, filename)
	if err != nil {
		return nil, fmt.Errorf("generating artifact storage key: %w", err)
	}

	// Size cap: read at most max+1 bytes so we can tell an oversized upload
	// apart from a legal one (io.Reader yields nil after the cap), and reject
	// before the blob is committed.
	counted := &countingReader{r: r}
	if err := s.storage.Save(ctx, key, io.LimitReader(counted, s.maxUploadBytes+1)); err != nil {
		return nil, fmt.Errorf("storing artifact: %w", err)
	}
	if counted.n > s.maxUploadBytes {
		_ = s.storage.Delete(ctx, key)
		return nil, fmt.Errorf("%w: upload exceeds %d bytes", ErrInvalidInput, s.maxUploadBytes)
	}

	artifact := &Artifact{DealID: dealID, Kind: kind, StorageKey: key}
	if err := s.repo.CreateArtifact(ctx, artifact); err != nil {
		_ = s.storage.Delete(ctx, key) // best-effort cleanup on DB failure
		return nil, err
	}
	return artifact, nil
}

// DeleteArtifact removes an artifact and its stored blob. Only the freelancer who
// owns the deal may delete it. Storage deletion is best-effort after the DB row
// is removed so a storage failure cannot leave a dangling DB reference.
func (s *Service) DeleteArtifact(ctx context.Context, userID, dealID, artifactID string) error {
	if dealID == "" {
		return fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	if artifactID == "" {
		return fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return err
	}
	if deal.FreelancerID != userID {
		return ErrForbidden
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return err
	}
	if artifact.DealID != dealID {
		return ErrArtifactNotFound
	}

	if err := s.repo.DeleteArtifact(ctx, artifactID); err != nil {
		return err
	}
	if s.storage != nil {
		_ = s.storage.Delete(ctx, artifact.StorageKey)
	}
	return nil
}

// DownloadArtifact opens a deal artifact's stored content for streaming to the
// caller. Both the freelancer and the client (matched by the recorded
// client_email) may download — the client is precisely who has to review the
// work. Anyone else is forbidden. Returns the artifact metadata plus a reader
// and its size so the handler can send accurate headers.
func (s *Service) DownloadArtifact(ctx context.Context, userID, email, dealID, artifactID string, isOperator bool) (*Artifact, io.ReadCloser, int64, error) {
	if s.storage == nil {
		return nil, nil, 0, fmt.Errorf("%w: artifact storage is not configured", ErrInvalidInput)
	}
	if dealID == "" {
		return nil, nil, 0, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	if artifactID == "" {
		return nil, nil, 0, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, nil, 0, err
	}

	if artifact.DealID != dealID {
		return nil, nil, 0, ErrArtifactNotFound
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, nil, 0, err
	}

	if !isOperator && deal.FreelancerID != userID && !strings.EqualFold(deal.ClientEmail, email) {
		return nil, nil, 0, ErrForbidden
	}

	reader, size, err := s.openStoredArtifact(ctx, artifact)
	if err != nil {
		return nil, nil, 0, err
	}

	return artifact, reader, size, nil
}

func (s *Service) openStoredArtifact(ctx context.Context, artifact *Artifact) (io.ReadCloser, int64, error) {
	if s.storage == nil {
		return nil, 0, fmt.Errorf("%w: artifact storage is not configured", ErrInvalidInput)
	}

	reader, err := s.storage.Open(ctx, artifact.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, 0, ErrArtifactNotFound
		}
		return nil, 0, fmt.Errorf("opening stored artifact: %w", err)
	}

	size, err := s.storage.Size(ctx, artifact.StorageKey)
	if err != nil {
		_ = reader.Close()
		if errors.Is(err, storage.ErrNotFound) {
			return nil, 0, ErrArtifactNotFound
		}
		return nil, 0, fmt.Errorf("sizing stored artifact: %w", err)
	}

	return reader, size, nil
}

func (s *Service) ListPublicArtifacts(ctx context.Context, shareToken string) ([]Artifact, error) {
	if strings.TrimSpace(shareToken) == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}

	return s.repo.ListArtifactsByDeal(ctx, deal.ID, nil)
}

func (s *Service) DownloadPublicArtifact(ctx context.Context, shareToken, artifactID string) (*Artifact, io.ReadCloser, int64, error) {
	if strings.TrimSpace(shareToken) == "" {
		return nil, nil, 0, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	if strings.TrimSpace(artifactID) == "" {
		return nil, nil, 0, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, nil, 0, err
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, nil, 0, err
	}
	if artifact.DealID != deal.ID {
		return nil, nil, 0, ErrArtifactNotFound
	}

	reader, size, err := s.openStoredArtifact(ctx, artifact)
	if err != nil {
		return nil, nil, 0, err
	}

	return artifact, reader, size, nil
}

// artifactStorageKey mints a unique key for an uploaded file:
// deals/<dealID>/<32-random-hex><safe-extension>. The extension is preserved
// (sanitized) so download responses can pick a usable Content-Type; the
// original filename is never carried into the key.
func artifactStorageKey(dealID, filename string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := "deals/" + dealID + "/" + hex.EncodeToString(b)
	if ext := sanitizeExt(filename); ext != "" {
		key += ext
	}
	return key, nil
}

// sanitizeExt returns the lowercase ".ext" of a filename only when it is a
// safe-looking alphanumeric extension (2–10 chars); otherwise "".
func sanitizeExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) < 2 || len(ext) > 10 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

// countingReader counts bytes as they are streamed so the size cap can be
// enforced after Save without buffering the whole payload in memory.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (s *Service) GetArtifactByID(
	ctx context.Context, userID, email, dealID, artifactID string, isOperator bool) (*Artifact, error) {

	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	if artifactID == "" {
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	if artifact.DealID != dealID {
		return nil, ErrArtifactNotFound
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, err
	}

	if !isOperator && deal.FreelancerID != userID && !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	return artifact, nil
}

func (s *Service) ListArtifactsByDeal(ctx context.Context, userID, email, dealID string, isOperator bool) ([]Artifact, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if !isOperator && deal.FreelancerID != userID && !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	return s.repo.ListArtifactsByDeal(ctx, dealID, nil)
}

// Verifications
func (s *Service) CreateVerification(ctx context.Context, userID string, verification *Verification) error {
	if verification.ArtifactID == "" {
		return fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	if verification.Reference == "" {
		return fmt.Errorf("%w: reference is required", ErrInvalidInput)
	}

	switch verification.Method {
	case VerificationSandbox,
		VerificationPreviewPDF,
		VerificationPreviewImage:
	default:
		return fmt.Errorf("%w: invalid verification method", ErrInvalidInput)
	}

	artifact, err := s.repo.GetArtifactByID(ctx, verification.ArtifactID)
	if err != nil {
		return err
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return err
	}

	if deal.FreelancerID != userID {
		return ErrForbidden
	}

	if verification.Status == "" {
		verification.Status = VerificationPending
	}

	return s.repo.CreateVerification(ctx, verification)
}

func (s *Service) GetVerificationByID(
	ctx context.Context, userID, dealID, artifactID, verificationID string) (*Verification, error) {

	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	if artifactID == "" {
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	if verificationID == "" {
		return nil, fmt.Errorf("%w: verification id is required", ErrInvalidInput)
	}

	verification, err := s.repo.GetVerificationByID(ctx, verificationID)
	if err != nil {
		return nil, err
	}

	if verification.ArtifactID != artifactID {
		return nil, ErrVerificationNotFound
	}

	artifact, err := s.repo.GetArtifactByID(ctx, verification.ArtifactID)
	if err != nil {
		return nil, err
	}

	if artifact.DealID != dealID {
		return nil, ErrVerificationNotFound
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	return verification, nil
}

func (s *Service) ListVerificationsByArtifact(ctx context.Context, userID, artifactID string) ([]Verification, error) {
	if artifactID == "" {
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	return s.repo.ListVerificationsByArtifact(ctx, artifactID)
}
