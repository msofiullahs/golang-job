# GoQueue

A high-performance job queue and background worker system built from scratch in Go. Inspired by Laravel Horizon, designed to demonstrate deep understanding of concurrency, queue systems, and production-ready Go patterns.

## Features

### Core
- **Priority Queues** — Jobs ordered by priority (`critical > high > normal > low`) using `container/heap`. Higher-priority jobs are always processed first; within the same priority, oldest jobs go first (FIFO).
- **Worker Pools** — Each named queue runs its own pool of goroutines with configurable concurrency. Workers are managed via `Start()`, `Stop()`, and `Scale()`.
- **Retry with Exponential Backoff** — Failed jobs are automatically retried with jittered exponential backoff (5s, 10s, 20s, 40s... capped at 30min). Jitter prevents thundering herd when many jobs fail at once.
- **Dead Letter Queue** — Jobs that exhaust all retry attempts are moved to the DLQ. You can inspect, retry, or delete dead jobs from the dashboard.
- **Scheduled / Delayed Jobs** — Enqueue jobs to run after a delay (`Delay: 10 * time.Second`) or at a specific time (`ScheduleAt: time.Date(...)`). A scheduler goroutine promotes due jobs every second.
- **Rate Limiting** — Token-bucket rate limiter per queue to control throughput (e.g., max 100 jobs/sec for an API queue).
- **Graceful Shutdown** — Listens for `SIGINT`/`SIGTERM`, stops accepting new jobs, waits for in-flight jobs to finish (with configurable timeout), then exits cleanly.

### Middleware
Composable `func(Handler) Handler` chain — the same pattern as `net/http` middleware:
- **Recovery** — Catches panics in job handlers, converts them to errors, logs stack traces.
- **Logging** — Structured logging via `slog` for every job start, completion, and failure with duration.
- **Timeout** — Enforces per-job execution timeout via `context.WithTimeout`.
- **Metrics** — Records processed/succeeded/failed counters per queue.

### Observability
- **Web Dashboard** — Real-time monitoring SPA embedded in the binary via `go:embed`. Shows live stats, queue depths, job table with retry/delete actions. Updates via Server-Sent Events.
- **Prometheus Metrics** — `/metrics` endpoint exposing `goqueue_jobs_processed_total`, `goqueue_jobs_failed_total`, `goqueue_throughput_jobs_per_second`, and more.
- **REST API** — Full JSON API for stats, queue listing, job inspection, retry, and delete.

### Storage Backends
- **In-Memory** — Zero-dependency backend using `container/heap` + `sync.Cond` for blocking dequeue. Great for development and single-instance deploys.
- **Redis** — Persistent, distributed backend using sorted sets (`ZSET`) with a composite score (`priority * 1e18 + timestamp`) for O(log N) priority-aware dequeue. Atomic operations via Lua scripts to prevent double-delivery.

## Requirements

- **Go 1.22+** (uses `net/http` method-based routing: `GET /path`)
- **Redis 5.0+** (optional — only if using the Redis backend, requires `BZPOPMIN`)
- **Docker & Docker Compose** (optional — for containerized setup)

## Installation

### From source

```bash
# Clone the repository
git clone https://github.com/sofi/goqueue.git
cd goqueue

# Download dependencies
go mod download

# Build the binary
make build
# Binary is created at ./bin/goqueue
```

### Using `go install`

```bash
go install github.com/sofi/goqueue/cmd/goqueue@latest
```

## Running

### Quick start (in-memory backend)

```bash
# Using make
make run

# Or directly
go run ./cmd/goqueue server

# Or from the built binary
./bin/goqueue server
```

This starts:
- **3 worker queues**: `critical` (5 workers), `default` (10 workers), `low` (3 workers)
- **Web dashboard** at http://localhost:8080
- **Demo jobs** that auto-enqueue to show the system in action

### With Redis backend

