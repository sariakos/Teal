-- 0001_initial_schema.sql
-- Initial Teal schema. Tables for the five core domain types plus the
-- migration bookkeeping table (created by the migration runner before this
-- file is applied, not here).
--
-- Conventions:
--   * Snake_case column names.
--   * INTEGER primary keys (SQLite ROWID alias) for everything.
--   * Timestamps stored as ISO-8601 TEXT in UTC. SQLite has no native
--     timestamp type; ISO-8601 sorts lexicographically and is round-trippable.
--   * Booleans stored as INTEGER 0/1.
--   * Foreign keys ON DELETE CASCADE where the child cannot exist without
--     the parent (e.g. EnvVar without App). RESTRICT where deletion would
--     orphan history (e.g. an App with a Deployment referenced from
--     AuditLog).

PRAGMA foreign_keys = ON;

CREATE TABLE users (
    id                       INTEGER PRIMARY KEY,
    email                    TEXT    NOT NULL UNIQUE,
    password_hash            BLOB    NOT NULL,
    role                     TEXT    NOT NULL CHECK (role IN ('viewer','member','admin')),
    totp_secret_encrypted    BLOB    NOT NULL DEFAULT x'',
    created_at               TEXT    NOT NULL,
    updated_at               TEXT    NOT NULL
);

CREATE TABLE apps (
    id                       INTEGER PRIMARY KEY,
    slug                     TEXT    NOT NULL UNIQUE,
    name                     TEXT    NOT NULL,
    compose_file             TEXT    NOT NULL DEFAULT '',
    auto_deploy_branch       TEXT    NOT NULL DEFAULT '',
    auto_deploy_enabled      INTEGER NOT NULL DEFAULT 0 CHECK (auto_deploy_enabled IN (0,1)),
    status                   TEXT    NOT NULL DEFAULT 'idle'
                                     CHECK (status IN ('idle','deploying','running','failed','stopped')),
    created_at               TEXT    NOT NULL,
    updated_at               TEXT    NOT NULL
);

CREATE INDEX apps_status_idx ON apps(status);

CREATE TABLE deployments (
    id                       INTEGER PRIMARY KEY,
    app_id                   INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    color                    TEXT    NOT NULL CHECK (color IN ('blue','green')),
    status                   TEXT    NOT NULL CHECK (status IN ('pending','running','succeeded','failed','canceled')),
    commit_sha               TEXT    NOT NULL DEFAULT '',
    triggered_by_user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    env_var_set_hash         TEXT    NOT NULL DEFAULT '',
    started_at               TEXT,
    completed_at             TEXT,
    failure_reason           TEXT    NOT NULL DEFAULT '',
    created_at               TEXT    NOT NULL
);

CREATE INDEX deployments_app_created_idx ON deployments(app_id, created_at DESC);
CREATE INDEX deployments_status_idx      ON deployments(status);

CREATE TABLE env_vars (
    id                       INTEGER PRIMARY KEY,
    scope                    TEXT    NOT NULL CHECK (scope IN ('app','shared')),
    app_id                   INTEGER REFERENCES apps(id) ON DELETE CASCADE,
    key                      TEXT    NOT NULL,
    value_encrypted          BLOB    NOT NULL,
    created_at               TEXT    NOT NULL,
    updated_at               TEXT    NOT NULL,

    -- Scope/AppID consistency: app-scoped rows must have an AppID, shared
    -- rows must not. Enforced as a CHECK so bad inserts are rejected at the
    -- DB layer, not just the repository.
    CHECK ((scope = 'app'    AND app_id IS NOT NULL) OR
           (scope = 'shared' AND app_id IS NULL))
);

-- Uniqueness: one key per app, and one shared key globally. Two partial
-- unique indexes express this exactly.
CREATE UNIQUE INDEX env_vars_app_key_uniq    ON env_vars(app_id, key) WHERE scope = 'app';
CREATE UNIQUE INDEX env_vars_shared_key_uniq ON env_vars(key)         WHERE scope = 'shared';

CREATE TABLE audit_logs (
    id                       INTEGER PRIMARY KEY,
    actor_user_id            INTEGER REFERENCES users(id) ON DELETE SET NULL,
    actor                    TEXT    NOT NULL,
    action                   TEXT    NOT NULL,
    target_type              TEXT    NOT NULL DEFAULT '',
    target_id                TEXT    NOT NULL DEFAULT '',
    ip                       TEXT    NOT NULL DEFAULT '',
    details                  TEXT    NOT NULL DEFAULT '',
    created_at               TEXT    NOT NULL
);

CREATE INDEX audit_logs_created_idx        ON audit_logs(created_at DESC);
CREATE INDEX audit_logs_action_created_idx ON audit_logs(action, created_at DESC);
