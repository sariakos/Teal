-- 0008_github_app.sql
-- GitHub App support: per-app installation ID + repo-full-name.
--
-- The App's own credentials (App ID, private key, webhook secret)
-- live in the platform_settings KV table; only per-installation data
-- belongs on the apps row.
--
-- github_app_installation_id   GitHub's numeric installation ID (one
--                              per repo or per account install). 0
--                              when no install exists yet.
-- github_app_repo              The "owner/repo" full name the user
--                              picked during the install flow. Used
--                              by the centralized webhook to route
--                              push events to the right Teal app.

PRAGMA foreign_keys = ON;

ALTER TABLE apps ADD COLUMN github_app_installation_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE apps ADD COLUMN github_app_repo            TEXT    NOT NULL DEFAULT '';

CREATE INDEX apps_github_app_repo_idx ON apps(github_app_repo) WHERE github_app_repo != '';
