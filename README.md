# MultiSpeed

MultiSpeed is a self-hosted Linux monitor for measuring and comparing several WAN connections from one host. Each persisted task owns its provider, schedule, timezone, target, interface, concrete source address, route profile, timeout, and overlap policy. Manual and scheduled runs use the same fail-closed execution path.

> **Security warning**
>
> MultiSpeed does not include authentication and must only be exposed to trusted networks unless protected by an authenticating reverse proxy.

The application is a single Go process with an embedded React interface and SQLite database. The supplied deployment runs one hardened container with host networking so source-address binding and read-only route inspection operate in the host network namespace.

## Highlights

- Independent five-field cron schedules with IANA timezones and next-run previews
- Explicit Linux interface and source IPv4/IPv6 selection per task
- Read-only route profiles that validate the intended interface, source, gateway, route table, reachability, and public IP before testing
- Ookla® Speedtest®, LibreSpeed, and native Cloudflare® edge adapters behind one normalized provider contract
- Download, upload, latency, jitter, packet loss where available, success rates, and execution diagnostics
- Raw, daily, ISO-weekly, monthly, yearly, and custom-range statistics
- WAN, task, provider, target, route-profile, source-address, and public-IP comparisons
- Responsive dark/light/system themes and live Server-Sent Event updates
- CSV/JSON result exports, portable configuration import/export, online SQLite backup, retention, and maintenance controls
- No external database, Redis, worker, cron container, frontend container, or Docker socket

## Provider support

| Provider | Packaging | Target modes | Network binding | Important limitation |
| --- | --- | --- | --- | --- |
| Cloudflare | Native Go adapter | Automatic edge selection | Concrete source IP | Its multi-request edge methodology is not directly equivalent to Ookla or LibreSpeed. |
| LibreSpeed CLI 1.0.13 | Built from the official tagged source into the image | Automatic, public server ID, custom server definition | Concrete source IP (`--source`) | Telemetry is disabled by default; custom servers are operator-controlled. |
| Speedtest CLI by Ookla 1.2.0.84 | **Not included or downloaded by MultiSpeed images** | Automatic or server ID | Concrete source IP (validated against the selected interface) | Requires an operator-supplied executable, explicit terms acknowledgement/CLI flag authorization, and any separate permission required by Ookla. |

Cloudflare is a trademark and/or registered trademark of Cloudflare, Inc. MultiSpeed is not affiliated with, endorsed by, or sponsored by Cloudflare, Inc.

Ookla® and Speedtest® are registered trademarks of Ookla, LLC. MultiSpeed is an independent project and is not affiliated with, endorsed by, or sponsored by Ookla.

See [Provider behavior](docs/providers.md) for capabilities, flags, licensing, and test-method differences.

## Requirements

- A Linux Docker host with Docker Engine and Compose v2
- `linux/amd64`; no other image architecture is published until every provider path is tested there
- One or more non-loopback interfaces with a concrete source address
- Source-based policy routing configured on the **host** when different WANs use different gateways
- A writable persistent directory owned by the non-root container UID/GID

The published image runs as UID/GID `10001:10001` unless the deployment explicitly overrides the container user. The supplied Compose file intentionally overrides it with `MULTISPEED_UID:MULTISPEED_GID`, which defaults to `1000:1000`. Environment variables named `PUID` or `PGID` are not interpreted by MultiSpeed.

Docker Desktop's host networking does not provide the same Linux host-interface semantics. Run MultiSpeed on the Linux host whose WAN paths it measures.

## Install with Docker Compose

```bash
git clone https://github.com/dude2k/MultiSpeed.git
cd MultiSpeed
cp .env.example .env
docker compose up -d
```

The tracked `data/` directory and the default Compose UID/GID (`1000:1000`) suit a typical first Linux user. If `id -u` or `id -g` differs, set `MULTISPEED_UID` and `MULTISPEED_GID` in `.env`, then ensure `./data` is writable by that identity.

Inspect startup and readiness:

```bash
docker compose ps
docker compose logs --follow multispeed
curl --fail http://127.0.0.1:8787/api/v1/healthz
curl --fail http://127.0.0.1:8787/api/v1/readyz
```

