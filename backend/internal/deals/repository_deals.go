package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (r *Repository) CreateDeal(ctx context.Context, deal *Deal) error {
	query := `
		INSERT INTO deals (
			freelancer_id,
			client_email,
			title,
			amount_sats,
			source_platform,
			preimage_hash,
			preimage,
			payee_invoice,
			invoice,
			checking_id,
			share_token,
			status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		RETURNING id, created_at;
	`

	row := r.q.QueryRowContext(
		ctx,
		query,
		deal.FreelancerID,
		deal.ClientEmail,
		deal.Title,
		deal.AmountSats,
		deal.SourcePlatform,
		nullString(deal.PreimageHash),
		nullString(func() string { v, _ := encryptPreimage(r.preimageKey, deal.Preimage); return v }()),
		nullString(deal.PayeeInvoice),
		nullString(deal.Invoice),
		nullString(deal.CheckingID),
		deal.ShareToken,
		deal.Status,
	)

	if err := row.Scan(&deal.ID, &deal.CreatedAt); err != nil {
		return fmt.Errorf("repository: create deal: %w", err)
	}

	return nil
}

// dealColumns is the shared SELECT list for the deals table so the query
// and scan lists stay in sync.
const dealColumns = `
		id,
		freelancer_id,
		client_email,
		title,
		amount_sats,
		source_platform,
		preimage_hash,
		preimage,
		payee_invoice,
		invoice,
		checking_id,
		share_token,
		status,
		dispute_reason,
		disputed_at,
		resolved_at,
		resolved_by,
		created_at,
		verified_at
	`

func (r *Repository) scanDeal(row interface{ Scan(dest ...any) error }) (*Deal, error) {
	var (
		preimageHash sql.NullString
		preimage     sql.NullString
		payeeInvoice sql.NullString
		checkingID   sql.NullString
		resolvedBy   sql.NullString
	)
	deal := &Deal{}
	if err := row.Scan(
		&deal.ID,
		&deal.FreelancerID,
		&deal.ClientEmail,
		&deal.Title,
		&deal.AmountSats,
		&deal.SourcePlatform,
		&preimageHash,
		&preimage,
		&payeeInvoice,
		&deal.Invoice,
		&checkingID,
		&deal.ShareToken,
		&deal.Status,
		&deal.DisputeReason,
		&deal.DisputedAt,
		&deal.ResolvedAt,
		&resolvedBy,
		&deal.CreatedAt,
		&deal.VerifiedAt,
	); err != nil {
		return nil, err
	}
	deal.PreimageHash = preimageHash.String
	decrypted, err := decryptPreimage(r.preimageKey, preimage.String)
	if err != nil {
		return nil, err
	}
	deal.Preimage = decrypted
	deal.PayeeInvoice = payeeInvoice.String
	deal.CheckingID = checkingID.String
	deal.ResolvedBy = resolvedBy.String
	return deal, nil
}

func (r *Repository) GetDealByID(ctx context.Context, id string) (*Deal, error) {
	query := "SELECT" + dealColumns + "FROM deals WHERE id = $1;"
	deal, err := r.scanDeal(r.q.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDealNotFound
		}
		return nil, fmt.Errorf("repository: get deal by id: %w", err)
	}
	return deal, nil
}

func (r *Repository) GetDealByCheckingID(ctx context.Context, checkingID string) (*Deal, error) {
	query := "SELECT" + dealColumns + "FROM deals WHERE checking_id = $1;"
	deal, err := r.scanDeal(r.q.QueryRowContext(ctx, query, checkingID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDealNotFound
		}
		return nil, fmt.Errorf("repository: get deal by checking_id: %w", err)
	}
	return deal, nil
}

// GetDealByShareToken looks a deal up by its public share-link token.
// share_token is a high-entropy, per-deal random value (separate from the
// DB id) so the public payment link is revocable: regenerating it kills the
// old link without ever exposing the internal UUID.
func (r *Repository) GetDealByShareToken(ctx context.Context, shareToken string) (*Deal, error) {
	query := "SELECT" + dealColumns + "FROM deals WHERE share_token = $1;"
	deal, err := r.scanDeal(r.q.QueryRowContext(ctx, query, shareToken))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDealNotFound
		}
		return nil, fmt.Errorf("repository: get deal by share_token: %w", err)
	}
	return deal, nil
}

