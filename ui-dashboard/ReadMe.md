# UI Dashboard

One place to run all four health-monitor implementations, validate each
against the shared API spec (`docs/documentation/openapi.yaml`), and watch
`monitor.db` — no manual `curl` / `sqlite3` lookups.

## Run

```shell
# from the repo root:
uv run ui-dashboard/dashboard.py

# or from inside this folder:
cd ui-dashboard && uv run dashboard.py
```

Then open <http://localhost:9000>.

No install step needed — `uv` resolves the backend deps (FastAPI, uvicorn,
httpx) from the inline PEP 723 header. The UI is React + TypeScript compiled
in the browser by Babel standalone — no build, and React/Babel are vendored
in `vendor/` so the page works fully offline.

Requires the usual toolchains on `PATH` only if you want the dashboard to
spawn servers for you: `mvn` (Java), `go`, `uv`, `npm`/`node`.

## What it does

- **4 language cards** — one per implementation. Each shows:
  - API up/down, managed PID or "external process" (something else already
    owns the port), live logs
  - An 11-check spec matrix: `healthz`, monitor CRUD, `?limit` handling,
    404 behavior, and **DB persistence** (does the created monitor actually
    land in `monitor.db`, or is the impl still in-memory?)
  - Start / Stop / Restart buttons
- **DB panels** — `monitors` table + latest 50 `check_results`, read
  straight from `monitor.db` (read-only connection)
- Auto-refreshes every 3s — as an implementation gets completed, its card
  turns green.

The probe creates and then deletes a `dashboard-probe-<lang>` monitor on
each poll cycle, so it exercises the full lifecycle.

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `DASH_PORT` | `9000` | dashboard's own port |
| `DB_PATH` | `<repo>/monitor.db` | SQLite file to display + check |
| `PORT_JAVA` | `8081` | port the managed Java server binds |
| `PORT_GO` | `8082` | port the managed Go server binds |
| `PORT_PYTHON` | `8083` | port the managed Python server binds |
| `PORT_TYPESCRIPT` | `8084` | port the managed TypeScript server binds |

Every spawned server is started with `PORT` + `DB_PATH` in its environment,
matching Section 4.6 of the spec.

## Files

- `dashboard.py` — FastAPI backend: process manager, spec-probe suite,
  SQLite snapshot, serves the UI
- `index.html` — page shell, styles, error trap
- `app.tsx` — the entire React frontend; fetched by Babel standalone via
  `data-src` (a real `.tsx` filename is required for TSX parsing)
- `vendor/` — react, react-dom, babel-standalone, served locally
