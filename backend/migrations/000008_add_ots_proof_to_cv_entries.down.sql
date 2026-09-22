-- Remove ots_proof column from cv_entries
ALTER TABLE cv_entries
DROP COLUMN IF EXISTS ots_proof;