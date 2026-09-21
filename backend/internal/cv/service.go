package cv

import (
	"context"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"log"
	"math"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/ots"
	"github.com/Amonochuka/ganji-backend/internal/storage"
	ghash "github.com/Amonochuka/ganji-backend/pkg/hash"
)

// Trust-score derivation. Every freelancer starts at the signup default
// (100); each released (verified) deal adds a fixed increment up to a cap.
// The number is recalculated whenever the public CV is read.
const (
	trustScoreBase    = 100.0
	trustScorePerDeal = 25.0
	trustScoreCap     = 1000.0
)

// OTSClient defines the interface for OpenTimestamps operations
type OTSClient interface {
	Submit(ctx context.Context, hash []byte) ([]byte, error)
	Upgrade(ctx context.Context, otsProof []byte) ([]byte, error)
	GetProof(ctx context.Context, hash []byte) ([]byte, error)
}

type Service struct {
	repo      CVRepository
	store     storage.Storage
	otsClient OTSClient
	hasher    func() hash.Hash
}

func NewService(repo CVRepository, store storage.Storage, otsClient OTSClient) *Service {
	return &Service{
		repo:      repo,
		store:     store,
		otsClient: otsClient,
		hasher:    ghash.NewSHA256,
	}
}

// GetProfile builds the public CV for a freelancer's slug. Before answering
// it self-heals any missing anchors: a released deal whose artifacts were
// never anchored (approve-time hook failed, or the deal predates Live CV)
// gets its entries written now. Trust score is also refreshed. All of that
// is derived data, so it never blocks reading the CV itself.
func (s *Service) GetProfile(ctx context.Context, slug string) (*Profile, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, ErrInvalidInput
	}

	row, err := s.repo.GetProfile(ctx, slug)
	if err != nil {
		return nil, err
	}

	if err := s.healAnchors(ctx, row.ID); err != nil {
		log.Printf("cv: healAnchors failed for %s: %v", row.Slug, err)
	}

	// A stale score must not take the CV down — fall back to the stored one.
	score := row.TrustScore
	if refreshed, err := s.refreshTrustScore(ctx, row.ID, row.TrustScore); err != nil {
		log.Printf("cv: refreshing trust score for %s: %v", row.Slug, err)
	} else {
		score = refreshed
	}

	entries, err := s.repo.ListEntries(ctx, slug)
	if err != nil {
		return nil, err
	}

	return &Profile{
		DisplayName: row.DisplayName,
		Slug:        row.Slug,
		TrustScore:  score,
		Entries:     entries,
	}, nil
}

// AnchorReleasedDeal writes the hash anchors for a just-released deal's
// artifacts. It is the CVAnchorer hook the deals service calls after the
// escrow is released (see deals.WithCVAnchorer). The SQL restricts to
// released deals owned by the freelancer and still without anchors, so a
// duplicate call is a no-op. Failure is best-effort — the CV self-heals on
// the next read.
func (s *Service) AnchorReleasedDeal(ctx context.Context, freelancerID, dealID string) error {
	candidates, err := s.repo.ListUnanchoredDealArtifacts(ctx, dealID, freelancerID)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	return s.insertAnchors(ctx, candidates)
}

// VerifyEntry checks that a CV entry on a freelancer's CV is intact. It
// recomputes the release-time SHA-256 anchor from the artifact's current
// file content and compares it to the stored hash. A slug mismatch — an
// entry that belongs to someone else's CV — is indistinguishable from
// "not found" (ErrNotFound) so the endpoint never confirms an entry's
// existence under a foreign slug.
// Also verifies OpenTimestamps proof if available.
func (s *Service) VerifyEntry(ctx context.Context, slug, entryID string) (*VerifyResult, error) {
	slug = strings.TrimSpace(slug)
	entryID = strings.TrimSpace(entryID)
	if slug == "" || entryID == "" {
		return nil, ErrInvalidInput
	}

	rec, err := s.repo.GetEntryForVerify(ctx, entryID, slug)
	if err != nil {
		return nil, err
	}

	r, err := s.store.Open(ctx, rec.StorageKey)
	if err != nil {
		return nil, err
	}
	h := s.hasher()
	if _, err := io.Copy(h, r); err != nil {
		r.Close()
		return nil, err
	}
	if err := r.Close(); err != nil {
		return nil, err
	}
	recomputed := h.Sum(nil)
	recomputedHex := hex.EncodeToString(recomputed)
	valid := rec.Hash == recomputedHex

	result := &VerifyResult{
		Valid:          valid,
		EntryID:        rec.ID,
		Slug:           slug,
		Hash:           rec.Hash,
		Algorithm:      rec.Algorithm,
		MatchesCurrent: recomputedHex == rec.Hash,
		DealTitle:      rec.DealTitle,
		VerifiedAt:     rec.VerifiedAt,
		OTSProof:       rec.OTSProof,
		OTSConfirmedAt: rec.OTSConfirmedAt,
	}

	// Verify OTS proof if available
	if len(rec.OTSProof) > 0 && rec.OTSConfirmedAt != nil && s.otsClient != nil {
		hashBytes, _ := hex.DecodeString(rec.Hash)
		blockHeight, timestamp, err := ots.VerifyProof(rec.OTSProof, hashBytes)
		if err == nil {
			result.OTSVerified = true
			result.OTSBlockHeight = blockHeight
			result.OTSConfirmedAt = &timestamp
		}
	}

	return result, nil
}

