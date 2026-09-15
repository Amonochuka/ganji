-- Add escrow columns needed for the network-as-escrow (hold invoice) design.
-- preimage      : raw hex preimage Ganji generated at deal creation. Required to
--                 settle the hold invoice later (LNbits /payments/settle takes the
--                 preimage). NULL for legacy custodial deals.
-- payee_invoice : Lightning invoice/address the freelancer wants the escrow paid
--                 out to after the client approves. Required before settling.
ALTER TABLE deals
    ADD COLUMN preimage TEXT,
    ADD COLUMN payee_invoice TEXT;

-- A dispute now results in cancelling the hold invoice, which refunds the client
-- on the network. That outcome needs a terminal 'refunded' status.
ALTER TABLE deals DROP CONSTRAINT valid_status;
ALTER TABLE deals
    ADD CONSTRAINT valid_status CHECK (
        status IN ('awaiting_payment', 'locked', 'work_submitted', 'reviewing', 'released', 'disputed', 'refunded')
    );