Compose intentionally sets `APP_LISTEN_ADDR=0.0.0.0:8787` for trusted-LAN access. A wildcard bind accepts concrete unicast IP addresses on port 8787 without a per-IP allowlist, so direct access such as `http://192.0.2.10:8787` works. Arbitrary DNS hostnames remain blocked against DNS rebinding; list only DNS names you intentionally use in `APP_TRUSTED_HOSTS`. Restrict traffic with host firewall rules or place an authenticating, TLS-terminating reverse proxy in front. Never expose port 8787 directly to the public internet.

To use a published image explicitly:

```bash
docker pull ghcr.io/dude2k/multispeed:latest
docker compose up -d --no-build
```

Tagged releases publish `latest`, major, major.minor, and full semantic-version tags.

## First run

1. Open `http://<linux-host>:8787` from the trusted network.
2. Open **System Information** and confirm that MultiSpeed sees the Linux host interfaces and the source addresses used by the intended WAN paths. Seeing only a Docker bridge interface such as `eth0` with a container-private address means the container is in the wrong network namespace; switch it to host networking before creating tasks.
3. Review the operational defaults in **Settings**.
4. Create and validate a route profile for every path whose gateway or routing table must be enforced.
5. Prepare the selected provider. The Ookla terms acknowledgement/CLI flag authorization and executable installation are separate steps described below.
6. Create a task, validate its current configuration, then run one manual test before relying on the schedule.
7. Create and verify a backup appropriate for the state you need to preserve.

## Install with `docker run`

```bash
mkdir -p data
docker run -d \
  --name multispeed \
  --platform linux/amd64 \
  --network host \
  --restart unless-stopped \
  --init \
  --user "$(id -u):$(id -g)" \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  --mount type=bind,src="$(pwd)/data",dst=/data \
  -e APP_LISTEN_ADDR=0.0.0.0:8787 \
  -e APP_DATA_DIR=/data \
  -e TZ=Europe/Berlin \
  -e ACCEPT_OOKLA_EULA=false \
  ghcr.io/dude2k/multispeed:latest
```

The image has no `EXPOSE`/port-publishing dependency because host networking is required. The application itself defaults to `127.0.0.1:8787` outside Compose.

## Install on Unraid or directly from GHCR

Use these settings when creating an Unraid container from `ghcr.io/dude2k/multispeed:<version>` or another direct-image deployment:

- Set **Network Type** to **Host**. Do not add a container-port mapping; MultiSpeed must share the network namespace that contains the WAN interfaces.
- Map one persistent host app-data directory read/write to container path `/data`.
- Leave the image user at its default `10001:10001`, or explicitly override the container user and use that same numeric identity for the data directory. `PUID` and `PGID` variables have no effect.
- Set `APP_LISTEN_ADDR=0.0.0.0:8787` for trusted-LAN access. Concrete unicast IPs require no allowlist. Leave `APP_TRUSTED_HOSTS` empty when using IP URLs; add only intentionally used LAN/reverse-proxy DNS names. It is not a firewall or authentication control.
- Keep `APP_DATA_DIR=/data`. Preserve the same host-to-`/data` mapping through every update.
- Add `APP_ALLOW_OOKLA_BINARY_UPLOAD=true` only if the managed upload described below is required.
- Prefer recording the Ookla terms acknowledgement and CLI flag authorization in the UI. `ACCEPT_OOKLA_EULA=true` is a legacy-named headless deployment override and cannot be revoked effectively in the UI until the variable is cleared and the container is restarted.

Prepare a new direct-image data directory before the first start, replacing the example host path with the exact directory configured in Unraid:

```bash
mkdir -p /path/to/multispeed-data
chown -R 10001:10001 /path/to/multispeed-data
chmod -R u+rwX,go-rwx /path/to/multispeed-data
```

Do not use `chmod 777`. If an existing deployment overrides the container user, substitute that numeric UID/GID instead of `10001:10001`. After startup, **System Information** must show the host WAN interfaces; a container-private `eth0` indicates bridge networking and cannot provide MultiSpeed's intended multi-WAN binding semantics.

## Multi-WAN routing

Selecting an interface does not create a route. Linux must already have a valid source-specific policy for each WAN. MultiSpeed validates the configured address and route profile, checks the effective route, and discovers public IP over that exact path. A mismatch produces a failed or skipped result; it never falls back to the default WAN and never changes routes, rules, gateways, addresses, or firewall state.