func (s *Service) healAnchors(ctx context.Context, freelancerID string) error {
	candidates, err := s.repo.ListUnanchoredReleasedArtifacts(ctx, freelancerID)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	return s.insertAnchors(ctx, candidates)
}

// insertAnchors hashes each artifact's file content and persists the
// anchors. Idempotent: any anchor that already exists (raced insert) is
// silently skipped. After anchoring, submits hash to OpenTimestamps calendars.
func (s *Service) insertAnchors(ctx context.Context, candidates []AnchorCandidate) error {
	for _, c := range candidates {
		r, err := s.store.Open(ctx, c.StorageKey)
		if err != nil {
			return err
		}
		h := s.hasher()
		if _, err := io.Copy(h, r); err != nil {
			r.Close()
			return err
		}
		if err := r.Close(); err != nil {
			return err
		}
		digest := h.Sum(nil)
		digestHex := hex.EncodeToString(digest)
		if err := s.repo.InsertAnchor(ctx, c.ArtifactID, digestHex); err != nil {
			return err
		}

		// Submit to OpenTimestamps (best-effort, non-blocking)
		if s.otsClient != nil {
			go func(artifactID string, hash []byte) {
				subCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()

				otsProof, err := s.otsClient.Submit(subCtx, hash)
				if err != nil {
					log.Printf("cv: ots submit failed for artifact %s: %v", artifactID, err)
					return
				}
				now := time.Now()
				if err := s.repo.UpdateAnchorOTS(subCtx, artifactID, otsProof, &now, nil); err != nil {
					log.Printf("cv: ots proof store failed for artifact %s: %v", artifactID, err)
				}
			}(c.ArtifactID, digest)
		}
	}
	return nil
}

// refreshTrustScore derives the score from the number of released deals,
// persists it only when it actually changed, and returns the value the CV
// should display.
func (s *Service) refreshTrustScore(ctx context.Context, accountID string, current float64) (float64, error) {
	released, err := s.repo.CountReleasedDeals(ctx, accountID)
	if err != nil {
		return 0, err
	}

	score := math.Min(trustScoreBase+float64(released)*trustScorePerDeal, trustScoreCap)
	if score == current {
		return score, nil
	}
	return score, s.repo.UpdateTrustScore(ctx, accountID, score)
}

// UpgradeOTSProofs checks calendars for upgraded proofs for pending anchors.
// Should be run periodically (e.g., every 6 hours) via a background worker.
func (s *Service) UpgradeOTSProofs(ctx context.Context) error {
	if s.otsClient == nil {
		return nil
	}

	pending, err := s.repo.ListPendingOTSAnchors(ctx)
	if err != nil {
		return err
	}

	for _, a := range pending {
		upgraded, err := s.otsClient.Upgrade(ctx, a.OTSProof)
		if err != nil {
			if errors.Is(err, ots.ErrProofNotReady) {
				continue // not ready yet, will retry next cycle
			}
			log.Printf("cv: ots upgrade failed for entry %s: %v", a.EntryID, err)
			continue
		}

		now := time.Now()
		if err := s.repo.UpdateAnchorOTS(ctx, a.ArtifactID, upgraded, nil, &now); err != nil {
			log.Printf("cv: ots upgraded proof store failed for entry %s: %v", a.EntryID, err)
		}
		log.Printf("cv: ots proof confirmed for entry %s", a.EntryID)
	}

	return nil
}
