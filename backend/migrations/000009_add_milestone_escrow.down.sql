DROP INDEX IF EXISTS idx_milestones_checking_id;
DROP INDEX IF EXISTS idx_milestones_preimage_hash;
ALTER TABLE milestones DROP CONSTRAINT IF EXISTS valid_milestone_status;
ALTER TABLE milestones ADD CONSTRAINT valid_milestone_status CHECK (
  status IN ('pending', 'in_progress', 'completed', 'exited', 'rejected')
);
ALTER TABLE milestones
  DROP COLUMN IF EXISTS payout_attempted_at,
  DROP COLUMN IF EXISTS payout_checking_id,
  DROP COLUMN IF EXISTS checking_id,
  DROP COLUMN IF EXISTS invoice,
  DROP COLUMN IF EXISTS payee_invoice,
  DROP COLUMN IF EXISTS preimage,
  DROP COLUMN IF EXISTS preimage_hash;
