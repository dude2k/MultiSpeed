# Providers

All provider adapters expose explicit capabilities and return the same internal units: throughput in integer bits per second, latency/jitter in milliseconds, packet loss as a percentage, bytes as integers, and timestamps in UTC. Raw output and stderr are sanitized and bounded before storage.

Server discovery and validation use the same configured network path as the final measurement. A binding failure is terminal; adapters do not retry through the default route.

## Cloudflare® edge

Cloudflare is implemented natively in Go with a source-bound `net.Dialer`. It uses bounded request sizes, sample counts, concurrency, payload totals, and hard deadlines to measure unloaded latency, jitter, download, and upload against the automatically selected Cloudflare edge. The detected colo is stored when the response provides it.

Target selection is always **Automatic edge selection**. There is no server-ID field. Its methodology differs from Ookla and LibreSpeed, so compare trends within a provider before treating cross-provider values as equivalent.

IPv4 and IPv6 are supported when the selected source address and host route policy support that family.

Cloudflare is a trademark and/or registered trademark of Cloudflare, Inc. MultiSpeed is not affiliated with, endorsed by, or sponsored by Cloudflare, Inc.

## LibreSpeed CLI

The production image builds official `librespeed/speedtest-cli` v1.0.13 from the immutable source tag as a separate, replaceable executable, then applies MultiSpeed's small LGPL source-bound DNS and destination-pinning overlay. Runtime notices, the complete overlay, license texts, and dependency notices are shipped under `/usr/share/doc/librespeed-cli`; the deterministic complete corresponding-source archive and checksum are under `/opt/multispeed/release-artifacts` and attached to each GitHub release.

MultiSpeed uses JSON output, `--source <address>`, `--no-icmp`, a bounded timeout, and `--telemetry-level disabled`. Upstream v1.0.13 binds HTTP sockets for `--source` but otherwise leaves DNS on the default resolver; the overlay installs a pure-Go resolver whose UDP and TCP connections bind the same source address, pins authorized custom runs to the pre-resolved IP addresses and canonical port, and rejects redirects before a follow-up request. The build replaces upstream's vulnerable `golang.org/x/net` v0.49.0 with pinned v0.55.0. The adapter refuses a replacement CLI unless its version carries the `+multispeed.dns2.xnet055` compatibility marker, preventing an unpatched or dependency-vulnerable executable from silently weakening the release baseline. It supports automatic selection, a public server ID, and deployment-authorized custom server definitions. Certificate verification remains enabled by default; any per-server bypass must be explicit and is recorded in result metadata.

Custom backend URLs are fail-closed. The deployment must list each complete base URL in the comma-separated `APP_ALLOWED_CUSTOM_SERVER_URLS` environment variable; an empty value authorizes none. Entries are canonicalized once, and a task URL is accepted only when its canonical form equals an allowlist entry. Credentials, queries, fragments, IPv6 zones, ambiguous hosts, encoded or traversal-like paths, and unsafe path characters are rejected. HTTPS is the default. Listing an `http://` URL authorizes the destination but does not bypass transport policy: the individual task must also enable its existing `allowInsecure` option. Keep this list limited to LibreSpeed servers operated or explicitly trusted by the deployment owner.

The upstream CLI is LGPL-3.0-only and the MultiSpeed overlay is LGPL-3.0-or-later. See [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md) and the source link there. The subprocess boundary keeps it independently replaceable, provided a replacement preserves the required bound-DNS behavior and marker.

## Speedtest CLI by Ookla

Ookla Speedtest CLI is proprietary. MultiSpeed images do not include it, download it, install its repository, or redistribute it. Official CLI `1.2.0.84` is the tested reference version. The adapter expects its compatible JSON format and supports automatic selection, server IDs, server discovery, source binding, deadlines, cancellation, and bounded diagnostics when an external executable is available. Managed installation checks that a file is Linux amd64 and that `--version` recognizably identifies Speedtest by Ookla, but that alone cannot guarantee compatibility with the JSON format of a newer vendor release. Ookla 1.2.0.84 treats `--interface` and `--ip` as mutually exclusive, so MultiSpeed passes the validated concrete source with `--ip`; it uses `--interface` only for internal callers that omit a source address.

Review Ookla's current [EULA](https://www.speedtest.net/about/eula), [Terms of Use](https://www.speedtest.net/about/terms), and [Privacy Policy](https://www.speedtest.net/about/privacy). The public EULA describes personal, non-commercial CLI use on one personal computer, excludes routers, modems, and other non-PC devices, and restricts making the CLI available on a network where more than one device can access it. Use only when the deployment fits the binding current documents or you have separate written Ookla authorization for its device, server, container, network access, automated, commercial, or redistribution scenario. The persisted **Settings → Ookla provider terms & authorization** acknowledgement and the legacy-named headless `ACCEPT_OOKLA_EULA` override authorize MultiSpeed to pass `--accept-license` and `--accept-gdpr` non-interactively and open only its technical gate; neither is legal advice nor a license grant.

Ookla® and Speedtest® are registered trademarks of Ookla, LLC. MultiSpeed is independent and is not affiliated with, endorsed by, or sponsored by Ookla.

For a single-file installation, the task editor and Settings page can accept an operator-supplied Linux amd64 executable after the terms acknowledgement and flag authorization. This is disabled by default because the endpoint necessarily validates the uploaded file by executing it as the non-root container user. Set `APP_ALLOW_OOKLA_BINARY_UPLOAD=true`, mount `/data` read/write, and keep `OOKLA_BINARY=/data/providers/ookla/speedtest` to opt in. Do not mount or create `speedtest` as a directory: it is the final regular-file path. The endpoint accepts only `application/octet-stream`, is size-limited to 64 MiB and rate-limited to two attempts per client address per hour, rejects non-Linux-amd64 ELF files, requires recognizable Speedtest by Ookla version output, and atomically preserves a previous regular executable when validation fails. Enable it only on a trusted private network or behind an authenticating reverse proxy.

