CREATE TABLE IF NOT EXISTS  artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_artifacts_deal
ON artifacts(deal_id);