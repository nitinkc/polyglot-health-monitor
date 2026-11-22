# ---- build ---------------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/chaos-target .

# ---- runtime -------------------------------------------------------------
# Alpine (not scratch) on purpose: busybox wget lets `kubectl exec` audit
# scripts read /meta and /chaos/* from inside the pod.
FROM alpine:3.20
RUN addgroup -S app && adduser -S app -G app
COPY --from=build /out/chaos-target /usr/local/bin/chaos-target
USER app
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=2s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["chaos-target"]
