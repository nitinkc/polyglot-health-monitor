# Polyglot Health Monitor — Shared Spec

One contract, four implementations (Java, Go, Python, TypeScript). The point: identical
external behavior, idiomatically different internals. If two implementations produce
different JSON for the same request, that's a bug — not a "language difference."

---

## 1. Domain Model

**Monitor**

| field                  | type                     | notes                                              |
|:-----------------------|:-------------------------|:---------------------------------------------------|
| `id`                   | string (UUID)            | server-generated                                   |
| `name`                 | string                   | user-provided label                                |
| `url`                  | string                   | must be valid http/https URL                       |
| `interval_seconds`     | int                      | how often to check, min 5                          |
| `timeout_ms`           | int                      | per-request timeout, min 100                       |
| `failure_threshold`    | int                      | consecutive failures before status flips to `DOWN` |
| `status`               | enum                     | `UNKNOWN` \| `UP` \| `DOWN`                        |
| `consecutive_failures` | int                      | resets to 0 on success                             |
| `created_at`           | timestamp (ISO 8601 UTC) |                                                    |
| `updated_at`           | timestamp (ISO 8601 UTC) |                                                    |

**CheckResult** (append-only history row)

| field         | type             | notes                               |
|:--------------|:-----------------|:------------------------------------|
| `id`          | string (UUID)    |                                     |
| `monitor_id`  | string (FK)      |                                     |
| `checked_at`  | timestamp        |                                     |
| `success`     | bool             |                                     |
| `status_code` | int, nullable    | null if connection/timeout failure  |
| `latency_ms`  | int, nullable    | null if request never completed     |
| `error`       | string, nullable | short error class/message if failed |

---

## 2. Database Schema (SQLite, identical file across all four)

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

Each language uses its own idiomatic driver (`database/sql` + `mattn/go-sqlite3` or
`modernc.org/sqlite` in Go; `sqlite3`/`aiosqlite` in Python; `better-sqlite3` in
Node; JDBC + `sqlite-jdbc` or Hikari in Java) — but the schema, file, and SQL are
identical so you can literally point all four servers at the same `.db` file during
manual testing and watch them interoperate.

---

## 3. REST API Contract

Base path: `/api/v1`. All bodies are JSON. All timestamps ISO 8601 UTC.

### `POST /monitors`
Create + start monitoring immediately.

Request:
```json
{
  "name": "example-api",
  "url": "https://example.com/health",
  "interval_seconds": 30,
  "timeout_ms": 2000,
  "failure_threshold": 3
}
```
Response `201`:
```json
{
  "id": "b3f1...",
  "name": "example-api",
  "url": "https://example.com/health",
  "interval_seconds": 30,
  "timeout_ms": 2000,
  "failure_threshold": 3,
  "status": "UNKNOWN",
  "consecutive_failures": 0,
  "created_at": "2026-07-07T12:00:00Z",
  "updated_at": "2026-07-07T12:00:00Z"
}
```
`400` on validation failure (bad URL, interval < 5, etc.) with:
```json
{ "error": "interval_seconds must be >= 5" }
```

### `GET /monitors`
List all monitors (no history). `200` with array of Monitor objects.

### `GET /monitors/:id`
Single monitor, current state only. `404` if missing.

### `GET /monitors/:id/status?limit=20`
Monitor + recent history, newest first. Default `limit=20`, max `100`.
```json
{
  "monitor": { ...Monitor... },
  "history": [
    { "checked_at": "...", "success": true, "status_code": 200, "latency_ms": 84, "error": null }
  ]
}
```

### `DELETE /monitors/:id`
Stops scheduling, deletes monitor + cascades history. `204` on success.

### `GET /healthz`
Liveness for the server itself (not the monitors). Always `200 {"status":"ok"}` if the process is up. Used by your own Docker healthcheck.

---

## 4. Behavioral Requirements (this is the actual point of the exercise)

These are the parts where language idioms diverge — implement all of them, don't skip to make the happy path work:

1. **Concurrent scheduling.** Every monitor runs on its own independent timer/loop. Adding monitor #50 must not delay checks for monitor #1. This is your concurrency primitive test:
   - Java: virtual threads (`Thread.ofVirtual()`) or `StructuredTaskScope`, one per monitor tick
   - Go: one goroutine per monitor + `time.Ticker`, coordinated via channels
   - Python: `asyncio` tasks + `asyncio.sleep` loop per monitor, run under `asyncio.run`
   - TypeScript: don't fake concurrency with `setInterval` per monitor and call it done — use it, but reason explicitly about the event loop and confirm checks aren't serialized

2. **Bounded concurrency / rate limiting.** Global cap on in-flight HTTP checks (e.g. max 10 concurrent), configurable via env var `MAX_CONCURRENT_CHECKS`. This forces:
   - Java: `Semaphore`
   - Go: buffered channel as semaphore, or `golang.org/x/sync/semaphore`
   - Python: `asyncio.Semaphore`
   - TypeScript: `p-limit` or a hand-rolled queue — and understand *why* you need one at all given there's no real parallelism

