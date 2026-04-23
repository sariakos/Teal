-- 0004_app_git.sql
-- Phase 4 — Git/GitHub integration columns.
--
--   git_url                         clone URL (https or ssh). Empty when
--                                   git is not configured for this app.
--   git_auth_kind                   '' | 'ssh' | 'pat'. Empty for public
--                                   repos that don't need credentials.
--   git_auth_credential_encrypted   AES-GCM ciphertext of the SSH private
--                                   key (PEM) or PAT (raw token).
--   git_branch                      explicit branch to clone. When empty,
--                                   the engine falls back to
--                                   auto_deploy_branch.
--   git_compose_path                relative path to compose file inside
--                                   the repo. Default 'docker-compose.yml'.
--   webhook_secret_encrypted        AES-GCM ciphertext of the per-app
--                                   webhook HMAC secret. Generated server-
--                                   side; shown to the user once.
--   last_deployed_commit_sha        denormalised from the latest succeeded
--                                   deployment so the dashboard list can
--                                   render without an N+1 join.
--
-- deployments.trigger_kind          '' | 'manual' | 'webhook' | 'rollback'.
--                                   Empty for rows created before this
--                                   migration (Phase 3 deployments).

PRAGMA foreign_keys = ON;

ALTER TABLE apps ADD COLUMN git_url                       TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN git_auth_kind                 TEXT NOT NULL DEFAULT ''
                                CHECK (git_auth_kind IN ('', 'ssh', 'pat'));
ALTER TABLE apps ADD COLUMN git_auth_credential_encrypted BLOB NOT NULL DEFAULT x'';
ALTER TABLE apps ADD COLUMN git_branch                    TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN git_compose_path              TEXT NOT NULL DEFAULT 'docker-compose.yml';
ALTER TABLE apps ADD COLUMN webhook_secret_encrypted      BLOB NOT NULL DEFAULT x'';
ALTER TABLE apps ADD COLUMN last_deployed_commit_sha      TEXT NOT NULL DEFAULT '';

ALTER TABLE deployments ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT ''
                                CHECK (trigger_kind IN ('', 'manual', 'webhook', 'rollback'));
