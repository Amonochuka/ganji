-- Add ots_proof column to cv_entries for OpenTimestamps proof
ALTER TABLE cv_entries
ADD COLUMN ots_proof BYTEA;

COMMENT ON COLUMN cv_entries.ots_proof IS 'Serialized OpenTimestamps proof (.ots file) for the artifact hash';