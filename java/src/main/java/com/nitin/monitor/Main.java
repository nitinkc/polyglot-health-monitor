package com.nitin.monitor;

import com.fasterxml.jackson.databind.ObjectMapper;
import io.javalin.Javalin;
import io.javalin.http.BadRequestResponse;
import io.javalin.http.NotFoundResponse;
import java.sql.Connection;
import java.util.List;
import java.util.Map;
import java.util.Optional;

public class Main {
    public static void main(String[] args) {
        int port = Integer.parseInt(System.getenv().getOrDefault("PORT", "8080"));
        int maxConcurrent = Integer.parseInt(System.getenv().getOrDefault("MAX_CONCURRENT_CHECKS", "10"));

        // fail fast if DB_PATH is missing, per Section 4.6
        String dbPath = System.getenv("DB_PATH");
        if (dbPath == null || dbPath.isBlank()) {
            System.err.println("FATAL: DB_PATH environment variable is required");
            System.exit(1);
        }

        Connection conn = Database.connect(dbPath);
        MonitorRepository monitorRepo = new MonitorRepository(conn);
        CheckResultRepository checkResultRepo = new CheckResultRepository(conn);
        Scheduler scheduler = new Scheduler(maxConcurrent, monitorRepo, checkResultRepo);
        ObjectMapper mapper = new ObjectMapper();

        // Re-register any monitors already in the DB (server restart case)
        for (Monitor m : monitorRepo.findAll()) {
            scheduler.register(m);
        }

        Javalin app = Javalin.create().start(port);

        app.get("/", ctx -> ctx.result("Hello, world!"));

        app.get("/api/v1/healthz", ctx -> ctx.json(Map.of("status", "ok")));

        app.post("/api/v1/monitors", ctx -> {
            //parse body with Jackson, validate, return 400 on bad input

            Map<String, Object> body = mapper.readValue(ctx.body(), Map.class);
            String name = (String) body.get("name");
            String url = (String) body.get("url");
            int interval = ((Number) body.get("interval_seconds")).intValue();
            int timeoutMs = ((Number) body.get("timeout_ms")).intValue();
            int threshold = ((Number) body.get("failure_threshold")).intValue();

            try {
                Monitor.validate(url, interval, timeoutMs, threshold);
            } catch (IllegalArgumentException e) {
                throw new BadRequestResponse(e.getMessage());
            }

            Monitor m = Monitor.create(name, url, interval, timeoutMs, threshold);
            monitorRepo.insert(m);
            scheduler.register(m);
            ctx.status(201).json(m);
        });

        app.get("/api/v1/monitors", ctx -> ctx.json(monitorRepo.findAll()));

        app.get("/api/v1/monitors/{id}", ctx -> {
            Monitor m = monitorRepo.findById(ctx.pathParam("id"))
                    .orElseThrow(() -> new NotFoundResponse("not found"));
            ctx.json(m);
        });

        app.get("/api/v1/monitors/{id}/status", ctx -> {
            String id = ctx.pathParam("id");
            Monitor m = monitorRepo.findById(id).orElseThrow(() -> new NotFoundResponse("not found"));
            int limit = Optional.ofNullable(ctx.queryParam("limit"))
                    .map(Integer::parseInt).orElse(20);
            limit = Math.min(Math.max(limit, 1), 100); // clamp per spec: default 20, max 100
            List<CheckResult> history = checkResultRepo.recentForMonitor(id, limit);
            ctx.json(Map.of("monitor", m, "history", history));
        });

        app.delete("/api/v1/monitors/{id}", ctx -> {
            String id = ctx.pathParam("id");
            scheduler.unregister(id);
            monitorRepo.delete(id); // cascades check_results via FK
            ctx.status(204);
        });

        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            scheduler.shutdown(10); // 10s grace period for in-flight checks
            app.stop();
            try {
                conn.close();
            } catch (Exception ignored) {
            }
        }));
    }
}
