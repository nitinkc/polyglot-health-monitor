package com.nitin.monitor;

import com.nitin.monitor.dto.CheckResult;
import com.nitin.monitor.dto.Monitor;
import com.nitin.monitor.repo.CheckResultRepository;
import com.nitin.monitor.repo.MonitorRepository;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.Executors;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Semaphore;
import java.util.concurrent.TimeUnit;

/**
 * One virtual thread per monitor, looping on its own interval.
 * Global Semaphore bounds concurrent in-flight HTTP checks.
 * Every check result is persisted, and the monitor's status/consecutive_failures
 * are updated per the Section 4 transition rules.
 *
 * <p>Chaos-target acceptance criteria — every poll endpoint must classify
 * correctly (see docs/documentation/chaos-target.openapi.yaml):
 *
 * <pre>
 *   /health, /ok, /jitter      -&gt; success; record status_code + latency_ms
 *   /slow?ms=N                 -&gt; success only if response lands before
 *                                 timeout_ms (seeded monitor uses 5s vs 2s)
 *   /hang, mode=pause          -&gt; failure "timeout"; the request must be
 *                                 cancelled, not just abandoned client-side
 *   /error/{429,500,502,503}   -&gt; failure; status_code still recorded
 *   /reset                     -&gt; failure "connection_reset", null status_code
 *   /eof                       -&gt; failure "unexpected_eof" — BodyHandlers.
 *                                 discarding() already reads the body, so the
 *                                 truncation surfaces as IOException; classify it
 *   /malformed                 -&gt; failure "invalid_response"
 *   connection refused (crash) -&gt; failure "connection_refused"
 *   /flap, mode=flapping       -&gt; alternating results drive
 *                                 consecutive_failures and DOWN→UP transitions
 * </pre>
 */
public class Scheduler {
    // TODO: remove this auto-stop — the spec never stops polling a monitor.
    // Keeping it means /flap and the chaos-west sick pod stop being checked
    // after 5 failures, so DOWN→UP recovery transitions never get recorded.
    private static final int STOP_AFTER_CONSECUTIVE_FAILURES = 5;
    private final ExecutorService virtualThreadExecutor = Executors.newVirtualThreadPerTaskExecutor();
    private final Semaphore checkLimiter;
    private final HttpClient httpClient = HttpClient.newHttpClient();
    private final Map<String, Monitor> monitors = new ConcurrentHashMap<>();
    private final MonitorRepository monitorRepo;
    private final CheckResultRepository checkResultRepo;
    private volatile boolean running = true;

    public Scheduler(int maxConcurrentChecks, MonitorRepository monitorRepo,
                      CheckResultRepository checkResultRepo) {
        this.checkLimiter = new Semaphore(maxConcurrentChecks);
        this.monitorRepo = monitorRepo;
        this.checkResultRepo = checkResultRepo;
    }

    public void register(Monitor monitor) {
        monitors.put(monitor.id, monitor);
        virtualThreadExecutor.submit(() -> runLoop(monitor));
    }

    private void runLoop(Monitor monitor) {
    System.out.println("Starting check loop for monitor: " + monitor.id + " With Thread" + Thread.currentThread().getName() );
        while (running && monitors.containsKey(monitor.id)) {
            try {
                checkLimiter.acquire(); // bounded concurrency
                try {
                    performCheck(monitor);
                } finally {
                    checkLimiter.release();
                }
                Thread.sleep(Duration.ofSeconds(monitor.intervalSeconds));
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return; // shutting down
            }
        }
    }

    private void performCheck(Monitor monitor) {
        CheckResult result;
        try {
            System.out.println("Performing check for monitor: " + monitor.id + " With Thread : " + Thread.currentThread().getName() );
            // Call the Service
            HttpRequest request = HttpRequest.newBuilder()
                    .uri(URI.create(monitor.url))
                    .timeout(Duration.ofMillis(monitor.timeoutMs))
                    .GET()
                    .build();

            //Capture time duration
            long start = System.currentTimeMillis();
            HttpResponse<Void> response = httpClient.send(request, HttpResponse.BodyHandlers.discarding());
            long latency = System.currentTimeMillis() - start;

            boolean success = response.statusCode() < 400;
            // TODO: on 4xx/5xx (/error/*, mode=down) keep statusCode populated
            // in the CheckResult — failure() currently nulls it. Error string
            // e.g. "http_503". Consider recording latencyMs on HTTP failures too.
            result = success
                    ? CheckResult.success(monitor.id, response.statusCode(), (int) latency)
                    : CheckResult.failure(monitor.id, "http_status_" + response.statusCode());
             System.out.println("Result is " + result);
            monitor.recordResult(success);
        } catch (java.net.http.HttpTimeoutException e) {
            result = CheckResult.failure(monitor.id, "timeout");
            monitor.recordResult(false);
        } catch (Exception e) {
            // TODO: classify into short stable error strings:
            //   ConnectException        -> "connection_refused" (crashed/down pod)
            //   IOException "reset"     -> "connection_reset" (/reset, /eof tail)
            //   IOException "EOF"/"Premature EOF" -> "unexpected_eof" (/eof)
            //   unparseable response    -> "invalid_response" (/malformed)
            // CheckResult: statusCode=null, latencyMs=null per spec.
            result = CheckResult.failure(monitor.id, e.getClass().getSimpleName() + ": " + e.getMessage());
            monitor.recordResult(false);
        }

        checkResultRepo.insert(result);
        monitorRepo.updateStatus(monitor.id, monitor.status, monitor.consecutiveFailures);

        if (!result.success && monitor.consecutiveFailures >= STOP_AFTER_CONSECUTIVE_FAILURES) {
            if (monitors.remove(monitor.id) != null) {
                System.err.printf(
                        "{\"level\":\"warn\",\"event\":\"monitor_auto_stopped\",\"monitor_id\":\"%s\",\"consecutive_failures\":%d,\"reason\":\"too_many_failures\"}%n",
                        monitor.id,
                        monitor.consecutiveFailures);
            }
        }

        // Structured log line (Section 4.5).
        // TODO: swap hand-rolled printf for slf4j + logback JSON encoder —
        // required by spec, and raw messages break JSON when error contains quotes.
        System.out.printf(
                "{\"level\":\"info\",\"event\":\"check_completed\",\"monitor_id\":\"%s\",\"success\":%b,\"status\":\"%s\",\"error\":%s,\"ts\":\"%s\"}%n",
                monitor.id, result.success, monitor.status,
                result.error == null ? "null" : "\"" + result.error + "\"",
                result.checkedAt);
    }

    public void unregister(String monitorId) {
        monitors.remove(monitorId);
    }

    /** Stops accepting new checks and waits up to gracePeriodSeconds for in-flight checks to finish. */
    public void shutdown(int gracePeriodSeconds) {
        running = false;
        virtualThreadExecutor.shutdown();
        try {
            if (!virtualThreadExecutor.awaitTermination(gracePeriodSeconds, TimeUnit.SECONDS)) {
                virtualThreadExecutor.shutdownNow();
            }
        } catch (InterruptedException e) {
            virtualThreadExecutor.shutdownNow();
            Thread.currentThread().interrupt();
        }
    }
}
