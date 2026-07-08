CREATE TABLE deals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    freelancer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    amount_sats BIGINT NOT NULL,
    source_platform TEXT NOT NULL,
    preimage_hash TEXT NOT NULL UNIQUE,
    invoice TEXT NOT NULL,
    checking_id TEXT,
    status TEXT NOT NULL DEFAULT 'awaiting_payment',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    verified_at TIMESTAMPTZ,

    CONSTRAINT valid_status CHECK (
        status IN ('awaiting_payment', 'locked', 'work_submitted', 'reviewing', 'released', 'disputed')
    )
);

CREATE INDEX idx_deals_freelancer_id ON deals(freelancer_id);
CREATE INDEX idx_deals_preimage_hash ON deals(preimage_hash);
CREATE INDEX idx_deals_status ON deals(status);