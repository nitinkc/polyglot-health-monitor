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

/**
 * One virtual thread per monitor, looping on its own interval.
 * Global Semaphore bounds concurrent in-flight HTTP checks.
 *
 * TODO: graceful shutdown (stop loops on SIGTERM, let in-flight finish)
 * TODO: persist CheckResult rows instead of just printing
 * TODO: structured JSON logging instead of System.out
 */
public class Scheduler {
    private final ExecutorService virtualThreadExecutor =
            Executors.newVirtualThreadPerTaskExecutor();
    private final Semaphore checkLimiter;
    private final HttpClient httpClient = HttpClient.newHttpClient();
    private final Map<String, Monitor> monitors = new ConcurrentHashMap<>();
    private volatile boolean running = true;

    public Scheduler(int maxConcurrentChecks) {
        this.checkLimiter = new Semaphore(maxConcurrentChecks);
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
            // TODO: update monitor.consecutiveFailures / status per Section 4 rules
            System.out.printf("{\"monitor_id\":\"%s\",\"status_code\":%d,\"latency_ms\":%d,\"success\":%b}%n",
                    monitor.id, response.statusCode(), latency, success);
        } catch (Exception e) {
            // TODO: distinguish timeout vs connection failure vs other; record CheckResult with error
            System.out.printf("{\"monitor_id\":\"%s\",\"error\":\"%s\"}%n", monitor.id, e.getMessage());
        }
    }

    public void unregister(String monitorId) {
        monitors.remove(monitorId);
    }

    public void shutdown() {
        running = false;
        virtualThreadExecutor.shutdown();
        // TODO: awaitTermination with a grace period, then shutdownNow()
    }
}
