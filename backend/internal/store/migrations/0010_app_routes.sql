-- 0010_app_routes.sql
-- Per-service routing: each app can declare multiple Traefik routes,
-- one per service the operator wants exposed. Old apps keep working
-- via the existing `domains` field — the engine treats domains as a
-- legacy single-route fallback when routes is empty.
--
-- routes shape (JSON, validated app-side):
--   [{
--     "service": "app",            // optional; empty = primary heuristic
--     "domain":  "myapp.example",  // required
--     "port":    3000              // optional; 0 = auto-probe
--   }, ...]
--
-- Stored as TEXT (SQLite has JSON1 functions but we read/write the
-- raw string from Go).

PRAGMA foreign_keys = ON;

ALTER TABLE apps ADD COLUMN routes TEXT NOT NULL DEFAULT '[]';
