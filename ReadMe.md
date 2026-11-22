## Documentation

[https://nitinkc.github.io/polyglot-health-monitor/](https://nitinkc.github.io/polyglot-health-monitor/)


```shell
cd docs/
uv venv
source .venv/bin/activate
uv pip install -r requirements.txt

mkdocs serve
```

## UI Dashboard

One place to run all four implementations, validate each against the API
spec, and watch `monitor.db` — no manual `curl` / `sqlite3` lookups.

```shell
uv run ui-dashboard/dashboard.py   # from the repo root, then open http://localhost:9000
```

## Monitored Apps

Two fixture apps under `monitored-app/` — the upstreams your monitors poll:

- `0-simple-demo/` — FastAPI app with just one endpoint `/health` (proof of concept)
- `chaos-target/` — controllable Go fixture: mode-gated poll endpoints plus a
  `/chaos/*` control plane (modes, pause/crash, timed **scenarios**, event
  history, `/metrics`). Run `go run .` in that dir, deploy the fleet via
  `monitored-app/k8s/minikube.yaml`, or use the dashboard's **Chaos** tab.

## Monitoring Services

| Language | Run Commands |
| -------- | ------------ |
| Java | `DB_PATH="$PWD/monitor.db" mvn -f monitoring-api/java/pom.xml compile exec:java -Dexec.mainClass=com.nitin.monitor.Main` |
| Go | `cd monitoring-api/go && go mod tidy && go run .` |
| Python | `cd monitoring-api/python && pip3 install -r requirements.txt && uvicorn main:app --reload` |
| TypeScript | `cd monitoring-api/typescript && npm install && npm run dev` |


## DB

```shell
brew install sqlite3
```

Additional DBeaver or DB Client set up

SQLLite JdBC Connecton