ALTER TABLE milestones
    ADD COLUMN delivery_days INTEGER NOT NULL DEFAULT 7,
    ADD COLUMN funded_at TIMESTAMPTZ;

ALTER TABLE milestones
    ADD CONSTRAINT valid_milestone_delivery_days CHECK (delivery_days BETWEEN 1 AND 3650);
