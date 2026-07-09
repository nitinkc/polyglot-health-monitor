#!/usr/bin/env bash
#
# Creates all labels + epic/story issues for the Polyglot Health Monitor project.
#
# Prereqs:
#   - GitHub CLI installed: https://cli.github.com
#   - Authenticated: gh auth login
#   - Run from inside the target repo (or pass --repo owner/name to gh, see below)
#
# Usage:
#   chmod +x create_issues.sh
#   ./create_issues.sh
#
# To target a specific repo instead of the current directory's repo, set:
#   REPO="owner/repo-name" ./create_issues.sh

set -euo pipefail

REPO="${REPO:-}"
REPO_FLAG=()
if [[ -n "$REPO" ]]; then
  REPO_FLAG=(--repo "$REPO")
fi

echo "==> Creating labels (safe to re-run; existing labels are skipped)"

create_label() {
  local name="$1" color="$2" desc="$3"
  if gh label list "${REPO_FLAG[@]}" --search "$name" | grep -qx -e "$name"$'\t'"$desc"$'\t'"$color" 2>/dev/null; then
    return
  fi
  gh label create "$name" --color "$color" --description "$desc" "${REPO_FLAG[@]}" --force >/dev/null 2>&1 || true
}

create_label "epic"          "5319E7" "Large body of work spanning multiple stories"
create_label "story"         "0E8A16" "User-facing unit of work with acceptance criteria"
create_label "task"          "BFD4F2" "Sub-task within a story"
create_label "java"          "B07219" "Java implementation"
create_label "go"            "00ADD8" "Go implementation"
create_label "python"        "3572A5" "Python implementation"
create_label "typescript"    "3178C6" "TypeScript implementation"
create_label "cross-cutting" "FBCA04" "Spans multiple implementations"
create_label "round-2"       "D93F0B" "Optional Postgres/Neon migration phase"

echo "==> Creating issues"

create_issue() {
  local title="$1" labels="$2"
  local body
  body="$(cat)"
  echo "  - $title"
  gh issue create "${REPO_FLAG[@]}" \
    --title "$title" \
    --label "$labels" \
    --body "$body" >/dev/null
}

# ---------------------------------------------------------------------------
# Epic 1: Shared Contract & Scaffolding
# ---------------------------------------------------------------------------

create_issue "Epic 1: Shared Contract & Scaffolding" "epic,cross-cutting" <<'EOF'
Establish the spec, schema, and API contract every implementation must satisfy before any language-specific work begins.

Stories: #Story 1.1
EOF

create_issue "Story 1.1: Finalize shared spec and schema" "story,cross-cutting" <<'EOF'
**As a** developer building four implementations,
**I want** one canonical spec, schema, and OpenAPI contract,
**so that** all four servers are provably interchangeable.

### Acceptance Criteria
- [ ] `spec.md` covers domain model, REST API, behavioral requirements, testing standards
- [ ] `schema.sql` defines `monitors` and `check_results` tables with correct constraints and FK cascade
- [ ] `openapi.yaml` matches `spec.md` exactly (status codes, field names, types)
- [ ] Reviewed for consistency: no field name or type mismatch between the three docs

### Testing
- [ ] `sqlite3 test.db < schema.sql` runs without error
- [ ] `openapi.yaml` validates with `npx @redocly/cli lint openapi.yaml` (or Swagger Editor) with zero errors

### Tasks
- [ ] Write `spec.md`
- [ ] Write `schema.sql`
- [ ] Write `openapi.yaml`
- [ ] Write `.github/copilot-instructions.md` (tech choices, non-negotiables)
EOF

# ---------------------------------------------------------------------------
# Epic 2: Java Reference Implementation (Javalin)
# ---------------------------------------------------------------------------

create_issue "Epic 2: Java Reference Implementation (Javalin)" "epic,java" <<'EOF'
The first fully-wired implementation; serves as the answer key for the other three.

Stories: #Story 2.1, #Story 2.2, #Story 2.3, #Story 2.4, #Story 2.5
EOF

create_issue "Story 2.1: Project scaffold and DB bootstrap (Java)" "story,java" <<'EOF'
**As a** developer, **I want** the Java project to boot, connect to SQLite, and apply the shared schema, **so that** I have a working base to build routes on.

