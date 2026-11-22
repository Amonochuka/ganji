DROP INDEX IF EXISTS idx_milestones_deal;
ALTER TABLE verifications DROP COLUMN IF EXISTS milestone_id;
ALTER TABLE artifacts DROP COLUMN IF EXISTS milestone_id;
DROP TABLE IF EXISTS milestones;
DROP TYPE IF EXISTS milestone_status;
