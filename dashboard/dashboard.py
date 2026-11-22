# /// script
# requires-python = ">=3.11"
# dependencies = ["fastapi>=0.110", "uvicorn[standard]>=0.29", "httpx>=0.27"]
# ///
"""Polyglot Health Monitor — dashboard backend.

Serves the UI, manages the four language implementations as subprocesses,
probes each one's API against the shared spec, and reads monitor.db directly.

Run from the repo root:  uv run dashboard/dashboard.py
Then open http://localhost:9000
"""

import asyncio
import collections
import os
import signal
import sqlite3
import subprocess
import threading
import time
import uuid
from dataclasses import dataclass, field
from pathlib import Path

import httpx
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.responses import FileResponse

REPO_ROOT = Path(__file__).resolve().parent.parent
DASH_DIR = Path(__file__).resolve().parent
DB_PATH = os.environ.get("DB_PATH", str(REPO_ROOT / "monitor.db"))
DASH_PORT = int(os.environ.get("DASH_PORT", "9000"))
PROBE_TIMEOUT = 4.0
LOG_TAIL = 300


@dataclass
class Language:
    key: str
    name: str
    port: int
    cwd: Path
    cmd: list[str]
    extra_env: dict[str, str] = field(default_factory=dict)

    @property
    def base(self) -> str:
        return f"http://127.0.0.1:{self.port}"


def _port(name: str, default: int) -> int:
    return int(os.environ.get(f"PORT_{name.upper()}", str(default)))


LANGUAGES: dict[str, Language] = {
    "java": Language(
        key="java",
        name="Java (Javalin)",
        port=_port("java", 8081),
        cwd=REPO_ROOT,
        cmd=[
            "mvn", "-q", "-f", "implementations/java/pom.xml",
            "compile", "exec:java",
            "-Dexec.mainClass=com.nitin.monitor.Main",
        ],
    ),
    "go": Language(
        key="go",
        name="Go (net/http)",
        port=_port("go", 8082),
        cwd=REPO_ROOT / "implementations/go",
        cmd=["go", "run", "."],
    ),
    "python": Language(
        key="python",
        name="Python (FastAPI)",
        port=_port("python", 8083),
        cwd=REPO_ROOT / "implementations/python",
        cmd=[
            "uv", "run", "--with-requirements", "requirements.txt",
            "uvicorn", "main:app", "--host", "127.0.0.1",
            "--port", str(_port("python", 8083)),
        ],
    ),
    "typescript": Language(
        key="typescript",
        name="TypeScript (Express)",
        port=_port("typescript", 8084),
        cwd=REPO_ROOT / "implementations/typescript",
        # install deps on first run, then hand the process slot to tsx
        cmd=["/bin/sh", "-c", "[ -d node_modules ] || npm install; exec npm run dev"],
    ),
}

# every spawned server gets the shared DB path + its assigned port
for lang in LANGUAGES.values():
    lang.extra_env = {"PORT": str(lang.port), "DB_PATH": DB_PATH}


# ---------------------------------------------------------------------------
# Process manager
# ---------------------------------------------------------------------------

class ManagedProc:
    def __init__(self, proc: subprocess.Popen):
        self.proc = proc
        self.started_at = time.time()
        self.logs: collections.deque[str] = collections.deque(maxlen=2000)
        threading.Thread(target=self._drain, daemon=True).start()

    def _drain(self):
        assert self.proc.stdout is not None
        for line in self.proc.stdout:
            self.logs.append(f"{time.strftime('%H:%M:%S')}  {line.rstrip()}")


PROCS: dict[str, ManagedProc] = {}
PROCS_LOCK = threading.Lock()


def _proc_entry(lang_key: str) -> dict:
    with PROCS_LOCK:
        mp = PROCS.get(lang_key)
        if mp and mp.proc.poll() is not None:  # exited on its own
            mp = None
        if mp:
            return {"managed": True, "running": True, "pid": mp.proc.pid,
                    "uptime_s": round(time.time() - mp.started_at)}
    return {"managed": False, "running": False}


def start_server(lang_key: str) -> dict:
    lang = _lang(lang_key)
    with PROCS_LOCK:
        existing = PROCS.get(lang_key)
        if existing and existing.proc.poll() is None:
            return {"ok": False, "detail": f"already running (pid {existing.proc.pid})"}
        env = dict(os.environ, **lang.extra_env)
        try:
            proc = subprocess.Popen(
                lang.cmd, cwd=lang.cwd, env=env,
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                text=True, start_new_session=True,
            )
        except FileNotFoundError as e:
            return {"ok": False, "detail": f"command not found: {e}"}
        PROCS[lang_key] = ManagedProc(proc)
    return {"ok": True, "pid": proc.pid}


