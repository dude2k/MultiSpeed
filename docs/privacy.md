# Privacy and data flow

MultiSpeed is self-hosted and has no user accounts, authentication, maintainer-operated analytics, advertising, or project telemetry endpoint. This does not make a reachable instance private: every client with network access to the unauthenticated listener can view operational data, change configuration, start tests, and download exports or backups. The deployment operator is responsible for access control, retention, notices, and any legal obligations that apply to the measured connections or stored addresses.

## Data stored locally

The SQLite database under `/data` can contain:

- task names and descriptions, schedules, timezones, provider choices, targets, custom backend definitions, and provider options;
- interface names, selected source addresses, route-profile descriptions and notes, expected gateways/tables, validation targets, and route-validation snapshots;
- timestamps, throughput, latency, jitter, loss, byte counts, process exit status, provider/server metadata, result URLs, detected public IP addresses, and sanitized bounded provider output or errors;
- operational settings and the legacy-named Ookla terms acknowledgement state, MultiSpeed internal review marker, and timestamp.

A managed Ookla executable is a separate file at `/data/providers/ookla/speedtest`; path-isolated CLI-created configuration/state is stored beneath `/data/providers/ookla/runtime`. Neither is stored in SQLite. The proprietary CLI controls the contents of its runtime files, which may include its settings, acceptance state, or device-related identifiers; protect and retain them as sensitive provider state. Deployment environment variables remain outside the database. The system API deliberately omits environment-variable values, but configuration chosen through those variables can still affect observable behavior.

The browser stores only the selected color-theme preference in local storage. The application does not use that value as a user identity. Production frontend assets are embedded and do not fetch scripts, fonts, or analytics from a CDN.

## Outbound network traffic

MultiSpeed necessarily sends traffic through the selected WAN path to third parties involved in a test:

- route validation resolves and contacts the operator-selected validation target and uses the trace endpoint operated by Cloudflare to observe the public IP and edge location;
- the native Cloudflare® adapter sends latency, download, upload, and trace requests to `speed.cloudflare.com`;
- LibreSpeed server discovery and tests contact the selected public or deployment-authorized custom backend. MultiSpeed invokes the bundled CLI with telemetry disabled, but the contacted server still observes the connection and test traffic;
- the optional external Ookla CLI performs its own discovery and measurement requests under Ookla's software and service terms. MultiSpeed supplies the chosen source path and target arguments but does not control every item the proprietary executable may transmit;
- bound DNS resolution uses the host-configured resolver when suitable and can use the family-matched public resolver operated by Cloudflare when the documented fail-closed resolver rules require it.

Provider servers, DNS operators, transit networks, and the ISP can observe information inherent in those connections, including source/public IP addresses, timing, payload volume, and target choice. Follow the applicable provider terms and local policy before scheduling tests.

Links to Ookla's EULA, Terms of Use, Privacy Policy, and CLI download page are ordinary external links and contact Ookla only when an operator opens them in the browser.

Cloudflare is a trademark and/or registered trademark of Cloudflare, Inc. MultiSpeed is not affiliated with, endorsed by, or sponsored by Cloudflare, Inc.

Ookla® and Speedtest® are registered trademarks of Ookla, LLC. MultiSpeed is not affiliated with, endorsed by, or sponsored by Ookla.

## Local access and browser boundary

The HTTP API is same-origin and does not enable cross-origin browser access. Mutation requests with an `Origin` header must match the request host, and every request is subject to Host validation. Non-browser clients may omit `Origin`, so these checks are not authentication. A wildcard listener accepts concrete unicast IP literals without an allowlist but rejects arbitrary DNS names against DNS rebinding; explicitly trusted DNS names, host firewall, and trusted-network policy remain the operator's responsibility.

Use an authenticating TLS reverse proxy and a loopback listener for remote access. Do not expose port 8787 directly to the public internet. Ensure proxy access logs receive protection appropriate for URLs, client addresses, and request timing.

## Exports, backups, logs, and support reports

The three portability mechanisms have different privacy scopes:

- configuration JSON excludes measurement history, the Ookla terms acknowledgement, and the managed executable, but still contains task descriptions, source addresses, route notes, target identifiers, custom backend URLs, and provider options;
- an online SQLite backup contains the full committed database, including result history, public/source addresses, diagnostics, and the Ookla terms acknowledgement;
- a stopped copy of `/data` additionally contains the managed Ookla executable and CLI runtime homes when present.

Structured application logs include request IDs, paths, status, timing, and operational/provider errors. Debug material and result diagnostics can reveal interface names, private or public addresses, route choices, targets, and server metadata. Before publishing an issue or support artifact:

1. remove private and public IP addresses that are not essential to the report;
2. remove hostnames, custom backend URLs, route notes, task descriptions, result URLs, and provider diagnostics that identify the deployment;
3. never include databases, executable uploads, credentials, environment files, reverse-proxy headers, or access tokens;
4. use GitHub private vulnerability reporting when the disclosure could be security-sensitive.

Deleting or retiring a task does not delete its historical results. The configured retention policy or an explicit result cleanup controls database history; it does not remove copies already downloaded as exports/backups or captured by external logging and backup systems.
