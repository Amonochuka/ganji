CREATE TABLE verifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    method TEXT NOT NULL,
    reference TEXT NOT NULL,
    status TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),

    CONSTRAINT valid_verification_status CHECK (
        status IN ('pending', 'ready', 'expired')
    )
);

CREATE INDEX idx_verifications_artifact
ON verifications (artifact_id);