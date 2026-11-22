#!/usr/bin/env bash
# Exercise every poll + control endpoint of one chaos-target instance.
#   ./smoke.sh http://$(minikube ip):30080   (minikube east NodePort)
#   ./smoke.sh http://localhost:8080         (local `go run`)
set -u
BASE="${1:-http://localhost:8080}"
bold() { printf "\033[1m%s\033[0m\n" "$*"; }

hit() { # path, extra curl args
  local path="$1"; shift
  curl -s -o /dev/null --max-time 3 -w "%{http_code}  %{time_total}s  $path  ${1:-}  %{errormsg}\n" "$@" "$BASE$path"
}

fault() { # fault endpoints are SUPPOSED to make curl fail — show what broke
  local path="$1"; shift
  curl -s -o /dev/null --max-time 2 -w "FAULT  $path -> %{errormsg}\n" "$@" "$BASE$path"
}

bold "== poll endpoints on $BASE =="
hit /health
hit /jitter
hit /jitter
hit /slow -G --data-urlencode "ms=800"
hit /error/429
hit /error/500
hit /error/503
for i in 1 2 3 4 5 6 7; do hit /flap; done
fault /reset            # expect: connection reset by peer
fault /eof              # expect: reset / EOF mid-body
fault /malformed        # expect: HTTP/0.9 or parse error
fault /hang --max-time 1 # expect: operation timed out, 0 bytes

bold "== control endpoints =="
curl -s "$BASE/meta" | head -c 400; echo; echo
hit /chaos/scenario          # running scenario + catalog
hit /chaos/history           # event log
curl -s "$BASE/metrics" | head -8; echo
printf "bogus scenario -> "; curl -s -o /dev/null -w "%{http_code}\n" -X POST "$BASE/chaos/scenario?name=nope"
curl -s -X POST "$BASE/chaos/mode?mode=down" -o /dev/null
echo -n "after mode=down:  /health -> "; curl -s -o /dev/null -w "%{http_code}" "$BASE/health"
echo -n "   /readyz -> ";              curl -s -o /dev/null -w "%{http_code}\n" "$BASE/readyz"
curl -s -X POST "$BASE/chaos/reset" -o /dev/null
echo -n "after reset:      /health -> "; curl -s -o /dev/null -w "%{http_code}\n" "$BASE/health"
