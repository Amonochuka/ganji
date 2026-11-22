DROP INDEX IF EXISTS idx_milestones_deal;
ALTER TABLE verifications DROP COLUMN IF EXISTS milestone_id;
DROP INDEX IF EXISTS idx_artifacts_milestone;
ALTER TABLE artifacts DROP COLUMN IF EXISTS milestone_id;
DROP TABLE IF EXISTS milestones;
DROP TYPE IF EXISTS milestone_status;
