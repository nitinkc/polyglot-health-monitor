package main

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func main() {
	level := slog.LevelInfo
	if envStr("LOG_LEVEL", "info") == "debug" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	hostname, _ := os.Hostname()
	mode := envStr("MODE", ModeHealthy)
	if !isValidMode(mode) {
		slog.Error("invalid MODE", "mode", mode, "valid", validModes)
		os.Exit(1)
	}

	st := &State{
		instanceID: envStr("INSTANCE_ID", hostname),
		region:     envStr("REGION", "local"),
		startedAt:  time.Now(),
		mode:       mode,
		latencyMs:  envInt("LATENCY_MS", 0),
		slowMs:     envInt("SLOW_MS", 5000),
		errorRate:  envFloat("ERROR_RATE", 0.5),
		flapFail:   envInt("FLAP_FAIL", 3),
		flapOK:     envInt("FLAP_OK", 3),
		jitterMs:   envInt("JITTER_MS", 15),
		spikeMs:    envInt("SPIKE_MS", 800),
	}

	mux := http.NewServeMux()

	// Poll endpoints — wrapped in mode enforcement.
	mux.HandleFunc("GET /health", st.poll(st.okHandler))
	mux.HandleFunc("GET /ok", st.poll(st.okHandler))
	mux.HandleFunc("GET /jitter", st.poll(st.jitterHandler))
	mux.HandleFunc("GET /slow", st.poll(st.slowHandler))
	mux.HandleFunc("GET /hang", st.poll(st.hangHandler))
	mux.HandleFunc("GET /error/{code}", st.poll(st.errorHandler))
	mux.HandleFunc("GET /reset", st.poll(st.resetHandler))
	mux.HandleFunc("GET /eof", st.poll(st.eofHandler))
	mux.HandleFunc("GET /malformed", st.poll(st.malformedHandler))
	mux.HandleFunc("GET /flap", st.poll(st.flapHandler))

	// Control endpoints — always reachable.
	mux.HandleFunc("GET /", st.indexHandler)
	mux.HandleFunc("GET /meta", st.metaHandler)
	mux.HandleFunc("GET /healthz", st.healthzHandler)
	mux.HandleFunc("GET /readyz", st.readyzHandler)
	mux.HandleFunc("GET /chaos/state", st.chaosStateHandler)
	mux.HandleFunc("POST /chaos/mode", st.chaosModeHandler)
	mux.HandleFunc("POST /chaos/reset", st.chaosResetHandler)
	mux.HandleFunc("POST /chaos/pause", st.chaosPauseHandler)
	mux.HandleFunc("POST /chaos/crash", st.chaosCrashHandler)

	srv := &http.Server{
		Addr:              ":" + envStr("PORT", "8080"),
		Handler:           st.identify(st.logRequests(mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		slog.Info("chaos-target listening",
			"addr", srv.Addr, "instance", st.instanceID,
			"region", st.region, "mode", mode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received, draining")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("bye")
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// identify stamps instance headers on every response so callers can tell which
// replica answered behind a load-balanced URL.
func (s *State) identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode, _, _, _, _, _, _, _ := s.snapshot()
		w.Header().Set("X-Instance-Id", s.instanceID)
		w.Header().Set("X-Region", s.region)
		w.Header().Set("X-Mode", mode)
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack preserves connection-hijack capability for the fault endpoints.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacking unsupported")
	}
	return hj.Hijack()
}

// logRequests emits one structured JSON line per request.
func (s *State) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request_completed",
			"method", r.Method, "path", r.URL.Path,
			"status", rec.status,
			"latency_ms", time.Since(start).Milliseconds(),
			"instance", s.instanceID, "region", s.region)
	})
}

// poll wraps a data-plane handler with instance-mode enforcement. This is the
// chaos lever: whatever the route would normally do, the mode runs first and
// can delay, fail, or kill the request.
func (s *State) poll(next func(http.ResponseWriter, *http.Request, time.Time)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		seq := s.seq.Add(1)
		w.Header().Set("X-Request-Seq", strconv.FormatInt(seq, 10))

		// Freeze gate: /chaos/pause makes the whole instance stall.
		if until := s.pausedUntil.Load(); until > time.Now().UnixNano() {
			sleepCtx(r.Context(), time.Duration(until-time.Now().UnixNano()))
		}

		mode, slowMs, errRate, ff, fo, latMs, jitMs, spikeMs := s.snapshot()

		// Fixed injected latency applies to every mode.
		if latMs > 0 {
			sleepCtx(r.Context(), time.Duration(latMs)*time.Millisecond)
		}

		switch mode {
		case ModeDown:
			s.writeErr(w, r, http.StatusServiceUnavailable, "instance down (mode=down)")
			return
		case ModeReset:
			s.resetHandler(w, r, start)
			return
		case ModeSlow:
			sleepCtx(r.Context(), time.Duration(slowMs)*time.Millisecond)
		case ModeDegraded:
			sleepCtx(r.Context(), time.Duration(rand.IntN(400))*time.Millisecond)
			if rand.Float64() < errRate {
				codes := []int{500, 502, 503}
				s.writeErr(w, r, codes[rand.IntN(len(codes))], "degraded instance (mode=degraded)")
				return
			}
		case ModeFlapping:
			if flapSaysFail(&s.flapCount, ff, fo) {
				s.writeErr(w, r, http.StatusServiceUnavailable, "flap window: down (mode=flapping)")
				return
			}
		case ModeJitter:
			d := jitMs
			if rand.IntN(10) == 0 {
				d += rand.IntN(max(spikeMs, 1))
			}
			sleepCtx(r.Context(), time.Duration(d)*time.Millisecond)
		}

		next(w, r, start)
	}
}
