#!/usr/bin/env bash
# Clean-state reset between test runs (per RedMe.md: "reset state cleanly
# between test runs"). Heals every compose instance; pass "k8s" to also heal
# every pod in the minikube fleets.
set -u
SCOPE="${1:-compose}"

if [ "$SCOPE" = "compose" ] || [ "$SCOPE" = "all" ]; then
  for port in 9001 9002 9003 9004 9005 9006 9007 9010 9011; do
    printf "reset :%-5s " "$port"
    curl -s -o /dev/null -w "%{http_code}\n" --max-time 2 -X POST "http://localhost:$port/chaos/reset" || echo "down"
  done
fi

if [ "$SCOPE" = "k8s" ] || [ "$SCOPE" = "all" ]; then
  for NS in chaos-east chaos-west; do
    kubectl get pods -n "$NS" -l app=chaos-target -o name | while read -r p; do
      printf "reset %s/%s " "$NS" "${p#pod/}"
      kubectl exec -n "$NS" "$p" -- wget -qO- --post-data='' --timeout=3 \
        http://127.0.0.1:8080/chaos/reset >/dev/null 2>&1 && echo ok || echo fail
    done
  done
fi
