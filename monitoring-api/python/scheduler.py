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

    TODO: persist CheckResult rows instead of just logging — monitors must keep
    polling through flap cycles so GET /status shows real DOWN→UP history
    (chaos-target /flap and the west sick-pod fixture depend on this).
    TODO: graceful shutdown — cancel all tasks, await them with a grace period.

    Chaos-target acceptance criteria — every poll endpoint must classify
    correctly (see docs/documentation/chaos-target.openapi.yaml):

        /health, /ok, /jitter      -> success; record status_code + latency_ms
        /slow?ms=N                 -> success only if response lands before
                                      timeout_ms (seeded monitor uses 5s vs 2s)
        /hang, mode=pause          -> failure "timeout"; the request must be
                                      cancelled, not just abandoned client-side
        /error/{429,500,502,503}   -> failure; status_code still recorded
        /reset                     -> failure "connection_reset", null status_code
        /eof                       -> failure "unexpected_eof" — httpx reads the
                                      body eagerly so it raises; classify it
        /malformed                 -> failure "invalid_response"
        connection refused (crash) -> failure "connection_refused"
        /flap, mode=flapping       -> alternating results drive
                                      consecutive_failures and DOWN→UP transitions
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
            # TODO: on 4xx/5xx (/error/*, mode=down) persist CheckResult with
            # status_code populated and error="http_<code>" — failure rows
            # currently lose the status code.
            # TODO: monitor.record_result(success), persist CheckResult, update
            # the monitors row (status, consecutive_failures, updated_at).
            await log.ainfo(
                "check_completed",
                monitor_id=monitor.id,
                status_code=resp.status_code,
                latency_ms=latency_ms,
                success=success,
            )
        except asyncio.TimeoutError:
            # TODO: record CheckResult(error="timeout", status_code=None,
            # latency_ms=None) — covers /hang, /slow beyond timeout_ms, pause.
            # Verify wait_for cancellation actually aborts the httpx request
            # so repeated /hang polls can't leak connections.
            await log.awarning("check_timeout", monitor_id=monitor.id)
        except httpx.HTTPError as e:
            # TODO: classify into short stable error strings:
            #   httpx.ConnectError      -> "connection_refused" (crashed/down pod)
            #   httpx.RemoteProtocolError -> "unexpected_eof" or "connection_reset"
            #                                (/eof truncated body, /reset RST)
            #   other HTTPError          -> "invalid_response" (/malformed)
            # CheckResult: success=false, status_code=None, latency_ms=None.
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
