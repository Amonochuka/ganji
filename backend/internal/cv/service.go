package cv

import (
	"context"
	"log"
	"math"
	"strings"

	"github.com/Amonochuka/ganji-backend/pkg/hash"
)

// Trust-score derivation. Every freelancer starts at the signup default
// (100); each released (verified) deal adds a fixed increment up to a cap.
// The number is recalculated whenever the public CV is read.
const (
	trustScoreBase    = 100.0
	trustScorePerDeal = 25.0
	trustScoreCap     = 1000.0
)

type Service struct {
	repo CVRepository
}

func NewService(repo CVRepository) *Service {
	return &Service{repo: repo}
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
		return nil, err
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
// storage reference and compares it to the stored hash. A slug mismatch — an
// entry that belongs to someone else's CV — is indistinguishable from
// "not found" (ErrNotFound) so the endpoint never confirms an entry's
// existence under a foreign slug.
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

	recomputed := hash.SumSHA256([]byte(rec.StorageKey))
	valid := rec.Hash == recomputed

	return &VerifyResult{
		Valid:          valid,
		EntryID:        rec.ID,
		Slug:           slug,
		Hash:           rec.Hash,
		Algorithm:      rec.Algorithm,
		MatchesCurrent: recomputed == rec.Hash,
		DealTitle:      rec.DealTitle,
		VerifiedAt:     rec.VerifiedAt,
	}, nil
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

// insertAnchors hashes each artifact's storage reference and persists the
// anchors. Idempotent: any anchor that already exists (raced insert) is
// silently skipped.
func (s *Service) insertAnchors(ctx context.Context, candidates []AnchorCandidate) error {
	for _, c := range candidates {
		digest := hash.SumSHA256([]byte(c.StorageKey))
		if err := s.repo.InsertAnchor(ctx, c.ArtifactID, digest); err != nil {
			return err
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
