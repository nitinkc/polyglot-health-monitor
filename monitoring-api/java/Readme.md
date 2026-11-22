
Run from the root directory
- Teh DB monitor.db is in the root folder
- Install maven for the build

```shell
DB_PATH="$PWD/monitor.db" mvn -f implementations/java/pom.xml compile exec:java -Dexec.mainClass=com.nitin.monitor.Main
```