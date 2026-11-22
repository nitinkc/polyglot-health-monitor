#!/usr/bin/env bash
# Flip ONE pod's behavior inside minikube, in place — the pod keeps running,
# it just turns bad (then you can heal it again).
#
#   ./chaos-pod.sh chaos-west chaos-target-7f9cbb4d55-x2v4b down
#   ./chaos-pod.sh chaos-east chaos-target-6d5f8c9b7-abcde flapping flap_fail=2
#   ./chaos-pod.sh chaos-west chaos-target-bad-...-zzzzz reset
set -u
NS="${1:?usage: chaos-pod.sh <namespace> <pod> <mode|pause|reset|crash> [key=value...]}"
POD="${2:?pod required}"
WHAT="${3:?mode required}"
shift 3

case "$WHAT" in
  reset) PATH_Q="chaos/reset" ;;
  crash) PATH_Q="chaos/crash" ;;
  pause) PATH_Q="chaos/pause?seconds=${1:-30}" ;;
  *)
    QS="mode=$WHAT"; for kv in "$@"; do QS="$QS&$kv"; done
    PATH_Q="chaos/mode?$QS"
    ;;
esac

# busybox wget ships in the image; --post-data forces a POST.
kubectl exec -n "$NS" "$POD" -- \
  wget -qO- --post-data='' "http://127.0.0.1:8080/$PATH_Q"
echo
