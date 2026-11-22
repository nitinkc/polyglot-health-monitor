package main

import (
	"time"

	"github.com/google/uuid"
)

type Monitor struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	URL                 string    `json:"url"`
	IntervalSeconds     int       `json:"interval_seconds"`
	TimeoutMs           int       `json:"timeout_ms"`
	FailureThreshold    int       `json:"failure_threshold"`
	Status              string    `json:"status"` // UNKNOWN | UP | DOWN
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func NewMonitor(name, url string, interval, timeoutMs, threshold int) *Monitor {
	now := time.Now().UTC()
	return &Monitor{
		ID:               uuid.NewString(),
		Name:             name,
		URL:              url,
		IntervalSeconds:  interval,
		TimeoutMs:        timeoutMs,
		FailureThreshold: threshold,
		Status:           "UNKNOWN",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// TODO: Validate() (*errors.New style, wrapped) — interval >= 5, timeout >= 100, etc.
// TODO: RecordResult(success bool) — status transition logic per Section 4
