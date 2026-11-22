import { v4 as uuidv4 } from "uuid";
import { z } from "zod";

export const MonitorCreateSchema = z.object({
  name: z.string(),
  url: z.string().url(),
  interval_seconds: z.number().int().min(5),
  timeout_ms: z.number().int().min(100),
  failure_threshold: z.number().int().min(1),
});
export type MonitorCreateRequest = z.infer<typeof MonitorCreateSchema>;

export type MonitorStatus = "UNKNOWN" | "UP" | "DOWN";

export interface Monitor {
  id: string;
  name: string;
  url: string;
  interval_seconds: number;
  timeout_ms: number;
  failure_threshold: number;
  status: MonitorStatus;
  consecutive_failures: number;
  created_at: string;
  updated_at: string;
}

export function createMonitor(req: MonitorCreateRequest): Monitor {
  const now = new Date().toISOString();
  return {
    id: uuidv4(),
    name: req.name,
    url: req.url,
    interval_seconds: req.interval_seconds,
    timeout_ms: req.timeout_ms,
    failure_threshold: req.failure_threshold,
    status: "UNKNOWN",
    consecutive_failures: 0,
    created_at: now,
    updated_at: now,
  };
}

// TODO: recordResult(monitor, success) — status transition logic per Section 4
