-- Ganji initial schema (consolidated)
-- Run with: psql -d ganji -f 000001_initial_schema.up.sql

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- users
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    bitcoin_address TEXT,
    trust_score NUMERIC DEFAULT 100,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_slug ON users(slug);
CREATE INDEX idx_users_email ON users(email);

-- deals (complete schema with all escrow columns)
CREATE TABLE deals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    freelancer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_email TEXT NOT NULL,
    title TEXT NOT NULL,
    amount_sats BIGINT NOT NULL,
    source_platform TEXT NOT NULL,
    preimage_hash TEXT NOT NULL UNIQUE,
    preimage TEXT,
    payee_invoice TEXT,
    invoice TEXT NOT NULL,
    checking_id TEXT,
    status TEXT NOT NULL DEFAULT 'awaiting_payment',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    verified_at TIMESTAMPTZ,

    CONSTRAINT valid_status CHECK (
        status IN (
            'awaiting_payment',
            'locked',
            'work_submitted',
            'reviewing',
            'released',
            'disputed',
            'refunded'
        )
    )
);

CREATE INDEX idx_deals_freelancer_id ON deals(freelancer_id);
CREATE INDEX idx_deals_preimage_hash ON deals(preimage_hash);
CREATE INDEX idx_deals_status ON deals(status);
CREATE INDEX idx_deals_client_email ON deals(client_email);

-- artifacts
CREATE TABLE artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_artifacts_deal ON artifacts(deal_id);

-- verifications
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

CREATE INDEX idx_verifications_artifact ON verifications(artifact_id);

-- cv_entries
CREATE TABLE cv_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    hash TEXT NOT NULL,
    algorithm TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cv_entries_artifact ON cv_entries(artifact_id);

-- refresh_tokens
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);