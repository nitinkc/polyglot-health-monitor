import asyncio
import time
import httpx
import structlog
from models import Monitor

log = structlog.get_logger()


class Scheduler:
    """
    One asyncio.Task per monitor, looping on its own interval.
    asyncio.Semaphore bounds concurrent in-flight HTTP checks (Section 4.2).

    TODO: persist CheckResult rows instead of just logging
    TODO: graceful shutdown — cancel all tasks, await them with a grace period
    """

    def __init__(self, max_concurrent_checks: int):
        self._semaphore = asyncio.Semaphore(max_concurrent_checks)
        self._tasks: dict[str, asyncio.Task] = {}
        self._client = httpx.AsyncClient()

    def register(self, monitor: Monitor) -> None:
        task = asyncio.create_task(self._run_loop(monitor))
        self._tasks[monitor.id] = task

    async def _run_loop(self, monitor: Monitor) -> None:
        try:
            while True:
                await asyncio.sleep(monitor.interval_seconds)
                async with self._semaphore:  # acquire/release bounded concurrency
                    await self._perform_check(monitor)
        except asyncio.CancelledError:
            # expected on unregister/shutdown — don't let it crash the scheduler
            raise

    async def _perform_check(self, monitor: Monitor) -> None:
        start = time.monotonic()
        try:
            resp = await asyncio.wait_for(
                self._client.get(monitor.url),
                timeout=monitor.timeout_ms / 1000,
            )
            latency_ms = int((time.monotonic() - start) * 1000)
            success = resp.status_code < 400
            # TODO: monitor.record_result(success) per Section 4 status rules
            await log.ainfo(
                "check_completed",
                monitor_id=monitor.id,
                status_code=resp.status_code,
                latency_ms=latency_ms,
                success=success,
            )
        except asyncio.TimeoutError:
            # TODO: record CheckResult with error="timeout"
            await log.awarning("check_timeout", monitor_id=monitor.id)
        except httpx.HTTPError as e:
            # TODO: record CheckResult with error=str(e)
            await log.awarning("check_failed", monitor_id=monitor.id, error=str(e))

    def unregister(self, monitor_id: str) -> None:
        task = self._tasks.pop(monitor_id, None)
        if task:
            task.cancel()

    async def shutdown(self) -> None:
        for task in self._tasks.values():
            task.cancel()
        await asyncio.gather(*self._tasks.values(), return_exceptions=True)
        await self._client.aclose()
