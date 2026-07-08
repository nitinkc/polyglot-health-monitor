Run commands:

| Language   | Run Commands                                                                             |
|:-----------|:-----------------------------------------------------------------------------------------|
| Java       | `mvn compile exec:java -Dexec.mainClass=com.nitin.monitor.Main (or package + java -jar)` |
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
