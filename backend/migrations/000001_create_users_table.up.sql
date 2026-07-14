CREATE TABLE IF NOT EXISTS users (
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