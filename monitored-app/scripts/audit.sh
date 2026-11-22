#!/usr/bin/env bash
# "Find the bad servers" — prints a per-pod mode table for the minikube fleets.
#
#   ./audit.sh        # scans every pod in chaos-east / chaos-west
set -u

row() { printf "%-42s %-8s %-9s %-7s %s\n" "$@"; }

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
