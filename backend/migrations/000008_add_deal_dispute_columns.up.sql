-- Dispute → arbitration. Disputing a deal no longer cancels the hold;
-- the deal freezes in the 'disputed' state with the client's written reason
-- while an arbiter decides. dispute_reason is the client's explanation, and
-- disputed_at records when the deal entered that state.
ALTER TABLE deals
    ADD COLUMN dispute_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN disputed_at TIMESTAMPTZ;