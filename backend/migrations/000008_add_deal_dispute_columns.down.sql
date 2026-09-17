ALTER TABLE deals
    DROP COLUMN IF EXISTS disputed_at,
    DROP COLUMN IF EXISTS dispute_reason;