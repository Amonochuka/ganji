package cv

import "time"

// Entry is one hash-anchored work line on a freelancer's public CV. It
// represents an artifact from a released deal: the hash binds the line to
// the artifact's storage reference at release time, so a claim on the CV can
// be cryptographically re-verified (see VerifyResult).
type Entry struct {
	ID             string    `json:"id"`
	DealTitle      string    `json:"deal_title"`
	AmountSats     int64     `json:"amount_sats"`
	SourcePlatform string    `json:"source_platform"`
	ArtifactKind   string    `json:"artifact_kind"`
	Hash           string    `json:"hash"`
	Algorithm      string    `json:"algorithm"`
	VerifiedAt     time.Time `json:"verified_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// Profile is the public Live CV of a freelancer: identity info plus all
// anchored entries, newest verified first. No auth required.
type Profile struct {
	DisplayName string  `json:"display_name"`
	Slug        string  `json:"slug"`
	TrustScore  float64 `json:"trust_score"`
	Entries     []Entry `json:"entries"`
}

// VerifyResult is the output of GET /cv/:slug/verify/:entryID. Valid reports
// whether the stored anchor still matches the artifact's current storage
// reference; if the release-time hash is ever recomputed to a different
// value the entry has been tampered with or its backing artifact changed.
type VerifyResult struct {
	Valid          bool      `json:"valid"`
	EntryID        string    `json:"entry_id"`
	Slug           string    `json:"slug"`
	Hash           string    `json:"hash"`
	Algorithm      string    `json:"algorithm"`
	MatchesCurrent bool      `json:"matches_current"`
	DealTitle      string    `json:"deal_title"`
	VerifiedAt     time.Time `json:"verified_at"`
}

// profileRow is the users row behind a CV. ID is the hidden DB handle used
// for self-healing anchoring and trust-score refreshes and is never exposed.
type profileRow struct {
	ID          string
	DisplayName string
	Slug        string
	TrustScore  float64
}

// AnchorCandidate is an artifact from a released deal that has no CV entry
// yet — its hash anchor still needs to be written.
type AnchorCandidate struct {
	ArtifactID string
	StorageKey string
	DealID     string
}

// entryRecord carries everything VerifyEntry needs to recompute the
// release-time hash from the artifact's current storage reference.
type entryRecord struct {
	ID         string
	Hash       string
	Algorithm  string
	StorageKey string
	DealTitle  string
	VerifiedAt time.Time
}
