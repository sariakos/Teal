-- 0003_apps_engine.sql
-- Phase 3 fields on apps for the deployment engine.
--
--   domains         comma-separated list of hostnames Traefik should route
--                   to this app. Empty until first configured. v1 keeps it
--                   as a flat string; a JSON column or split table can come
--                   later if features demand it (wildcards, per-domain TLS).
--
--   active_color    'blue' / 'green' / '' (no successful deploy yet). Read
--                   by the engine to determine which color the next deploy
--                   targets, and which color Traefik routes to.
--
--   queue_deploys   reserved. v1 always rejects concurrent deploy attempts
--                   with 409. The column lands now so a future enable-the-
--                   queue change is a migration of behaviour, not schema.

PRAGMA foreign_keys = ON;

ALTER TABLE apps ADD COLUMN domains       TEXT    NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN active_color  TEXT    NOT NULL DEFAULT '' CHECK (active_color IN ('', 'blue', 'green'));
ALTER TABLE apps ADD COLUMN queue_deploys INTEGER NOT NULL DEFAULT 0   CHECK (queue_deploys IN (0, 1));
