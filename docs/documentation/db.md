

``` mermaid
erDiagram

    MONITORS {
        TEXT id PK "Monitor identifier"
        TEXT name "Display name"
        TEXT url "Endpoint URL"
        INTEGER interval_seconds "Polling interval >= 5"
        INTEGER timeout_ms "Request timeout >= 100"
        INTEGER failure_threshold "Failures before unhealthy >= 1"
        TEXT status "UNKNOWN/UP/DOWN"
        INTEGER consecutive_failures "Current failure count"
        TEXT created_at "ISO timestamp"
        TEXT updated_at "ISO timestamp"
    }

    CHECK_RESULTS {
        TEXT id PK "Check result identifier"
        TEXT monitor_id FK "References monitors.id"
        TEXT checked_at "ISO timestamp"
        INTEGER success "0=false, 1=true"
        INTEGER status_code "HTTP status"
        INTEGER latency_ms "Response latency"
        TEXT error "Error message"
    }

    MONITORS ||--o{ CHECK_RESULTS : generates
```