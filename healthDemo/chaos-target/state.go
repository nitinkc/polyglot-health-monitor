package main

import (
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
