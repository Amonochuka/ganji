-- Allow milestone-mode deals to use per-milestone escrow legs (no single upfront deal hold).
ALTER TABLE deals ALTER COLUMN preimage_hash DROP NOT NULL;
ALTER TABLE deals ALTER COLUMN invoice DROP NOT NULL;
-- checking_id already nullable; preimage already nullable.
-- Keep unique on preimage_hash where present (Postgres allows multiple NULLs).
