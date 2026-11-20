package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const milestoneColumns = `
		id, deal_id, title, amount_sats, "order", status, completed_at, created_at,
		preimage_hash, preimage, payee_invoice, invoice, checking_id, payout_checking_id, payout_attempted_at
	`

func (r *Repository) scanMilestone(row interface{ Scan(dest ...any) error }) (*Milestone, error) {
	var (
		preimageHash sql.NullString
		preimage     sql.NullString
		payeeInvoice sql.NullString
		invoice      sql.NullString
		checkingID   sql.NullString
		payoutID     sql.NullString
	)
	m := &Milestone{}
	if err := row.Scan(
		&m.ID, &m.DealID, &m.Title, &m.AmountSats, &m.Order, &m.Status, &m.CompletedAt, &m.CreatedAt,
		&preimageHash, &preimage, &payeeInvoice, &invoice, &checkingID, &payoutID, &m.PayoutAttemptedAt,
	); err != nil {
		return nil, err
	}
	decrypted, err := decryptPreimage(r.preimageKey, preimage.String)
	if err != nil {
		return nil, err
	}
	m.Preimage = decrypted
	m.PreimageHash = preimageHash.String
	m.PayeeInvoice = payeeInvoice.String
	m.Invoice = invoice.String
	m.CheckingID = checkingID.String
	m.PayoutCheckingID = payoutID.String
	return m, nil
}

func (r *Repository) GetMilestoneByID(ctx context.Context, milestoneID string) (*Milestone, error) {
	query := "SELECT" + milestoneColumns + "FROM milestones WHERE id = $1;"
	m, err := r.scanMilestone(r.q.QueryRowContext(ctx, query, milestoneID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMilestoneNotFound
		}
		return nil, fmt.Errorf("repository: get milestone by id: %w", err)
	}
	return m, nil
}

func (r *Repository) GetMilestoneForUpdate(ctx context.Context, milestoneID string) (*Milestone, error) {
	query := "SELECT" + milestoneColumns + "FROM milestones WHERE id = $1 FOR UPDATE;"
	m, err := r.scanMilestone(r.q.QueryRowContext(ctx, query, milestoneID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMilestoneNotFound
		}
		return nil, fmt.Errorf("repository: get milestone for update: %w", err)
	}
	return m, nil
}

func (r *Repository) GetMilestonesByDeal(ctx context.Context, dealID string) ([]Milestone, error) {
	query := "SELECT" + milestoneColumns + `FROM milestones WHERE deal_id = $1 ORDER BY "order" ASC;`
	rows, err := r.q.QueryContext(ctx, query, dealID)
	if err != nil {
		return nil, fmt.Errorf("repository: list milestones by deal: %w", err)
	}
	defer rows.Close()
	var milestones []Milestone
	for rows.Next() {
		m, err := r.scanMilestone(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan milestone: %w", err)
		}
		milestones = append(milestones, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate milestones: %w", err)
	}
	return milestones, nil
}

func (r *Repository) UpdateMilestoneStatus(ctx context.Context, milestoneID string, status MilestoneStatus, completedAt *time.Time) error {
	var query string
	var args []any
	if completedAt != nil {
		query = `
			UPDATE milestones
			SET status = $1, completed_at = $2
			WHERE id = $3;`
		args = []any{status, completedAt, milestoneID}
	} else {
		query = `
			UPDATE milestones
			SET status = $1
			WHERE id = $3;`
		args = []any{status, milestoneID}
	}
	result, err := r.q.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("repository: update milestone status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: update milestone status rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}
	return nil
}

func (r *Repository) DeleteMilestone(ctx context.Context, milestoneID string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM milestones WHERE id = $1;`, milestoneID)
	if err != nil {
		return fmt.Errorf("repository: delete milestone: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: delete milestone rows affected: %w", err)
	}
	if n == 0 {
		return ErrDealNotFound
	}
	return nil
}

// CreateMilestone inserts a new milestone for a deal.
func (r *Repository) CreateMilestone(ctx context.Context, milestone *Milestone) error {
	query := `
		INSERT INTO milestones (deal_id, title, amount_sats, "order", status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at;`
	err := r.q.QueryRowContext(ctx, query,
		milestone.DealID,
		milestone.Title,
		milestone.AmountSats,
		milestone.Order,
		milestone.Status,
	).Scan(&milestone.ID, &milestone.CreatedAt)
	if err != nil {
		return fmt.Errorf("repository: create milestone: %w", err)
	}
	return nil
}

func (r *Repository) UpdateMilestoneEscrow(ctx context.Context, m *Milestone) error {
	enc, err := encryptPreimage(r.preimageKey, m.Preimage)
	if err != nil {
		return fmt.Errorf("repository: encrypt milestone preimage: %w", err)
	}
	_, err = r.q.ExecContext(ctx, `UPDATE milestones SET preimage_hash=$1, preimage=$2, payee_invoice=$3, invoice=$4, checking_id=$5, status=$6 WHERE id=$7;`,
		nullString(m.PreimageHash), nullString(enc), nullString(m.PayeeInvoice), nullString(m.Invoice), nullString(m.CheckingID), m.Status, m.ID)
	if err != nil {
		return fmt.Errorf("repository: update milestone escrow: %w", err)
	}
	return nil
}

func (r *Repository) UpdateMilestonePayout(ctx context.Context, milestoneID, payoutCheckingID string) error {
	_, err := r.q.ExecContext(ctx, `UPDATE milestones SET payout_checking_id=$1, payout_attempted_at=COALESCE(payout_attempted_at,NOW()) WHERE id=$2;`, payoutCheckingID, milestoneID)
	if err != nil {
		return fmt.Errorf("repository: update milestone payout: %w", err)
	}
	return nil
}

func (r *Repository) MarkMilestonePayoutAttempted(ctx context.Context, milestoneID string) error {
	_, err := r.q.ExecContext(ctx, `UPDATE milestones SET payout_attempted_at=NOW() WHERE id=$1;`, milestoneID)
	return err
}

func (r *Repository) ClearMilestonePayoutTracking(ctx context.Context, milestoneID string) error {
	_, err := r.q.ExecContext(ctx, `UPDATE milestones SET payout_checking_id=NULL, payout_attempted_at=NULL WHERE id=$1;`, milestoneID)
	return err
}
