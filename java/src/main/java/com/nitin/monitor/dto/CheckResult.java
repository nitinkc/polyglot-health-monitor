package com.nitin.monitor.dto;

import java.time.Instant;
import java.util.UUID;

public class CheckResult {
    public String id;
    public String monitorId;
    public Instant checkedAt;
    public boolean success;
    public Integer statusCode; // nullable
    public Integer latencyMs;  // nullable
    public String error;       // nullable

    public static CheckResult success(String monitorId, int statusCode, int latencyMs) {
        CheckResult r = new CheckResult();
        r.id = UUID.randomUUID().toString();
        r.monitorId = monitorId;
        r.checkedAt = Instant.now();
        r.success = true;
        r.statusCode = statusCode;
        r.latencyMs = latencyMs;
        return r;
    }

    public static CheckResult failure(String monitorId, String error) {
        CheckResult r = new CheckResult();
        r.id = UUID.randomUUID().toString();
        r.monitorId = monitorId;
        r.checkedAt = Instant.now();
        r.success = false;
        r.error = error;
        return r;
    }
}
