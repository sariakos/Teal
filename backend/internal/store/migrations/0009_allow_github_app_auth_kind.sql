-- 0009_allow_github_app_auth_kind.sql
-- Extend the apps.git_auth_kind CHECK constraint to allow 'github_app'.
--
-- The original constraint from 0001 is `CHECK (git_auth_kind IN ('',
-- 'ssh', 'pat'))`. Go-side, validateGitAuthKind already accepts
-- 'github_app' (added in 0008's accompanying code change), but every
-- INSERT failed at the SQL layer with a CHECK constraint violation.
--
-- SQLite has no `ALTER TABLE ... DROP CONSTRAINT` and no way to
-- modify a CHECK in place. The standard escape hatch is the
-- writable_schema PRAGMA, which lets us rewrite the schema string
-- stored in sqlite_master directly. The replacement is purely
-- additive (one literal added to the IN list) so it can't change
-- column shape — same technique Alembic uses for SQLite CHECK
-- migrations.
--
-- The PRAGMA schema_version bump at the end forces SQLite to invalidate
-- its in-memory schema cache so subsequent INSERTs on the same
-- connection see the relaxed constraint without a restart.

PRAGMA writable_schema = ON;

UPDATE sqlite_master
SET sql = REPLACE(sql,
    "CHECK (git_auth_kind IN ('', 'ssh', 'pat'))",
    "CHECK (git_auth_kind IN ('', 'ssh', 'pat', 'github_app'))")
WHERE type = 'table' AND name = 'apps';

PRAGMA writable_schema = OFF;

-- Force a schema reload on this connection. The "+ 1" pattern is
-- canonical — schema_version is monotonically increasing and any bump
-- triggers cache invalidation.
PRAGMA schema_version = 99999999;
