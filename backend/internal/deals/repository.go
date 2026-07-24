package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
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

func (r *Repository) CreateDeal(ctx context.Context, deal *Deal) error {
	query := `
		INSERT INTO deals (
			freelancer_id,
			title,
			amount_sats,
			source_platform,
			preimage_hash,
			invoice,
			status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
		RETURNING id, created_at;
	`

	row := r.q.QueryRowContext(
		ctx,
		query,
		deal.FreelancerID,
		deal.Title,
		deal.AmountSats,
		deal.SourcePlatform,
		deal.PreimageHash,
		deal.Invoice,
		deal.Status,
	)

	if err := row.Scan(&deal.ID, &deal.CreatedAt); err != nil {
		return fmt.Errorf("repository: create deal: %w", err)
	}

	return nil
}

func (r *Repository) GetDealByID(ctx context.Context, id string) (*Deal, error) {
	query := `
		SELECT
			id,
			freelancer_id,
			title,
			amount_sats,
			source_platform,
			preimage_hash,
			invoice,
			checking_id,
			status,
			created_at,
			verified_at
		FROM deals
		WHERE id = $1;
	`
	deal := &Deal{}
	row := r.q.QueryRowContext(ctx, query, id)
	if err := row.Scan(
		&deal.ID,
		&deal.FreelancerID,
		&deal.Title,
		&deal.AmountSats,
		&deal.SourcePlatform,
		&deal.PreimageHash,
		&deal.Invoice,
		&deal.CheckingID,
		&deal.Status,
		&deal.CreatedAt,
		&deal.VerifiedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDealNotFound
		}
		return nil, fmt.Errorf("repository: get deal by id: %w", err)
	}
	return deal, nil
}

