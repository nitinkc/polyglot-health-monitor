#!/usr/bin/env bash
# "Find the bad servers" — prints a per-instance mode table.
#
#   ./audit.sh compose     # scans the published ports 9001-9011
#   ./audit.sh k8s         # scans every pod in chaos-east / chaos-west
set -u
SCOPE="${1:-compose}"

row() { printf "%-42s %-8s %-9s %-7s %s\n" "$@"; }

if [ "$SCOPE" = "compose" ]; then
  row INSTANCE REGION MODE REQ UPTIME
  for port in 9001 9002 9003 9004 9005 9006 9007 9010 9011; do
    meta=$(curl -s --max-time 2 "http://localhost:$port/meta") || continue
    [ -z "$meta" ] && continue
    echo "$meta" | python3 -c '
import json,sys
m=json.load(sys.stdin)
print(f"{m[\"instance\"]:<42} {m[\"region\"]:<8} {m[\"mode\"]:<9} {m[\"requests_seen\"]:<7} {m[\"uptime_seconds\"]}s")'
  done

elif [ "$SCOPE" = "k8s" ]; then
  for NS in chaos-east chaos-west; do
    echo "--- $NS ---"
    row POD READY MODE REQ UPTIME
    kubectl get pods -n "$NS" -l app=chaos-target -o json | \
      python3 -c 'import json,sys; [print(p["metadata"]["name"]) for p in json.load(sys.stdin)["items"]]' | \
    while read -r pod; do
      meta=$(kubectl exec -n "$NS" "$pod" -- wget -qO- --timeout=3 http://127.0.0.1:8080/meta 2>/dev/null) || meta=""
      ready=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.containerStatuses[0].ready}')
      if [ -n "$meta" ]; then
        echo "$meta" | python3 -c "
import json,sys
m=json.load(sys.stdin)
print(f'$pod'.ljust(42), '$ready'.ljust(8), m['mode'].ljust(9), str(m['requests_seen']).ljust(7), str(m['uptime_seconds'])+'s')"
      else
        row "$pod" "$ready" "unreachable" "-" "-"
      fi
    done
  done
else
  echo "usage: audit.sh [compose|k8s]" >&2; exit 1
fi
