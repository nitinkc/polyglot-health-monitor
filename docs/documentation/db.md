# Database Design

Two tables — `monitors` (config + current derived state) and `check_results`
(append-only observation log). This mirrors real monitoring systems
(targets + samples), and deliberately stays small: the learning value of this
project is in concurrency, cancellation, and error handling — not schema
surface area.

``` mermaid
erDiagram

    MONITORS {
        TEXT id PK "Monitor identifier"
        TEXT name "Display name"
        TEXT url "Endpoint URL"
        INTEGER interval_seconds "Polling interval >= 5"
        INTEGER timeout_ms "Request timeout >= 100"
        INTEGER failure_threshold "Failures before unhealthy >= 1"
        TEXT status "UNKNOWN/UP/DOWN"
        INTEGER consecutive_failures "Current failure count"
        TEXT created_at "ISO timestamp"
        TEXT updated_at "ISO timestamp"
    }

    CHECK_RESULTS {
        TEXT id PK "Check result identifier"
        TEXT monitor_id FK "References monitors.id"
        TEXT checked_at "ISO timestamp"
        INTEGER success "0=false, 1=true"
        INTEGER status_code "HTTP status"
        INTEGER latency_ms "Response latency"
        TEXT error "Error message"
    }

    MONITORS ||--o{ CHECK_RESULTS : generates
```

## Schema
```sql
CREATE TABLE monitors (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    url                 TEXT NOT NULL,
    interval_seconds    INTEGER NOT NULL CHECK (interval_seconds >= 5),
    timeout_ms          INTEGER NOT NULL CHECK (timeout_ms >= 100),
    failure_threshold   INTEGER NOT NULL CHECK (failure_threshold >= 1),
    status              TEXT NOT NULL DEFAULT 'UNKNOWN',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);

CREATE TABLE check_results (
    id          TEXT PRIMARY KEY,
    monitor_id  TEXT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    checked_at  TEXT NOT NULL,
    success     INTEGER NOT NULL,   -- 0/1
    status_code INTEGER,
    latency_ms  INTEGER,
    error       TEXT
);

CREATE INDEX idx_check_results_monitor_time
    ON check_results (monitor_id, checked_at DESC);
```

## Postgres schema (Round 2)

Same two tables translated for Postgres — used by the optional Neon migration in
[spec.md Section 8](spec.md#8-appendix-round-2-remote-postgres-neon-migration):

```sql
CREATE TABLE monitors (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT NOT NULL,
    url                  TEXT NOT NULL,
    interval_seconds     INTEGER NOT NULL CHECK (interval_seconds >= 5),
    timeout_ms           INTEGER NOT NULL CHECK (timeout_ms >= 100),
    failure_threshold    INTEGER NOT NULL CHECK (failure_threshold >= 1),
    status               TEXT NOT NULL DEFAULT 'UNKNOWN',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE check_results (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    monitor_id  UUID NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    checked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    success     BOOLEAN NOT NULL,
    status_code INTEGER,
    latency_ms  INTEGER,
    error       TEXT
);

CREATE INDEX idx_check_results_monitor_time
    ON check_results (monitor_id, checked_at DESC);
```

Enable `pgcrypto` (`CREATE EXTENSION IF NOT EXISTS pgcrypto;`) if
`gen_random_uuid()` isn't available on your Neon project by default.

## What chaos looks like in the DB

Chaos events are **not** stored here — by design. Two separate streams exist:

| Stream | Where it lives | What it records |
| ------ | -------------- | --------------- |
| Control plane (cause) | `chaos-target`'s in-memory log, via `GET /chaos/history` | Mode flips, pauses, scenario steps |
| Data plane (symptom) | `check_results` in `monitor.db` | 503s, timeouts, latency spikes, resets |

A real monitor can't know *why* a target broke — only that it did. Keeping the
observer blind to the cause is the point; persisting chaos events in
`monitor.db` would let the monitor "cheat" with ground truth it could never
have in production.

## Future enhancements (optional Round 3)

Add these only after all four implementations work — none are required:

- **`chaos_events` correlation table** — the one gap in the current design:
  cause→effect correlation is manual. A table populated by the dashboard's
  Chaos tab (`flipped_at`, `mode`, `target`) would let you measure
  **detection latency** — time from `POST /chaos/mode` to the monitor flipping
  `DOWN` — by timestamp-joining against `check_results`. Useful for tuning
  `interval_seconds` / `failure_threshold` trade-offs.
- **Derived incidents / uptime** — status windows and uptime % are derivable
  from `check_results` today; writing the SQL to compute them is a good
  exercise before deciding an `incidents` table earns its keep.
- **Retention policy** — `check_results` grows unbounded; real monitors rollup
  or expire history. A `DELETE ... WHERE checked_at < ?` sweeper per
  implementation is a cheap, practical exercise.

Remote Postgres migration (pooling, retries, `DATABASE_URL`) is already
covered — see [spec.md Section 8](spec.md#8-appendix-round-2-remote-postgres-neon-migration).

