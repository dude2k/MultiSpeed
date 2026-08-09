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
- Ookla, LibreSpeed, and native Cloudflare edge adapters behind one normalized provider contract
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
| Ookla Speedtest CLI 1.2.0.84 | **Not included or downloaded by MultiSpeed images** | Automatic or server ID | Concrete source IP (validated against the selected interface) | Requires an operator-supplied executable, explicit EULA acceptance, and any permission required by Ookla's terms. |

See [Provider behavior](docs/providers.md) for capabilities, flags, licensing, and test-method differences.

## Requirements

- A Linux Docker host with Docker Engine and Compose v2
- `linux/amd64`; no other image architecture is published until every provider path is tested there
- One or more non-loopback interfaces with a concrete source address
- Source-based policy routing configured on the **host** when different WANs use different gateways
- A writable persistent directory owned by the non-root container UID/GID

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

Compose intentionally sets `APP_LISTEN_ADDR=0.0.0.0:8787` for trusted-LAN access. A wildcard bind accepts any syntactically valid HTTP host on port 8787, so no per-host allowlist is required. Restrict traffic with host firewall rules or place an authenticating, TLS-terminating reverse proxy in front. Never expose port 8787 directly to the public internet.

To use a published image explicitly:

```bash
docker pull ghcr.io/dude2k/multispeed:latest
docker compose up -d --no-build
```

Tagged releases publish `latest`, major, major.minor, and full semantic-version tags.

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

## Multi-WAN routing

Selecting an interface does not create a route. Linux must already have a valid source-specific policy for each WAN. MultiSpeed validates the configured address and route profile, checks the effective route, and discovers public IP over that exact path. A mismatch produces a failed or skipped result; it never falls back to the default WAN and never changes routes, rules, gateways, addresses, or firewall state.

Read [Linux networking and policy routing](docs/networking.md) before enabling tasks on hosts with multiple default gateways.

## Ookla setup

The published image does not contain, download, or redistribute Ookla Speedtest CLI. Ookla's CLI terms are restrictive, including limitations relevant to server/container and commercial use. Before using it:

1. Review the current Ookla EULA and obtain any necessary permission directly from Ookla.
2. Install the official CLI outside MultiSpeed.
3. Make the executable available to the container through an explicit read-only bind mount or build a private, non-redistributed image under terms that permit your use.
4. Set `OOKLA_BINARY` to that in-container path.
5. In **Settings → Ookla provider licensing**, review the current terms, explicitly confirm acceptance, and save it. Headless deployments may instead set `ACCEPT_OOKLA_EULA=true` as a process-level override.

Persisted UI acceptance and `ACCEPT_OOKLA_EULA=true` only open MultiSpeed's technical gate; neither grants a license nor overrides Ookla's terms. Persisted acceptance records the reviewed EULA revision and requires renewed confirmation when MultiSpeed changes that revision. The UI identifies whether the effective gate comes from the database or the environment; an environment override must be cleared and MultiSpeed restarted before UI revocation can block Ookla. With neither form of acceptance, the adapter reports unavailable and other providers continue to work. A concrete bind-mount example is in [Provider behavior](docs/providers.md).

## Persistence, backup, and recovery

All state lives under `/data`; the default database is `/data/multispeed.db`. SQLite uses WAL, foreign keys, a busy timeout, migrations, periodic checkpoints, and graceful close. Do not copy only `multispeed.db` while the application is running: committed data may still be in the WAL.

Use `POST /api/v1/backup` for a consistent online backup. For filesystem-level backup, stop the container first and preserve the database together with any `-wal` and `-shm` sidecars. See [Backup and recovery](docs/backup-recovery.md).

For configuration-only transfer, use **Settings → Configuration import & export** or `GET /api/v1/config/export` and `POST /api/v1/config/import`. The versioned JSON contains operational settings, active tasks, and active route profiles. Import validates the whole document before an atomic replacement, preserves task/profile IDs and historical results, and never imports or changes Ookla EULA acceptance. Deployment environment policy is not exported: custom LibreSpeed task URLs must already be authorized through `APP_ALLOWED_CUSTOM_SERVER_URLS` on the destination. Configuration transfer is intentionally not a substitute for a full database backup.

Deleting a task removes it from scheduling but preserves its historical results. Route profiles referenced by live tasks cannot be deleted; historical route snapshots remain attached to results.

## Configuration

Process configuration is intentionally small; persisted runtime settings are managed through the UI/API.

| Variable | Safe process default | Compose default | Purpose |
| --- | --- | --- | --- |
| `APP_LISTEN_ADDR` | `127.0.0.1:8787` | `0.0.0.0:8787` | HTTP listen address |
| `APP_TRUSTED_HOSTS` | empty | empty | Comma-separated exact proxy/LAN DNS names or IPs accepted when binding to a specific host; unnecessary with `0.0.0.0` or `[::]` |
| `APP_ALLOWED_CUSTOM_SERVER_URLS` | empty | empty | Comma-separated exact LibreSpeed custom backend base URLs; empty disables custom URLs, and plain HTTP additionally requires the task's explicit insecure opt-in |
| `APP_DATA_DIR` | `/data` | `/data` | Database and backup directory |
| `APP_LOG_LEVEL` | `INFO` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR` |
| `APP_SHUTDOWN_TIMEOUT` | `20s` | `20s` | Graceful shutdown deadline |
| `TZ` | Image default | `Europe/Berlin` | Container timezone data; tasks retain their own IANA timezone |
| `LIBRESPEED_BINARY` | `librespeed-cli` | `/usr/local/bin/librespeed-cli` | LibreSpeed executable path |
| `OOKLA_BINARY` | `speedtest` | `/opt/multispeed/providers/speedtest` | Optional external Ookla executable path |
| `ACCEPT_OOKLA_EULA` | `false` | `false` | Optional headless override for the persisted in-app acceptance gate |

No environment-variable values are exposed by the system API.

## Development

Prerequisites are Go 1.26.5, Node.js 24, npm 11, and Linux for networking/integration behavior.

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
- Security policy: [SECURITY.md](SECURITY.md)
- Third-party licensing: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)

## Operational limitations

- MultiSpeed is Linux-only and requires host networking for its intended interface view.
- There is deliberately no authentication or multi-user isolation.
- The initial image is `linux/amd64` only.
- Ookla is unavailable until a compliant external executable is supplied and acceptance is explicit.
- Provider numbers are not interchangeable: endpoints, load patterns, sample selection, and protocols differ.
- Speed tests consume significant bandwidth and may affect production traffic; stagger schedules and start with global concurrency one.
- MultiSpeed observes and validates routing but never configures it.

## License

MultiSpeed is licensed under the [MIT License](LICENSE). Bundled and build-time dependencies retain their own terms; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). LibreSpeed CLI is a separate replaceable LGPL-3.0 executable built from v1.0.13 plus the shipped source-bound DNS overlay, patched `golang.org/x/net` dependency, and integration test.