### Acceptance Criteria
- [ ] `mvn compile` succeeds
- [ ] App fails fast with a clear error if `DB_PATH` is unset
- [ ] On startup, `schema.sql` is applied if tables don't exist
- [ ] `GET /api/v1/healthz` returns `200 {"status":"ok"}`

### Testing
- [ ] Unit test: startup throws if `DB_PATH` missing
- [ ] Manual: `curl localhost:8080/api/v1/healthz`

### Tasks
- [ ] `pom.xml` with Javalin, sqlite-jdbc, Jackson, slf4j
- [ ] `Database.java` — connect + apply schema
- [ ] `Main.java` — boot Javalin, wire `/healthz`
EOF

create_issue "Story 2.2: Monitor CRUD endpoints (Java)" "story,java" <<'EOF'
**As an** API consumer, **I want** to create, list, fetch, and delete monitors, **so that** I can manage what's being checked.

### Acceptance Criteria
- [ ] `POST /api/v1/monitors` validates input (URL scheme, interval >= 5, timeout >= 100, threshold >= 1); returns `201` + full Monitor, or `400` with `{"error": "..."}`
- [ ] `GET /api/v1/monitors` returns all monitors
- [ ] `GET /api/v1/monitors/{id}` returns `200` + Monitor, or `404`
- [ ] `DELETE /api/v1/monitors/{id}` returns `204`, cascades `check_results` deletion

### Testing
- [ ] Unit: `Monitor.validate()` rejects each invalid field individually
- [ ] Integration: create -> get -> delete -> get again returns 404
- [ ] Integration: deleting a monitor removes its `check_results` rows

### Tasks
- [ ] `Monitor.java` model + `validate()`
- [ ] `MonitorRepository.java` (insert/findById/findAll/delete)
- [ ] Wire routes in `Main.java`
EOF

create_issue "Story 2.3: Concurrent scheduler with bounded checks (Java)" "story,java" <<'EOF'
**As the** system, **I want** each monitor checked independently and concurrency-bounded, **so that** one slow monitor never blocks another and target hosts aren't overwhelmed.

### Acceptance Criteria
- [ ] One virtual thread per monitor; adding monitor #50 doesn't delay monitor #1's checks
- [ ] Global `Semaphore` bounds in-flight checks to `MAX_CONCURRENT_CHECKS`
- [ ] Each monitor's `timeout_ms` cancels the in-flight HTTP request at the transport level
- [ ] Every check result is persisted to `check_results`
- [ ] Monitor `status`/`consecutive_failures` update per transition rules

### Testing
- [ ] Integration: point a monitor at a local test server that always 500s; assert status flips to `DOWN` only after `failure_threshold` consecutive failures
- [ ] Integration: one success after failures resets `consecutive_failures` to 0 and flips back to `UP`
- [ ] Integration: monitor pointed at a server that sleeps longer than `timeout_ms` records a `"timeout"` error, not a hang
- [ ] Load test: register 50 monitors with `MAX_CONCURRENT_CHECKS=10`, confirm no more than 10 concurrent outbound requests

### Tasks
- [ ] `CheckResult.java` model
- [ ] `CheckResultRepository.java`
- [ ] `Scheduler.java` — virtual threads, semaphore, timeout, persistence, status update
EOF

create_issue "Story 2.4: History endpoint, graceful shutdown, structured logging (Java)" "story,java" <<'EOF'
**As an** operator, **I want** to see check history and know the server shuts down cleanly, **so that** I can trust it in production.

### Acceptance Criteria
- [ ] `GET /api/v1/monitors/{id}/status?limit=N` returns monitor + history, newest first, `limit` clamped 1-100, default 20
- [ ] `SIGTERM` stops new checks, lets in-flight checks finish (grace period, e.g. 10s), closes DB cleanly
- [ ] One structured JSON log line per completed check (`event: "check_completed"`)

### Testing
- [ ] Integration: `?limit=200` is clamped to 100 rows returned
- [ ] Manual: send `SIGTERM` mid-check, confirm no exception/stack trace on exit, DB connection closes
- [ ] Manual: log output is valid JSON per line

