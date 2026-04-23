-- 0007_notifications.sql
-- Phase 7 — outbound notifications, in-app notifications, per-app
-- resource limits, downsampled metrics.
--
-- App columns:
--   cpu_limit                          Compose-style ("0.5", "2"); empty disables
--   memory_limit                       Compose-style ("512m", "1g"); empty disables
--   notification_webhook_url           Empty disables outbound webhook
--   notification_webhook_secret_encrypted  HMAC secret (Codec purpose webhook.outbound)
--   notification_email                 Empty disables failure emails
--
-- notifications:                       In-app feed for the bell. user_id
--                                      NULL means "broadcast to all
--                                      admins" — we resolve at read time.
--                                      Pruned aggressively (keep last 200
--                                      per user).
--
-- metrics_samples_1m:                  Pre-aggregated 1-minute buckets;
--                                      raw samples >6h old are rolled
--                                      into this table by the downsampler
--                                      and dropped from metrics_samples.
--                                      Retained 24h total (raw + 1m).

PRAGMA foreign_keys = ON;

ALTER TABLE apps ADD COLUMN cpu_limit                              TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN memory_limit                           TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN notification_webhook_url               TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN notification_webhook_secret_encrypted  BLOB NOT NULL DEFAULT x'';
ALTER TABLE apps ADD COLUMN notification_email                     TEXT NOT NULL DEFAULT '';

CREATE TABLE notifications (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
    level       TEXT    NOT NULL CHECK (level IN ('info','warn','error')),
    kind        TEXT    NOT NULL,
    title       TEXT    NOT NULL,
    body        TEXT    NOT NULL DEFAULT '',
    app_slug    TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL,
    read_at     TEXT
);

CREATE INDEX notifications_user_created_idx ON notifications(user_id, created_at DESC);
CREATE INDEX notifications_unread_idx       ON notifications(user_id, read_at) WHERE read_at IS NULL;

CREATE TABLE metrics_samples_1m (
    id              INTEGER PRIMARY KEY,
    container_id    TEXT    NOT NULL,
    container_name  TEXT    NOT NULL,
    app_slug        TEXT    NOT NULL,
    color           TEXT    NOT NULL,
    bucket_ts       TEXT    NOT NULL,        -- ISO-8601 UTC, minute-aligned
    cpu_pct_avg     REAL    NOT NULL DEFAULT 0,
    mem_bytes_avg   INTEGER NOT NULL DEFAULT 0,
    mem_limit       INTEGER NOT NULL DEFAULT 0,  -- last seen in window
    net_rx          INTEGER NOT NULL DEFAULT 0,  -- last seen in window (cumulative)
    net_tx          INTEGER NOT NULL DEFAULT 0,
    blk_rx          INTEGER NOT NULL DEFAULT 0,
    blk_tx          INTEGER NOT NULL DEFAULT 0,
    sample_count    INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX metrics_samples_1m_uniq    ON metrics_samples_1m(container_id, bucket_ts);
CREATE INDEX metrics_samples_1m_app_bucket_idx ON metrics_samples_1m(app_slug, bucket_ts DESC);
CREATE INDEX metrics_samples_1m_bucket_idx     ON metrics_samples_1m(bucket_ts);
