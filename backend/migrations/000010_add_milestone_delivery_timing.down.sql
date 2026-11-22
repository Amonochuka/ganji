ALTER TABLE milestones DROP CONSTRAINT IF EXISTS valid_milestone_delivery_days;
ALTER TABLE milestones DROP COLUMN IF EXISTS funded_at;
ALTER TABLE milestones DROP COLUMN IF EXISTS delivery_days;
