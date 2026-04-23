-- 0006_metrics.sql
-- Phase 6 — per-container metric samples scraped from `docker stats`.
--
-- Storage shape: one row per (container, scrape tick). Pruned by a
-- background goroutine that drops rows older than the configured
-- retention window. v1 stores only raw samples; downsampling is deferred
-- to Phase 7.
--
-- Indexes optimise the two read paths:
--   * (app_slug, ts DESC) — overview chart for an app
--   * (container_id, ts DESC) — per-container series

PRAGMA foreign_keys = ON;

CREATE TABLE metrics_samples (
    id              INTEGER PRIMARY KEY,
    container_id    TEXT    NOT NULL,
    container_name  TEXT    NOT NULL,
    app_slug        TEXT    NOT NULL,
    color           TEXT    NOT NULL,        -- 'blue' | 'green' (no CHECK so unknowns survive a future renaming)
    ts              TEXT    NOT NULL,        -- ISO-8601 UTC, ns precision
    cpu_pct         REAL    NOT NULL DEFAULT 0,
    mem_bytes       INTEGER NOT NULL DEFAULT 0,
    mem_limit       INTEGER NOT NULL DEFAULT 0,
    net_rx          INTEGER NOT NULL DEFAULT 0,
    net_tx          INTEGER NOT NULL DEFAULT 0,
    blk_rx          INTEGER NOT NULL DEFAULT 0,
    blk_tx          INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX metrics_samples_app_ts_idx       ON metrics_samples(app_slug, ts DESC);
CREATE INDEX metrics_samples_container_ts_idx ON metrics_samples(container_id, ts DESC);
CREATE INDEX metrics_samples_ts_idx           ON metrics_samples(ts);