Read [Linux networking and policy routing](docs/networking.md) before enabling tasks on hosts with multiple default gateways.

## Ookla setup

The published image does not contain, download, or redistribute Ookla Speedtest CLI. Ookla's CLI terms are restrictive, including limitations relevant to server/container and commercial use. Before using it:

1. Review Ookla's current [EULA](https://www.speedtest.net/about/eula), [Terms of Use](https://www.speedtest.net/about/terms), and [Privacy Policy](https://www.speedtest.net/about/privacy). The public EULA describes personal, non-commercial CLI use on one personal computer, excludes routers, modems, and other non-PC devices, and restricts making the CLI available on a network where more than one device can access it. Use only when the deployment fits the binding current documents or you have separate written Ookla authorization for the device, server, container, network access, automated, or commercial scenario.
2. Obtain the official Linux amd64 executable separately from Ookla.
3. Choose exactly one installation mode:
   - **Managed upload:** keep the default `OOKLA_BINARY=/data/providers/ookla/speedtest`, mount `/data` read/write, set `APP_ALLOW_OOKLA_BINARY_UPLOAD=true`, and do not create a separate volume or directory at the `speedtest` file path.
   - **External read-only file:** leave managed upload disabled, bind-mount the executable file read-only at a path outside `/data`, and set `OOKLA_BINARY` to that file.
4. In **Settings → Ookla provider terms & authorization**, agree to the current EULA and Terms, acknowledge reviewing the Privacy Policy, authorize MultiSpeed to pass `--accept-license` and `--accept-gdpr` non-interactively, confirm the deployment—including any network access from multiple devices—fits the express grant or has separate written authorization, and record the acknowledgement. Headless deployments may instead set the legacy-named `ACCEPT_OOKLA_EULA=true` process override with the same meaning.

The persisted acknowledgement and `ACCEPT_OOKLA_EULA=true` authorize MultiSpeed to pass the two acceptance flags and open only its technical gate; neither grants a license nor overrides Ookla's terms. The persisted record stores a MultiSpeed internal review marker—not an official Ookla document version—and requires renewed confirmation when that marker changes. The UI identifies whether the effective gate comes from the database or the environment; an environment override must be cleared and MultiSpeed restarted before UI revocation can block Ookla. With neither form, the adapter reports unavailable and other providers continue to work. A concrete bind-mount example is in [Provider behavior](docs/providers.md).

Ookla CLI `1.2.0.84` is the tested reference version. Managed upload accepts a separately obtained Linux amd64 executable only after its architecture and recognizable Speedtest by Ookla version output pass validation; that does not guarantee that a newer vendor release retains a compatible JSON result format. MultiSpeed gives the CLI writable, path-isolated runtime homes under `/data/providers/ookla/runtime`; these hold CLI-created state and avoid writes to the image user's nonexistent system home.

## Persistence, backup, and recovery

All persistent application state lives under `/data`; process policy remains in deployment environment variables. The default database is `/data/multispeed.db`, a managed Ookla executable is stored separately at `/data/providers/ookla/speedtest`, and Ookla CLI runtime homes are stored beneath `/data/providers/ookla/runtime`. SQLite uses WAL, foreign keys, a busy timeout, migrations, periodic checkpoints, and graceful close. Do not copy only `multispeed.db` while the application is running: committed data may still be in the WAL.

Use `POST /api/v1/backup` for a consistent online backup. For filesystem-level backup, stop the container first and preserve the database together with any `-wal` and `-shm` sidecars. See [Backup and recovery](docs/backup-recovery.md).

For configuration-only transfer, use **Settings → Configuration import & export** or `GET /api/v1/config/export` and `POST /api/v1/config/import`. The versioned JSON contains operational settings, active tasks, and active route profiles. Import validates the whole document before an atomic replacement, preserves task/profile IDs and historical results, and never imports or changes the Ookla terms acknowledgement. Deployment environment policy is not exported: custom LibreSpeed task URLs must already be authorized through `APP_ALLOWED_CUSTOM_SERVER_URLS` on the destination. Configuration transfer is intentionally not a substitute for a full database backup.

An online SQLite backup includes persisted settings, tasks, results, and the legacy-named Ookla terms acknowledgement, but not the separately stored Ookla executable, CLI runtime homes, or deployment environment variables. A configuration export includes only its documented portable subset. Only a stopped, complete copy of `/data` preserves the database and all managed provider files; deployment variables must always be backed up separately.

