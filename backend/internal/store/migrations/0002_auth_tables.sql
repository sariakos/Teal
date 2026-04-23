-- 0002_auth_tables.sql
-- Tables for session-based auth and API-key auth.
--
-- sessions: one row per active browser session. Cookie carries the session
--   ID; CSRF token is bound to the session and rotates on issue. Sliding
--   expiry: last_seen_at is bumped on use, expires_at is set at issue and
--   slid forward by the auth middleware (only if the slide is meaningful,
--   to avoid a write per request).
--
-- api_keys: one row per programmatic key issued for a user. Only the SHA-256
--   of the raw key is stored. The raw key is shown once (at creation) and
--   never recoverable.

PRAGMA foreign_keys = ON;

CREATE TABLE sessions (
    id              TEXT    PRIMARY KEY,                  -- 32-byte random, base32-encoded
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token      TEXT    NOT NULL,                     -- 32-byte random, base32-encoded
    ip              TEXT    NOT NULL DEFAULT '',
    user_agent      TEXT    NOT NULL DEFAULT '',
    expires_at      TEXT    NOT NULL,
    last_seen_at    TEXT    NOT NULL,
    created_at      TEXT    NOT NULL
);

CREATE INDEX sessions_user_idx     ON sessions(user_id);
CREATE INDEX sessions_expires_idx  ON sessions(expires_at);

CREATE TABLE api_keys (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT    NOT NULL,                     -- human label, e.g. "ci-deploy"
    key_hash        BLOB    NOT NULL UNIQUE,              -- SHA-256 of raw key
    last_used_at    TEXT,
    revoked_at      TEXT,
    created_at      TEXT    NOT NULL
);

CREATE INDEX api_keys_user_idx     ON api_keys(user_id);