```bash
# Start Redis (if not already running)
redis-server

# Run with Redis
USE_REDIS=true REDIS_ADDR=localhost:6379 go run ./cmd/goqueue server
```

### With Docker Compose

```bash
# Start GoQueue + Redis
make docker-up

# View logs
docker compose logs -f goqueue

# Stop
make docker-down
```

### Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DASHBOARD_ADDR` | `:8080` | Dashboard and API listen address |
| `USE_REDIS` | `false` | Use Redis backend instead of in-memory |
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `REDIS_PASSWORD` | _(empty)_ | Redis password |
| `REDIS_DB` | `0` | Redis database number |

### CLI Commands

```bash
goqueue server     # Start the worker server with dashboard
goqueue version    # Print version
goqueue help       # Show help
```

## Architecture

```
                    +-----------+
                    |  CLI /    |
                    |  Client   |
                    +-----+-----+
                          |
                    Enqueue(job)
                          |
                          v
    +---------------------------------------------+
    |                   Broker                     |
    |                                              |
    |  +----------+  +----------+  +-----------+  |
    |  | Queue:   |  | Queue:   |  | Queue:    |  |
    |  | critical |  | default  |  | low       |  |
    |  | (w:5)    |  | (w:10)   |  | (w:3)     |  |
    |  +----+-----+  +----+-----+  +----+------+  |
    |       |             |             |          |
    |  +----v-----+  +----v-----+  +----v------+  |
    |  | Worker   |  | Worker   |  | Worker    |  |
    |  | Pool     |  | Pool     |  | Pool      |  |
    |  +----+-----+  +----+-----+  +----+------+  |
    |       |             |             |          |
    |       +--------+----+----+--------+          |
    |                |         |                   |
    |          +-----v---+ +---v-------+           |
    |          |Completed| |Failed/DLQ |           |
    |          +---------+ +-----------+           |
    |                                              |
    |  +------------+  +----------+  +----------+  |
    |  | Scheduler  |  | Rate     |  | Metrics  |  |
    |  | (delayed)  |  | Limiter  |  | (Prom)   |  |
    |  +------------+  +----------+  +----+-----+  |
    +---------------------------------------------+
                                          |
                        +-----------------+---------+
                        |                           |
                  +-----v-------+         +---------v------+
                  |  Dashboard  |         |  Prometheus    |
                  |  (SSE+REST) |         |  /metrics      |
                  +-------------+         +----------------+

    Storage Backends:
    +----------------+     +------------------+
    | In-Memory      |     | Redis            |
    | (heap + Cond)  | OR  | (ZSET + Lua)     |
    +----------------+     +------------------+
```

## Usage as a Library

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"

    "github.com/sofi/goqueue/pkg/broker"
    "github.com/sofi/goqueue/pkg/job"
    "github.com/sofi/goqueue/pkg/middleware"
    "github.com/sofi/goqueue/pkg/queue"
)

