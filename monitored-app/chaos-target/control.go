package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------
// Control plane — /chaos/*, /meta, /healthz, /readyz, index.
// These routes are deliberately NOT wrapped by mode enforcement: you must be
// able to reach /chaos/reset on an instance that is otherwise "down".
// ---------------------------------------------------------------------------

type chaosConfig struct {
	Mode      string   `json:"mode"`
	SlowMs    *int     `json:"slow_ms,omitempty"`
	LatencyMs *int     `json:"latency_ms,omitempty"`
	ErrorRate *float64 `json:"error_rate,omitempty"`
	FlapFail  *int     `json:"flap_fail,omitempty"`
	FlapOK    *int     `json:"flap_ok,omitempty"`
	JitterMs  *int     `json:"jitter_ms,omitempty"`
	SpikeMs   *int     `json:"spike_ms,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *State) indexHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"app":      "chaos-target",
		"instance": s.instanceID,
		"region":   s.region,
		"poll_endpoints": map[string]string{
			"GET /health, /ok":             "fast 200 OK",
			"GET /jitter":                  "200 with baseline latency + occasional spikes",
			"GET /slow?ms=N":               "holds N ms then 200 (default = SLOW_MS env)",
			"GET /hang":                    "accepts connection, never sends bytes",
			"GET /error/{429|500|502|503}": "fixed error codes, 429 carries Retry-After",
			"GET /reset":                   "TCP RST (connection reset by peer)",
			"GET /eof":                     "valid head + truncated body, then RST",
			"GET /malformed":               "non-HTTP garbage bytes, then RST",
			"GET /flap":                    "flapFail x 503 then flapOK x 200, repeating",
		},
		"control_endpoints": map[string]string{
			"GET  /meta":                  "instance identity, mode, uptime, counters",
			"GET  /chaos/state":           "current behavior config",
			"POST /chaos/mode":            "set mode + tunables (query params or JSON body)",
			"POST /chaos/reset":           "back to healthy, zero counters",
			"POST /chaos/pause?seconds=N": "freeze instance (requests + readyz stall)",
			"POST /chaos/crash":           "exit(1) shortly after responding — crash recovery tests",
			"GET  /healthz":               "liveness, always 200",
			"GET  /readyz":                "readiness, reflects mode",
		},
	})
}

func (s *State) metaHandler(w http.ResponseWriter, _ *http.Request) {
	mode, slowMs, errRate, ff, fo, lat, jit, spike := s.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"instance":       s.instanceID,
		"region":         s.region,
		"mode":           mode,
		"uptime_seconds": int64(s.uptimeSeconds()),
		"requests_seen":  s.seq.Load(),
		"paused":         time.Now().UnixNano() < s.pausedUntil.Load(),
		"config": map[string]any{
			"slow_ms": slowMs, "latency_ms": lat, "error_rate": errRate,
			"flap_fail": ff, "flap_ok": fo, "jitter_ms": jit, "spike_ms": spike,
		},
		"ts": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *State) chaosStateHandler(w http.ResponseWriter, _ *http.Request) {
	s.metaHandler(w, nil)
}

func (s *State) chaosModeHandler(w http.ResponseWriter, r *http.Request) {
	var cfg chaosConfig

	// Query params first (curl-friendly), then optional JSON body overrides.
	q := r.URL.Query()
	cfg.Mode = q.Get("mode")
	if r.Body != nil && r.Header.Get("Content-Type") == "application/json" {
		json.NewDecoder(r.Body).Decode(&cfg)
	}

	intParam := func(name string, dst **int) {
		if v := q.Get(name); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = &n
			}
		}
	}
	intParam("slow_ms", &cfg.SlowMs)
	intParam("latency_ms", &cfg.LatencyMs)
	intParam("flap_fail", &cfg.FlapFail)
	intParam("flap_ok", &cfg.FlapOK)
	intParam("jitter_ms", &cfg.JitterMs)
	intParam("spike_ms", &cfg.SpikeMs)
	if v := q.Get("error_rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.ErrorRate = &f
		}
	}

	if cfg.Mode == "" {
		cfg.Mode, _, _, _, _, _, _, _ = s.snapshot()
	}
	if !isValidMode(cfg.Mode) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "unknown mode", "valid_modes": validModes,
		})
		return
	}

	s.mu.Lock()
	s.mode = cfg.Mode
	if cfg.SlowMs != nil {
		s.slowMs = max(*cfg.SlowMs, 0)
	}
	if cfg.LatencyMs != nil {
		s.latencyMs = max(*cfg.LatencyMs, 0)
	}
	if cfg.ErrorRate != nil {
		s.errorRate = min(max(*cfg.ErrorRate, 0), 1)
	}
	if cfg.FlapFail != nil {
		s.flapFail = max(*cfg.FlapFail, 1)
	}
	if cfg.FlapOK != nil {
		s.flapOK = max(*cfg.FlapOK, 1)
	}
	if cfg.JitterMs != nil {
		s.jitterMs = max(*cfg.JitterMs, 0)
	}
	if cfg.SpikeMs != nil {
		s.spikeMs = max(*cfg.SpikeMs, 0)
	}
	s.mu.Unlock()

	s.metaHandler(w, nil)
}

func (s *State) chaosResetHandler(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.mode = ModeHealthy
	s.mu.Unlock()
	s.pausedUntil.Store(0)
	s.flapCount.Store(0)
	s.routeFlap.Store(0)
	s.metaHandler(w, nil)
}

// chaosPauseHandler freezes the instance: poll requests block and readiness
// fails until the deadline — a "pod is hung" scenario.
func (s *State) chaosPauseHandler(w http.ResponseWriter, r *http.Request) {
	secs := 30
	if v := r.URL.Query().Get("seconds"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			secs = min(n, 600)
		}
	}
	s.pausedUntil.Store(time.Now().Add(time.Duration(secs) * time.Second).UnixNano())
	writeJSON(w, http.StatusOK, map[string]any{
		"paused_for_seconds": secs, "instance": s.instanceID,
	})
}

// chaosCrashHandler exits the process shortly after the response is flushed —
// for restart/crash-recovery testing.
func (s *State) chaosCrashHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"crashing": true, "instance": s.instanceID,
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(1)
	}()
}

func (s *State) healthzHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz reflects the instance's effective health: k8s will pull it out of the
// Service endpoints when it's "down", which is exactly what you want a monitor
// (or a human doing `kubectl get pods`) to see.
func (s *State) readyzHandler(w http.ResponseWriter, _ *http.Request) {
	if time.Now().UnixNano() < s.pausedUntil.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "paused"})
		return
	}
	mode, _, _, ff, fo, _, _, _ := s.snapshot()
	switch mode {
	case ModeDown, ModeReset:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "mode": mode})
	case ModeFlapping:
		// Readiness flaps in lockstep with the data plane.
		pos := s.flapCount.Load() % int64(max(ff+fo, 1))
		if pos < int64(ff) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "mode": mode})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "mode": mode})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "mode": mode})
	}
}
