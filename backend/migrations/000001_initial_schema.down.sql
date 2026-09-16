-- Ganji initial schema rollback
-- Run with: psql -d ganji -f 000001_initial_schema.down.sql

DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS cv_entries;
DROP TABLE IF EXISTS verifications;
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS deals;
DROP TABLE IF EXISTS users;
DROP EXTENSION IF EXISTS pgcrypto;