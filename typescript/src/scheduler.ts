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
 * TODO: persist CheckResult rows instead of just logging
 * TODO: graceful shutdown — clear all timers, await in-flight checks
 */
export class Scheduler {
  private limit: ReturnType<typeof pLimit>;
  private timers = new Map<string, NodeJS.Timeout>();

  constructor(maxConcurrentChecks: number) {
    this.limit = pLimit(maxConcurrentChecks);
  }

  register(monitor: Monitor): void {
    const timer = setInterval(() => {
      // limit() queues the check if we're already at max concurrency
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
      const latencyMs = Date.now() - start;
      const success = res.status < 400;
      // TODO: recordResult(monitor, success) per Section 4 status rules
      log.info({ monitor_id: monitor.id, status_code: res.status, latency_ms: latencyMs, success });
    } catch (err) {
      if (controller.signal.aborted) {
        // TODO: record CheckResult with error="timeout"
        log.warn({ monitor_id: monitor.id }, "check_timeout");
      } else {
        // TODO: record CheckResult with error=String(err)
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
