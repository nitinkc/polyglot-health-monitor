## Simple App


```shell
cd demo-app/health-demo
uv run fastapi dev --port 8090
```

Only one end point 
```shell
curl --location 'localhost:8090/health'
```