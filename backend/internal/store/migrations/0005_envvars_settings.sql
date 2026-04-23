-- 0005_envvars_settings.sql
-- Phase 5 — env-vars plumbing and platform-wide settings.
--
--   platform_settings               Key/value settings editable by admins
--                                   (ACME email, HTTPS redirect toggle,
--                                   ACME staging flag, etc.). TEXT values;
--                                   semantics owned by the consumer.
--
--   app_shared_env_vars             Explicit allow-list of shared env-var
--                                   keys an App opts into. Shared vars are
--                                   NOT universal — each App chooses which
--                                   ones to inject. Key references the key
--                                   stored in env_vars where scope='shared';
--                                   since a key can be re-created after
--                                   delete we do not enforce a FK to the
--                                   shared row (engine treats "allow-listed
--                                   but missing" as a no-op with a warning
--                                   in the deploy log).
--
-- env_vars + deployments.env_var_set_hash already exist from 0001; no
-- schema change needed for them.

PRAGMA foreign_keys = ON;

CREATE TABLE platform_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE app_shared_env_vars (
    app_id     INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key        TEXT    NOT NULL,
    created_at TEXT    NOT NULL,

    PRIMARY KEY (app_id, key)
);

CREATE INDEX app_shared_env_vars_key_idx ON app_shared_env_vars(key);
