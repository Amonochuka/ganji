-- deals (complete schema with all escrow columns).
-- Folded in from the later client_email / escrow ALTER migrations because this
-- is a fresh schema for a new environment: client_email identifies the client,
-- preimage settles the hold invoice later, and payee_invoice is the payout leg.
CREATE TABLE deals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    freelancer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_email TEXT NOT NULL,
    title TEXT NOT NULL,
    amount_sats BIGINT NOT NULL,
    source_platform TEXT NOT NULL,
    preimage_hash TEXT NOT NULL UNIQUE,
    preimage TEXT,
    payee_invoice TEXT,
    invoice TEXT NOT NULL,
    checking_id TEXT,
    -- Payout checking_id tracks the outgoing payment to the freelancer.
    -- Filled after PayInvoice succeeds so a retry can verify the payout
    -- instead of sending again (double-pay prevention).
    payout_checking_id TEXT,
    -- Shareable payment-link token: a separate high-entropy random value from
    -- the deal's UUID so the public link can be revoked/rotated without
    -- exposing the internal id. NOT NULL DEFAULT backfills existing deals;
    -- UNIQUE doubles as the lookup index for GET /public/deals/:shareToken.
    share_token TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    status TEXT NOT NULL DEFAULT 'awaiting_payment',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    verified_at TIMESTAMPTZ,
    -- Dispute/arbitration columns
    dispute_reason TEXT NOT NULL DEFAULT '',
    disputed_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT,

    CONSTRAINT valid_status CHECK (
        status IN (
            'awaiting_payment',
            'locked',
            'work_submitted',
            'reviewing',
            'released',
            'disputed',
            'refunded'
        )
    )
);

CREATE INDEX idx_deals_freelancer_id ON deals(freelancer_id);
CREATE INDEX idx_deals_preimage_hash ON deals(preimage_hash);
CREATE INDEX idx_deals_status ON deals(status);
CREATE INDEX idx_deals_client_email ON deals(client_email);
CREATE INDEX idx_deals_disputed_at ON deals(disputed_at);