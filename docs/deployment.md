# Docker deployment

The supported deployment is one `linux/amd64` container on a Linux host. The supplied [compose.yaml](../compose.yaml) contains exactly one service and no external database, worker, proxy, scheduler, Redis, or frontend container.

## Security boundary

Compose applies:

- `network_mode: host` for the host interface namespace;
- a non-root UID/GID;
- `cap_drop: [ALL]`;
- `no-new-privileges:true`;
- a read-only root filesystem;
- a `noexec,nosuid,nodev` `/tmp` tmpfs;
- one writable `./data:/data` bind mount;
- no privileged mode, Docker socket, host-root mount, or added capabilities.

Host networking means Docker port publishing is neither required nor used. Protect TCP 8787 with host firewall rules. MultiSpeed has no authentication.

The published image declares runtime UID/GID `10001:10001`. The supplied Compose file overrides that identity with `MULTISPEED_UID:MULTISPEED_GID` so a normal host user can own `./data`. Direct image deployments that do not override `--user`, including a default Unraid container, must make the `/data` bind source writable by `10001:10001`. MultiSpeed does not interpret `PUID` or `PGID` environment variables.

## Prepare the host

```bash
git clone https://github.com/dude2k/MultiSpeed.git
cd MultiSpeed
cp .env.example .env
```

Record the host identity that should own data:

```bash
id -u
id -g
```

Set those values as `MULTISPEED_UID` and `MULTISPEED_GID` in `.env`. Ensure `./data` is owned and writable by that identity. Do not use UID 0 or make the directory world-writable. The supplied Compose deployment deliberately fixes `APP_DATA_DIR=/data`; changing an `APP_DATA_DIR` value in `.env` would not relocate the mount.

```bash
mkdir -p data
chown -R "$(id -u):$(id -g)" data
chmod -R u+rwX,go-rwx data
```

Review `APP_LISTEN_ADDR`. Compose defaults to `0.0.0.0:8787` for a trusted LAN and accepts concrete unicast IP literals on port 8787 without an allowlist; use `127.0.0.1:8787` when only a local reverse proxy should connect. Wildcard listeners reject arbitrary DNS names against DNS rebinding, so add each intentionally used LAN or reverse-proxy DNS name through `APP_TRUSTED_HOSTS`. Assigned IPs and loopback are accepted automatically.

Custom LibreSpeed backends require a separate deployment-owned allowlist. Put their exact base URLs in comma-separated `APP_ALLOWED_CUSTOM_SERVER_URLS`; leave it empty to disable custom URLs. Entries may use HTTPS or HTTP and may include a safe base path and explicit port, but not credentials, queries, fragments, IPv6 zones, encoded/traversal-like paths, or ambiguous host syntax. Plain HTTP still requires the task's explicit `allowInsecure` setting. Changing this environment variable requires recreating the service.

Managed Ookla executable upload is separately fail-closed. Leave `APP_ALLOW_OOKLA_BINARY_UPLOAD=false` unless operators must install a separately obtained single-file Linux amd64 CLI through the UI. Enabling it permits a client that can reach the listener to supply code that MultiSpeed executes for version validation and later speed tests; use it only on a private trusted network or behind an authenticating reverse proxy. Upload is enabled only when `OOKLA_BINARY` is exactly `APP_DATA_DIR/providers/ookla/speedtest` (the default is `/data/providers/ookla/speedtest`), and neither the data directory nor its provider subdirectories may be symbolic links.

Choose one Ookla installation model. For managed upload, mount `/data` read/write, keep `OOKLA_BINARY=/data/providers/ookla/speedtest`, and do not create a separate mount or directory at the final `speedtest` file path. For an operator-managed read-only executable, leave upload disabled, mount the file at a path outside `/data`, and point `OOKLA_BINARY` at that file. Do not combine the two models. In either mode, the CLI receives writable, source-path-isolated runtime homes beneath `/data/providers/ookla/runtime`, so `/data/providers/ookla` must remain writable by the runtime user.

## Unraid and other direct-image deployments

Create the container from a pinned full semantic-version image such as `ghcr.io/dude2k/multispeed:<version>`, then configure:

1. **Network Type: Host.** Do not publish a container port. Bridge mode exposes Docker's virtual interface and NAT route instead of the host WAN paths.
2. A persistent read/write path from the chosen app-data directory to container path `/data`.
3. `APP_LISTEN_ADDR=0.0.0.0:8787` for a trusted LAN, `APP_DATA_DIR=/data`, and the desired timezone/log level.
4. No `PUID` or `PGID` variables. Keep the image's `10001:10001` user unless an explicit Unraid extra parameter overrides the container user.
5. `APP_ALLOW_OOKLA_BINARY_UPLOAD=true` only when managed upload is required. Prefer recording the terms acknowledgement and CLI flag authorization in Settings instead of setting the legacy-named environment override.

