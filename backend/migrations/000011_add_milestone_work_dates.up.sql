ALTER TABLE milestones
    ADD COLUMN work_start_at TIMESTAMPTZ,
    ADD COLUMN work_due_at TIMESTAMPTZ;

ALTER TABLE milestones
    ADD CONSTRAINT milestones_work_dates_order
    CHECK (work_start_at IS NULL OR work_due_at IS NULL OR work_due_at >= work_start_at);
