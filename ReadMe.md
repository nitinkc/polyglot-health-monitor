[https://nitinkc.github.io/polyglot-health-monitor/](https://nitinkc.github.io/polyglot-health-monitor/)

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

## Documentation

[https://nitinkc.github.io/polyglot-health-monitor/](https://nitinkc.github.io/polyglot-health-monitor/)

```shell
python3 -m venv .venv
source .venv/bin/activate
pip install --upgrade pip
pip install -r requirements.txt

.venv/bin/python -m mkdocs serve
```