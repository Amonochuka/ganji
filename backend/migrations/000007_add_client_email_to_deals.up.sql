ALTER TABLE deals ADD COLUMN client_email TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_deals_client_email ON deals(client_email);