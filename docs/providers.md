# Providers

All provider adapters expose explicit capabilities and return the same internal units: throughput in integer bits per second, latency/jitter in milliseconds, packet loss as a percentage, bytes as integers, and timestamps in UTC. Raw output and stderr are sanitized and bounded before storage.

Server discovery and validation use the same configured network path as the final measurement. A binding failure is terminal; adapters do not retry through the default route.

## Cloudflare edge

Cloudflare is implemented natively in Go with a source-bound `net.Dialer`. It uses bounded request sizes, sample counts, concurrency, payload totals, and hard deadlines to measure unloaded latency, jitter, download, and upload against the automatically selected Cloudflare edge. The detected colo is stored when the response provides it.

Target selection is always **Automatic edge selection**. There is no server-ID field. Its methodology differs from Ookla and LibreSpeed, so compare trends within a provider before treating cross-provider values as equivalent.

IPv4 and IPv6 are supported when the selected source address and host route policy support that family.

## LibreSpeed CLI

The production image builds official `librespeed/speedtest-cli` v1.0.13 from the immutable source tag as a separate, replaceable executable, then applies MultiSpeed's small LGPL source-bound DNS and destination-pinning overlay. The upstream source archive, complete overlay, build-time UDP/TCP DNS integration test, license, and module metadata are shipped under `/usr/share/doc/librespeed-cli`.

MultiSpeed uses JSON output, `--source <address>`, `--no-icmp`, a bounded timeout, and `--telemetry-level disabled`. Upstream v1.0.13 binds HTTP sockets for `--source` but otherwise leaves DNS on the default resolver; the overlay installs a pure-Go resolver whose UDP and TCP connections bind the same source address, pins authorized custom runs to the pre-resolved IP addresses and canonical port, and rejects redirects before a follow-up request. The build replaces upstream's vulnerable `golang.org/x/net` v0.49.0 with pinned v0.55.0. The adapter refuses a replacement CLI unless its version carries the `+multispeed.dns2.xnet055` compatibility marker, preventing an unpatched or dependency-vulnerable executable from silently weakening the release baseline. It supports automatic selection, a public server ID, and deployment-authorized custom server definitions. Certificate verification remains enabled by default; any per-server bypass must be explicit and is recorded in result metadata.

Custom backend URLs are fail-closed. The deployment must list each complete base URL in the comma-separated `APP_ALLOWED_CUSTOM_SERVER_URLS` environment variable; an empty value authorizes none. Entries are canonicalized once, and a task URL is accepted only when its canonical form equals an allowlist entry. Credentials, queries, fragments, IPv6 zones, ambiguous hosts, encoded or traversal-like paths, and unsafe path characters are rejected. HTTPS is the default. Listing an `http://` URL authorizes the destination but does not bypass transport policy: the individual task must also enable its existing `allowInsecure` option. Keep this list limited to LibreSpeed servers operated or explicitly trusted by the deployment owner.

The CLI and overlay are LGPL-3.0-or-later. See [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md) and the source link there. The subprocess boundary keeps it independently replaceable, provided a replacement preserves the required bound-DNS behavior and marker.

## Ookla Speedtest CLI

Ookla Speedtest CLI is proprietary. MultiSpeed images do not include it, download it, install its repository, or redistribute it. The adapter expects compatible JSON from official CLI v1.2.0.84 and supports automatic selection, server IDs, server discovery, source binding, deadlines, cancellation, and bounded diagnostics when an external executable is available. Ookla 1.2.0.84 treats `--interface` and `--ip` as mutually exclusive, so MultiSpeed passes the validated concrete source with `--ip`; it uses `--interface` only for internal callers that omit a source address.

Ookla's published terms include restrictions relevant to personal/non-commercial, server, container, and redistribution scenarios. Confirm your intended deployment directly against Ookla's current terms and obtain permission where necessary. The persisted **Settings → Ookla provider licensing** acknowledgement and the headless `ACCEPT_OOKLA_EULA` override are only technical gates; neither is legal advice nor a license grant.

For a single-file installation, the task editor and Settings page can accept an operator-supplied Linux amd64 executable after EULA acceptance. This is disabled by default because the endpoint necessarily validates the uploaded file by executing it as the non-root container user. Set `APP_ALLOW_OOKLA_BINARY_UPLOAD=true` and keep `OOKLA_BINARY=/data/providers/ookla/speedtest` to opt in. The endpoint accepts only `application/octet-stream`, is rate- and size-limited to 64 MiB, rejects non-Linux-amd64 ELF files, requires recognizable Speedtest by Ookla version output, and atomically preserves the previous executable when validation fails. Enable it only on a trusted private network or behind an authenticating reverse proxy.

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

With acceptance false, a missing executable, or an incompatible version, Ookla is marked unavailable and its tasks do not execute. LibreSpeed and Cloudflare remain usable.

Interactive operators should leave `ACCEPT_OOKLA_EULA=false`, open MultiSpeed Settings, follow the hard-coded official EULA link, explicitly confirm acceptance, and record it. The decision, UTC timestamp, and reviewed revision persist in SQLite and can be revoked without restarting MultiSpeed. A changed required revision invalidates the old technical acknowledgement without deleting its audit metadata. The environment variable remains a backward-compatible effective override for headless deployments; the API and UI identify that source, and it must be cleared followed by a restart before Ookla is fully disabled.

## Capability differences

| Capability | Ookla | LibreSpeed | Cloudflare |
| --- | ---: | ---: | ---: |
| Server discovery | Yes | Yes | No |
| Fixed server ID | Yes | Yes | No |
| Custom backend URL/definition | No | Yes, when deployment-allowlisted | No |
| Interface/source binding | Exact validated source (`--ip`) | Exact source (`--source`) | Exact source (native socket binding) |
| Source-address binding | CLI interface path | Yes | Yes |
| IPv4 / IPv6 | Yes / Yes, subject to CLI/network | Yes / Yes | Yes / Yes |
| Jitter | Yes | When returned | Yes |
| Packet loss | When returned | Not consistently available | Not reported |
| Result/share URL | When returned | Only with explicit telemetry/share behavior | No |

Unavailable metrics are stored as null, never as zero-valued sentinels.

## Responsible scheduling

Public tests consume substantial traffic and server resources. Use reasonable intervals, random start jitter, and timeouts. Respect provider acceptable-use policies and custom-server ownership. CI and repository tests use fake providers and local deterministic endpoints; they must not invoke public speed tests.
