# Security policy

## Supported versions

Security fixes are provided for the latest tagged release. The `main` branch is development code and may change before a release.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Older releases | No |

## Deployment boundary

MultiSpeed does not include authentication and must only be exposed to trusted networks unless protected by an authenticating reverse proxy.

The application defaults to `127.0.0.1:8787`. The Compose example listens on the trusted LAN through host networking and therefore requires host firewall policy. A wildcard bind accepts any syntactically valid hostname or unicast IP on the listen port; a specific bind can authorize additional proxy/LAN names through `APP_TRUSTED_HOSTS`. Do not expose it directly to the internet. Backups, exports, results, task definitions, source addresses, route snapshots, public IPs, and provider diagnostics can be sensitive.

The supported container drops every Linux capability, uses `no-new-privileges`, runs non-root, has a read-only root filesystem, and receives no Docker socket or host-root mount. A report that requires `privileged`, `CAP_NET_ADMIN`, arbitrary command hooks, wildcard CORS, or hidden authentication is not an acceptable fix.

## Report a vulnerability

Use GitHub's private vulnerability reporting or open a private draft security advisory in `dude2k/MultiSpeed`. Do not open a public issue containing exploit details, private addresses, databases, backups, raw provider output, or credentials from surrounding infrastructure.

Include:

- affected version/image digest and architecture;
- impact and prerequisites;
- minimal reproduction with sensitive values removed;
- whether the default hardened Compose deployment is affected;
- relevant request IDs and sanitized logs; and
- a suggested mitigation if known.

Maintainers should acknowledge a complete report within seven days and coordinate disclosure after a fix is available. Please allow a reasonable remediation period before public disclosure.

## Scope priorities

High-priority issues include same-origin bypasses, arbitrary command or argument injection, arbitrary file access, unsafe backup behavior, stored provider-output injection, subprocess escape, container privilege escalation, denial of service that defeats bounds/rate limits, and unintended fallback to another WAN path.

The deliberate absence of built-in authentication is documented behavior, not itself a vulnerability. Unexpected exposure despite loopback configuration, origin validation bypass, or disclosure beyond the documented no-auth boundary is in scope.

## Provider disclosure

Ookla Speedtest CLI is not distributed by MultiSpeed. Reports about Ookla's proprietary executable should also follow Ookla's process. LibreSpeed CLI and upstream dependencies retain their own security channels; report MultiSpeed integration flaws here and coordinate upstream issues responsibly.
