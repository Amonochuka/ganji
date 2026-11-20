package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (r *Repository) CreateArtifact(ctx context.Context, artifact *Artifact) error {
	query := `INSERT INTO artifacts (deal_id, milestone_id, kind, storage_key) VALUES ($1, $2, $3, $4)
		RETURNING id, milestone_id, uploaded_at;`
	args := []interface{}{artifact.DealID, artifact.MilestoneID, artifact.Kind, artifact.StorageKey}
	err := r.q.QueryRowContext(ctx, query, args...).Scan(&artifact.ID, &artifact.MilestoneID, &artifact.UploadedAt)
	if err != nil {
		return fmt.Errorf("repository: create artifact: %w", err)
	}
	return nil
}

func (r *Repository) DeleteArtifact(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM artifacts WHERE id = $1;`, id)
	if err != nil {
		return fmt.Errorf("repository: delete artifact: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: delete artifact: %w", err)
	}
	if n == 0 {
		return ErrArtifactNotFound
	}
	return nil
}

func (r *Repository) GetArtifactByID(ctx context.Context, id string) (*Artifact, error) {
	query := `SELECT id, deal_id, milestone_id, kind, storage_key, uploaded_at FROM artifacts WHERE id = $1;`
	artifact := Artifact{}
	err := r.q.QueryRowContext(ctx, query, id).Scan(&artifact.ID, &artifact.DealID, &artifact.MilestoneID, &artifact.Kind, &artifact.StorageKey, &artifact.UploadedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("repository: get artifact by id: %w", err)
	}
	return &artifact, nil
}

func (r *Repository) ListArtifactsByDeal(ctx context.Context, dealID string, milestoneID *string) ([]Artifact, error) {
	query := `SELECT id, deal_id, milestone_id, kind, storage_key, uploaded_at FROM artifacts WHERE deal_id = $1`
	args := []interface{}{dealID}
	if milestoneID != nil {
		query += ` AND milestone_id = $2`
		args = append(args, milestoneID)
	}
	query += ` ORDER BY uploaded_at ASC;`
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list artifacts: %w", err)
	}
	defer rows.Close()
	artifacts := make([]Artifact, 0)
	for rows.Next() {
		artifact := Artifact{}
		err := rows.Scan(&artifact.ID, &artifact.DealID, &artifact.MilestoneID, &artifact.Kind, &artifact.StorageKey, &artifact.UploadedAt)
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
