package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

func main() {
	port := getEnv("PORT", "8080")
	maxConcurrent, _ := strconv.Atoi(getEnv("MAX_CONCURRENT_CHECKS", "10"))
	// TODO: fail fast if DB_PATH is missing, per Section 4.6

	scheduler := NewScheduler(maxConcurrent)
	store := &monitorStore{data: make(map[string]*Monitor)} // TODO: replace with SQLite-backed repo

	mux := http.NewServeMux()

  // hello world
  mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Hello, World!"))
  })

	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/v1/monitors", func(w http.ResponseWriter, r *http.Request) {
		// TODO: decode + validate request body, return 400 on bad input
		m := NewMonitor("placeholder", "https://example.com", 30, 2000, 3)
		store.put(m)
		scheduler.Register(m)
		writeJSON(w, http.StatusCreated, m)
	})

	mux.HandleFunc("GET /api/v1/monitors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.list())
	})

	mux.HandleFunc("GET /api/v1/monitors/{id}", func(w http.ResponseWriter, r *http.Request) {
		m, ok := store.get(r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, m)
	})

	mux.HandleFunc("GET /api/v1/monitors/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		// TODO: join with check_results history, respect ?limit=
		m, _ := store.get(r.PathValue("id"))
		writeJSON(w, http.StatusOK, map[string]any{"monitor": m, "history": []any{}})
	})

	mux.HandleFunc("DELETE /api/v1/monitors/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		store.remove(id)
		scheduler.Unregister(id)
		w.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	// Graceful shutdown on SIGTERM/SIGINT (Section 4.4)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scheduler.Shutdown(shutdownCtx)
	server.Shutdown(shutdownCtx)
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type monitorStore struct {
	mu   sync.RWMutex
	data map[string]*Monitor
}

func (s *monitorStore) put(m *Monitor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[m.ID] = m
}

func (s *monitorStore) get(id string) (*Monitor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.data[id]
	return m, ok
}

func (s *monitorStore) list() []*Monitor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Monitor, 0, len(s.data))
	for _, m := range s.data {
		out = append(out, m)
	}
	return out
}

func (s *monitorStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
}
