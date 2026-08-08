# Third-party notices

MultiSpeed is MIT-licensed. Third-party components remain under their respective licenses. This file is informational and does not replace the license text shipped by each component.

## Runtime provider component

### LibreSpeed CLI

- Project: `librespeed/speedtest-cli`
- Version: v1.0.13 (tagged 2026-04-30)
- Source: <https://github.com/librespeed/speedtest-cli/tree/v1.0.13>
- License: GNU Lesser General Public License v3.0 (LGPL-3.0)
- Relationship: separately built and separately executed, replaceable subprocess; it is not linked into the MultiSpeed binary

The production image builds LibreSpeed CLI from the tagged source and applies the MultiSpeed `multispeed.dns2.xnet055` overlay under LGPL-3.0-or-later. The overlay makes UDP DNS and TCP fallback bind the same selected source address as the HTTP test sockets, pins authorized custom runs to their pre-resolved IP:port endpoints, blocks redirects before follow-up requests, and pins `golang.org/x/net` v0.55.0 in place of upstream v0.49.0. It includes the upstream license, exact upstream source archive, complete overlay source and integration test, patched module metadata, and build information under `/usr/share/doc/librespeed-cli`. Telemetry is disabled by default.

LibreSpeed CLI copyright remains with its upstream contributors. It is provided without warranty under the LGPL-3.0. You may replace `/usr/local/bin/librespeed-cli` in a private deployment with a compatible build, subject to the component's license. MultiSpeed fails closed unless that build advertises the `+multispeed.dns2.xnet055` marker and therefore attests to the required source-bound resolver, destination-pinning, and patched dependency baseline.

## Optional operator component not distributed by MultiSpeed

### Ookla Speedtest CLI

- Expected compatible version: 1.2.0.84
- Official product: <https://www.speedtest.net/apps/cli>
- License: proprietary Ookla Speedtest CLI EULA
- Relationship: optional external operator-supplied executable

MultiSpeed does not contain, download, install, or redistribute Ookla Speedtest CLI. Ookla's terms include restrictions relevant to personal/non-commercial, server/container, and redistribution use. `ACCEPT_OOKLA_EULA=true` is a technical availability gate, not permission or a license grant. Operators are solely responsible for reviewing current terms and obtaining any required authorization.

## Methodology reference

### Cloudflare speedtest

- Project: `cloudflare/speedtest`
- Source: <https://github.com/cloudflare/speedtest>
- License: MIT
- Relationship: methodology and endpoint reference; MultiSpeed uses an original native Go adapter and does not bundle the browser application

Cloudflare names and services remain the property of Cloudflare. Measurements against the automatically selected edge are labeled as a distinct methodology.

## Go dependencies

Exact versions and transitive checksums are recorded in `go.mod` and `go.sum`.

| Component | License |
| --- | --- |
| `github.com/go-chi/chi/v5` | MIT |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/robfig/cron/v3` | MIT |
| `modernc.org/sqlite` and its Go dependencies | BSD-style and component-specific permissive licenses; see upstream module files |

The Go standard library is distributed under the Go Project's BSD-style license.

## Frontend dependencies

Exact direct and transitive versions are recorded in `web/package-lock.json`. The application uses React, TypeScript, Vite, Tailwind CSS, Radix UI primitives, TanStack Query/Table, React Hook Form, Zod, ECharts, Vitest, and Playwright under their respective upstream licenses (predominantly MIT; Playwright is Apache-2.0). Production assets are bundled locally and do not load code, fonts, or scripts from a CDN.

## Build and distribution metadata

Release workflows produce an SPDX-format software bill of materials for the final image. The SBOM is the authoritative version-level inventory for a particular image digest. Container base-image packages retain their Debian licenses and notices under `/usr/share/doc` as supplied by Debian.

If a notice appears incomplete, open an issue before redistribution so the relevant artifact can be corrected.
