# Architecture

MultiSpeed is a Linux-only modular monolith. One Go process serves the versioned API, embedded React application, scheduler, execution workers, Server-Sent Events, retention jobs, and SQLite persistence. The production container adds the separate LibreSpeed CLI executable; Ookla is external and optional.

```text
Browser
  | same-origin HTTP + SSE
  v
Go HTTP server
  |-- API validation and security middleware
  |-- persisted scheduler ----+
  |-- execution coordinator <-+-- manual and scheduled triggers
  |      |-- route validation and per-WAN locks
  |      |-- provider adapter (Cloudflare / LibreSpeed / optional Ookla)
  |      `-- normalized result persistence and events
  |-- statistics, export, backup, retention
  `-- embedded React/Vite assets
             |
             v
       SQLite under /data
```

## Package boundaries

- `cmd/multispeed` owns process startup, signals, version metadata, and the healthcheck subcommand.
- `internal/config` validates the small process-level environment surface.
- `internal/database` owns SQLite connection policy, repositories, transactions, migration state, backup, and maintenance.
- `internal/migrations` embeds monotonic SQL migrations.
- `internal/models` contains provider-neutral persisted/API models.
- `internal/network` discovers interfaces and performs read-only route validation.
- `internal/providers` defines capabilities and normalized execution contracts. Provider-specific subprocess flags and JSON belong only in adapters.
- `internal/execution` coordinates validation, state transitions, cancellation, provider execution, persistence, and SSE publication.
- `internal/scheduler` owns cron/timezone parsing, persisted rescheduling, jitter, overlap rules, bounded concurrency, and graceful shutdown.
- `internal/statistics` and `internal/retention` own analytical and maintenance queries; export streaming is exposed through `internal/api` over database snapshots.
- `internal/api` exposes `/api/v1`, validation, bounded pagination, same-origin protection, rate limits, and error envelopes.
- `internal/events` is a bounded SSE broker with event IDs, heartbeats, reconnection, and slow-consumer cleanup.
- `internal/frontend` embeds the production files generated from `web/dist`.

The API, scheduler, and statistics layers operate on provider-neutral models. They do not switch on provider-specific output formats.

## Execution lifecycle

Manual and scheduled requests enter one coordinator:

1. Persist a `queued` result and publish an event.
2. Acquire the global concurrency slot and, where applicable, the interface/source-address lock.
3. Move to `validating` and verify that the interface and concrete source address still exist.
4. Validate the read-only route profile and public IP on the same path the provider will use.
5. Fail closed on a mismatch; never retry through the default route.
6. Move to `running` and invoke the chosen adapter with a deadline and cancellation context.
7. Normalize, sanitize, and size-limit provider output.
8. Persist the terminal state and publish the result event.
9. Release all locks even on cancellation or panic recovery.

External providers are started with argument arrays, never a user-derived shell command. Cancellation terminates the complete process group so child processes do not survive a timed-out test.

## Scheduling and concurrency

Each task persists a standard five-field cron expression and IANA timezone. The scheduler loads enabled tasks after migrations, calculates next runs without backfilling all downtime, and updates jobs immediately after create, edit, enable, disable, or delete operations. Cron calculations occur in the task timezone and persisted timestamps are UTC.

Concurrency is bounded twice:

- A global semaphore defaults to one active measurement.
- A keyed lock prevents two tests from sharing the same interface/source-address path.

Separate-WAN concurrency remains disabled unless the persisted setting explicitly enables it. `preventOverlap` additionally rejects a second run of the same task.

## Persistence

SQLite is opened with WAL, foreign keys, a busy timeout, explicit transactions, and conservative connection limits. Migrations are embedded in the binary and run before readiness. Timestamps use fixed-width UTC text so lexical comparisons remain chronological; throughput is integer bits per second and byte counts are integers.

The main entities are:

- `tasks`: independent provider, target, schedule, source path, route policy, and execution settings
- `route_profiles`: expected read-only route characteristics and last validation snapshot
- `results`: lifecycle, normalized metrics, selected route, provider metadata, bounded diagnostics, and build provenance
- `settings`: the singleton operational/display configuration

`POST /api/v1/backup` uses SQLite's online consistency mechanism. Retention deletes bounded batches and never removes task definitions.

Task and route-profile deletion is logical deletion: scheduler visibility is removed immediately while immutable historical result references and route snapshots remain intact. Active executions reject deletion with HTTP 409, and referenced route profiles must be detached from live tasks first.

## HTTP and browser security

MultiSpeed deliberately has no authentication. The process default listener is loopback. A specific listen host accepts localhost, loopback, assigned host IPs, the concrete listen host, and exact `APP_TRUSTED_HOSTS` entries. A wildcard bind intentionally accepts any syntactically valid hostname or unicast IP on the listen port and therefore relies on the trusted-network boundary. Browser mutations additionally require a same-origin `Origin`; cross-origin API access is not enabled. Middleware adds request IDs, body/response limits, JSON content checks, security headers, a restrictive CSP, sanitized error envelopes, and focused rate limits.

The frontend ships no CDN assets and talks only to relative `/api/v1` URLs. Remote access belongs behind an authenticating reverse proxy, TLS, and network policy.

## Container boundary

The Compose deployment contains exactly one service and uses `network_mode: host`. It runs non-root with a read-only root filesystem, all Linux capabilities dropped, `no-new-privileges`, a small `/tmp` tmpfs, and only `/data` mounted writable. There is no Docker socket, host-root mount, network administration capability, or route mutation path.

The published image is `linux/amd64`. It contains the Go binary, embedded frontend, CA certificates, timezone data, `ip` for read-only route inspection, and LibreSpeed CLI v1.0.13 with the documented source-bound DNS overlay. It contains no Go toolchain, Node runtime, frontend sources, provider build cache, or Ookla executable.