3. **Timeout enforcement.** The `timeout_ms` per monitor must actually cancel the in-flight HTTP request, not just stop waiting for it client-side while the socket stays open.
   - Java: `HttpClient` with per-request timeout
   - Go: `context.WithTimeout` passed into the request
   - Python: `asyncio.wait_for` around the aiohttp/httpx call, or client-level timeout
   - TypeScript: `AbortController` + `AbortSignal.timeout()`

4. **Graceful shutdown.** On `SIGTERM`, stop accepting new monitor checks, let in-flight checks finish (or hard-cancel after a grace period, your choice — but document it), close the DB connection cleanly, then exit. No orphaned goroutines/threads/tasks logged after shutdown.

5. **Structured logging.** One JSON log line per check result minimum:
   ```json
   {"level":"info","monitor_id":"...","event":"check_completed","success":true,"latency_ms":84,"ts":"..."}
   ```
   Use each ecosystem's idiomatic structured logger (`slf4j`+logback w/ JSON encoder for Java; `slog` for Go; `structlog` for Python; `pino` for TypeScript) rather than hand-rolled string concatenation.

6. **Config via env vars**, same names across all four: `PORT`, `DB_PATH`, `MAX_CONCURRENT_CHECKS`, `LOG_LEVEL`. Fail fast with a clear error if required config is missing/invalid — don't silently default something like `DB_PATH`.

7. **Error handling idiom** — this is the single biggest "muscle memory" item:
   - Java: checked vs unchecked exceptions, where you actually catch vs propagate
   - Go: explicit `if err != nil` returns, wrapped with `%w` and `errors.Is`/`errors.As`
   - Python: exceptions + context managers; don't let a single monitor's failure crash the scheduler loop
   - TypeScript: `Result`-style discriminated unions vs try/catch — pick one and be consistent, then articulate why

---

## 5. Testing Requirements (same for all four)

- Unit tests: URL validation, threshold/status transition logic (pure logic, no I/O)
- Integration test: spin up the real HTTP server + real SQLite (temp file or `:memory:` where supported), hit a local test HTTP server (stand up a throwaway one) that you can force to fail/timeout/succeed on command, assert status transitions to `DOWN` after `failure_threshold` consecutive failures and back to `UP` on next success
- Idiomatic test tooling per language: JUnit 5 (Java), `testing` + `httptest` (Go), `pytest` + `pytest-asyncio` (Python), `vitest`/`jest` + `supertest` (TypeScript)

---

## 6. Dockerfile requirement

Multi-stage build in all four (compile/build stage → slim runtime stage). This is a good forcing function for understanding each language's deployment footprint:
- Java: build with Maven/Gradle stage → run on a JRE (not JDK) base image
- Go: build stage → `scratch` or `distroless` static binary
- Python: pip install stage → slim runtime, non-root user
- TypeScript: `npm ci && npm run build` stage → `node:slim` runtime, only `dist/` + prod deps copied over

---

## 7. Suggested Build Order & Time-box

| Order | Language         | Time-box  | Focus                                                                                     |
|:------|:-----------------|:----------|:------------------------------------------------------------------------------------------|
| 1     | Java             | 1 weekend | Reference implementation; also learn virtual threads/structured concurrency if new to you |
| 2     | Go               | 1 weekend | Biggest conceptual jump — do it while motivation is high                                  |
| 3     | Python (asyncio) | 1 weekend | Closest to typical FDE/integration work                                                   |
| 4     | TypeScript/Node  | 1 weekend | Event loop reasoning, strict typing discipline                                            |

After all four: write yourself a short "nuances" doc comparing how each handled cancellation, concurrency bounding, and error propagation. That comparison doc is arguably worth more for interview prep than the code itself — it's the kind of cross-stack fluency an FDE role actually tests for.

---

## 8. Appendix: Round 2 — Remote Postgres (Neon) Migration

Optional fifth pass, once all four local-SQLite builds work. Goal: same schema and
API contract, but now the DB is over the network — so you have to handle real
latency, pooling, and transient failures instead of a local file. This is closer to
what a client's actual environment looks like.

