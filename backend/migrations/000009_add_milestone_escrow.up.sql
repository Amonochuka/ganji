-- Per-milestone escrow: sequential funding/release (M1 -> M2 ...).
-- Each milestone carries its own hold-invoice escrow leg.
ALTER TABLE milestones
  ADD COLUMN preimage_hash TEXT,
  ADD COLUMN preimage TEXT,
  ADD COLUMN payee_invoice TEXT,
  ADD COLUMN invoice TEXT,
  ADD COLUMN checking_id TEXT,
  ADD COLUMN payout_checking_id TEXT,
  ADD COLUMN payout_attempted_at TIMESTAMPTZ;

-- Expand milestone status to cover escrow lifecycle.
ALTER TABLE milestones DROP CONSTRAINT IF EXISTS valid_milestone_status;
ALTER TABLE milestones ADD CONSTRAINT valid_milestone_status CHECK (
  status IN (
    'pending',
    'awaiting_payment',
    'funded',
    'in_progress',
    'work_submitted',
    'completed',
    'released',
    'exited',
    'rejected',
    'refunded'
  )
);

CREATE INDEX IF NOT EXISTS idx_milestones_preimage_hash ON milestones(preimage_hash);
CREATE INDEX IF NOT EXISTS idx_milestones_checking_id ON milestones(checking_id);