func (r *Repository) ListByFreelancer(ctx context.Context, freelancerID string) ([]Deal, error) {
	query := `
		SELECT
			id,
			freelancer_id,
			title,
			amount_sats,
			source_platform,
			preimage_hash,
			invoice,
			checking_id,
			status,
			created_at,
			verified_at
		FROM deals
		WHERE freelancer_id = $1
		ORDER BY created_at DESC;
	`

	rows, err := r.q.QueryContext(ctx, query, freelancerID)
	if err != nil {
		return nil, fmt.Errorf("repository: list deals by freelancer: %w", err)
	}
	defer rows.Close()
	var deals []Deal
	for rows.Next() {
		var deal Deal
		if err := rows.Scan(
			&deal.ID,
			&deal.FreelancerID,
			&deal.Title,
			&deal.AmountSats,
			&deal.SourcePlatform,
			&deal.PreimageHash,
			&deal.Invoice,
			&deal.CheckingID,
			&deal.Status,
			&deal.CreatedAt,
			&deal.VerifiedAt,
		); err != nil {
			return nil, fmt.Errorf("repository: scan deal: %w", err)
		}
		deals = append(deals, deal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate deals: %w", err)
	}
	return deals, nil
}

func (r *Repository) UpdateCheckingID(ctx context.Context, dealID string, checkingID string) error {
	query := `
		UPDATE deals
		SET checking_id = $1
		WHERE id = $2;
	`
	result, err := r.q.ExecContext(ctx, query, checkingID, dealID)
	if err != nil {
		return fmt.Errorf("repository: update checking id: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: checking affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}
	return nil
}

func (r *Repository) UpdateStatus(ctx context.Context, dealID string, status Status) error {
	query := `
		UPDATE deals
		SET status = $1
		WHERE id = $2;
	`
	result, err := r.q.ExecContext(ctx, query, status, dealID)
	if err != nil {
		return fmt.Errorf("repository: update deal status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: update deal status: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}

func (r *Repository) CreateArtifact(ctx context.Context, artifact *Artifact) error {
	query := `INSERT INTO artifacts (deal_id, kind, storage_key) VALUES ($1, $2, $3)
		RETURNING id, uploaded_at;`
	err := r.q.QueryRowContext(ctx, query, artifact.DealID, artifact.Kind, artifact.StorageKey).Scan(&artifact.ID, &artifact.UploadedAt)
	if err != nil {
		return fmt.Errorf("repository: create artifact: %w", err)
	}
	return nil
}

func (r *Repository) GetArtifactByID(ctx context.Context, id string) (*Artifact, error) {
	query := `SELECT id, deal_id, kind, storage_key, uploaded_at FROM artifacts WHERE id = $1;`
	artifact := Artifact{}
	err := r.q.QueryRowContext(ctx, query, id).Scan(&artifact.ID, &artifact.DealID, &artifact.Kind, &artifact.StorageKey, &artifact.UploadedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("repository: get artifact by id: %w", err)
	}
	return &artifact, nil
}

func (r *Repository) ListArtifactsByDeal(ctx context.Context, dealID string) ([]Artifact, error) {
	query := `SELECT id, deal_id, kind, storage_key, uploaded_at FROM artifacts WHERE deal_id = $1
			ORDER BY uploaded_at ASC;`
	rows, err := r.q.QueryContext(ctx, query, dealID)
	if err != nil {
		return nil, fmt.Errorf("repository: list artifacts: %w", err)
	}
	defer rows.Close()
	var artifacts []Artifact
	for rows.Next() {
		artifact := Artifact{}

		err := rows.Scan(&artifact.ID, &artifact.DealID, &artifact.Kind, &artifact.StorageKey, &artifact.UploadedAt)
		if err != nil {
			return nil, fmt.Errorf("repository: scan artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate artifacts: %w", err)
	}
	return artifacts, nil
}

func (r *Repository) CreateVerification(ctx context.Context, verification *Verification) error {
	query := `
		INSERT INTO verifications (artifact_id, method, reference, status, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at;
	`
	err := r.q.QueryRowContext(ctx, query,
		verification.ArtifactID,
		verification.Method,
		verification.Reference,
		verification.Status,
		verification.ExpiresAt,
	).Scan(&verification.ID, &verification.CreatedAt)
	if err != nil {
		return fmt.Errorf("repository: create verification: %w", err)
	}
	return nil
}

func (r *Repository) GetVerificationByID(ctx context.Context, id string) (*Verification, error) {
	query := `SELECT id, artifact_id, method, reference, status, expires_at, created_at
			  FROM verifications
			  WHERE id = $1;`
	verification := Verification{}
	err := r.q.QueryRowContext(ctx, query, id).Scan(
		&verification.ID,
		&verification.ArtifactID,
		&verification.Method,
		&verification.Reference,
		&verification.Status,
		&verification.ExpiresAt,
		&verification.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrVerificationNotFound
		}
		return nil, fmt.Errorf("repository: get verification by id: %w", err)
	}
	return &verification, nil
}

func (r *Repository) ListVerificationsByArtifact(ctx context.Context, artifactID string) ([]Verification, error) {
	query := `SELECT id, artifact_id, method, reference, status, expires_at, created_at
			  FROM verifications
			  WHERE artifact_id = $1
			  ORDER BY created_at ASC;`
	rows, err := r.q.QueryContext(ctx, query, artifactID)
	if err != nil {
		return nil, fmt.Errorf("repository: list verifications: %w", err)
	}
	defer rows.Close()
	var verifications []Verification
	for rows.Next() {
		verification := Verification{}
		err := rows.Scan(
			&verification.ID,
			&verification.ArtifactID,
			&verification.Method,
			&verification.Reference,
			&verification.Status,
			&verification.ExpiresAt,
			&verification.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("repository: scan verification: %w", err)
		}

		verifications = append(verifications, verification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate verifications: %w", err)
	}
	return verifications, nil
}

func (r *Repository) WithTx(tx *sql.Tx) *Repository {
	return &Repository{
		db: r.db,
		q:  tx,
	}
}

func (r *Repository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}