package com.nitin.monitor;

import io.javalin.Javalin;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

public class Main {
    public static void main(String[] args) {
        int port = Integer.parseInt(System.getenv().getOrDefault("PORT", "8080"));
        int maxConcurrent = Integer.parseInt(System.getenv().getOrDefault("MAX_CONCURRENT_CHECKS", "10"));
        // TODO: fail fast if DB_PATH is missing, per Section 4.6

        Scheduler scheduler = new Scheduler(maxConcurrent);
        Map<String, Monitor> store = new ConcurrentHashMap<>(); // TODO: replace with SQLite-backed repo

        Javalin app = Javalin.create().start(port);

        app.get("/", ctx -> ctx.result("Hello, world!"));

        app.get("/api/v1/healthz", ctx -> ctx.json(Map.of("status", "ok")));

        app.post("/api/v1/monitors", ctx -> {
            // TODO: parse body with Jackson, validate, return 400 on bad input
            Monitor m = Monitor.create("placeholder", "https://example.com", 30, 2000, 3);
            store.put(m.id, m);
            scheduler.register(m);
            ctx.status(201).json(m);
        });

        app.get("/api/v1/monitors", ctx -> ctx.json(List.copyOf(store.values())));

        app.get("/api/v1/monitors/{id}", ctx -> {
            Monitor m = store.get(ctx.pathParam("id"));
            if (m == null) { ctx.status(404).json(Map.of("error", "not found")); return; }
            ctx.json(m);
        });

        app.get("/api/v1/monitors/{id}/status", ctx -> {
            // TODO: join with check_results history, respect ?limit=
            ctx.json(Map.of("monitor", store.get(ctx.pathParam("id")), "history", List.of()));
        });

        app.delete("/api/v1/monitors/{id}", ctx -> {
            store.remove(ctx.pathParam("id"));
            scheduler.unregister(ctx.pathParam("id"));
            ctx.status(204);
        });

        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            scheduler.shutdown();
            app.stop();
        }));
    }
}
