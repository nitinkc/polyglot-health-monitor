package com.nitin.monitor;

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
 */
public class Scheduler {
    private final ExecutorService virtualThreadExecutor =
            Executors.newVirtualThreadPerTaskExecutor();
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
            HttpRequest request = HttpRequest.newBuilder()
                    .uri(URI.create(monitor.url))
                    .timeout(Duration.ofMillis(monitor.timeoutMs))
                    .GET()
                    .build();

            long start = System.currentTimeMillis();
            HttpResponse<Void> response = httpClient.send(request, HttpResponse.BodyHandlers.discarding());
            long latency = System.currentTimeMillis() - start;

            boolean success = response.statusCode() < 400;
            result = success
                    ? CheckResult.success(monitor.id, response.statusCode(), (int) latency)
                    : CheckResult.failure(monitor.id, "http_status_" + response.statusCode());

            monitor.recordResult(success);
        } catch (java.net.http.HttpTimeoutException e) {
            result = CheckResult.failure(monitor.id, "timeout");
            monitor.recordResult(false);
        } catch (Exception e) {
            result = CheckResult.failure(monitor.id, e.getClass().getSimpleName() + ": " + e.getMessage());
            monitor.recordResult(false);
        }

        checkResultRepo.insert(result);
        monitorRepo.updateStatus(monitor.id, monitor.status, monitor.consecutiveFailures);

        // Structured log line (Section 4.5). Swap for slf4j+logback JSON encoder in production.
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
