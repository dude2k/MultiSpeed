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

Set those values as `MULTISPEED_UID` and `MULTISPEED_GID` in `.env`. Ensure `./data` is owned and writable by that identity. Do not use UID 0.

Review `APP_LISTEN_ADDR`. Compose defaults to `0.0.0.0:8787` for a trusted LAN and accepts every syntactically valid host on port 8787 without an allowlist; use `127.0.0.1:8787` when only a local reverse proxy should connect. With a specific bind address, add additional DNS names through `APP_TRUSTED_HOSTS`; assigned host IPs and loopback are accepted automatically.

Custom LibreSpeed backends require a separate deployment-owned allowlist. Put their exact base URLs in comma-separated `APP_ALLOWED_CUSTOM_SERVER_URLS`; leave it empty to disable custom URLs. Entries may use HTTPS or HTTP and may include a safe base path and explicit port, but not credentials, queries, fragments, IPv6 zones, encoded/traversal-like paths, or ambiguous host syntax. Plain HTTP still requires the task's explicit `allowInsecure` setting. Changing this environment variable requires recreating the service.

## Start or upgrade

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --follow multispeed
```

Pin `MULTISPEED_IMAGE` to a full semantic version in production instead of `latest`. Before upgrading, download an online backup, read [CHANGELOG.md](../CHANGELOG.md), pull the new image, and recreate the single service. Migrations run automatically before readiness.

Rollback can be unsafe after a schema migration. Preserve the pre-upgrade backup and consult release notes; restore only with the container stopped.

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

## Smoke test

The repository smoke script creates isolated temporary state, launches a hardened container with fake/local provider behavior, checks health/readiness/frontend, restarts it, verifies persistence, and cleans up:

```bash
docker build --platform linux/amd64 -t multispeed:local .
bash scripts/docker-smoke.sh multispeed:local
```

It does not run a public speed test.
