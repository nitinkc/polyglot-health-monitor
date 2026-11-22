package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------
// Poll (data-plane) endpoints — what the health monitors actually check.
// Every response carries X-Instance-Id / X-Region / X-Mode / X-Request-Seq so a
// monitor can tell *which* backend answered, which matters once several
// instances sit behind one URL.
// ---------------------------------------------------------------------------

type pollPayload struct {
	Instance  string `json:"instance"`
	Region    string `json:"region"`
	Mode      string `json:"mode"`
	Endpoint  string `json:"endpoint"`
	Seq       int64  `json:"seq"`
	LatencyMs int64  `json:"latency_ms"`
	Ts        string `json:"ts"`
}

func (s *State) writeOK(w http.ResponseWriter, r *http.Request, start time.Time) {
	mode, _, _, _, _, _, _, _ := s.snapshot()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(pollPayload{
		Instance:  s.instanceID,
		Region:    s.region,
		Mode:      mode,
		Endpoint:  r.URL.Path,
		Seq:       s.seq.Load(),
		LatencyMs: time.Since(start).Milliseconds(),
		Ts:        time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *State) writeErr(w http.ResponseWriter, r *http.Request, code int, msg string) {
	mode, _, _, _, _, _, _, _ := s.snapshot()
	if code == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "5")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"error":    msg,
		"instance": s.instanceID,
		"region":   s.region,
		"mode":     mode,
		"endpoint": r.URL.Path,
		"seq":      s.seq.Load(),
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// sleepCtx sleeps but wakes early if the client hangs up.
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func hijackConn(w http.ResponseWriter) (net.Conn, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("hijacking not supported")
	}
	conn, _, err := hj.Hijack()
	return conn, err
}

// tcpReset closes the socket with SO_LINGER=0 so the peer sees ECONNRESET
// instead of a clean FIN.
func tcpReset(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	conn.Close()
}

// --- handlers ---------------------------------------------------------------

func (s *State) okHandler(w http.ResponseWriter, r *http.Request, start time.Time) {
	s.writeOK(w, r, start)
}

func (s *State) jitterHandler(w http.ResponseWriter, r *http.Request, start time.Time) {
	_, _, _, _, _, _, jitterMs, spikeMs := s.snapshot()
	delay := int64(jitterMs)
	if rand.IntN(10) == 0 { // ~10% of requests spike
		delay += int64(rand.IntN(max(spikeMs, 1)))
	}
	sleepCtx(r.Context(), time.Duration(delay)*time.Millisecond)
	s.writeOK(w, r, start)
}

func (s *State) slowHandler(w http.ResponseWriter, r *http.Request, start time.Time) {
	_, slowMs, _, _, _, _, _, _ := s.snapshot()
	ms := slowMs
	if v := r.URL.Query().Get("ms"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ms = min(n, 60_000)
		}
	}
	sleepCtx(r.Context(), time.Duration(ms)*time.Millisecond)
	s.writeOK(w, r, start)
}

// hangHandler opens the connection and never sends a single byte. The client
// must give up via its own timeout — the purest "server accepts but never
// answers" fixture. Capped at 2 minutes so leaked polls can't wedge the server.
func (s *State) hangHandler(w http.ResponseWriter, r *http.Request, _ time.Time) {
	conn, err := hijackConn(w)
	if err != nil {
		s.writeErr(w, r, http.StatusInternalServerError, "hijack unsupported")
		return
	}
	defer conn.Close()
	select {
	case <-r.Context().Done():
	case <-time.After(2 * time.Minute):
	}
}

func (s *State) errorHandler(w http.ResponseWriter, r *http.Request, _ time.Time) {
	code, err := strconv.Atoi(r.PathValue("code"))
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, "code must be numeric")
		return
	}
	msgs := map[int]string{
		429: "rate limited, back off",
		500: "internal server error",
		502: "bad gateway: upstream returned junk",
		503: "service unavailable",
	}
	msg, ok := msgs[code]
	if !ok {
		s.writeErr(w, r, http.StatusBadRequest, "supported codes: 429, 500, 502, 503")
		return
	}
	s.writeErr(w, r, code, msg)
}

// resetHandler: accept the request, then RST the socket.
func (s *State) resetHandler(w http.ResponseWriter, r *http.Request, _ time.Time) {
	conn, err := hijackConn(w)
	if err != nil {
		s.writeErr(w, r, http.StatusInternalServerError, "hijack unsupported")
		return
	}
	tcpReset(conn)
}

// eofHandler: send a valid response head plus a truncated body, then reset.
// Clients see "unexpected EOF" mid-read.
func (s *State) eofHandler(w http.ResponseWriter, r *http.Request, _ time.Time) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		s.writeErr(w, r, http.StatusInternalServerError, "hijack unsupported")
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 512\r\n\r\n{\"instance\":\"%s\",\"truncated\":", s.instanceID)
	rw.Flush()
	tcpReset(conn)
}

// malformedHandler: bytes that are not HTTP at all.
func (s *State) malformedHandler(w http.ResponseWriter, r *http.Request, _ time.Time) {
	conn, err := hijackConn(w)
	if err != nil {
		s.writeErr(w, r, http.StatusInternalServerError, "hijack unsupported")
		return
	}
	defer tcpReset(conn)
	conn.Write([]byte("GARBAGE-RESPONSE not-http/9.9 \x00\x01\xff\xfe\r\n\r\n"))
}

// flapHandler: per-route flap — flapFail failures then flapOK successes,
// forever. Exercises monitor status transition history.
func (s *State) flapHandler(w http.ResponseWriter, r *http.Request, start time.Time) {
	_, _, _, flapFail, flapOK, _, _, _ := s.snapshot()
	if flapSaysFail(&s.routeFlap, flapFail, flapOK) {
		s.writeErr(w, r, http.StatusServiceUnavailable, "flap window: down")
		return
	}
	s.writeOK(w, r, start)
}
