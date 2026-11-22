#!/usr/bin/env bash
# Register the whole fixture suite with a running health-monitor API
# (any of the four implementations — they share one contract).
#
#   ./seed-monitors.sh http://localhost:8080          # monitor on host, targets on host ports
#   HOSTBASE=http://ok.local:8080 ...                  # monitor inside the fixtures network
#
# Respects the shared spec: interval_seconds >= 5, timeout_ms >= 100.
set -u
API="${1:-http://localhost:8080}/api/v1"

# name|url|interval|timeout_ms|threshold
SPECS="
ok|http://localhost:9001/health|5|2000|3
jitter|http://localhost:9002/jitter|5|2000|3
slow-vs-timeout|http://localhost:9003/slow?ms=5000|5|2000|2
flappy|http://localhost:9004/flap|5|2000|3
degraded|http://localhost:9005/health|5|2000|3
connreset|http://localhost:9006/reset|5|2000|2
east-fleet|http://localhost:9010/health|5|2000|3
west-fleet|http://localhost:9011/health|5|2000|3
"

echo "$SPECS" | grep -v '^$' | while IFS='|' read -r name url ivl tmo thr; do
  printf "%-16s -> " "$name"
  curl -s -o /dev/null -w "%{http_code}\n" -X POST "$API/monitors" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"$name\",\"url\":\"$url\",\"interval_seconds\":$ivl,\"timeout_ms\":$tmo,\"failure_threshold\":$thr}"
done
echo "Registered with $API — watch status:  curl $API/monitors"
