# Demo App API Reference

Two fixture apps live in `monitored-app/` — the upstreams your health monitors
poll. `chaos-target` has a full OpenAPI contract (rendered below);
`0-simple-demo` is a single-route FastAPI app documented inline.

## `0-simple-demo` — the always-good baseline

A FastAPI app with one route and no configuration.

| Route | Response |
| ----- | -------- |
| `GET /health` | `200 OK`, `text/plain` body `OK` |

## `chaos-target` — controllable fixture

The API has two planes:

- **Poll endpoints** (`/health`, `/ok`, `/jitter`, `/slow`, `/hang`,
  `/error/{code}`, `/reset`, `/eof`, `/malformed`, `/flap`) — what a monitor's
  `url` points at. Every poll request passes through the instance's current
  **mode gate** before the route's own behavior runs, so a single
  `POST /chaos/mode` turns a whole "server" bad.
- **Control endpoints** (`/chaos/*`, `/meta`, `/healthz`, `/readyz`,
  `/metrics`) — always reachable, even while the instance is "down", so you can
  flip, pause, crash, heal, and run timed **scenarios** mid-test.

Every response carries `X-Instance-Id`, `X-Region`, and `X-Mode` headers; poll
responses also carry `X-Request-Seq`, so callers behind a load-balanced URL can
tell which replica answered.

Modes: `healthy` `jitter` `degraded` `slow` `flapping` `reset` `down` — set with
`POST /chaos/mode` (query params or JSON body), applied to *every* poll
endpoint. Canned timed sequences (`outage`, `brownout`, `flap-storm`,
`slow-leak`, `chaos-tour`) run via `POST /chaos/scenario?name=X`.

<swagger-ui src="chaos-target.openapi.yaml"/>