### 8.1 Why Neon
- Free tier, standard Postgres wire protocol — every language has a first-class,
  mature driver (no niche client library to learn instead of the concurrency
  lesson you're actually after).
- Serverless/autosuspend behavior on the free tier means your first request after
  idle time will be slow (cold start) — this is a *feature* for this exercise, not
  a bug. It forces you to handle connection timeouts and retries for real.
- Gives you a real DSN (`postgresql://user:pass@host/dbname?sslmode=require`) to
  drop into each language's config the same way.

*(Turso is a reasonable alternative if you'd rather stay on SQLite's dialect
instead of moving to Postgres — same idea, distributed libSQL over the network.
Neon is recommended here because Postgres driver maturity/pooling patterns are
more broadly transferable to client engagements.)*

### 8.2 Schema changes (minimal)
Postgres needs a few type swaps from the SQLite schema in Section 2:

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
Enable `pgcrypto` (`CREATE EXTENSION IF NOT EXISTS pgcrypto;`) if `gen_random_uuid()`
isn't available on your Neon project by default.

### 8.3 New config
Add one env var across all four: `DATABASE_URL` (Postgres DSN). Keep `DB_PATH` as
a fallback for local dev — don't rip out the SQLite path, just make the driver
layer swappable behind an interface/trait/protocol so you're practicing
**abstraction over a data layer**, itself a good FDE skill (client swaps their DB
under you constantly).

### 8.4 What each language should focus on

| Language   | Driver                                                                            | Focus                                                                                                                                                 |
|:-----------|:----------------------------------------------------------------------------------|:------------------------------------------------------------------------------------------------------------------------------------------------------|
| Java       | `pgjdbc` + HikariCP                                                               | Pool sizing (`maximumPoolSize`), connection validation query, `SQLTransientConnectionException` retry handling                                        |
| Go         | `pgx` (prefer over `lib/pq`, which is in maintenance mode)                        | `pgxpool.Pool` config, context-aware queries so your existing `context.WithTimeout` work from Section 4 now also bounds DB calls, not just HTTP calls |
| Python     | `asyncpg` (not `psycopg2` — stay async, consistent with your `asyncio` scheduler) | Pool via `asyncpg.create_pool`, and reasoning about pool size vs `MAX_CONCURRENT_CHECKS`                                                              |
| TypeScript | `pg` or `postgres.js`                                                             | Connection pool config, and handling a cold-start timeout gracefully (retry with backoff) rather than crashing the request                            |

##### The core tension

- **Heavy framework** (Spring Boot, NestJS, Django/FastAPI-with-all-the-batteries) → closer to what you'll touch on a real client engagement, but the framework does a lot of the concurrency/error-handling/DI work *for* you. You learn the framework's opinions, not the language's nuances.
- **Thin/stdlib-first** → you write the scheduler, the semaphore, the timeout handling yourself. You feel the language's raw behavior. But it's less representative of day-to-day client code, where nobody hand-rolls a connection pool.

Given your stated goal — muscle memory in language nuances, not framework mastery — **thin-but-industry-standard** is the right middle path: use the framework that's genuinely the default choice at most companies, but pick the *minimal* one in each ecosystem so it doesn't absorb the concurrency/error-handling work you're trying to practice.

##### Concrete recommendation per language

| Language       | Use this                                                                                 | Not this         | Why                                                                                                                                                                            |
|:---------------|:-----------------------------------------------------------------------------------------|:-----------------|:-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **Java**       | Plain `com.sun.net.httpserver` or **Javalin** (thin wrapper, no DI magic)                | Spring Boot      | Spring's `@Async`, `@Scheduled`, and auto-configured thread pools would do steps 4.1–4.3 *for* you — exactly the nuance you want to write yourself                             |
| **Go**         | Standard library `net/http` + `http.ServeMux` (Go 1.22+ has decent routing built in now) | Gin, Echo, Fiber | Go's stdlib HTTP is already ergonomic and idiomatic — a router framework buys you almost nothing here and hides `context` propagation, which is the whole point                |
| **Python**     | **FastAPI** (but only for routing/validation), asyncio hand-rolled for scheduling        | Celery, Django   | FastAPI's Pydantic validation is genuinely industry-standard and worth knowing; but don't let Celery do your background scheduling — that's the asyncio nuance you're here for |
| **TypeScript** | **Express** or raw `node:http`                                                           | NestJS           | NestJS's decorators/DI/module system are powerful but they abstract away exactly the event-loop reasoning (Promise scheduling, AbortController) you want reps on               |

##### Why this split specifically
- Javalin, stdlib `net/http`, FastAPI-for-routing-only, and Express are all **real, industry-common choices** — you're not building toy code, you'd ship any of these to production. So it still counts as "best practices."
- None of them impose an opinionated concurrency or DI model on top of the language — which means when you write the scheduler, semaphore, and shutdown handling, you're writing *language* code, not *framework* code. That's where the muscle memory actually lives.
- If you later interview somewhere that uses Spring or NestJS specifically, layering that framework on top of a project where you already deeply understand the underlying concurrency model is a much faster ramp than learning both at once.


### 8.5 New behavioral requirement: retry with backoff
Because Neon's free tier autosuspends after idle, your first query after a gap
may take several seconds or fail outright. Add:
- One retry with a short exponential backoff (e.g. 200ms, 800ms) on connection
  acquisition failure, not on every query — don't mask real query bugs
- Log the retry as a structured `event: "db_connection_retry"` line, not silently

This is the actual point of Round 2: local SQLite never taught you what to do when
the DB is slow to wake up or the network blips. Now you'll have written that
handling four times, in four idioms, which is exactly the kind of thing an FDE
interview loves to probe ("what happens when your DB connection times out
mid-request?").
