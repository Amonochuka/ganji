package cv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// DBTX mirrors the deals package's minimal query interface so the repository
// can run against either *sql.DB or an in-flight *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CVRepository is the data surface the CV service needs. Defined as an
// interface so service logic is testable against a fake.
type CVRepository interface {
	GetProfile(ctx context.Context, slug string) (*profileRow, error)
	ListEntries(ctx context.Context, slug string) ([]Entry, error)
	ListUnanchoredReleasedArtifacts(ctx context.Context, freelancerID string) ([]AnchorCandidate, error)
	ListUnanchoredDealArtifacts(ctx context.Context, dealID, freelancerID string) ([]AnchorCandidate, error)
	InsertAnchor(ctx context.Context, artifactID, hash string) error
	CountReleasedDeals(ctx context.Context, freelancerID string) (int, error)
	UpdateTrustScore(ctx context.Context, accountID string, score float64) error
	GetEntryForVerify(ctx context.Context, entryID, slug string) (*entryRecord, error)
}

type Repository struct {
	db *sql.DB
	q  DBTX
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
		q:  db,
	}
}

func (r *Repository) GetProfile(ctx context.Context, slug string) (*profileRow, error) {
	query := `
		SELECT id, display_name, slug, trust_score
		FROM users
		WHERE slug = $1;
	`
	row := profileRow{}
	err := r.q.QueryRowContext(ctx, query, slug).Scan(
		&row.ID, &row.DisplayName, &row.Slug, &row.TrustScore,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repository: get cv profile: %w", err)
	}
	return &row, nil
}

func (r *Repository) ListEntries(ctx context.Context, slug string) ([]Entry, error) {
	query := `
		SELECT
			cv.id,
			d.title,
			d.amount_sats,
			d.source_platform,
			a.kind,
			cv.hash,
			cv.algorithm,
			d.verified_at,
			cv.created_at
		FROM cv_entries cv
		JOIN artifacts a ON a.id = cv.artifact_id
		JOIN deals d ON d.id = a.deal_id
		JOIN users u ON u.id = d.freelancer_id
		WHERE u.slug = $1 AND d.status = 'released'
		ORDER BY d.verified_at DESC, cv.created_at DESC;
	`
	rows, err := r.q.QueryContext(ctx, query, slug)
	if err != nil {
		return nil, fmt.Errorf("repository: list cv entries: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		verifiedAt := sql.NullTime{}
		if err := rows.Scan(
			&e.ID,
			&e.DealTitle,
			&e.AmountSats,
			&e.SourcePlatform,
			&e.ArtifactKind,
			&e.Hash,
			&e.Algorithm,
			&verifiedAt,
			&e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository: scan cv entry: %w", err)
		}
		if verifiedAt.Valid {
			e.VerifiedAt = verifiedAt.Time
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate cv entries: %w", err)
	}
	return entries, nil
}

// releasedArtifactSelector is the shared FROM/JOIN that restricts an artifact
// set to released deals owned by a given freelancer and lacking a CV anchor.
const releasedArtifactSelector = `
		SELECT a.id, a.storage_key, a.deal_id
		FROM artifacts a
		JOIN deals d ON d.id = a.deal_id
		WHERE d.freelancer_id = $1
		  AND d.status = 'released'
		  AND NOT EXISTS (
		  	SELECT 1 FROM cv_entries cv WHERE cv.artifact_id = a.id
		  )
`

func (r *Repository) ListUnanchoredReleasedArtifacts(ctx context.Context, freelancerID string) ([]AnchorCandidate, error) {
	query := releasedArtifactSelector + ";"
	rows, err := r.q.QueryContext(ctx, query, freelancerID)
	return r.scanCandidates(ctx, rows, err)
}

func (r *Repository) ListUnanchoredDealArtifacts(ctx context.Context, dealID, freelancerID string) ([]AnchorCandidate, error) {
	query := releasedArtifactSelector + " AND d.id = $2;"
	rows, err := r.q.QueryContext(ctx, query, freelancerID, dealID)
	return r.scanCandidates(ctx, rows, err)
}

func (r *Repository) scanCandidates(ctx context.Context, rows *sql.Rows, err error) ([]AnchorCandidate, error) {
	if err != nil {
		return nil, fmt.Errorf("repository: list unanchored artifacts: %w", err)
	}
	defer rows.Close()

	var candidates []AnchorCandidate
	for rows.Next() {
		var c AnchorCandidate
		if err := rows.Scan(&c.ArtifactID, &c.StorageKey, &c.DealID); err != nil {
			return nil, fmt.Errorf("repository: scan anchor candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate anchor candidates: %w", err)
	}
	return candidates, nil
}

// InsertAnchor writes a CV entry for an artifact. The UNIQUE constraint on
// artifact_id makes this idempotent: a duplicate (double release, concurrent
// reads racing on self-heal) is a no-op, never a second, conflicting line.
func (r *Repository) InsertAnchor(ctx context.Context, artifactID, hash string) error {
	query := `
		INSERT INTO cv_entries (artifact_id, hash, algorithm)
		VALUES ($1, $2, 'sha256')
		ON CONFLICT (artifact_id) DO NOTHING;
	`
	if _, err := r.q.ExecContext(ctx, query, artifactID, hash); err != nil {
		return fmt.Errorf("repository: insert cv anchor: %w", err)
	}
	return nil
}

func (r *Repository) CountReleasedDeals(ctx context.Context, freelancerID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM deals
		WHERE freelancer_id = $1 AND status = 'released';
	`
	var n int
	if err := r.q.QueryRowContext(ctx, query, freelancerID).Scan(&n); err != nil {
		return 0, fmt.Errorf("repository: count released deals: %w", err)
	}
	return n, nil
}

func (r *Repository) UpdateTrustScore(ctx context.Context, accountID string, score float64) error {
	query := `UPDATE users SET trust_score = $1 WHERE id = $2;`
	if _, err := r.q.ExecContext(ctx, query, score, accountID); err != nil {
		return fmt.Errorf("repository: update trust score: %w", err)
	}
	return nil
}

func (r *Repository) GetEntryForVerify(ctx context.Context, entryID, slug string) (*entryRecord, error) {
	query := `
		SELECT
			cv.id,
			cv.hash,
			cv.algorithm,
			a.storage_key,
			d.title,
			d.verified_at
		FROM cv_entries cv
		JOIN artifacts a ON a.id = cv.artifact_id
		JOIN deals d ON d.id = a.deal_id
		JOIN users u ON u.id = d.freelancer_id
		WHERE cv.id = $1 AND u.slug = $2;
	`
	rec := entryRecord{}
	verifiedAt := sql.NullTime{}
	err := r.q.QueryRowContext(ctx, query, entryID, slug).Scan(
		&rec.ID,
		&rec.Hash,
		&rec.Algorithm,
		&rec.StorageKey,
		&rec.DealTitle,
		&verifiedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repository: get cv entry for verify: %w", err)
	}
	if verifiedAt.Valid {
		rec.VerifiedAt = verifiedAt.Time
	}
	return &rec, nil
}