func main() {
    logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

    // 1. Pick a backend
    backend := queue.NewMemory()

    // 2. Register job handlers
    registry := job.NewRegistry()
    registry.Register("email.send", job.HandlerFunc(
        func(ctx context.Context, payload []byte) error {
            fmt.Printf("Sending email: %s\n", payload)
            return nil
        },
    ))

    // 3. Create and configure the server
    srv := broker.NewServer(backend, registry, logger)
    srv.Use(
        middleware.Recovery(logger),
        middleware.Logging(logger),
        middleware.Timeout(5 * time.Minute),
    )
    srv.AddQueue(broker.QueueConfig{Name: "default", Concurrency: 5})

    // 4. Enqueue jobs
    srv.OnStarted = func() {
        go func() {
            srv.Enqueue(context.Background(), "email.send",
                map[string]string{"to": "user@example.com"},
                job.Options{
                    Queue:      "default",
                    Priority:   job.PriorityHigh,
                    MaxRetries: 3,
                },
            )
        }()
    }

    // 5. Start (blocks until SIGINT/SIGTERM)
    srv.Start(context.Background())
}
```

### Enqueue with delay

```go
srv.Enqueue(ctx, "report.generate", payload, job.Options{
    Queue:      "low",
    Priority:   job.PriorityNormal,
    MaxRetries: 3,
    Delay:      10 * time.Second, // runs 10s from now
})
```

### Enqueue at specific time

```go
srv.Enqueue(ctx, "report.generate", payload, job.Options{
    Queue:      "low",
    MaxRetries: 3,
    ScheduleAt: time.Date(2026, 4, 1, 9, 0, 0, 0, time.Local),
})
```

## Dashboard

The web dashboard is accessible at http://localhost:8080 when the server is running.

**What it shows:**
- Real-time stats cards — processed, succeeded, failed, enqueued, retried, dead, throughput (jobs/sec)
- Queue overview — bar chart of pending/processing/completed/failed/dead per queue
- Job table — browse jobs by queue, see status, attempts, errors, created time
- Actions — retry or delete failed/dead jobs directly from the UI

**API endpoints:**
```
GET  /api/stats                    Aggregate metrics + queue stats
GET  /api/queues                   All queues with counts
GET  /api/queues/{name}/jobs       Jobs in a queue (?status=failed)
GET  /api/jobs/{id}                Single job detail
POST /api/jobs/{id}/retry          Retry a failed/dead job
DELETE /api/jobs/{id}              Delete a job
GET  /api/events                   SSE stream for live updates
GET  /metrics                      Prometheus metrics
```

## Project Structure

```
cmd/goqueue/        CLI entrypoint (server, version, help)
pkg/
  job/              Handler interface, Info struct, Priority, Status, Registry, BackoffFunc
  queue/            Queue interface, in-memory backend (heap + Cond), Redis backend (ZSET + Lua)
  worker/           Worker goroutine loop, Pool (Start/Stop/Scale), PoolOptions
  broker/           Server orchestrator — ties queues + pools + scheduler + shutdown
  scheduler/        Promotes delayed/scheduled jobs when their time arrives
  middleware/       Recovery, Logging, Timeout, Metrics — composable chain
  ratelimit/        Token-bucket rate limiter per queue
  metrics/          Internal collector, Prometheus text exposition
  config/           Environment-based configuration
internal/
  dashboard/        HTTP handlers, SSE hub, embedded static SPA (Tailwind + vanilla JS)
examples/
  basic/            Minimal 30-line usage example
  full/             Full demo: multiple queues, dashboard, continuous job stream
bench/              Benchmarks for enqueue/dequeue throughput
```

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Standard library first | `net/http`, `container/heap`, `sync.Cond`, `slog`, `embed` — no frameworks |
| Interface-driven | `Queue` interface allows swapping memory/Redis without changing worker code |
| `func(Handler) Handler` middleware | Same pattern as `net/http` middleware — idiomatic Go |
| Jittered exponential backoff | Prevents thundering herd on mass retries |
| SSE over WebSockets | Simpler, unidirectional, works through HTTP/2 |
| Embedded dashboard | `go:embed` produces a single self-contained binary |
| `sync.Cond` for blocking dequeue | Efficient blocking without polling in the in-memory backend |
| Composite ZSET score in Redis | `priority * 1e18 + timestamp` gives O(log N) priority dequeue |
| Lua scripts for atomic dequeue | Prevents double-delivery in the Redis backend |

## Development

```bash
make build       # Build binary to ./bin/goqueue
make run         # Build and run the server
make test        # Run tests with race detector
make bench       # Run benchmarks
make lint        # Run go vet
make fmt         # Format code
make clean       # Remove build artifacts
make docker      # Build Docker image
make docker-up   # Start with docker-compose (GoQueue + Redis)
make docker-down # Stop docker-compose
make help        # List all targets
```

## License

MIT
