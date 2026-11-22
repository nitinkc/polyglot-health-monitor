#!/usr/bin/env bash
# Flip a chaos-target instance into a new behavior mode.
#
#   ./chaos.sh http://localhost:9010 down
#   ./chaos.sh http://localhost:9011 degraded error_rate=0.8
#   ./chaos.sh http://localhost:9004 flapping flap_fail=2 flap_ok=5
#   ./chaos.sh http://localhost:9010 pause 30        # freeze 30s
#   ./chaos.sh http://localhost:9010 reset           # back to healthy
set -u
BASE="${1:?usage: chaos.sh <base-url> <mode|pause|reset|crash> [key=value...]}"
WHAT="${2:?mode required}"
shift 2

case "$WHAT" in
  reset) curl -s -X POST "$BASE/chaos/reset" ;;
  crash) curl -s -X POST "$BASE/chaos/crash" ;;
  pause) curl -s -X POST "$BASE/chaos/pause?seconds=${1:-30}" ;;
  *)
    QS="mode=$WHAT"
    for kv in "$@"; do QS="$QS&$kv"; done
    curl -s -X POST "$BASE/chaos/mode?$QS"
    ;;
esac
echo
