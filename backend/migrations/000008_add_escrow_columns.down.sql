ALTER TABLE deals DROP CONSTRAINT valid_status;
ALTER TABLE deals
    ADD CONSTRAINT valid_status CHECK (
        status IN ('awaiting_payment', 'locked', 'work_submitted', 'reviewing', 'released', 'disputed')
    );

ALTER TABLE deals
    DROP COLUMN IF EXISTS preimage,
    DROP COLUMN IF EXISTS payee_invoice;