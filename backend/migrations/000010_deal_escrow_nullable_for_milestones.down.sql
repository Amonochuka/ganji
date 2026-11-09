-- Down migration is lossy/unsafe if milestone-mode rows exist with NULL escrow fields.
-- Best-effort restore constraints (will fail if NULLs exist).
UPDATE deals SET preimage_hash = COALESCE(preimage_hash, encode(gen_random_bytes(32),'hex')) WHERE preimage_hash IS NULL;
UPDATE deals SET invoice = COALESCE(invoice, '') WHERE invoice IS NULL;
ALTER TABLE deals ALTER COLUMN preimage_hash SET NOT NULL;
ALTER TABLE deals ALTER COLUMN invoice SET NOT NULL;