When permitted, an operator-managed executable can be exposed read-only without changing the shipped service. For example, create a Compose override that still has one service:

```yaml
services:
  multispeed:
    volumes:
      - ./data:/data
      - /absolute/operator/path/speedtest:/opt/multispeed/providers/speedtest:ro
    environment:
      OOKLA_BINARY: /opt/multispeed/providers/speedtest
      ACCEPT_OOKLA_EULA: "true"
```

The executable must be a Linux amd64 binary compatible with the Debian runtime and readable/executable by the configured non-root UID. A single-file bind mount does not add package libraries; supply them separately only if Ookla's installation and terms require them. Do not copy the binary into this repository or a published derivative image.

MultiSpeed assigns Ookla CLI provider-availability checks, discovery, and tests writable runtime homes beneath `/data/providers/ookla/runtime`. The provider-availability version probe uses a default home, while discovery and tests use deterministic, isolated subdirectories derived from the selected interface/source path. This prevents the non-root CLI from trying to create configuration under the image user's nonexistent home and avoids sharing mutable CLI state between WAN paths. Treat this runtime subtree as local provider state; it is included only in a complete `/data` filesystem backup, not in the online SQLite backup or configuration export.

### Managed upload troubleshooting

Select the extracted `speedtest` executable from Ookla's Linux x86_64 archive, not the archive itself. Before retrying a failed upload, inspect the container log and the host-side directory rather than submitting the file repeatedly: every request that reaches the upload handler consumes one of the two hourly attempts, including requests that later fail validation or installation. The limiter is in process memory; normally wait for the one-hour window, or restart the container once after correcting a deployment problem during installation testing.

For the published image's default user, the persistent directory must be writable by numeric UID/GID `10001:10001`. The supplied Compose file overrides the user, so substitute its configured `MULTISPEED_UID:MULTISPEED_GID` there. On the host, replace the example path with the exact directory mounted at `/data`:

```bash
mkdir -p /path/to/multispeed-data/providers/ookla
chown -R 10001:10001 /path/to/multispeed-data/providers
chmod -R u+rwX,go-rwx /path/to/multispeed-data/providers
ls -ldn /path/to/multispeed-data/providers /path/to/multispeed-data/providers/ookla
```

Do not use `chmod 777`. If the log reports `permission denied`, correct the owner of the bind source for the actual container user. If activation reports `file exists`, inspect `/path/to/multispeed-data/providers/ookla/speedtest`: it must be absent or a regular file, never a directory or separate volume target. Stop the container and move an accidental directory aside before retrying; do not delete unknown contents without reviewing them. A generic browser message such as `Failed to fetch` does not identify the server-side cause, so preserve the corresponding structured log line and request ID.

If the CLI starts but reports a connection timeout, the installation and runtime-home checks have already succeeded. The remaining failure concerns reachability from the exact selected source path to the discovered or fixed Ookla server. Verify the host's source-specific route, DNS, firewall, and server availability; try current discovery or automatic selection when a fixed server is stale. MultiSpeed never retries such a failure through another source address or the default WAN.

With the technical acknowledgement false, a missing executable, or an incompatible version, Ookla is marked unavailable and its tasks do not execute. LibreSpeed and Cloudflare remain usable.

Interactive operators should leave `ACCEPT_OOKLA_EULA=false`, open MultiSpeed Settings, follow all three official links, agree to the current EULA and Terms, acknowledge reviewing the Privacy Policy, authorize both non-interactive CLI acceptance flags, confirm the deployment—including any network access from multiple devices—fits the express scope or has separate written authorization, and record the acknowledgement. The decision, UTC timestamp, and MultiSpeed internal review marker persist in SQLite and can be revoked without restarting MultiSpeed. A changed marker invalidates the old technical acknowledgement without deleting its audit metadata. The marker is not an official document version and MultiSpeed cannot automatically detect vendor-document changes; operators must review the live links. The legacy-named environment variable remains a backward-compatible effective override for headless deployments with the same meaning; the API and UI identify that source, and it must be cleared followed by a restart before Ookla is fully disabled.

## Capability differences

| Capability | Ookla | LibreSpeed | Cloudflare |
| --- | ---: | ---: | ---: |
| Server discovery | Yes | Yes | No |
| Fixed server ID | Yes | Yes | No |
| Custom backend URL/definition | No | Yes, when deployment-allowlisted | No |
| Interface/source binding | Exact validated source (`--ip`) | Exact source (`--source`) | Exact source (native socket binding) |
| Source-address binding | Yes, validated source via `--ip` | Yes, via `--source` | Yes, native socket binding |
| IPv4 / IPv6 | Yes / Yes, subject to CLI/network | Yes / Yes | Yes / Yes |
| Jitter | Yes | When returned | Yes |
| Packet loss | When returned | Not consistently available | Not reported |
| Result/share URL | When returned | Only with explicit telemetry/share behavior | No |

Unavailable metrics are stored as null, never as zero-valued sentinels.

## Responsible scheduling

Public tests consume substantial traffic and server resources. Use reasonable intervals, random start jitter, and timeouts. Respect provider acceptable-use policies and custom-server ownership. CI and repository tests use fake providers and local deterministic endpoints; they must not invoke public speed tests.
