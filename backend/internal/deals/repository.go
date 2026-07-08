package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrDealNotFound is returned when a lookup or update targets a deal that
// doesn't exist. Handlers translate this into a 404.
var ErrDealNotFound = errors.New("deal not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// CreateDeal inserts a new deal without a checking_id — that gets filled
// in afterward via UpdateCheckingID once the Lightning invoice has been
// generated. This keeps deal creation durable even if invoice generation
// fails or is delayed.
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

	row := r.db.QueryRowContext(
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

func (r *Repository) GetByID(ctx context.Context, id string) (*Deal, error) {
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

	row := r.db.QueryRowContext(ctx, query, id)

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

	rows, err := r.db.QueryContext(ctx, query, freelancerID)
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
	result, err := r.db.ExecContext(ctx, query, checkingID, dealID)
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

	result, err := r.db.ExecContext(ctx, query, status, dealID)
	if err != nil {
		return fmt.Errorf("repository: update deal status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: get affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}