Deleting a task removes it from scheduling but preserves its historical results. Route profiles referenced by live tasks cannot be deleted; historical route snapshots remain attached to results.

## Configuration

Process configuration is intentionally small; persisted runtime settings are managed through the UI/API.

| Variable | Safe process default | Compose default | Purpose |
| --- | --- | --- | --- |
| `APP_LISTEN_ADDR` | `127.0.0.1:8787` | `0.0.0.0:8787` | HTTP listen address |
| `APP_TRUSTED_HOSTS` | empty | empty | Comma-separated exact proxy/LAN DNS names or IPs; wildcard listeners already accept unicast IP literals but require explicit DNS names to prevent DNS rebinding |
| `APP_ALLOWED_CUSTOM_SERVER_URLS` | empty | empty | Comma-separated exact LibreSpeed custom backend base URLs; empty disables custom URLs, and plain HTTP additionally requires the task's explicit insecure opt-in |
| `APP_ALLOW_OOKLA_BINARY_UPLOAD` | `false` | `false` | Enables bounded upload and execution validation of one operator-supplied Linux amd64 Ookla executable; no authentication, trusted private networks only |
| `APP_METRICS_ENABLED` | `false` | `false` | Enables the Prometheus/OpenMetrics endpoint at `/metrics`; protected by the listener's trusted-host policy but not by separate authentication |
| `APP_DATA_DIR` | `/data` | `/data` (fixed by supplied Compose) | Database and managed-provider directory; keep the `/data` mount writable |
| `APP_LOG_LEVEL` | `INFO` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR` |
| `APP_SHUTDOWN_TIMEOUT` | `20s` | `20s` | Graceful shutdown deadline |
| `TZ` | Image default | `Europe/Berlin` | Container timezone data; tasks retain their own IANA timezone |
| `LIBRESPEED_BINARY` | `librespeed-cli` | `/usr/local/bin/librespeed-cli` | LibreSpeed executable path |
| `OOKLA_BINARY` | `/data/providers/ookla/speedtest` | `/data/providers/ookla/speedtest` | Managed or externally supplied Ookla executable path |
| `ACCEPT_OOKLA_EULA` | `false` | `false` | Legacy-named headless override authorizing non-interactive `--accept-license`/`--accept-gdpr`; technical gate only, never a license grant |

Boolean variables reject malformed values instead of silently falling back. No environment-variable values are exposed by the system API.

## Development

Prerequisites are Go 1.26.6, Node.js 24, npm 11, and Linux for networking/integration behavior.

```bash
go mod download
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/multispeed

cd web
npm ci
npm run lint
npm run typecheck
npm run test
npm run build
npm run test:e2e
```

Real public speed tests must not run in automated tests. Provider tests use deterministic fake executables and local HTTP servers. See [Local development](docs/development.md) and [Contributing](CONTRIBUTING.md).

## API and operations

- OpenAPI 3.1 contract: [openapi.yaml](openapi.yaml)
- Human-readable API notes: [docs/api.md](docs/api.md)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Docker deployment: [docs/deployment.md](docs/deployment.md)
- Privacy and data flow: [docs/privacy.md](docs/privacy.md)
- Security policy: [SECURITY.md](SECURITY.md)
- Third-party licensing: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)

## Operational limitations

- MultiSpeed is Linux-only and requires host networking for its intended interface view.
- There is deliberately no authentication or multi-user isolation.
- The initial image is `linux/amd64` only.
- Ookla is unavailable until a compatible external executable is supplied and the terms acknowledgement/CLI flag authorization is explicit; separate deployment permission remains the operator's responsibility.
- Provider numbers are not interchangeable: endpoints, load patterns, sample selection, and protocols differ.
- Speed tests consume significant bandwidth and may affect production traffic; stagger schedules and start with global concurrency one.
- MultiSpeed observes and validates routing but never configures it.

## License

MultiSpeed is licensed under the [MIT License](LICENSE). Bundled and build-time dependencies retain their own terms; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). LibreSpeed CLI is a separate replaceable LGPL-3.0 executable built from v1.0.13 plus the shipped source-bound DNS overlay, patched `golang.org/x/net` dependency, and integration test.
