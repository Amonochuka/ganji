-- deals (complete schema with all escrow columns).
-- Fresh schema for milestone-only escrow model: milestone holds live on milestones;
-- deal-level hold fields are nullable/legacy-compatible.
CREATE TABLE deals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    freelancer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_email TEXT NOT NULL,
    title TEXT NOT NULL,
    amount_sats BIGINT NOT NULL,
    source_platform TEXT NOT NULL,
    preimage_hash TEXT UNIQUE,
    preimage TEXT,
    payee_invoice TEXT,
    invoice TEXT,
    checking_id TEXT,
    -- Shareable payment-link token: separate high-entropy random value from
    -- the deal UUID so the public link can be revoked/rotated without
    -- exposing the internal id.
    share_token TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex') UNIQUE,
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
