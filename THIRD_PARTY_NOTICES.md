# Third-party notices

MultiSpeed is MIT-licensed. Third-party components remain under their respective licenses. This file is informational and does not replace the license text shipped by each component.

## Runtime provider component

### LibreSpeed CLI

- Project: `librespeed/speedtest-cli`
- Version: v1.0.13 (tagged 2026-04-30)
- Source: <https://github.com/librespeed/speedtest-cli/tree/v1.0.13>
- License: GNU Lesser General Public License v3.0 (LGPL-3.0-only)
- Relationship: separately built and separately executed, replaceable subprocess; it is not linked into the MultiSpeed binary

The production image builds LibreSpeed CLI from the tagged source and applies the MultiSpeed `multispeed.dns2.xnet056` overlay under LGPL-3.0-or-later. The overlay makes UDP DNS and TCP fallback bind the same selected source address as the HTTP test sockets, pins authorized custom runs to their pre-resolved IP:port endpoints, blocks redirects before follow-up requests, and pins `golang.org/x/net` v0.56.0 in place of upstream v0.49.0. The image and release assets include a deterministic complete corresponding-source archive containing the exact patched tree, vendored dependency source, build script, integration test, module metadata, and the full GPLv3 and LGPLv3 license texts. Telemetry is disabled by default.

LibreSpeed CLI copyright remains with its upstream contributors. It is provided without warranty under LGPL-3.0-only; the MultiSpeed overlay files are offered under LGPL-3.0-or-later. You may replace `/usr/local/bin/librespeed-cli` in a private deployment with a compatible build, subject to the component's license. MultiSpeed fails closed unless that build advertises the `+multispeed.dns2.xnet056` marker and therefore attests to the required source-bound resolver, destination-pinning, and patched dependency baseline.

## Optional operator component not distributed by MultiSpeed

### Ookla Speedtest CLI

- Expected compatible version: 1.2.0.84
- Official product: <https://www.speedtest.net/apps/cli>
- Official EULA: <https://www.speedtest.net/about/eula>
- Official Terms of Use: <https://www.speedtest.net/about/terms>
- Official Privacy Policy: <https://www.speedtest.net/about/privacy>
- License: proprietary Ookla Speedtest CLI EULA
- Relationship: optional external operator-supplied executable

MultiSpeed does not contain, download, or redistribute Speedtest CLI by Ookla. An opt-in private-network endpoint can validate and persist one separately obtained, operator-supplied executable outside the image. After an operator explicitly records the technical acknowledgement, MultiSpeed passes `--accept-license` and `--accept-gdpr` to that executable non-interactively. The persisted acknowledgement and the legacy-named `ACCEPT_OOKLA_EULA=true` override only authorize MultiSpeed to pass those flags and open its technical gate; neither is permission, a license grant, legal advice, or a substitute for separate authorization.

Ookla's public EULA describes personal, non-commercial CLI use on one personal computer, excludes routers, modems, and other non-PC devices, and restricts making the CLI available on a network where more than one device can access it. Operators must use the integration only when their deployment fits the binding current documents or they have separate written Ookla authorization for the device, server, container, network access, automated, commercial, or redistribution scenario. Ookla® and Speedtest® are registered trademarks of Ookla, LLC. MultiSpeed is an independent project and is not affiliated with, endorsed by, or sponsored by Ookla, LLC.

## Methodology reference

### Cloudflare speedtest

- Project: `cloudflare/speedtest`
- Source: <https://github.com/cloudflare/speedtest>
- License: MIT
- Relationship: methodology and endpoint reference; MultiSpeed uses an original native Go adapter and does not bundle the browser application

Cloudflare® is a trademark and/or registered trademark of Cloudflare, Inc. MultiSpeed is an independent project and is not affiliated with, endorsed by, or sponsored by Cloudflare, Inc. Measurements against the automatically selected edge are labeled as a distinct methodology.

## Go dependencies

Exact versions and transitive checksums are recorded in `go.mod` and `go.sum`. The production image contains the complete license and notice bundle for the modules used by the MultiSpeed binary under `/usr/share/doc/multispeed/third-party/go`; LibreSpeed's Go dependency notices are under `/usr/share/doc/librespeed-cli/dependency-licenses`.

| Component | License |
| --- | --- |
| `github.com/go-chi/chi/v5` | MIT |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/robfig/cron/v3` | MIT |
| `modernc.org/sqlite` and its Go dependencies | BSD-style and component-specific permissive licenses; see upstream module files |

The Go standard library is distributed under the Go Project's BSD-style license.

## Frontend dependencies

Exact direct and transitive versions are recorded in `web/package-lock.json`. A manifest with the full license and notice files for every package included in the production frontend graph is generated from the lockfile and installed under `/usr/share/doc/multispeed/third-party/npm`. The bundle also includes Vite, Rolldown (including its third-party notices), and Tailwind CSS because those build tools contribute runtime helpers or generated code/styles to the distributed assets; unrelated development-only tools are excluded. Production assets are bundled locally and do not load code, fonts, or scripts from a CDN.

## Build and distribution metadata

Release workflows produce a Trivy SPDX-format scanner inventory for the final image. Sanitized package metadata beside each shipped npm notice makes bundled frontend components discoverable without publishing upstream maintainer or repository fields; CI compares every npm `name@version` in the license manifest with the SPDX output. The SPDX file must still be read together with the versioned npm and Go license manifests and the LibreSpeed corresponding-source archive because no single artifact replaces the applicable license texts or source obligations. The image carries reproducible `multispeed-third-party-licenses.tar.gz` and LibreSpeed corresponding-source archives with SHA-256 checksum files under `/opt/multispeed/release-artifacts`, and release automation publishes those files with the scanner SBOM. Container base-image packages retain their Debian licenses and notices under `/usr/share/doc` as supplied by Debian.

If a notice appears incomplete, open an issue before redistribution so the relevant artifact can be corrected.