### Tasks
- [ ] Wire `/status` route with `CheckResultRepository.recentForMonitor()`
- [ ] Shutdown hook calling `Scheduler.shutdown(gracePeriodSeconds)`
- [ ] Swap `System.out.printf` for `slf4j` + JSON encoder
EOF

create_issue "Story 2.5: Java test suite + Dockerfile" "story,java" <<'EOF'
### Acceptance Criteria
- [ ] JUnit 5 unit tests for validation and status-transition logic (no I/O)
- [ ] One integration test spinning up the real server + a temp SQLite file + a local throwaway HTTP test server
- [ ] Multi-stage Dockerfile: Maven build stage -> JRE (not JDK) runtime stage

### Testing
- [ ] `mvn test` passes in CI
- [ ] `docker build` succeeds; resulting image runs `/healthz` successfully

### Tasks
- [ ] Unit tests
- [ ] Integration test
- [ ] `Dockerfile`
EOF

# ---------------------------------------------------------------------------
# Epic 3: Go Implementation (stdlib net/http)
# ---------------------------------------------------------------------------

create_issue "Epic 3: Go Implementation (stdlib net/http)" "epic,go" <<'EOF'
Mirror of Epic 2's stories, in Go idiom.

Stories: #Story 3.1, #Story 3.2, #Story 3.3, #Story 3.4, #Story 3.5
EOF

create_issue "Story 3.1: Scaffold, DB bootstrap, healthz (Go)" "story,go" <<'EOF'
### Acceptance Criteria
- [ ] `go run .` boots; fails fast (non-zero exit, clear stderr message) if `DB_PATH` unset
- [ ] Applies `schema.sql` on startup if tables don't exist
- [ ] `GET /api/v1/healthz` -> `200`
EOF

create_issue "Story 3.2: Monitor CRUD (Go)" "story,go" <<'EOF'
### Acceptance Criteria
- [ ] Same as Java Story 2.2, Go idiom: explicit `if err != nil` checks, errors wrapped with `%w`
- [ ] Validation errors return `400` with `{"error": "..."}`

### Testing
- [ ] `httptest`-based integration test for each endpoint
EOF

create_issue "Story 3.3: Concurrent scheduler with bounded checks (Go)" "story,go" <<'EOF'
### Acceptance Criteria
- [ ] One goroutine per monitor + `time.Ticker`
- [ ] `context.WithTimeout` per check, propagated from a cancellable parent context tied to shutdown
- [ ] Buffered channel (or `x/sync/semaphore`) bounds concurrent checks to `MAX_CONCURRENT_CHECKS`
- [ ] Status transition + persistence identical behavior to Java reference

### Testing
- [ ] Same status-transition and timeout tests as Java Story 2.3, adapted to Go's `httptest`
- [ ] Race detector clean: `go test -race ./...`
EOF

create_issue "Story 3.4: History endpoint, graceful shutdown, structured logging (Go)" "story,go" <<'EOF'
### Acceptance Criteria
- [ ] `/status?limit=` behavior matches spec
- [ ] `SIGTERM`/`SIGINT` cancels all monitor contexts, `sync.WaitGroup` waits (bounded by a shutdown timeout) before exit
- [ ] `log/slog` JSON output, one line per check

### Testing
- [ ] Manual: `kill -TERM` mid-check, confirm clean exit within grace period
EOF

create_issue "Story 3.5: Go test suite + Dockerfile" "story,go" <<'EOF'
### Acceptance Criteria
- [ ] Unit tests for validation/status-transition (table-driven, idiomatic Go style)
- [ ] Integration test via `httptest`
- [ ] Multi-stage Dockerfile -> static binary on `distroless` or `scratch`

### Testing
- [ ] `go test ./...` passes
- [ ] `docker build` succeeds, image size reasonably small (<20MB is a good sanity check for a static Go binary)
EOF

# ---------------------------------------------------------------------------
# Epic 4: Python Implementation (FastAPI + asyncio)
# ---------------------------------------------------------------------------

create_issue "Epic 4: Python Implementation (FastAPI + asyncio)" "epic,python" <<'EOF'
Stories: #Story 4.1, #Story 4.2, #Story 4.3, #Story 4.4, #Story 4.5
EOF

