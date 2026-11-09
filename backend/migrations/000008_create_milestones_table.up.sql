-- milestones (per-deal deliverables). Fresh/complete schema for sequential per-milestone escrow (M1 -> M2...).
CREATE TABLE milestones (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    amount_sats BIGINT NOT NULL DEFAULT 0,
    "order" INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'pending',
    preimage_hash TEXT,
    preimage TEXT,
    payee_invoice TEXT,
    invoice TEXT,
    checking_id TEXT,
    payout_checking_id TEXT,
    payout_attempted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT valid_milestone_status CHECK (
        status IN (
            'pending','awaiting_payment','funded','in_progress','work_submitted',
            'completed','released','exited','rejected','refunded'
        )
    ),
    CONSTRAINT unique_deal_milestone_order UNIQUE (deal_id, "order")
);

CREATE INDEX idx_milestones_deal_id ON milestones(deal_id);
CREATE INDEX idx_milestones_status ON milestones(status);
CREATE INDEX idx_milestones_preimage_hash ON milestones(preimage_hash);
CREATE INDEX idx_milestones_checking_id ON milestones(checking_id);

-- Milestone-mode deals use per-milestone escrow legs (no single upfront deal hold).
ALTER TABLE deals ALTER COLUMN preimage_hash DROP NOT NULL;
ALTER TABLE deals ALTER COLUMN invoice DROP NOT NULL;
