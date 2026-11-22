package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Instance behavior modes. The mode is applied to every poll endpoint before
// the endpoint's own behavior runs, so a single toggle turns a whole "server"
// bad — the thing a health monitor is supposed to notice.
const (
	ModeHealthy  = "healthy"  // fast 200s
	ModeJitter   = "jitter"   // 200s with baseline latency + occasional spikes
	ModeDegraded = "degraded" // probabilistic 5xx + added latency
	ModeSlow     = "slow"     // every request held for slowMs then served
	ModeFlapping = "flapping" // windows: flapFail failures, then flapOK successes
	ModeReset    = "reset"    // every poll connection is TCP-reset
	ModeDown     = "down"     // instant 503 on every poll endpoint, readyz fails
)

var validModes = []string{
	ModeHealthy, ModeJitter, ModeDegraded, ModeSlow, ModeFlapping, ModeReset, ModeDown,
}

func isValidMode(m string) bool {
	for _, v := range validModes {
		if v == m {
			return true
		}
	}
	return false
}

type State struct {
	instanceID string
	region     string
	startedAt  time.Time

	mu        sync.Mutex
	mode      string
	latencyMs int     // fixed extra latency injected on every poll request
	slowMs    int     // hold time used by ModeSlow and as /slow default
	errorRate float64 // 5xx probability in ModeDegraded
	flapFail  int     // failures per flap window
	flapOK    int     // successes per flap window
	jitterMs  int     // baseline jitter latency
	spikeMs   int     // occasional spike ceiling

	pausedUntil atomic.Int64 // unixnano; poll requests block until this passes
	flapCount   atomic.Int64 // cycle counter for mode=flapping
	routeFlap   atomic.Int64 // dedicated counter for the /flap route
	seq         atomic.Int64 // total poll requests seen

	stats reqStats    // per-endpoint/per-code poll counters
	hist  eventLog    // recent chaos actions (mode flips, pauses, scenarios)
	sc    scenarioCtl // at most one timed scenario running
}

func (s *State) snapshot() (mode string, slowMs int, errRate float64, flapFail, flapOK, latencyMs, jitterMs, spikeMs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode, s.slowMs, s.errorRate, s.flapFail, s.flapOK, s.latencyMs, s.jitterMs, s.spikeMs
}

// flapSaysFail reports whether this request falls in the failing part of a
// flap window and advances the given counter.
func flapSaysFail(counter *atomic.Int64, fail, ok int) bool {
	if fail < 1 {
		fail = 1
	}
	if ok < 1 {
		ok = 1
	}
	pos := (counter.Add(1) - 1) % int64(fail+ok)
	return pos < int64(fail)
}

// uptimeSeconds since process start.
func (s *State) uptimeSeconds() float64 {
	return time.Since(s.startedAt).Seconds()
}

// ---------------------------------------------------------------------------
// Request stats — who got polled and what they got back, per endpoint and per
// status code. Code 0 means the socket was hijacked with no HTTP response
// (reset/eof/malformed/hang) — recorded as "fault".
// ---------------------------------------------------------------------------

type reqStats struct {
	mu    sync.Mutex
	pairs map[string]int64 // "endpoint|code" — single source for both views
	total int64
}

func (r *reqStats) record(endpoint string, code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pairs == nil {
		r.pairs = map[string]int64{}
	}
	r.pairs[endpoint+"|"+strconv.Itoa(code)]++
	r.total++
}

// snapshot returns derived by-endpoint and by-code maps plus the raw pairs.
func (r *reqStats) snapshot() (map[string]int64, map[string]int64, map[string]int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byEp := map[string]int64{}
	byCode := map[string]int64{}
	pairs := map[string]int64{}
	for k, n := range r.pairs {
		pairs[k] = n
		ep, code, _ := splitPair(k)
		byEp[ep] += n
		byCode[code] += n
	}
	return byEp, byCode, pairs
}

func splitPair(k string) (ep, code string, _ error) {
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] == '|' {
			ep, code = k[:i], k[i+1:]
			if code == "0" {
				code = "fault"
			}
			return ep, code, nil
		}
	}
	return k, "fault", nil
}

// ---------------------------------------------------------------------------
// Chaos event history — a bounded log of control actions so a demo can
// correlate "monitor went DOWN at T" with "mode was set to X at T-ε".
// ---------------------------------------------------------------------------

const historyCap = 100

type histEvent struct {
	Ts     string `json:"ts"`
	Kind   string `json:"kind"` // mode, pause, crash, reset, scenario
	Detail string `json:"detail"`
}

type eventLog struct {
	mu     sync.Mutex
	events []histEvent // chronological, oldest first
}

