package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
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
			"GET  /meta":                       "instance identity, mode, uptime, counters, stats",
			"GET  /chaos/state":                "current behavior config",
			"POST /chaos/mode":                 "set mode + tunables (query params or JSON body)",
			"POST /chaos/reset":                "back to healthy, cancel scenario, zero counters",
			"POST /chaos/pause?seconds=N":      "freeze instance (requests + readyz stall)",
			"POST /chaos/crash":                "exit(1) shortly after responding — crash recovery tests",
			"GET  /chaos/scenario":             "running scenario + available named sequences",
			"POST /chaos/scenario?name=X":      "run a canned timed mode sequence (outage, chaos-tour, …)",
			"DELETE /chaos/scenario":           "cancel the running scenario, keep current mode",
			"GET  /chaos/history":              "recent chaos events (mode flips, pauses, scenario steps)",
			"GET  /metrics":                    "Prometheus exposition: uptime, paused, polls by route+code",
			"GET  /healthz":                    "liveness, always 200",
			"GET  /readyz":                     "readiness, reflects mode",
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
		"scenario":       s.scenarioInfo(),
		"stats":          statsPayload(s.stats.snapshot()),
		"config": map[string]any{
			"slow_ms": slowMs, "latency_ms": lat, "error_rate": errRate,
			"flap_fail": ff, "flap_ok": fo, "jitter_ms": jit, "spike_ms": spike,
		},
		"ts": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// statsPayload shapes the derived counters for JSON output. "fault" counts
// poll requests that ended with a hijacked socket — no HTTP response at all.
func statsPayload(byEp, byCode, _ map[string]int64) map[string]any {
	return map[string]any{"by_endpoint": byEp, "by_code": byCode}
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
	prev := s.mode
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

	if prev != cfg.Mode {
		s.hist.add("mode", prev+" -> "+cfg.Mode)
	}
	s.metaHandler(w, nil)
}

func (s *State) chaosResetHandler(w http.ResponseWriter, _ *http.Request) {
	s.stopScenario() // a running scenario would just flip the mode back
	s.mu.Lock()
	prev := s.mode
	s.mode = ModeHealthy
	s.mu.Unlock()
	if prev != ModeHealthy {
		s.hist.add("reset", prev+" -> "+ModeHealthy)
	} else {
		s.hist.add("reset", "cleared pause + counters")
	}
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
	s.hist.add("pause", fmt.Sprintf("frozen for %ds", secs))
	writeJSON(w, http.StatusOK, map[string]any{
		"paused_for_seconds": secs, "instance": s.instanceID,
	})
}

// chaosCrashHandler exits the process shortly after the response is flushed —
// for restart/crash-recovery testing.
func (s *State) chaosCrashHandler(w http.ResponseWriter, _ *http.Request) {
	s.hist.add("crash", "exit(1) scheduled")
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

// chaosHistoryHandler returns the bounded event log — newest entries last.
// Correlate a monitor's status flip with the chaos action that caused it.
func (s *State) chaosHistoryHandler(w http.ResponseWriter, _ *http.Request) {
	events := s.hist.list()
	writeJSON(w, http.StatusOK, map[string]any{
		"events": events, "count": len(events), "instance": s.instanceID,
	})
}

// scenarioGetHandler reports the running scenario (if any) plus the catalog.
func (s *State) scenarioGetHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"scenario":  s.scenarioInfo(),
		"available": scenarios,
		"instance":  s.instanceID,
	})
}

// scenarioStartHandler begins a named canned sequence. ?name=X or a JSON body
// {"name": "X"}; starting one cancels whatever was running.
func (s *State) scenarioStartHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if r.Body != nil && r.Header.Get("Content-Type") == "application/json" {
		var body struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(r.Body).Decode(&body) == nil && body.Name != "" {
			name = body.Name
		}
	}
	if err := s.startScenario(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     err.Error(),
			"available": scenarioNames(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scenario": s.scenarioInfo(), "instance": s.instanceID,
	})
}

func (s *State) scenarioStopHandler(w http.ResponseWriter, _ *http.Request) {
	stopped := s.stopScenario()
	writeJSON(w, http.StatusOK, map[string]any{
		"cancelled": stopped, "instance": s.instanceID,
	})
}

func scenarioNames() []string {
	names := make([]string, 0, len(scenarios))
	for n := range scenarios {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// metricsHandler emits Prometheus text exposition — enough for `promtool`,
// a scrape, or just eyeballing counters during a demo.
func (s *State) metricsHandler(w http.ResponseWriter, _ *http.Request) {
	mode, _, _, _, _, _, _, _ := s.snapshot()
	_, _, pairs := s.stats.snapshot()

	paused := 0
	if time.Now().UnixNano() < s.pausedUntil.Load() {
		paused = 1
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# HELP chaos_target_uptime_seconds Seconds since process start.\n")
	fmt.Fprintf(&b, "# TYPE chaos_target_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "chaos_target_uptime_seconds %.0f\n\n", s.uptimeSeconds())

	fmt.Fprintf(&b, "# HELP chaos_target_paused 1 while a /chaos/pause freeze is in effect.\n")
	fmt.Fprintf(&b, "# TYPE chaos_target_paused gauge\n")
	fmt.Fprintf(&b, "chaos_target_paused %d\n\n", paused)

	fmt.Fprintf(&b, "# HELP chaos_target_mode_info Current behavior mode (value is always 1).\n")
	fmt.Fprintf(&b, "# TYPE chaos_target_mode_info gauge\n")
	fmt.Fprintf(&b, "chaos_target_mode_info{mode=%q,instance=%q,region=%q} 1\n\n",
		mode, s.instanceID, s.region)

	fmt.Fprintf(&b, "# HELP chaos_target_poll_requests_total Poll requests by endpoint and HTTP code (code=\"fault\" = transport fault, no response written).\n")
	fmt.Fprintf(&b, "# TYPE chaos_target_poll_requests_total counter\n")
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ep, code, _ := splitPair(k)
		fmt.Fprintf(&b, "chaos_target_poll_requests_total{endpoint=%q,code=%q} %d\n",
			ep, code, pairs[k])
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(b.String()))
}