def stop_server(lang_key: str) -> dict:
    _lang(lang_key)
    with PROCS_LOCK:
        mp = PROCS.pop(lang_key, None)
    if not mp or mp.proc.poll() is not None:
        return {"ok": False, "detail": "no managed process running"}
    try:
        os.killpg(mp.proc.pid, signal.SIGTERM)
        mp.proc.wait(timeout=8)
    except (ProcessLookupError, subprocess.TimeoutExpired):
        try:
            os.killpg(mp.proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    return {"ok": True}


def get_logs(lang_key: str) -> dict:
    _lang(lang_key)
    with PROCS_LOCK:
        mp = PROCS.get(lang_key)
        lines = list(mp.logs)[-LOG_TAIL:] if mp else []
    return {"lines": lines}


# ---------------------------------------------------------------------------
# SQLite snapshot
# ---------------------------------------------------------------------------

def db_snapshot() -> dict:
    out: dict = {"path": DB_PATH, "exists": Path(DB_PATH).exists(),
                 "monitors": [], "recent_results": [], "counts": {}}
    if not out["exists"]:
        return out
    out["mtime"] = time.strftime(
        "%Y-%m-%d %H:%M:%S", time.localtime(Path(DB_PATH).stat().st_mtime))
    try:
        conn = sqlite3.connect(f"file:{DB_PATH}?mode=ro", uri=True, timeout=2)
        conn.row_factory = sqlite3.Row
        try:
            out["monitors"] = [dict(r) for r in conn.execute(
                "SELECT * FROM monitors ORDER BY created_at DESC")]
            out["recent_results"] = [dict(r) for r in conn.execute(
                "SELECT cr.*, m.name AS monitor_name FROM check_results cr "
                "LEFT JOIN monitors m ON m.id = cr.monitor_id "
                "ORDER BY cr.checked_at DESC LIMIT 50")]
            out["counts"] = dict(conn.execute(
                "SELECT (SELECT COUNT(*) FROM monitors) AS monitors, "
                "(SELECT COUNT(*) FROM check_results) AS check_results"
            ).fetchone())
        finally:
            conn.close()
    except sqlite3.Error as e:
        out["error"] = str(e)
    return out


def db_has_monitor(monitor_id: str) -> bool:
    try:
        conn = sqlite3.connect(f"file:{DB_PATH}?mode=ro", uri=True, timeout=2)
        try:
            row = conn.execute(
                "SELECT 1 FROM monitors WHERE id = ?", (monitor_id,)).fetchone()
            return row is not None
        finally:
            conn.close()
    except sqlite3.Error:
        return False


# ---------------------------------------------------------------------------
# Spec validation probes — one sequential suite per language
# ---------------------------------------------------------------------------

def _check(name: str, expected: str, actual: str, ok: bool | None,
           latency_ms: int | None) -> dict:
    return {"name": name, "expected": expected, "actual": actual,
            "result": "pass" if ok else ("skip" if ok is None else "fail"),
            "latency_ms": latency_ms}


async def _req(client: httpx.AsyncClient, method: str, path: str,
               **kw) -> tuple[int | None, dict | list | str | None, int]:
    t0 = time.perf_counter()
    try:
        r = await client.request(method, path, **kw)
    except httpx.HTTPError as e:
        return None, f"request failed: {e}", 0
    ms = round((time.perf_counter() - t0) * 1000)
    try:
        body: dict | list | str | None = r.json()
    except ValueError:
        body = r.text[:200]
    return r.status_code, body, ms


def _code(status: int | None, body) -> str:
    if status is None:
        return str(body)[:80]
    return f"HTTP {status}"


async def validate_language(lang: Language) -> dict:
    """Run the spec checklist against a live server. Mutating probes create and
    then delete a probe monitor; the persistence check peeks at monitor.db while
    the monitor exists."""
    checks: list[dict] = []
    base = f"{lang.base}/api/v1"
    bogus = str(uuid.uuid4())
    probe = {
        "name": f"dashboard-probe-{lang.key}",
        "url": "https://example.com/health",
        "interval_seconds": 30,
        "timeout_ms": 2000,
        "failure_threshold": 3,
    }

    async with httpx.AsyncClient(base_url=base, timeout=PROBE_TIMEOUT) as client:
        status, body, ms = await _req(client, "GET", "/healthz")
        up = status == 200
        checks.append(_check(
            "GET /healthz → 200 status ok", "200 + {status: ok}",
            _code(status, body),
            up and isinstance(body, dict) and body.get("status") == "ok", ms))
        if not up:
            return {"up": False, "checks": checks,
                    "passed": sum(c["result"] == "pass" for c in checks),
                    "total": len(CHECKLIST), "skipped": len(CHECKLIST) - 1}

        monitor_id = None
        st, body, ms = await _req(client, "POST", "/monitors", json=probe)
        ok = st == 201 and isinstance(body, dict) and bool(body.get("id"))
        if ok:
            monitor_id = body["id"]
        checks.append(_check("POST /monitors (valid) → 201 + id", "201",
                             _code(st, body), ok, ms))

        st, body, ms = await _req(client, "POST", "/monitors",
                                 json={**probe, "interval_seconds": 1})
        checks.append(_check("POST /monitors (invalid) → 400", "400",
                             _code(st, body), st == 400, ms))

        if monitor_id:
            st, body, ms = await _req(client, "GET", "/monitors")
            found = (st == 200 and isinstance(body, list)
                     and any(m.get("id") == monitor_id for m in body
                             if isinstance(m, dict)))
            checks.append(_check("GET /monitors → list contains new id",
                                 "200 + id present", _code(st, body), found, ms))

            st, body, ms = await _req(client, "GET", f"/monitors/{monitor_id}")
            ok = (st == 200 and isinstance(body, dict)
                  and body.get("name") == probe["name"])
            checks.append(_check("GET /monitors/{id} → 200 + fields", "200",
                                 _code(st, body), ok, ms))

            st, body, ms = await _req(
                client, "GET", f"/monitors/{monitor_id}/status?limit=5")
            ok = (st == 200 and isinstance(body, dict)
                  and "monitor" in body and isinstance(body.get("history"), list)
                  and len(body["history"]) <= 5)
            checks.append(_check(
                "GET /monitors/{id}/status?limit=5 → monitor + history",
                "200 + history ≤ 5", _code(st, body), ok, ms))

            persisted = await asyncio.to_thread(db_has_monitor, monitor_id)
            checks.append(_check("DB persistence (row in monitor.db)",
                                 "monitor.id present in monitors table",
                                 "found" if persisted else "not in DB",
                                 persisted, None))

            st, body, ms = await _req(client, "DELETE",
                                      f"/monitors/{monitor_id}")
            checks.append(_check("DELETE /monitors/{id} → 204", "204",
                                 _code(st, body), st == 204, ms))

            st, body, ms = await _req(client, "GET", f"/monitors/{monitor_id}")
            checks.append(_check("GET deleted id → 404", "404",
                                 _code(st, body), st == 404, ms))
        else:
            for name in ("GET /monitors list", "GET /monitors/{id}",
                         "GET /monitors/{id}/status", "DB persistence",
                         "DELETE /monitors/{id}", "GET deleted → 404"):
                checks.append(_check(name, "—", "skipped (create failed)",
                                     None, None))

        st, body, ms = await _req(client, "GET", f"/monitors/{bogus}")
        checks.append(_check("GET /monitors/{bogus} → 404", "404",
                             _code(st, body), st == 404, ms))

        st, body, ms = await _req(client, "GET", f"/monitors/{bogus}/status")
        checks.append(_check("GET /monitors/{bogus}/status → 404", "404",
                             _code(st, body), st == 404, ms))

    passed = sum(c["result"] == "pass" for c in checks)
    skipped = sum(c["result"] == "skip" for c in checks)
    return {"up": True, "checks": checks, "passed": passed,
            "total": len(checks), "skipped": skipped}


# checklist size shown when a server is down (1 run + rest marked skipped)
CHECKLIST = ["healthz", "create", "create_invalid", "list", "get", "status",
             "db_persist", "delete", "get_deleted", "get_404", "status_404"]


# ---------------------------------------------------------------------------
# FastAPI app
# ---------------------------------------------------------------------------

app = FastAPI(title="Polyglot Health Monitor Dashboard")


def _lang(lang_key: str) -> Language:
    if lang_key not in LANGUAGES:
        raise HTTPException(404, f"unknown language '{lang_key}'")
    return LANGUAGES[lang_key]


@app.get("/", include_in_schema=False)
def index():
    return FileResponse(DASH_DIR / "index.html")


@app.get("/api/dashboard")
async def dashboard():
    probes = await asyncio.gather(
        *(validate_language(l) for l in LANGUAGES.values()))
    langs = []
    for lang, probe in zip(LANGUAGES.values(), probes):
        proc = _proc_entry(lang.key)
        langs.append({
            "key": lang.key, "name": lang.name, "port": lang.port,
            "managed": proc["managed"], "proc_running": proc["running"],
            "pid": proc.get("pid"), "uptime_s": proc.get("uptime_s"),
            "external": probe["up"] and not proc["running"],
            "probe": probe,
        })
    return {"generated_at": time.strftime("%Y-%m-%d %H:%M:%S"),
            "db": db_snapshot(), "languages": langs}


@app.post("/api/servers/{lang_key}/start")
def api_start(lang_key: str):
    return start_server(lang_key)


@app.post("/api/servers/{lang_key}/stop")
def api_stop(lang_key: str):
    return stop_server(lang_key)


@app.post("/api/servers/{lang_key}/restart")
def api_restart(lang_key: str):
    _lang(lang_key)
    stop_server(lang_key)
    return start_server(lang_key)


@app.get("/api/servers/{lang_key}/logs")
def api_logs(lang_key: str):
    return get_logs(lang_key)


if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=DASH_PORT, log_level="warning")
