#!/usr/bin/env bash
# Register the minikube fixture fleets with a running health-monitor API
# (any of the four implementations — they share one contract).
#
#   ./seed-monitors.sh http://localhost:8080     # monitor on host, pods via NodePort
#
# Respects the shared spec: interval_seconds >= 5, timeout_ms >= 100.
set -u
API="${1:-http://localhost:8080}/api/v1"
IP="${MINIKUBE_IP:-$(minikube ip 2>/dev/null)}"
: "${IP:?could not resolve minikube ip — is minikube running?}"

# name|url|interval|timeout_ms|threshold
SPECS="
east-jitter|http://$IP:30080/health|5|2000|3
east-flap|http://$IP:30080/flap|5|2000|3
east-slow-vs-timeout|http://$IP:30080/slow|5|2000|2
west-sick-pod|http://$IP:30081/health|5|2000|3
"

echo "$SPECS" | grep -v '^$' | while IFS='|' read -r name url ivl tmo thr; do
  printf "%-16s -> " "$name"
  curl -s -o /dev/null -w "%{http_code}\n" -X POST "$API/monitors" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"$name\",\"url\":\"$url\",\"interval_seconds\":$ivl,\"timeout_ms\":$tmo,\"failure_threshold\":$thr}"
done
echo "Registered with $API — watch status:  curl $API/monitors"