func (r *Repository) ListByFreelancer(ctx context.Context, freelancerID string) ([]Deal, error) {
	query := "SELECT" + dealColumns + `
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
		deal, err := r.scanDeal(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan deal: %w", err)
		}
		deals = append(deals, *deal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate deals: %w", err)
	}
	return deals, nil
}

// ListForUser returns deals where the user is either the freelancer or the
// client (matched by email). This is what the client needs to see the deals
// they are asked to review and approve.
func (r *Repository) ListForUser(ctx context.Context, userID, email string) ([]Deal, error) {
	query := "SELECT" + dealColumns + `
		FROM deals
		WHERE freelancer_id = $1 OR client_email = $2
		ORDER BY created_at DESC;
	`

	rows, err := r.q.QueryContext(ctx, query, userID, email)
	if err != nil {
		return nil, fmt.Errorf("repository: list deals for user: %w", err)
	}
	defer rows.Close()
	var deals []Deal
	for rows.Next() {
		deal, err := r.scanDeal(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan deal: %w", err)
		}
		deals = append(deals, *deal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate deals: %w", err)
	}
	return deals, nil
}

// ListAllDeals returns all deals for operator triage/dashboard.
func (r *Repository) ListAllDeals(ctx context.Context) ([]Deal, error) {
	query := "SELECT" + dealColumns + `
		FROM deals
		ORDER BY created_at DESC;
	`

	rows, err := r.q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository: list all deals: %w", err)
	}
	defer rows.Close()
	var deals []Deal
	for rows.Next() {
		deal, err := r.scanDeal(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan deal: %w", err)
		}
		deals = append(deals, *deal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate deals: %w", err)
	}
	return deals, nil
}

// UpdatePayeeInvoice swaps the freelancer's payout destination. Allowed for
// any open deal — once released/refunded the money has already moved.
func (r *Repository) UpdatePayeeInvoice(ctx context.Context, dealID, payeeInvoice string) error {
	query := `UPDATE deals SET payee_invoice = $1 WHERE id = $2;`

	result, err := r.q.ExecContext(ctx, query, payeeInvoice, dealID)
	if err != nil {
		return fmt.Errorf("repository: update payee invoice: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: update payee invoice: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}

func (r *Repository) UpdateMilestonePayeeInvoices(ctx context.Context, dealID, payeeInvoice string) error {
	query := `UPDATE milestones SET payee_invoice = $1 WHERE deal_id = $2 AND status::text NOT IN ('released', 'refunded', 'exited');`

	if _, err := r.q.ExecContext(ctx, query, payeeInvoice, dealID); err != nil {
		return fmt.Errorf("repository: update milestone payee invoices: %w", err)
	}
	return nil
}

func (r *Repository) UpdateMilestonePayeeInvoice(ctx context.Context, milestoneID, payeeInvoice string) error {
	query := `UPDATE milestones SET payee_invoice = $1 WHERE id = $2;`

	if _, err := r.q.ExecContext(ctx, query, payeeInvoice, milestoneID); err != nil {
		return fmt.Errorf("repository: update milestone payee invoice: %w", err)
	}
	return nil
}

// UpdateShareToken replaces a deal's public share-link token. Used by the
// freelancer's "regenerate link" endpoint: the old token stops resolving
// immediately, so a leaked link can be revoked. Empty token is rejected by
// the service layer (never store an un-guessable-but-empty token).
func (r *Repository) UpdateShareToken(ctx context.Context, dealID, shareToken string) error {
	query := `UPDATE deals SET share_token = $1 WHERE id = $2;`

	result, err := r.q.ExecContext(ctx, query, shareToken, dealID)
	if err != nil {
		return fmt.Errorf("repository: update share token: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: update share token: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}

// ListOpenBefore returns deals still awaiting payment, locked, work_submitted,
// reviewing, or disputed. The cutoff is retained for interface compatibility;
// per-milestone hold expiries vary, so sweep checks LNbits hold status for all
// open deals and only reconciles UNPAID/EXPIRED/CANCELLED holds.
func (r *Repository) ListOpenBefore(ctx context.Context, _ time.Time) ([]Deal, error) {
	query := "SELECT" + dealColumns + `
		FROM deals
		WHERE status IN ($1, $2, $3, $4, $5)
		ORDER BY created_at ASC;
	`

	rows, err := r.q.QueryContext(ctx, query, StatusAwaitingPayment, StatusLocked, StatusWorkSubmitted, StatusReviewing, StatusDisputed)
	if err != nil {
		return nil, fmt.Errorf("repository: list open deals: %w", err)
	}
	defer rows.Close()

	var deals []Deal
	for rows.Next() {
		deal, err := r.scanDeal(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan deal: %w", err)
		}
		deals = append(deals, *deal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate deals: %w", err)
	}
	return deals, nil
}

func (r *Repository) UpdateStatus(ctx context.Context, dealID string, status Status) error {
	query := `
		UPDATE deals
		SET status = $1,
			verified_at = CASE WHEN $1 = 'released' THEN NOW() ELSE verified_at END
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

func (r *Repository) UpdateStatusIfCurrent(ctx context.Context, dealID string, expected, status Status) (bool, error) {
	query := `
		UPDATE deals
		SET status = $1,
			verified_at = CASE WHEN $1 = 'released' THEN NOW() ELSE verified_at END
		WHERE id = $2 AND status = $3;
	`
	result, err := r.q.ExecContext(ctx, query, status, dealID, expected)
	if err != nil {
		return false, fmt.Errorf("repository: conditionally update deal status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("repository: conditionally update deal status: %w", err)
	}
	return rowsAffected == 1, nil
}

// UpdateDisputeIfCurrent raises a dispute only if the deal row is still in
// expected (the status the caller read before any network legs). Without the
// guard, a dispute racing an approve that just released the deal could flip a
// released deal back to disputed after the money moved.
func (r *Repository) UpdateDisputeIfCurrent(ctx context.Context, dealID string, expected Status, reason string) (bool, error) {
	query := `
		UPDATE deals
		SET status = 'disputed',
			dispute_reason = $1,
			disputed_at = NOW()
		WHERE id = $2 AND status = $3;
	`
	result, err := r.q.ExecContext(ctx, query, reason, dealID, expected)
	if err != nil {
		return false, fmt.Errorf("repository: conditionally raise dispute: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("repository: conditionally raise dispute: %w", err)
	}
	return rowsAffected == 1, nil
}

// ListDisputed returns every deal frozen in the disputed state, oldest
// dispute first — the arbitration queue an operator works through.
func (r *Repository) ListDisputed(ctx context.Context) ([]Deal, error) {
	query := "SELECT" + dealColumns + `
		FROM deals
		WHERE status = $1
		ORDER BY disputed_at ASC;
	`

	rows, err := r.q.QueryContext(ctx, query, StatusDisputed)
	if err != nil {
		return nil, fmt.Errorf("repository: list disputed deals: %w", err)
	}
	defer rows.Close()

	var deals []Deal
	for rows.Next() {
		deal, err := r.scanDeal(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan deal: %w", err)
		}
		deals = append(deals, *deal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate deals: %w", err)
	}
	return deals, nil
}

// UpdateDisputeResolution closes out a disputed deal after the operator has
// run the network leg: it moves the status to released or refunded and
// records who decided and when. A release also stamps verified_at, matching
// UpdateStatus, so the Live CV anchors the accepted work.
func (r *Repository) UpdateDisputeResolution(ctx context.Context, dealID string, status Status, resolvedBy string) error {
	query := `
		UPDATE deals
		SET status = $1,
			resolved_by = $2,
			resolved_at = NOW(),
			verified_at = CASE WHEN $1 = 'released' THEN NOW() ELSE verified_at END
		WHERE id = $3;
	`
	result, err := r.q.ExecContext(ctx, query, status, resolvedBy, dealID)
	if err != nil {
		return fmt.Errorf("repository: update dispute resolution: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: update dispute resolution: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}

// GetDealForUpdate locks a deal row for update (SELECT FOR UPDATE) so
// concurrent ResolveDispute calls serialize on the same deal.
func (r *Repository) GetDealForUpdate(ctx context.Context, dealID string) (*Deal, error) {
	query := "SELECT" + dealColumns + "FROM deals WHERE id = $1 FOR UPDATE;"
	deal, err := r.scanDeal(r.q.QueryRowContext(ctx, query, dealID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDealNotFound
		}
		return nil, fmt.Errorf("repository: get deal for update: %w", err)
	}
	return deal, nil
}
