package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
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
			client_email,
			title,
			amount_sats,
			source_platform,
			preimage_hash,
			preimage,
			payee_invoice,
			invoice,
			checking_id,
			payout_checking_id,
			share_token,
			status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
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
		deal.PreimageHash,
		nullString(deal.Preimage),
		nullString(deal.PayeeInvoice),
		deal.Invoice,
		deal.CheckingID,
		nullString(deal.PayoutCheckingID),
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
		payout_checking_id,
		payout_attempted_at,
		share_token,
		status,
		dispute_reason,
		disputed_at,
		resolved_at,
		resolved_by,
		created_at,
		verified_at
	`

func scanDeal(row interface{ Scan(dest ...any) error }) (*Deal, error) {
	var (
		preimage     sql.NullString
		payeeInvoice sql.NullString
		checkingID   sql.NullString
		payoutID     sql.NullString
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
		&deal.PreimageHash,
		&preimage,
		&payeeInvoice,
		&deal.Invoice,
		&checkingID,
		&payoutID,
		&deal.PayoutAttemptedAt,
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
	deal.Preimage = preimage.String
	deal.PayeeInvoice = payeeInvoice.String
	deal.CheckingID = checkingID.String
	deal.PayoutCheckingID = payoutID.String
	deal.ResolvedBy = resolvedBy.String
	return deal, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *Repository) GetDealByID(ctx context.Context, id string) (*Deal, error) {
	query := "SELECT" + dealColumns + "FROM deals WHERE id = $1;"
	deal, err := scanDeal(r.q.QueryRowContext(ctx, query, id))
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
	deal, err := scanDeal(r.q.QueryRowContext(ctx, query, checkingID))
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
	deal, err := scanDeal(r.q.QueryRowContext(ctx, query, shareToken))
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
		deal, err := scanDeal(rows)
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
		deal, err := scanDeal(rows)
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

// UpdatePayoutCheckingID records the LNbits checking_id for the outgoing
// payout to the freelancer. Used for idempotency: on retry we can check if
// the payout was already sent instead of sending again.
func (r *Repository) UpdatePayoutCheckingID(ctx context.Context, dealID, payoutCheckingID string) error {
	query := `UPDATE deals SET payout_checking_id = $1 WHERE id = $2;`

	result, err := r.q.ExecContext(ctx, query, payoutCheckingID, dealID)
	if err != nil {
		return fmt.Errorf("repository: update payout checking_id: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: update payout checking_id: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}

// MarkPayoutAttempted durably stamps that a payout attempt is starting. It is
// committed (in its own transaction) BEFORE any money moves, so a release that
// crashes in the middle of the network legs leaves a visible marker and any
// retry refuses to auto-resend (double-pay prevention).
func (r *Repository) MarkPayoutAttempted(ctx context.Context, dealID string) error {
	query := `UPDATE deals SET payout_attempted_at = NOW() WHERE id = $1;`

	result, err := r.q.ExecContext(ctx, query, dealID)
	if err != nil {
		return fmt.Errorf("repository: mark payout attempted: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: mark payout attempted: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}

// ClearPayoutTracking resets both payout tracking fields. Called only when a
// payout is PROVABLY not in flight — a definitive LNbits refusal before the
// money moved, or a confirmed-failed prior payout. Never call it on an
// ambiguous outcome: that would re-open the double-pay window.
func (r *Repository) ClearPayoutTracking(ctx context.Context, dealID string) error {
	query := `UPDATE deals SET payout_checking_id = NULL, payout_attempted_at = NULL WHERE id = $1;`

	result, err := r.q.ExecContext(ctx, query, dealID)
	if err != nil {
		return fmt.Errorf("repository: clear payout tracking: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: clear payout tracking: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDealNotFound
	}

	return nil
}

// ListOpenBefore returns deals still awaiting payment, locked, work_submitted,
// reviewing, or disputed that were created before the cutoff — the candidates
// for the hold-expiry sweep. Disputed, work_submitted, and reviewing deals are
// included because if the hold expires while in these states, the network
// returns funds to the client but the DB stays in the old state forever.
func (r *Repository) ListOpenBefore(ctx context.Context, cutoff time.Time) ([]Deal, error) {
	query := "SELECT" + dealColumns + `
		FROM deals
		WHERE status IN ($1, $2, $3, $4, $5) AND created_at < $6
		ORDER BY created_at ASC;
	`

	rows, err := r.q.QueryContext(ctx, query, StatusAwaitingPayment, StatusLocked, StatusWorkSubmitted, StatusReviewing, StatusDisputed, cutoff)
	if err != nil {
		return nil, fmt.Errorf("repository: list open deals before cutoff: %w", err)
	}
	defer rows.Close()

	var deals []Deal
	for rows.Next() {
		deal, err := scanDeal(rows)
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
		deal, err := scanDeal(rows)
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
	deal, err := scanDeal(r.q.QueryRowContext(ctx, query, dealID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDealNotFound
		}
		return nil, fmt.Errorf("repository: get deal for update: %w", err)
	}
	return deal, nil
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

func (r *Repository) WithTx(tx *sql.Tx) DealRepository {
	return &Repository{
		db: r.db,
		q:  tx,
	}
}

func (r *Repository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}