create_issue "Story 4.1: Scaffold, DB bootstrap, healthz (Python)" "story,python" <<'EOF'
### Acceptance Criteria
- [ ] `uvicorn main:app` boots; fails fast if `DB_PATH` unset
- [ ] Applies `schema.sql` via `aiosqlite` on startup if tables don't exist
- [ ] `GET /api/v1/healthz` -> `200`
EOF

create_issue "Story 4.2: Monitor CRUD with Pydantic validation (Python)" "story,python" <<'EOF'
### Acceptance Criteria
- [ ] `MonitorCreateRequest` Pydantic model enforces field constraints; FastAPI's `422` remapped to spec's `400` shape `{"error": "..."}` via exception handler
- [ ] CRUD routes match spec

### Testing
- [ ] `pytest` covering valid + each invalid field case
EOF

create_issue "Story 4.3: Concurrent scheduler with bounded checks (Python)" "story,python" <<'EOF'
### Acceptance Criteria
- [ ] One `asyncio.Task` per monitor
- [ ] `asyncio.Semaphore` bounds concurrent checks
- [ ] `asyncio.wait_for` enforces `timeout_ms`
- [ ] A single monitor's exception never crashes the scheduler loop or other monitors' tasks
- [ ] Status transition + persistence identical behavior to reference

### Testing
- [ ] `pytest-asyncio` test: one monitor's task raising an unexpected exception doesn't stop other monitors' loops
- [ ] Timeout test using a slow local test server
EOF

create_issue "Story 4.4: History endpoint, graceful shutdown, structured logging (Python)" "story,python" <<'EOF'
### Acceptance Criteria
- [ ] `/status?limit=` behavior matches spec
- [ ] FastAPI `lifespan` shutdown cancels all tasks, awaits with `return_exceptions=True`, closes DB pool
- [ ] `structlog` JSON output, one line per check

### Testing
- [ ] Integration: trigger app shutdown, confirm no `asyncio.CancelledError` traceback leaks to stdout uncaught
EOF

create_issue "Story 4.5: Python test suite + Dockerfile" "story,python" <<'EOF'
### Acceptance Criteria
- [ ] Unit tests for validation/status-transition
- [ ] Integration test with real `aiosqlite` temp DB + local test server
- [ ] Multi-stage Dockerfile -> slim runtime, non-root user

### Testing
- [ ] `pytest` passes
- [ ] `docker build`, container runs as non-root (`whoami` check inside container)
EOF

# ---------------------------------------------------------------------------
# Epic 5: TypeScript Implementation (Express)
# ---------------------------------------------------------------------------

create_issue "Epic 5: TypeScript Implementation (Express)" "epic,typescript" <<'EOF'
Stories: #Story 5.1, #Story 5.2, #Story 5.3, #Story 5.4, #Story 5.5
EOF

create_issue "Story 5.1: Scaffold, DB bootstrap, healthz (TypeScript)" "story,typescript" <<'EOF'
### Acceptance Criteria
- [ ] `npm run dev` boots; fails fast if `DB_PATH` unset
- [ ] Applies `schema.sql` via `better-sqlite3` on startup if tables don't exist
- [ ] `GET /api/v1/healthz` -> `200`
EOF

create_issue "Story 5.2: Monitor CRUD with zod validation (TypeScript)" "story,typescript" <<'EOF'
### Acceptance Criteria
- [ ] `MonitorCreateSchema` (zod) enforces field constraints; failed validation -> `400` `{"error": "..."}`
- [ ] CRUD routes match spec

### Testing
- [ ] `vitest`/`jest` + `supertest` covering valid + invalid input cases
EOF

create_issue "Story 5.3: Concurrent scheduling with bounded checks (TypeScript)" "story,typescript" <<'EOF'
### Acceptance Criteria
- [ ] Per-monitor timer independent of others
- [ ] `p-limit` (or hand-rolled queue) bounds concurrent in-flight checks
- [ ] `AbortController`/`AbortSignal.timeout()` cancels the fetch at `timeout_ms`
- [ ] Status transition + persistence identical behavior to reference
- [ ] A short written note (code comment or PR description) on why a concurrency bound is still needed despite no true parallelism

