# Copilot / AI Assistant Instructions — Polyglot Health Monitor

This repo contains four independent implementations of the same service — Java, Go,
Python, and TypeScript — built to develop language-nuance muscle memory for Forward
Deployed Engineering work. Read this fully before generating or suggesting code.

## Project Goal

Each implementation must satisfy the **same external contract** (REST API, DB schema,
behavior) using **idiomatic, industry-standard patterns for its own language** — not
a direct syntax translation of another language's implementation. If you're asked to
port logic from one language folder to another, adapt the *idiom*, not the *code shape*.
A Go `if err != nil` chain should not become an exception try/catch just because that's
what the Java version did, and vice versa.

Prioritize idiomatic correctness and matching this spec over cleverness or brevity.

## Repository Layout

```
/java/        Java 21, Javalin, sqlite-jdbc
/go/          Go 1.22+, stdlib net/http, modernc.org/sqlite
/python/      Python 3.12+, FastAPI (routing/validation only) + asyncio (scheduling)
/typescript/  Node 20+, Express, better-sqlite3
/schema.sql   Shared SQLite schema — identical across all four
/openapi.yaml Shared API contract — identical across all four
/spec.md      Full project spec (source of truth for behavior requirements)
```

## Technology Choices (deliberate — do not suggest swapping these)

| Language   | Framework                                                                     | Why this and not the "bigger" alternative                                                                                                       |
|:-----------|:------------------------------------------------------------------------------|:------------------------------------------------------------------------------------------------------------------------------------------------|
| Java       | **Javalin** (thin wrapper, no DI)                                             | Not Spring Boot — Spring's `@Async`/`@Scheduled`/auto-config threading would do the concurrency work *for* us, defeating the point              |
| Go         | **Standard library `net/http`** (Go 1.22+ `ServeMux` routing)                 | Not Gin/Echo/Fiber — Go's stdlib HTTP is already idiomatic; a router framework hides `context` propagation, which is the nuance being practiced |
| Python     | **FastAPI** for routing/validation only; hand-rolled `asyncio` for scheduling | Not Celery/Django — don't let a task queue do the scheduling; the `asyncio.Semaphore`/`asyncio.wait_for` reps are the point                     |
| TypeScript | **Express** (or raw `node:http`)                                              | Not NestJS — its DI/decorators abstract away the event-loop and `AbortController` reasoning being practiced                                     |

Concurrency primitive per language (do not substitute a different pattern without a
noted reason):
- **Java**: one virtual thread per monitor (`Thread.ofVirtual()` / `Executors.newVirtualThreadPerTaskExecutor()`), bounded by `java.util.concurrent.Semaphore`
- **Go**: one goroutine per monitor + `time.Ticker`, `context.WithTimeout` per request, buffered channel (or `golang.org/x/sync/semaphore`) as the concurrency bound
- **Python**: one `asyncio.Task` per monitor, `asyncio.Semaphore` for bounding, `asyncio.wait_for` for timeouts
- **TypeScript**: `setInterval` per monitor, `AbortController`/`AbortSignal.timeout()` for cancellation, `p-limit` for bounding

## Database

- Single shared `schema.sql` (SQLite). All four implementations use the **same file
  and same schema** — do not let any language auto-migrate or alter the schema.
- Optional Round 2: Postgres via Neon. If working on that branch, see `spec.md`
  Section 8 before changing schema types (`UUID`, `TIMESTAMPTZ`, etc.).
- Driver per language: `sqlite-jdbc` (Java), `modernc.org/sqlite` (Go — pure Go, no
  cgo needed), `aiosqlite` (Python — stay async, don't suggest the sync `sqlite3` module
  in the async scheduler path), `better-sqlite3` (TypeScript — synchronous by design,
  this is intentional and fine since it's fast/local, don't "fix" it to be async).

## API Contract

Defined in `openapi.yaml`. All four implementations must produce byte-for-byte
equivalent JSON shapes for the same input (field names, types, status codes). If a
generated implementation diverges from the contract, that's a bug to flag, not a
"language difference" to justify.

## Non-negotiable Behavioral Requirements (see `spec.md` Section 4 for full detail)

1. Each monitor runs on an independent concurrent loop — never serialize checks
   across monitors.
2. Global concurrency bound on in-flight HTTP checks, configurable via
   `MAX_CONCURRENT_CHECKS` env var.
3. `timeout_ms` must cancel the in-flight HTTP request at the transport/context
   level, not just stop waiting for it client-side.
4. Graceful shutdown on SIGTERM: stop scheduling new checks, let in-flight checks
   finish (or hard-cancel after a documented grace period), close DB cleanly.
5. Structured JSON logging (one line per check result minimum) using each
   ecosystem's idiomatic logger — `slf4j`+logback JSON encoder (Java), `log/slog`
   (Go), `structlog` (Python), `pino` (TypeScript). Never suggest raw string
   concatenation or `print`/`console.log`/`System.out.println` for these logs in
   anything beyond a starter stub.
6. Config via env vars, same names across all four: `PORT`, `DB_PATH`,
   `DATABASE_URL` (Round 2 only), `MAX_CONCURRENT_CHECKS`, `LOG_LEVEL`. Fail fast
   with a clear error on missing/invalid required config — never silently default
   `DB_PATH`.
7. Error handling must use each language's own idiom, not another language's:
   - Java: checked vs. unchecked exceptions, intentional catch vs. propagate
   - Go: explicit `if err != nil`, `%w` wrapping, `errors.Is`/`errors.As`
   - Python: exceptions + context managers; one monitor's failure must never
     crash the whole scheduler loop
   - TypeScript: pick either discriminated-union `Result` types or try/catch and
     be consistent within the file — don't mix both styles in one implementation

## Testing Standards

- Unit tests: pure logic only (URL validation, status transition rules) — no
  network or DB in unit tests.
- Integration test: real HTTP server + real SQLite (temp file or `:memory:`),
  hitting a local throwaway test server that can be forced to succeed/fail/timeout
  on command. Assert the `DOWN` transition after `failure_threshold` consecutive
  failures, and recovery to `UP` on the next success.
- Frameworks: JUnit 5 (Java), `testing`+`httptest` (Go), `pytest`+`pytest-asyncio`
  (Python), `vitest` or `jest`+`supertest` (TypeScript).

## Docker

Multi-stage builds required in all four:
- Java: Maven/Gradle build stage → run on a JRE (not JDK) base image
- Go: build stage → `distroless` or `scratch` static binary
- Python: pip install stage → slim runtime, non-root user
- TypeScript: `npm ci && npm run build` stage → `node:slim` runtime, prod deps + `dist/` only

## What NOT to do

- Do not suggest replacing a language's chosen framework with a "more popular"
  alternative (e.g. don't suggest Spring Boot for Java, NestJS for TypeScript,
  Gin for Go) — that defeats the learning goal, see the table above.
- Do not port code shape directly across languages (e.g. don't wrap Go error
  returns in a try/catch just because the TypeScript version uses one).
- Do not add ORMs (no Hibernate/GORM/SQLAlchemy/Prisma) — raw SQL against
  `schema.sql` is intentional so each driver's idiom is visible.
- Do not silently swap `MAX_CONCURRENT_CHECKS` semantics between languages — it
  must mean "max concurrent in-flight HTTP checks" everywhere, not "max monitors"
  or "thread pool size."
- Do not add authentication/authorization — out of scope for this exercise.
