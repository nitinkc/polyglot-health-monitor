# Monitored Apps — the upstreams your monitors poll

Two fixture apps live here:

| App | What it is |
| --- | ---------- |
| `0-simple-demo/` | FastAPI app with a single `GET /health` route (`200 OK`, plain-text `OK`) — the "always good" baseline |
| `chaos-target/` | Single-binary Go app with mode-gated poll endpoints and a `/chaos/*` control plane — simulate healthy, jittered, erroring, slow, hanging, resetting, flapping upstreams |

## Run the fixtures on minikube

The `chaos-target` fleet runs as real Kubernetes workloads — namespaces
`chaos-east` (3 jittered replicas) and `chaos-west` (2 healthy + 1 born-bad
flapping pod sharing the Service → intermittent 503s).

```shell
minikube start
eval $(minikube docker-env)                  # build the image inside minikube
docker build -t chaos-target:latest chaos-target
kubectl apply -f k8s/minikube.yaml
kubectl get pods -n chaos-west -w            # one pod flaps on purpose
```

Host access: `$(minikube ip):30080` (east) / `$(minikube ip):30081` (west).

```shell
scripts/audit.sh                                    # spot the bad server
scripts/chaos-pod.sh chaos-west <pod-name> down     # break one pod
scripts/chaos-pod.sh chaos-west <pod-name> reset    # heal it
```

## Run the simple demo

```shell
cd 0-simple-demo
uv run fastapi dev --port 8090
curl localhost:8090/health          # → OK
```

## Docs

Full details — endpoints, modes, scenarios, bare-metal runs, and env config:

- [demo-app.md](../docs/documentation/demo-app.md) — app guide
- [demo-api.md](../docs/documentation/demo-api.md) — API reference
  ([chaos-target.openapi.yaml](../docs/documentation/chaos-target.openapi.yaml))

The [UI dashboard](../ui-dashboard/) Chaos tab manages a target on port 9020 —
mode buttons, scenarios, event history, and one-click "watch" that registers
`/health` as a monitor on a running implementation.
