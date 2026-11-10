-- recreate artifacts table with milestone_id for per-milestone scoping
DROP TABLE IF EXISTS artifacts CASCADE;

CREATE TABLE artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    milestone_id UUID REFERENCES milestones(id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_artifacts_deal ON artifacts(deal_id);
CREATE INDEX idx_artifacts_milestone ON artifacts(milestone_id);