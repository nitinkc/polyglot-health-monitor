# Simple Polyglot Health Monitor — Shared Spec

One contract, four implementations (Java, Go, Python, TypeScript). The point: identical
external behavior, idiomatically different internals.

If two implementations produce **different JSON** for the same request, **that's a bug — not a "language difference."**

## 0. High-Level Purpose

At a high level, this application is a **URL health monitoring service** with one shared API and 
schema across four language implementations.

The system lets a user register endpoints to monitor (for example, `https://example.com/health`) 
and then continuously checks those endpoints on a configured interval. 

For each check, it records :-

- whether the request succeeded, 
- how long it took, 
- and any error information if it failed.

### What the application does end-to-end

1. Accepts monitor definitions via API (`name`, `url`, `interval_seconds`, `timeout_ms`, `failure_threshold`).
2. Runs checks concurrently so each monitor is independent and one slow target does not block others.
3. Enforces a **global cap** on in-flight outbound checks (`MAX_CONCURRENT_CHECKS`).
4. **Persists** every check result to `check_results` and updates monitor state (`UNKNOWN` / `UP` / `DOWN`) 
   based on consecutive-failure rules.
5. **Exposes APIs** to list monitors, fetch current state, fetch recent history, and delete monitors.
6. Shuts down gracefully on `SIGTERM` by stopping new checks, finishing (or timing out) in-flight checks, and closing DB resources cleanly.

This project is intentionally designed to practice **concurrency, cancellation, shutdown, and error-handling idioms** in each language while preserving the same external behavior.

---

## 1. Domain Model

Two entities: **Monitor** (config + derived current state) and **CheckResult**
(append-only observation log). Fields, types, and relationships are defined once
in [db.md](db.md) — they map 1:1 onto the API schemas in Section 3. Don't invent
per-language field names.

---

## 2. Database Schema (SQLite, identical file across all four)

The DDL lives in [db.md](db.md#schema) — identical schema, file, and SQL across
all four implementations, so you can point all four servers at the same `.db`
file during manual testing and watch them interoperate.

Each language uses its own idiomatic driver:

- `database/sql` + `mattn/go-sqlite3` or `modernc.org/sqlite` in Go; 
- `sqlite3`/`aiosqlite` in Python; 
- `better-sqlite3` in Node; 
- JDBC + `sqlite-jdbc` or Hikari in Java

---

## 3. REST API Contract

The authoritative contract is [openapi.yaml](openapi.yaml) (rendered on the
[API Reference](API.md) page). All four servers must satisfy it identically —
field names, validation minimums, status codes, and the `UNKNOWN`/`UP`/`DOWN`
enum are defined there, not restated per language.

- `GET /healthz` — liveness for the server process itself
- `GET`/`POST /monitors` — list, or create and start scheduling immediately
- `GET`/`DELETE /monitors/{id}` — current state, or delete (cascades history)
- `GET /monitors/{id}/status?limit=N` — monitor plus recent check history

---

## 4. Behavioral Requirements (this is the actual point of the exercise)

These are the parts where language idioms diverge — implement all of them, **don't skip to make the happy path work**:

1. **Concurrent scheduling.** Every monitor runs on its **own independent timer/loop**. Adding monitor #50 must not delay 
  checks for monitor #1. This is your concurrency primitive test:
     - Java: virtual threads (`Thread.ofVirtual()`) or `StructuredTaskScope`, one per monitor tick
     - Go: one goroutine per monitor + `time.Ticker`, coordinated via channels
     - Python: `asyncio` tasks + `asyncio.sleep` loop per monitor, run under `asyncio.run`
     - TypeScript: don't fake concurrency with `setInterval` per monitor and call it done — use it, but reason explicitly
       about the event loop and confirm checks aren't serialized

2. **Bounded concurrency / rate limiting.** Global cap on in-flight HTTP checks (e.g. max 10 concurrent), configurable via
  env var `MAX_CONCURRENT_CHECKS`. This forces:
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

Same two tables, translated to Postgres types — DDL is in
[db.md](db.md#postgres-schema-round-2). The deltas from Section 2:

- `TEXT` PKs → `UUID DEFAULT gen_random_uuid()` (enable `pgcrypto` if needed)
- `TEXT` timestamps → `TIMESTAMPTZ DEFAULT now()`
- `INTEGER` success flag → `BOOLEAN`

Checks and indexes carry over unchanged.

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