func (h *eventLog) add(kind, detail string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, histEvent{
		Ts:     time.Now().UTC().Format(time.RFC3339),
		Kind:   kind,
		Detail: detail,
	})
	if len(h.events) > historyCap {
		h.events = append([]histEvent(nil), h.events[len(h.events)-historyCap:]...)
	}
}

func (h *eventLog) list() []histEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]histEvent(nil), h.events...)
}

// ---------------------------------------------------------------------------
// Scenarios — canned timed mode sequences so a demo doesn't need a human
// flipping /chaos/mode. One runs at a time; starting a new one (or
// /chaos/reset) cancels the previous.
// ---------------------------------------------------------------------------

type scenarioStep struct {
	Mode  string `json:"mode"`
	HoldS int    `json:"hold_seconds"` // 0 = final step, mode persists
}

type scenarioDef struct {
	Desc  string         `json:"desc"`
	Steps []scenarioStep `json:"steps"`
}

var scenarios = map[string]scenarioDef{
	"outage": {
		Desc: "full outage for 30s, then recover",
		Steps: []scenarioStep{
			{Mode: ModeDown, HoldS: 30},
			{Mode: ModeHealthy},
		},
	},
	"brownout": {
		Desc: "degraded 5xx storm for 30s, then recover",
		Steps: []scenarioStep{
			{Mode: ModeDegraded, HoldS: 30},
			{Mode: ModeHealthy},
		},
	},
	"flap-storm": {
		Desc: "flapping up/down windows for 45s, then recover",
		Steps: []scenarioStep{
			{Mode: ModeFlapping, HoldS: 45},
			{Mode: ModeHealthy},
		},
	},
	"slow-leak": {
		Desc: "everything responds, just slowly (30s), then recover",
		Steps: []scenarioStep{
			{Mode: ModeSlow, HoldS: 30},
			{Mode: ModeHealthy},
		},
	},
	"chaos-tour": {
		Desc: "jitter 15s → degraded 15s → down 10s → healthy",
		Steps: []scenarioStep{
			{Mode: ModeJitter, HoldS: 15},
			{Mode: ModeDegraded, HoldS: 15},
			{Mode: ModeDown, HoldS: 10},
			{Mode: ModeHealthy},
		},
	},
}

type scenarioRun struct {
	name   string
	step   int // 0-based index of the step currently applied
	total  int
	ctx    context.Context
	cancel context.CancelFunc
}

type scenarioCtl struct {
	mu  sync.Mutex
	run *scenarioRun
}

// startScenario validates the name and launches the step runner. Any scenario
// already in flight is cancelled — starting one is a deliberate takeover.
func (s *State) startScenario(name string) error {
	def, ok := scenarios[name]
	if !ok {
		names := make([]string, 0, len(scenarios))
		for n := range scenarios {
			names = append(names, n)
		}
		sort.Strings(names)
		return fmt.Errorf("unknown scenario %q (available: %v)", name, names)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.sc.mu.Lock()
	if s.sc.run != nil {
		s.sc.run.cancel()
	}
	s.sc.run = &scenarioRun{name: name, total: len(def.Steps), ctx: ctx, cancel: cancel}
	s.sc.mu.Unlock()
	s.hist.add("scenario", "start "+name)
	go s.runScenario(ctx, def)
	return nil
}

func (s *State) runScenario(ctx context.Context, def scenarioDef) {
	for i, st := range def.Steps {
		s.sc.mu.Lock()
		if s.sc.run == nil || s.sc.run.ctx != ctx {
			s.sc.mu.Unlock()
			return // superseded or cancelled
		}
		s.sc.run.step = i
		s.sc.mu.Unlock()

		s.mu.Lock()
		s.mode = st.Mode
		s.mu.Unlock()
		s.hist.add("scenario", fmt.Sprintf("step %d/%d: mode=%s", i+1, len(def.Steps), st.Mode))

		if st.HoldS <= 0 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(st.HoldS) * time.Second):
		}
	}
	s.sc.mu.Lock()
	s.sc.run = nil
	s.sc.mu.Unlock()
	s.hist.add("scenario", "done")
}

// stopScenario cancels the running scenario, leaving the mode wherever the
// last applied step put it. Returns false when nothing was running.
func (s *State) stopScenario() bool {
	s.sc.mu.Lock()
	defer s.sc.mu.Unlock()
	if s.sc.run == nil {
		return false
	}
	s.sc.run.cancel()
	s.hist.add("scenario", "cancelled "+s.sc.run.name)
	s.sc.run = nil
	return true
}

func (s *State) scenarioInfo() map[string]any {
	s.sc.mu.Lock()
	defer s.sc.mu.Unlock()
	if s.sc.run == nil {
		return nil
	}
	return map[string]any{
		"running": true,
		"name":    s.sc.run.name,
		"step":    s.sc.run.step + 1,
		"total":   s.sc.run.total,
	}
}
