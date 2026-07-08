import os
from contextlib import asynccontextmanager
from fastapi import FastAPI, HTTPException
from models import Monitor, MonitorCreateRequest
from scheduler import Scheduler

# TODO: fail fast if DB_PATH is missing, per Section 4.6
MAX_CONCURRENT_CHECKS = int(os.environ.get("MAX_CONCURRENT_CHECKS", "10"))

scheduler = Scheduler(MAX_CONCURRENT_CHECKS)
store: dict[str, Monitor] = {}  # TODO: replace with aiosqlite-backed repo


@asynccontextmanager
async def lifespan(app: FastAPI):
    yield
    # Graceful shutdown (Section 4.4)
    await scheduler.shutdown()


app = FastAPI(lifespan=lifespan)

@app.get("/")
async def root():
    return {"message": "Hello, World!"}

@app.get("/api/v1/healthz")
async def healthz():
    return {"status": "ok"}


@app.post("/api/v1/monitors", status_code=201)
async def create_monitor(req: MonitorCreateRequest):
    monitor = Monitor.create(req)
    store[monitor.id] = monitor
    scheduler.register(monitor)
    return monitor


@app.get("/api/v1/monitors")
async def list_monitors():
    return list(store.values())


@app.get("/api/v1/monitors/{monitor_id}")
async def get_monitor(monitor_id: str):
    monitor = store.get(monitor_id)
    if not monitor:
        raise HTTPException(status_code=404, detail="not found")
    return monitor


@app.get("/api/v1/monitors/{monitor_id}/status")
async def get_monitor_status(monitor_id: str, limit: int = 20):
    # TODO: join with check_results history, respect limit (max 100)
    monitor = store.get(monitor_id)
    if not monitor:
        raise HTTPException(status_code=404, detail="not found")
    return {"monitor": monitor, "history": []}


@app.delete("/api/v1/monitors/{monitor_id}", status_code=204)
async def delete_monitor(monitor_id: str):
    store.pop(monitor_id, None)
    scheduler.unregister(monitor_id)


# Run with: uvicorn main:app --host 0.0.0.0 --port ${PORT:-8080}
