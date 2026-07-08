# Polyglot Health Monitor

**A single service, built four times, to make language nuances muscle memory.**

## Why this project exists

This project is a structured, hands-on path from "expert Java developer" to
**Forward Deployed Engineer** — someone who can be dropped into an unfamiliar
client stack and write idiomatic, production-grade code on day one, in whatever
language that stack happens to use.

The premise is simple: pick one project with enough real engineering surface
area — concurrency, I/O, timeouts, persistence, graceful shutdown — and build it
four separate times, once each in **Java, Go, Python, and TypeScript**. Same
external contract every time. Different internals, on purpose. The goal isn't to
translate code from one language into another — it's to feel each language's own
answer to the same problems: how does *this* language want you to bound
concurrency? Cancel a request? Propagate an error?

## The project: a Concurrent URL/Service Health Monitor

A REST API for registering "monitors" — a URL, a check interval, a timeout, and
a failure threshold — backed by a scheduler that pings each one independently
and tracks whether it's `UP` or `DOWN`.

It's small enough to finish in a weekend per language, but has just enough teeth
that you can't fake your way through with boilerplate:

- **Independent concurrent scheduling** — every monitor runs on its own timer;
  adding monitor #50 can't delay monitor #1
- **Bounded concurrency** — a global cap on in-flight HTTP checks, so the rate
  limiter idiom (semaphore, channel, or queue) has to show up in every language
- **Real timeout enforcement** — the configured timeout has to cancel the
  in-flight request at the transport level, not just stop waiting for it
- **Graceful shutdown** — no orphaned goroutines, threads, or tasks on `SIGTERM`
- **Structured logging, env-based config, and a real persistence layer**

## One contract, four idioms

| Language   | Framework                          | Concurrency primitive                         |
|:-----------|:-----------------------------------|:----------------------------------------------|
| Java       | Javalin (thin, no DI)              | Virtual threads + `Semaphore`                 |
| Go         | Standard library `net/http`        | Goroutines + `context` + channel-as-semaphore |
| Python     | FastAPI (routing only) + `asyncio` | `asyncio.Task` + `asyncio.Semaphore`          |
| TypeScript | Express                            | `setInterval` + `AbortController` + `p-limit` |

Frameworks were chosen deliberately *thin* — no Spring Boot, no NestJS, no Gin —
so the language does the work being practiced, not the framework's opinions on
top of it.

## Project artifacts

- **[Shared spec](spec.md)** — the full contract: data model, REST API,
  behavioral requirements, and testing standards every implementation must meet
- **[OpenAPI spec](API.md)** — machine-readable API contract, importable
  into Swagger/Postman/Insomnia for interactive testing
- **[Database schema](db.md)** — one SQLite schema shared verbatim across
  all four implementations, so a monitor created from the Go server is
  immediately visible from the Python server

## Build order

1. **Java** — reference implementation; fast to build given existing expertise
2. **Go** — the biggest conceptual jump; tackled early while motivation is high
3. **Python (asyncio)** — closest to typical day-to-day FDE integration work
4. **TypeScript/Node** — event-loop reasoning and strict typing discipline

An optional fifth pass swaps local SQLite for a remote **Postgres database
(via Neon)**, adding connection pooling and retry-with-backoff under real
network latency — a closer approximation of a client's actual environment.

## What "done" looks like

Four servers, each idiomatic in its own language, that produce byte-for-byte
identical JSON for the same request. Alongside them, a short personal write-up
comparing how each language handled cancellation, concurrency bounding, and
error propagation — arguably the more valuable artifact of the two, since
that comparison is exactly the kind of cross-stack fluency an FDE interview
tests for.