#!/usr/bin/env bash
# Flip a chaos-target instance into a new behavior mode.
#
#   ./chaos.sh http://$(minikube ip):30080 down                     # break the east fleet
#   ./chaos.sh http://$(minikube ip):30081 degraded error_rate=0.8  # brown out west
#   ./chaos.sh http://$(minikube ip):30080 pause 30                 # freeze 30s
#   ./chaos.sh http://$(minikube ip):30080 reset                    # back to healthy
#   ./chaos.sh http://$(minikube ip):30080 scenario outage          # timed sequence
#   ./chaos.sh http://$(minikube ip):30080 scenstop                 # cancel running scenario
#   ./chaos.sh http://$(minikube ip):30080 scenarios                # list the catalog
# Note: a NodePort request lands on ONE pod behind the Service — to flip a
# specific pod use chaos-pod.sh instead.
set -u
BASE="${1:?usage: chaos.sh <base-url> <mode|pause|reset|crash|scenario|scenstop|scenarios> [key=value...]}"
WHAT="${2:?mode required}"
shift 2

case "$WHAT" in
  reset) curl -s -X POST "$BASE/chaos/reset" ;;
  crash) curl -s -X POST "$BASE/chaos/crash" ;;
  pause) curl -s -X POST "$BASE/chaos/pause?seconds=${1:-30}" ;;
  scenario) curl -s -X POST "$BASE/chaos/scenario?name=${1:?scenario name required}" ;;
  scenstop) curl -s -X DELETE "$BASE/chaos/scenario" ;;
  scenarios) curl -s "$BASE/chaos/scenario" ;;
  *)
    QS="mode=$WHAT"
    for kv in "$@"; do QS="$QS&$kv"; done
    curl -s -X POST "$BASE/chaos/mode?$QS"
    ;;
esac
echo
