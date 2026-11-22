## Documentation

[https://nitinkc.github.io/polyglot-health-monitor/](https://nitinkc.github.io/polyglot-health-monitor/)

## Dashboard

One place to run all four implementations, validate each against the API
spec, and watch `monitor.db` — no manual `curl` / `sqlite3` lookups.

```shell
uv run dashboard/dashboard.py   # from the repo root, then open http://localhost:9000
```

Per-language cards let you start/stop each server (default ports
Java :8081, Go :8082, Python :8083, TypeScript :8084 — override with
`PORT_<LANG>` env vars), stream its logs, and show a live pass/fail matrix
of the spec endpoints plus a DB-persistence check. The bottom panels show
the `monitors` and `check_results` tables from `monitor.db`.

## Installations
For Java
- Brew - `brew install maven`


Run commands:

| Language   | Run Commands                                                                             |
|:-----------|:-----------------------------------------------------------------------------------------|
| Java       | `DB_PATH="$PWD/monitor.db" mvn -f java/pom.xml compile exec:java -Dexec.mainClass=com.nitin.monitor.Main` |
| Go         | `go mod tidy && go run .`                                                                |
| Python     | `pip3 install -r requirements.txt && uvicorn main:app --reload`                          |
| TypeScript | `npm install && npm run dev`                                                             |


### java

```bash
export JAVA_HOME=$(/usr/libexec/java_home -v 26)
export PATH="$JAVA_HOME/bin:$PATH"

java -version
javac -version
mvn -version
```

### Python

```shell

```

### Go
```shell
brew install go

export GOPATH=$HOME/go
export GOMODCACHE=$HOME/go/pkg/mod

mkdir -p "$GOMODCACHE"

go mod tidy
go run .
```

## DB

brew install sqlite3

```shell
python3 -m venv .venv
source .venv/bin/activate
pip install --upgrade pip
pip install -r requirements.txt

.venv/bin/python -m mkdocs serve
```