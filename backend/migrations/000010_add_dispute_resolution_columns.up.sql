-- Arbitration resolution trail. When an operator resolves a dispute the
-- network leg runs (settle+payout for release, cancel hold for refund) and
-- these columns record who closed it and when, so a dispute is never a
-- silent state change on the money.
ALTER TABLE deals ADD COLUMN resolved_at TIMESTAMPTZ;
ALTER TABLE deals ADD COLUMN resolved_by TEXT;