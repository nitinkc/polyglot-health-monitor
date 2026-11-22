# ---- build ---------------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/chaos-target .

# ---- runtime -------------------------------------------------------------
# Alpine (not scratch) on purpose: iproute2 gives real `tc netem` packet
# delay/loss, and busybox wget lets `kubectl exec` audit scripts read /meta.
FROM alpine:3.20
RUN apk add --no-cache iproute2 && \
    addgroup -S app && adduser -S app -G app
COPY entrypoint.sh /entrypoint.sh
COPY --from=build /out/chaos-target /usr/local/bin/chaos-target
RUN chmod +x /entrypoint.sh
USER app
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=2s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/entrypoint.sh"]
