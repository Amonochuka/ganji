-- 000008: Milestone-based escrow
CREATE TYPE milestone_status AS ENUM ('pending', 'awaiting_payment', 'funded', 'in_progress', 'work_submitted', 'completed', 'released', 'refunded', 'disputed', 'rejected');

CREATE TABLE milestones (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    amount_sats BIGINT NOT NULL CHECK (amount_sats > 0),
    "order" INT NOT NULL DEFAULT 0,
    status milestone_status NOT NULL DEFAULT 'pending',
    preimage_hash TEXT,
    preimage TEXT,
    payee_invoice TEXT,
    invoice TEXT,
    checking_id TEXT,
    payout_checking_id TEXT,
    payout_attempted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivery_days INT NOT NULL DEFAULT 7 CHECK (delivery_days > 0),
    funded_at TIMESTAMPTZ
);

CREATE INDEX idx_milestones_deal ON milestones(deal_id);

ALTER TABLE artifacts ADD COLUMN milestone_id UUID REFERENCES milestones(id) ON DELETE SET NULL;
ALTER TABLE verifications ADD COLUMN milestone_id UUID REFERENCES milestones(id) ON DELETE SET NULL;