### Testing
- [ ] Test: N monitors registered, confirm no more than `MAX_CONCURRENT_CHECKS` concurrent outbound requests at once
- [ ] Timeout test using a slow local test server
EOF

create_issue "Story 5.4: History endpoint, graceful shutdown, structured logging (TypeScript)" "story,typescript" <<'EOF'
### Acceptance Criteria
- [ ] `/status?limit=` behavior matches spec
- [ ] `SIGTERM` clears all timers, awaits any in-flight `p-limit` calls, closes DB
- [ ] `pino` JSON output, one line per check

### Testing
- [ ] Manual: send SIGTERM mid-check, confirm clean process exit
EOF

create_issue "Story 5.5: TypeScript test suite + Dockerfile" "story,typescript" <<'EOF'
### Acceptance Criteria
- [ ] Unit tests for validation/status-transition
- [ ] Integration test via `supertest` + temp SQLite file
- [ ] Multi-stage Dockerfile: `npm ci && npm run build` -> `node:slim` runtime, prod deps + `dist/` only

### Testing
- [ ] `npm test` passes
- [ ] `docker build` succeeds
EOF

# ---------------------------------------------------------------------------
# Epic 6: Cross-Language Verification
# ---------------------------------------------------------------------------

create_issue "Epic 6: Cross-Language Verification" "epic,cross-cutting" <<'EOF'
Confirms the whole point of the project: interchangeable, contract-identical servers.

Stories: #Story 6.1, #Story 6.2
EOF

create_issue "Story 6.1: Cross-language interop smoke test" "story,cross-cutting" <<'EOF'
**As the** project owner, **I want** to prove all four servers are truly contract-identical, **so that** the "same behavior, different idiom" goal is verified, not assumed.

### Acceptance Criteria
- [ ] All four servers pointed at the same SQLite file
- [ ] A monitor created via the Go server is visible (identical JSON shape) via `GET /monitors` on Java, Python, and TypeScript servers
- [ ] Deleting via Python server removes it from all servers' views

### Testing
- [ ] Manual or scripted `curl` walkthrough across all four ports, documented with example requests/responses
EOF

create_issue "Story 6.2: Nuances comparison write-up" "story,cross-cutting" <<'EOF'
### Acceptance Criteria
- [ ] Short doc (or blog post) comparing how each language handled: concurrency bounding, timeout/cancellation, error propagation, graceful shutdown
- [ ] Written in your own words -- this is the actual interview-prep artifact

### Tasks
- [ ] Draft comparison doc
- [ ] Publish to Jekyll blog (optional)
EOF

# ---------------------------------------------------------------------------
# Epic 7 (Optional): Postgres/Neon Migration
# ---------------------------------------------------------------------------

create_issue "Epic 7: Postgres/Neon Migration (Round 2, optional)" "epic,round-2" <<'EOF'
See spec.md Section 8 for full detail. Optional fifth pass after all four local-SQLite builds work.

Stories: #Story 7.1, #Story 7.2
EOF

create_issue "Story 7.1: Schema + config migration to Postgres" "story,round-2" <<'EOF'
### Acceptance Criteria
- [ ] Postgres schema variant created (`UUID`, `TIMESTAMPTZ`, `gen_random_uuid()`)
- [ ] `DATABASE_URL` env var supported alongside existing `DB_PATH` fallback, behind a swappable data-layer interface/trait/protocol per language
EOF

create_issue "Story 7.2: Connection pooling + retry with backoff" "story,round-2" <<'EOF'
### Acceptance Criteria
- [ ] Each language uses its recommended driver (HikariCP/Java, pgx/Go, asyncpg/Python, pg or postgres.js/TypeScript)
- [ ] One retry with short exponential backoff on connection acquisition failure (not per-query)
- [ ] Retry logged as a structured `event: "db_connection_retry"` line

### Testing
- [ ] Simulate Neon cold-start delay (or a deliberately slow/blocked port) and confirm retry-then-succeed behavior, not a crash
EOF

echo "==> Done. $(gh issue list "${REPO_FLAG[@]}" --limit 100 --state all | wc -l) issues now in the repo (approx, includes pre-existing)."
