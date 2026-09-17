-- Arbitration operators: users who may resolve disputes (release or refund
-- frozen escrow). A boolean on the users table so the role is stored with the
-- account, survives token issuance (embedded in the access-token claims), and
-- is queryable. Operators are promoted via OPERATOR_EMAILS at startup, never
-- self-serve.
ALTER TABLE users ADD COLUMN is_operator BOOLEAN NOT NULL DEFAULT FALSE;