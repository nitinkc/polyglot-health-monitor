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
// TODO: persist CheckResult rows instead of printing
// TODO: structured JSON logging via log/slog instead of fmt.Printf
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
		// TODO: distinguish ctx.DeadlineExceeded (timeout) vs connection refused, etc.
		fmt.Printf(`{"monitor_id":%q,"error":%q}`+"\n", m.ID, err.Error())
		return
	}
	defer resp.Body.Close()

	success := resp.StatusCode < 400
	// TODO: call m.RecordResult(success) and update status per Section 4
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
