#!/usr/bin/env bash
# Clean-state reset between test runs — POSTs /chaos/reset to every pod in
# the minikube fleets (chaos-east / chaos-west).
set -u

for NS in chaos-east chaos-west; do
  kubectl get pods -n "$NS" -l app=chaos-target -o name | while read -r p; do
    printf "reset %s/%s " "$NS" "${p#pod/}"
    kubectl exec -n "$NS" "$p" -- wget -qO- --post-data='' --timeout=3 \
      http://127.0.0.1:8080/chaos/reset >/dev/null 2>&1 && echo ok || echo fail
  done
done
