# Changelog

All notable changes to MultiSpeed are documented here. The project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html), and dates use ISO 8601.

## [Unreleased]

### Fixed

- Plot dashboard throughput on a proportional time axis and keep each task's measurements independent from unrelated task timestamps.
- Accept valid LAN hostnames and unicast IPs without a per-host allowlist when listening on `0.0.0.0` or `[::]`.

## [1.0.0] - 2026-08-09

### Added

- Initial modular-monolith implementation of the Go API, scheduler, execution pipeline, SQLite persistence, and embedded React interface.
- Independent tasks with per-task provider, target, schedule, timezone, interface, source address, route profile, timeout, jitter, and overlap settings.
- Read-only Linux interface discovery and fail-closed route-profile validation.
- Native Cloudflare edge measurements, bundled LibreSpeed CLI v1.0.13 integration, and optional externally supplied Ookla adapter.
- Raw and aggregated statistics, comparison, export, backup, retention, system information, and SSE endpoints.
- Strict, versioned configuration export and atomic import for settings, tasks, and route profiles without transferring result history or EULA acceptance.
- Hardened single-service Docker deployment for `linux/amd64`.
- Pinned CI, security scanning, SBOM, release, provenance, and GHCR workflows.
- Architecture, networking, provider, API, deployment, development, backup, security, and contribution documentation.
- Persisted, revocable Ookla EULA acknowledgement in Settings with an explicit confirmation gate and acceptance timestamp.

### Fixed

- Pass only the validated source IP to Ookla CLI 1.2.0.84 instead of combining its mutually exclusive `--interface` and `--ip` options.
- Normalize LibreSpeed CLI v1.0.13 JSON throughput from its observed Mbit/s wire values into integer bit/s.
- Preserve Cloudflare upload transfer time instead of subtracting request-body receipt time, and avoid double-counting overlapping Server-Timing metrics.

### Security

- Loopback process default, same-origin mutation checks, restrictive browser headers, request/output bounds, execution/discovery rate limits, and sanitized diagnostics.
- All-request trusted-Host enforcement protects the unauthenticated listener from DNS rebinding.
- Custom LibreSpeed base URLs require an exact deployment-owned allowlist entry; authorized runs use source-bound DNS, pin the CLI to the pre-resolved IP:port set, and reject redirects before follow-up requests.
- Result pagination avoids request-derived allocation capacity and fails safely for extreme page numbers; statistics cap input rows, materialized points, group cardinality, dimension size, and filter lists.
- React Router was upgraded to 8.3.0 to remediate GHSA-qwww-vcr4-c8h2.
- `modernc.org/sqlite` was upgraded to 1.56.0 for the upstream SQLite 3.53.3 journal-rollback data-corruption fix.
- The LibreSpeed `multispeed.dns2.xnet055` overlay binds UDP and TCP DNS resolution to the selected WAN source address, pins authorized custom runs against DNS rebinding and cross-origin redirects, upgrades `golang.org/x/net` to v0.55.0, and rejects unpatched replacement executables.
- CI, security, and pre-publication image scans use Trivy 0.73.0.
- Non-root read-only container with all capabilities dropped and `no-new-privileges`.
- Ookla Speedtest CLI is never downloaded or redistributed by the project image; in-app acceptance remains separate from installation and licensing permission.

[Unreleased]: https://github.com/dude2k/MultiSpeed/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/dude2k/MultiSpeed/releases/tag/v1.0.0
