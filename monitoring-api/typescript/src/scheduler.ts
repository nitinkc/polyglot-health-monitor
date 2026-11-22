import pLimit from "p-limit";
import pino from "pino";
import type { Monitor } from "./monitor.js";

const log = pino();

/**
 * One setInterval-driven loop per monitor. p-limit bounds concurrent
 * in-flight HTTP checks (Section 4.2) since Node has no real parallelism —
 * reason explicitly about why you still need this given a single-threaded
 * event loop (I/O concurrency, not CPU parallelism).
 *
 * TODO: persist CheckResult rows instead of just logging — monitors must keep
 * polling through flap cycles so GET /status shows real DOWN→UP history
 * (chaos-target /flap and the west sick-pod fixture depend on this).
 * TODO: graceful shutdown — clear all timers, await in-flight checks.
 *
 * Chaos-target acceptance criteria — every poll endpoint must classify
 * correctly (see docs/documentation/chaos-target.openapi.yaml):
 *
 *   /health, /ok, /jitter      -> success; record status_code + latency_ms
 *   /slow?ms=N                 -> success only if response lands before
 *                                 timeout_ms (seeded monitor uses 5s vs 2s)
 *   /hang, mode=pause          -> failure "timeout"; the request must be
 *                                 aborted, not just abandoned client-side
 *   /error/{429,500,502,503}   -> failure; status_code still recorded
 *   /reset                     -> failure "connection_reset", null status_code
 *   /eof                       -> failure "unexpected_eof" — only detectable if
 *                                 the response BODY is consumed, not just headers
 *   /malformed                 -> failure "invalid_response"
 *   connection refused (crash) -> failure "connection_refused"
 *   /flap, mode=flapping       -> alternating results drive
 *                                 consecutive_failures and DOWN→UP transitions
 */
export class Scheduler {
  private limit: ReturnType<typeof pLimit>;
  private timers = new Map<string, NodeJS.Timeout>();

  constructor(maxConcurrentChecks: number) {
    this.limit = pLimit(maxConcurrentChecks);
  }

  register(monitor: Monitor): void {
    const timer = setInterval(() => {
      // limit() queues the check if we're already at max concurrency.
      // TODO: skip or drop queued checks when shutting down / when the monitor
      // was unregistered, so a stale tick can't fire after delete.
      void this.limit(() => this.performCheck(monitor));
    }, monitor.interval_seconds * 1000);
    this.timers.set(monitor.id, timer);
  }

  private async performCheck(monitor: Monitor): Promise<void> {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), monitor.timeout_ms);
    const start = Date.now();

    try {
      const res = await fetch(monitor.url, { signal: controller.signal });
      // TODO: consume the body (await res.arrayBuffer()/text()) before judging
      // success — fetch resolves on headers, so /eof's truncated body would
      // otherwise be a false success. A body-read failure is a check failure.
      const latencyMs = Date.now() - start;
      const success = res.status < 400;
      // TODO: on 4xx/5xx (/error/*, mode=down) persist CheckResult with
      // status_code populated and error="http_<code>" — failure rows
      // currently lose the status code.
      // TODO: recordResult(monitor, success), persist CheckResult, update the
      // monitors row (status, consecutive_failures, updated_at).
      log.info({ monitor_id: monitor.id, status_code: res.status, latency_ms: latencyMs, success });
    } catch (err) {
      if (controller.signal.aborted) {
        // TODO: record CheckResult(error="timeout", status_code=null,
        // latency_ms=null) — covers /hang, /slow beyond timeout_ms, pause.
        log.warn({ monitor_id: monitor.id }, "check_timeout");
      } else {
        // TODO: classify into short stable error strings (inspect err.cause):
        //   ECONNREFUSED            -> "connection_refused" (crashed/down pod)
        //   ECONNRESET / aborted read -> "connection_reset" (/reset, /eof tail)
        //   undici truncated/body error -> "unexpected_eof" (/eof)
        //   response parse failure  -> "invalid_response" (/malformed)
        // CheckResult: success=false, status_code=null, latency_ms=null.
        log.warn({ monitor_id: monitor.id, error: String(err) }, "check_failed");
      }
    } finally {
      clearTimeout(timeoutId);
    }
  }

  unregister(monitorId: string): void {
    const timer = this.timers.get(monitorId);
    if (timer) {
      clearInterval(timer);
      this.timers.delete(monitorId);
    }
  }

  shutdown(): void {
    for (const timer of this.timers.values()) clearInterval(timer);
    this.timers.clear();
    // TODO: await any in-flight this.limit() calls before resolving
  }
}
