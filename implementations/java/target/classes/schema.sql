-- Polyglot Health Monitor — SQLite schema
-- Identical across all four language implementations (Section 2 of spec).

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS monitors (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL,
    url                  TEXT NOT NULL,
    interval_seconds     INTEGER NOT NULL CHECK (interval_seconds >= 5),
    timeout_ms           INTEGER NOT NULL CHECK (timeout_ms >= 100),
    failure_threshold    INTEGER NOT NULL CHECK (failure_threshold >= 1),
    status               TEXT NOT NULL DEFAULT 'UNKNOWN',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS check_results (
    id          TEXT PRIMARY KEY,
    monitor_id  TEXT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    checked_at  TEXT NOT NULL,
    success     INTEGER NOT NULL,   -- 0 = false, 1 = true
    status_code INTEGER,
    latency_ms  INTEGER,
    error       TEXT
);

CREATE INDEX IF NOT EXISTS idx_check_results_monitor_time
    ON check_results (monitor_id, checked_at DESC);
