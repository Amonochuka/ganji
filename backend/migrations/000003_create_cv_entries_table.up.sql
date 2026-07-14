CREATE TABLE cv_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL
        REFERENCES artifacts(id)
        ON DELETE CASCADE,

    hash TEXT NOT NULL,
    algorithm TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cv_entries_artifact
ON cv_entries(artifact_id);