Before the first start, prepare the exact host directory selected in the Unraid path mapping:

```bash
mkdir -p /path/to/multispeed-data
chown -R 10001:10001 /path/to/multispeed-data
chmod -R u+rwX,go-rwx /path/to/multispeed-data
```

Never substitute `chmod 777`. Start the container, open **System Information**, and verify that the expected host interfaces and source addresses are present. If only a Docker `eth0` with a container-private address appears, stop and change the network type to Host before creating or enabling tasks.

## Start or upgrade

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --follow multispeed
```

Pin `MULTISPEED_IMAGE` to a full semantic version in production instead of `latest`. Before upgrading, read [CHANGELOG.md](../CHANGELOG.md) and create the backups appropriate to the state being protected. An online backup contains SQLite state but not a managed Ookla executable or CLI runtime homes; a stopped copy of the complete `/data` directory contains them. Back up deployment environment variables separately. Pull the new image and recreate the single service with the same host-to-`/data` mapping and numeric user. Migrations run automatically before readiness.

Rollback can be unsafe after a schema migration. Preserve the pre-upgrade backup and consult release notes; restore only with the container stopped.

## First-start acknowledgement

Before enabling schedules:

1. Verify health and readiness.
2. Open **System Information** and confirm that the expected host interfaces and source addresses are visible; a Docker-private `eth0` means host networking is not active.
3. Review Settings and the trusted-network warning.
4. Create and validate route profiles for enforced gateway/table paths.
5. Confirm provider availability. For Ookla, record agreement to the current documents and authorization for both non-interactive acceptance flags, confirm any separate deployment permission, and install the separately obtained executable as distinct operations.
6. Create a task, run its preflight, perform one manual test, and verify the recorded source path/public IP before relying on the schedule.
7. Create a test backup and record the container image tag and numeric runtime identity.

## Health and readiness

The image healthcheck invokes the binary directly, avoiding an extra HTTP client package:

```bash
docker compose exec multispeed /usr/local/bin/multispeed healthcheck
curl --fail http://127.0.0.1:8787/api/v1/healthz
curl --fail http://127.0.0.1:8787/api/v1/readyz
```

`healthz` indicates that the process is alive. `readyz` additionally reflects startup dependencies such as migrations/database access. The Docker healthcheck targets the configured local listener.

## Reverse proxy

MultiSpeed does not implement authentication. For remote access, use a separate operator-managed reverse proxy with TLS and authentication, bind MultiSpeed to loopback, and preserve streaming for `/api/v1/events`. Add the public DNS name to `APP_TRUSTED_HOSTS` and forward that hostname consistently so same-origin mutation validation remains effective. The backend rejects an explicit Host port different from `APP_LISTEN_ADDR`; configure the proxy's upstream `Host` header without the external port (for example, an Nginx `$host`) or with the backend listen port. Do not add permissive CORS headers.

The reverse proxy is intentionally not included in Compose because the application deployment must remain one container and proxy/authentication choices are site-specific.

## Image contents

The multi-stage build:

1. installs locked frontend dependencies and builds Vite assets;
2. builds LibreSpeed CLI v1.0.13 from official tagged LGPL source, applies the shipped source-bound DNS overlay, and runs its UDP/TCP binding integration test;
3. embeds the frontend and compiles a stripped, reproducible Go binary with version, Git commit, and build timestamp;
4. copies only runtime artifacts into Debian slim.

The runtime has CA certificates, timezone data, read-only route-inspection support, the two binaries, and required notices. It has no compiler, Go/Node runtime, frontend source, package-manager cache, or Ookla executable.

## Logs

Application logs go to stdout/stderr in structured form and include request, task, result, provider, duration, version, and scheduler context where relevant.

```bash
docker compose logs --since 1h multispeed
```

Never publish debug logs without reviewing provider diagnostics, interface addresses, public IPs, target metadata, and route snapshots.

See [Privacy and data flow](privacy.md) for the local data inventory, outbound provider traffic, and disclosure considerations for logs, exports, and backups.

## Smoke test

The repository smoke script creates isolated temporary state, launches a hardened container with fake/local provider behavior, checks health/readiness/frontend, restarts it, verifies persistence, and cleans up:

```bash
docker build --platform linux/amd64 -t multispeed:local .
bash scripts/docker-smoke.sh multispeed:local
```

It does not run a public speed test.
