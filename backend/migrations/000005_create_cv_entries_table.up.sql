-- cv_entries (content-verification hash anchors)
CREATE TABLE cv_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- UNIQUE: one CV anchor per artifact. Makes anchoring idempotent —
    -- INSERT ... ON CONFLICT (artifact_id) DO NOTHING keeps a released
    -- deal's work from being anchored twice even if the deal is released
    -- again or retried.
    artifact_id UUID NOT NULL UNIQUE REFERENCES artifacts(id) ON DELETE CASCADE,
    hash TEXT NOT NULL,
    algorithm TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cv_entries_artifact ON cv_entries(artifact_id);