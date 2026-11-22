import express from "express";
import { createMonitor, MonitorCreateSchema, type Monitor } from "./monitor.js";
import { Scheduler } from "./scheduler.js";

// TODO: fail fast if DB_PATH is missing, per Section 4.6
const PORT = process.env.PORT ?? "8080";
const MAX_CONCURRENT_CHECKS = Number(process.env.MAX_CONCURRENT_CHECKS ?? "10");

const scheduler = new Scheduler(MAX_CONCURRENT_CHECKS);
const store = new Map<string, Monitor>(); // TODO: replace with better-sqlite3-backed repo

const app = express();
app.use(express.json());

app.get("/api/v1/healthz", (_req, res) => {
  res.json({ status: "ok" });
});

app.post("/api/v1/monitors", (req, res) => {
  const parsed = MonitorCreateSchema.safeParse(req.body);
  if (!parsed.success) {
    res.status(400).json({ error: parsed.error.message });
    return;
  }
  const monitor = createMonitor(parsed.data);
  store.set(monitor.id, monitor);
  scheduler.register(monitor);
  res.status(201).json(monitor);
});

app.get("/api/v1/monitors", (_req, res) => {
  res.json([...store.values()]);
});

app.get("/api/v1/monitors/:id", (req, res) => {
  const monitor = store.get(req.params.id);
  if (!monitor) {
    res.status(404).json({ error: "not found" });
    return;
  }
  res.json(monitor);
});

app.get("/api/v1/monitors/:id/status", (req, res) => {
  // TODO: join with check_results history, respect ?limit=
  const monitor = store.get(req.params.id);
  if (!monitor) {
    res.status(404).json({ error: "not found" });
    return;
  }
  res.json({ monitor, history: [] });
});

app.delete("/api/v1/monitors/:id", (req, res) => {
  store.delete(req.params.id);
  scheduler.unregister(req.params.id);
  res.status(204).send();
});

const server = app.listen(Number(PORT), () => {
  console.log(`listening on ${PORT}`);
});

// Graceful shutdown (Section 4.4)
process.on("SIGTERM", () => {
  scheduler.shutdown();
  server.close(() => process.exit(0));
});
