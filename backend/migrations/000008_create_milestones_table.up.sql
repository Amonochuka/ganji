-- milestones (per-deal deliverables within a job).
-- A deal may have one or more milestones. Each milestone represents a
-- step-by-step deliverable that the freelancer completes and the client
-- reviews before moving to the next milestone or releasing the deal.
CREATE TABLE milestones (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    amount_sats BIGINT NOT NULL DEFAULT 0,
    "order" INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'pending',
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT valid_milestone_status CHECK (
        status IN ('pending', 'in_progress', 'completed', 'exited', 'rejected')
    ),
    CONSTRAINT unique_deal_milestone_order UNIQUE (deal_id, "order")
);

CREATE INDEX idx_milestones_deal_id ON milestones(deal_id);
CREATE INDEX idx_milestones_status ON milestones(status);