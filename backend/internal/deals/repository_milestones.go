package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const milestoneColumns = `
		id, deal_id, title, amount_sats, "order", status, completed_at, created_at, delivery_days, work_start_at, work_due_at, funded_at,
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
		&m.ID, &m.DealID, &m.Title, &m.AmountSats, &m.Order, &m.Status, &m.CompletedAt, &m.CreatedAt, &m.DeliveryDays, &m.WorkStartAt, &m.WorkDueAt, &m.FundedAt,
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
	query := `
		UPDATE milestones
		SET status = $1, completed_at = COALESCE($2, completed_at)
		WHERE id = $3;`
	result, err := r.q.ExecContext(ctx, query, status, completedAt, milestoneID)
	if err != nil {
		return fmt.Errorf("repository: update milestone status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: update milestone status rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrMilestoneNotFound
	}
	return nil
}

func (r *Repository) UpdateMilestoneStatusIfCurrent(ctx context.Context, milestoneID string, expected, status MilestoneStatus) (bool, error) {
	result, err := r.q.ExecContext(ctx, `
		UPDATE milestones
		SET status = $1,
		    completed_at = CASE WHEN $1 IN ('released','refunded','exited') THEN COALESCE(completed_at, NOW()) ELSE completed_at END
		WHERE id = $2 AND status = $3;`, status, milestoneID, expected)
	if err != nil {
		return false, fmt.Errorf("repository: update milestone status if current: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("repository: update milestone status if current rows affected: %w", err)
	}
	return rowsAffected > 0, nil
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
		return ErrMilestoneNotFound
	}
	return nil
}

// UpdateDealStatusFromMilestones recomputes and persists deal status from
// milestone states in milestone-only model.
func (r *Repository) UpdateDealStatusFromMilestones(ctx context.Context, dealID string) (Status, error) {
	ms, err := r.GetMilestonesByDeal(ctx, dealID)
	if err != nil {
		return "", err
	}
	if len(ms) == 0 {
		return "", fmt.Errorf("%w: deal has no milestones", ErrInvalidTransition)
	}
	allReleased := true
	allRefunded := true
	anyDisputed := false
	anySubmitted := false
	anyFunded := false
	for i := range ms {
		s := ms[i].Status
		if s != MilestoneReleased {
			allReleased = false
		}
		if s != MilestoneRefunded && s != MilestoneExited {
			allRefunded = false
		}
		if s == MilestoneDisputed || s == MilestoneRejected {
			anyDisputed = true
		}
		if s == MilestoneWorkSubmitted || s == MilestoneCompleted {
			anySubmitted = true
		}
		if s == MilestoneAwaitingPayment || s == MilestonePending {
			anyFunded = false
		}
		if s == MilestoneFunded || s == MilestoneInProgress || s == MilestoneWorkSubmitted || s == MilestoneCompleted || s == MilestoneDisputed || s == MilestoneRejected {
			anyFunded = true
		}
	}
	var next Status
	switch {
	case allReleased:
		next = StatusReleased
	case allRefunded:
		next = StatusRefunded
	case anyDisputed:
		next = StatusDisputed
	case anySubmitted:
		next = StatusWorkSubmitted
	case anyFunded:
		next = StatusLocked
	default:
		next = StatusAwaitingPayment
	}
	if err := r.UpdateStatus(ctx, dealID, next); err != nil {
		return "", err
	}
	return next, nil
}

// CreateMilestone inserts a new milestone for a deal.
func (r *Repository) CreateMilestone(ctx context.Context, milestone *Milestone) error {
	query := `
		INSERT INTO milestones (deal_id, title, amount_sats, "order", status, delivery_days, work_start_at, work_due_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at;`
	err := r.q.QueryRowContext(ctx, query,
		milestone.DealID,
		milestone.Title,
		milestone.AmountSats,
		milestone.Order,
		milestone.Status,
		milestone.DeliveryDays,
		milestone.WorkStartAt,
		milestone.WorkDueAt,
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
	_, err = r.q.ExecContext(ctx, `UPDATE milestones SET preimage_hash=$1, preimage=$2, payee_invoice=$3, invoice=$4, checking_id=$5, status=$6::text::milestone_status, funded_at=CASE WHEN $6::text IN ('funded','in_progress') THEN COALESCE(funded_at,NOW()) ELSE funded_at END WHERE id=$7;`,
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
