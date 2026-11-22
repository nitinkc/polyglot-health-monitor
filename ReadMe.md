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

## App-Monitored
A simple app with just one end point `/health` to do proof of concept.

## Monitoring Services

| Language   | Run Commands                                                                             |
|:-----------|:-----------------------------------------------------------------------------------------|
| Java       | `DB_PATH="$PWD/monitor.db" mvn -f java/pom.xml compile exec:java -Dexec.mainClass=com.nitin.monitor.Main` |
| Go         | `go mod tidy && go run .`                                                                |
| Python     | `pip3 install -r requirements.txt && uvicorn main:app --reload`                          |
| TypeScript | `npm install && npm run dev`                                                             |


## DB

```shell
brew install sqlite3
```

Additional DBeaver or DB Client set up

SQLLite JdBC Connecton 