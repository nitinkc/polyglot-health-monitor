package com.nitin.monitor;

import java.time.Instant;
import java.util.UUID;

public class Monitor {
    public String id;
    public String name;
    public String url;
    public int intervalSeconds;
    public int timeoutMs;
    public int failureThreshold;
    public String status = "UNKNOWN"; // UNKNOWN | UP | DOWN
    public int consecutiveFailures = 0;
    public Instant createdAt;
    public Instant updatedAt;

    public static Monitor create(String name, String url, int intervalSeconds,
                                  int timeoutMs, int failureThreshold) {
        Monitor m = new Monitor();
        m.id = UUID.randomUUID().toString();
        m.name = name;
        m.url = url;
        m.intervalSeconds = intervalSeconds;
        m.timeoutMs = timeoutMs;
        m.failureThreshold = failureThreshold;
        m.createdAt = Instant.now();
        m.updatedAt = Instant.now();
        return m;
    }

    /** Throws IllegalArgumentException with a message suitable for a 400 response. */
    public static void validate(String url, int intervalSeconds, int timeoutMs, int failureThreshold) {
        try {
            java.net.URI uri = java.net.URI.create(url);
            if (uri.getScheme() == null || !(uri.getScheme().equals("http") || uri.getScheme().equals("https"))) {
                throw new IllegalArgumentException("url must be http or https");
            }
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("url is not a valid URL: " + url);
        }
        if (intervalSeconds < 5) throw new IllegalArgumentException("interval_seconds must be >= 5");
        if (timeoutMs < 100) throw new IllegalArgumentException("timeout_ms must be >= 100");
        if (failureThreshold < 1) throw new IllegalArgumentException("failure_threshold must be >= 1");
    }

    /** Applies Section 4 status-transition rules given the outcome of one check. */
    public void recordResult(boolean success) {
        if (success) {
            consecutiveFailures = 0;
            status = "UP";
        } else {
            consecutiveFailures += 1;
            if (consecutiveFailures >= failureThreshold) {
                status = "DOWN";
            }
            // else: stays in current status (UNKNOWN or UP) until threshold is hit
        }
        updatedAt = Instant.now();
    }
}
