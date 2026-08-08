# API

MultiSpeed exposes a same-origin JSON API under `/api/v1`. The normative OpenAPI 3.1 document is [openapi.yaml](../openapi.yaml).

There is no authentication. The process defaults to loopback, every request validates its `Host`, cross-origin API access is disabled, and browser mutation requests validate `Origin`. An absent `Origin` remains usable for trusted command-line automation on the same protected network; network access to the listener is the security boundary. `APP_TRUSTED_HOSTS` explicitly opts exact proxy or LAN DNS names into the Host policy.

## Conventions

- JSON request bodies require `Content-Type: application/json`.
- Timestamps are RFC 3339 UTC strings.
- Throughput fields are integer bits per second; durations identify their units in field names.
- Optional/unsupported metrics are `null`, not sentinel values.
- Every result exposes `tlsVerificationDisabled`; audit any `true` value because the test used an explicit certificate-verification bypass.
- Collection endpoints use bounded `page` and `pageSize` parameters and return pagination metadata.
- Invalid filters, UUIDs, provider IDs, cron expressions, timezones, addresses, targets, and body fields are rejected.
- Responses include `X-Request-ID`; provide it when reporting an error.

Errors use one envelope:

```json
{
  "error": {
    "code": "ROUTE_VALIDATION_FAILED",
    "message": "The configured route does not use the selected WAN interface.",
    "requestId": "4ea13778-8fd7-40e4-8e4f-74665f5c01cc"
  }
}
```

Messages are safe for display but deliberately omit internal paths, command output beyond sanitized bounds, and environment values.

## Main resources

- `/healthz`, `/readyz`: liveness and readiness
- `/tasks`: CRUD, duplication, validation, manual runs, and next-run preview
- `/results`: paginated/filterable results and bounded deletion
- `/statistics`: raw or aggregated metrics and comparisons; queries are bounded to 50,000 input rows, 5,000 materialized output points, and 1,000 groups
- `/interfaces`: discovery and explicit refresh
- `/route-profiles`: CRUD and read-only path validation
- `/providers`: capabilities, availability, server discovery, and server validation
- `/settings`: singleton operational/display settings
- `/settings/ookla-eula`: explicit, separately persisted Ookla acceptance or revocation; acceptance requires `confirmed=true`
- `/retention/cleanup`: bounded manual result cleanup using policy or a past cutoff
- `/exports/results.csv`, `/exports/results.json`: filtered result exports
- `/config/export`, `/config/import`: versioned portable configuration download and atomic restore
- `/backup`: consistent online SQLite backup download
- `/events`: Server-Sent Events
- `/system`: sanitized build, database, provider, interface, and uptime information

Manual execution and provider discovery are rate-limited. A `429` response may include `Retry-After`.

## Server-Sent Events

Connect with:

```bash
curl -N -H 'Accept: text/event-stream' http://127.0.0.1:8787/api/v1/events
```

Events carry monotonic IDs and JSON data, but the bounded broker is not a replay log. Browsers reconnect automatically, and clients must refetch authoritative REST resources whenever the stream opens. Heartbeats keep intermediaries from closing an idle stream. Slow consumers are disconnected instead of blocking execution or silently missing events.

## Backup example

```bash
curl --fail-with-body \
  --request POST \
  --output multispeed-backup.db \
  http://127.0.0.1:8787/api/v1/backup
```

Keep the backup private: it contains task configuration, route/interface details, public IP history, and diagnostics.

## Configuration transfer example

Export settings, active tasks, and active route profiles without measurement history or licensing acknowledgement state:

```bash
curl --fail-with-body \
  --output multispeed-config.json \
  http://127.0.0.1:8787/api/v1/config/export
```

Import the complete document only after reviewing it. The operation replaces the current portable configuration in one transaction, preserves results and Ookla EULA acceptance, and returns `409` while a test is queued, validating, or running:

```bash
curl --fail-with-body \
  --request POST \
  --header 'Content-Type: application/json' \
  --data-binary @multispeed-config.json \
  http://127.0.0.1:8787/api/v1/config/import
```

Only format version `1` is accepted. Unknown, missing, oversized, duplicate-ID, invalid schedule/target, dangling route-profile, and invalid enabled-interface configurations are rejected before persistence. Process-level environment policy is deliberately not portable: an imported custom LibreSpeed task is accepted only when its canonical base URL is already present in that deployment's `APP_ALLOWED_CUSTOM_SERVER_URLS` allowlist.

## API changes

Breaking API changes follow project semantic versioning. Additive fields may appear in minor releases; clients should ignore unknown response fields. The checked-in OpenAPI file is validated in CI and must change with handlers and API-facing models.
