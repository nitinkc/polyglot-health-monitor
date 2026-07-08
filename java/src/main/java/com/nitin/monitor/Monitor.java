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

    // TODO: validation (URL parses, interval >= 5, timeout >= 100, threshold >= 1)
    // TODO: status transition logic — recordSuccess() / recordFailure()
}
