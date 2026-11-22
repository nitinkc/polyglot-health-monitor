package com.nitin.monitor;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.nitin.monitor.dto.CheckResult;
import com.nitin.monitor.dto.Monitor;
import com.nitin.monitor.repo.CheckResultRepository;
import com.nitin.monitor.repo.MonitorRepository;
import io.javalin.Javalin;
import io.javalin.config.RoutesConfig;
import io.javalin.http.BadRequestResponse;
import io.javalin.http.NotFoundResponse;
import io.javalin.json.JavalinJackson;
import java.sql.Connection;
import java.util.List;
import java.util.Map;
import java.util.Optional;

public class Main {
    static void main(String[] ignoredArgs) {
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
        ObjectMapper mapper = buildObjectMapper();

        // Re-register any monitors already in the DB (server restart case)
        for (Monitor m : monitorRepo.findAll()) {
            scheduler.register(m);
        }

        Javalin app =
                Javalin.create(config -> {
                    config.jsonMapper(new JavalinJackson(mapper, false));
                    registerRoutes(config.routes, mapper, monitorRepo, checkResultRepo, scheduler);
                }).start(port);

        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            scheduler.shutdown(10); // 10s grace period for in-flight checks
            app.stop();
            try {
                conn.close();
            } catch (Exception ignored) {
            }
        }));
    }

    private static ObjectMapper buildObjectMapper() {
        ObjectMapper mapper = new ObjectMapper();
        mapper.registerModule(new JavaTimeModule());
        mapper.disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS);
        return mapper;
    }

    private static void registerRoutes(
            RoutesConfig routes,
            ObjectMapper mapper,
            MonitorRepository monitorRepo,
            CheckResultRepository checkResultRepo,
            Scheduler scheduler) {
        routes.get("/", ctx -> ctx.result("Hello, world!"));

        routes.get("/api/v1/healthz", ctx -> ctx.json(Map.of("status", "ok")));

        routes.post("/api/v1/monitors", ctx -> {
            Map<String, Object> body = mapper.readValue(ctx.body(), new TypeReference<>() {});
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

        routes.get("/api/v1/monitors", ctx -> ctx.json(monitorRepo.findAll()));

        routes.get("/api/v1/monitors/{id}", ctx -> {
            Monitor m = monitorRepo.findById(ctx.pathParam("id"))
                    .orElseThrow(() -> new NotFoundResponse("not found"));
            ctx.json(m);
        });

        routes.get("/api/v1/monitors/{id}/status", ctx -> {
            String id = ctx.pathParam("id");
            Monitor m = monitorRepo.findById(id).orElseThrow(() -> new NotFoundResponse("not found"));
            int limit = Optional.ofNullable(ctx.queryParam("limit"))
                    .map(Integer::parseInt).orElse(20);
            limit = Math.clamp(limit, 1, 100);
            List<CheckResult> history = checkResultRepo.recentForMonitor(id, limit);
            ctx.json(Map.of("monitor", m, "history", history));
        });

        routes.delete("/api/v1/monitors/{id}", ctx -> {
            String id = ctx.pathParam("id");
            scheduler.unregister(id);
            monitorRepo.delete(id); // cascades check_results via FK
            ctx.status(204);
        });
    }
}
