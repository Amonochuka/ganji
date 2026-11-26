ALTER TABLE milestones
    DROP CONSTRAINT IF EXISTS milestones_work_dates_order;

ALTER TABLE milestones
    DROP COLUMN IF EXISTS work_due_at,
    DROP COLUMN IF EXISTS work_start_at;
