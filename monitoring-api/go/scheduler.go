package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Scheduler runs one goroutine per monitor. A buffered channel acts as a
// semaphore to bound concurrent in-flight HTTP checks (Section 4.2).
//
// TODO: persist CheckResult rows instead of printing — monitors must keep
// polling through flap cycles so GET /status shows real DOWN→UP history
// (chaos-target /flap and the west sick-pod fixture depend on this).
// TODO: structured JSON logging via log/slog instead of fmt.Printf
//
// Chaos-target acceptance criteria — every poll endpoint must classify
// correctly (see docs/documentation/chaos-target.openapi.yaml):
//
//	/health, /ok, /jitter         -> success; record status_code + latency_ms
//	/slow?ms=N                    -> success only if response lands before
//	                               timeout_ms (seeded monitor uses 5s vs 2s)
//	/hang, mode=pause             -> failure "timeout"; request must be
//	                               cancelled, not just abandoned client-side
//	/error/{429,500,502,503}      -> failure; status_code still recorded
//	/reset                        -> failure "connection_reset", null status_code
//	/eof                          -> failure "unexpected_eof" — only detectable
//	                               if the response BODY is fully read
//	/malformed                    -> failure "invalid_response"
//	connection refused (crash)    -> failure "connection_refused"
//	/flap, mode=flapping          -> alternating results drive
//	                               consecutive_failures and DOWN→UP transitions
type Scheduler struct {
	sem      chan struct{}
	client   *http.Client
	cancels  map[string]context.CancelFunc
	mu       sync.Mutex
	wg       sync.WaitGroup
}

func NewScheduler(maxConcurrentChecks int) *Scheduler {
	return &Scheduler{
		sem:     make(chan struct{}, maxConcurrentChecks),
		client:  &http.Client{},
		cancels: make(map[string]context.CancelFunc),
	}
}

func (s *Scheduler) Register(m *Monitor) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[m.ID] = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go s.runLoop(ctx, m)
}

func (s *Scheduler) runLoop(ctx context.Context, m *Monitor) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Duration(m.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return // unregistered or shutting down
		case <-ticker.C:
			// TODO: semaphore acquire should respect ctx cancellation so a
			// queued check can't stall shutdown behind a wedged /hang slot.
			s.sem <- struct{}{} // acquire
			s.performCheck(ctx, m)
			<-s.sem // release
		}
	}
}

func (s *Scheduler) performCheck(parent context.Context, m *Monitor) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(m.TimeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		fmt.Printf(`{"monitor_id":%q,"error":%q}`+"\n", m.ID, err.Error())
		return
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		// TODO: classify transport errors into short stable error strings:
		//   - ctx.DeadlineExceeded / url.Error.Timeout() -> "timeout" (/hang, /slow, pause)
		//   - "connection refused"  -> "connection_refused" (crashed/down pod)
		//   - "connection reset"    -> "connection_reset" (/reset, /eof tail)
		//   - "unexpected EOF"      -> "unexpected_eof" (/eof)
		//   - parse failures        -> "invalid_response" (/malformed)
		// CheckResult: success=false, status_code=nil, latency_ms=nil
		fmt.Printf(`{"monitor_id":%q,"error":%q}`+"\n", m.ID, err.Error())
		return
	}
	// TODO: drain the body (io.Copy(io.Discard, resp.Body)) before Close —
	// required to (a) detect /eof's truncated body instead of a false success,
	// (b) keep the connection reusable for keep-alive.
	defer resp.Body.Close()

	success := resp.StatusCode < 400
	// TODO: on 4xx/5xx (/error/*, mode=down) still record status_code in the
	// CheckResult — failure rows currently lose it. Error string e.g. "http_503".
	// TODO: call m.RecordResult(success), persist CheckResult, update the
	// monitors row (status, consecutive_failures, updated_at) per Section 4.
	fmt.Printf(`{"monitor_id":%q,"status_code":%d,"latency_ms":%d,"success":%v}`+"\n",
		m.ID, resp.StatusCode, latency, success)
}

func (s *Scheduler) Unregister(monitorID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.cancels[monitorID]; ok {
		cancel()
		delete(s.cancels, monitorID)
	}
}

// Shutdown cancels all monitor loops and waits (with a grace period) for
// in-flight checks to finish. Call from a SIGTERM handler.
func (s *Scheduler) Shutdown(ctx context.Context) {
	s.mu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		// TODO: log "forced shutdown, some checks may not have finished"
	}
}
