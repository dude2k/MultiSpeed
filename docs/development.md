# Local development

MultiSpeed's networking behavior is Linux-specific. Editing and unit tests can run elsewhere, but route discovery, process-group cancellation, container hardening, and end-to-end network binding must be verified on Linux.

## Toolchain

- Go 1.26.6
- Node.js 24 and npm 11
- Docker Engine with Compose v2 and BuildKit
- `golangci-lint` v2 (version pinned in CI)

Do not install provider dependencies globally for tests. Fake executables and local HTTP fixtures prevent public speed tests and make failure/cancellation deterministic.

## Backend

```bash
go mod download
go fmt ./...
go vet ./...
go test ./...
go build -o bin/multispeed ./cmd/multispeed
```

Run with a repository-local data directory and loopback listener:

```bash
APP_DATA_DIR="$(pwd)/.local-data" \
APP_LISTEN_ADDR=127.0.0.1:8787 \
ACCEPT_OOKLA_EULA=false \
go run ./cmd/multispeed
```

Never commit `.local-data`, databases, provider binaries, or `.env`.

## Frontend

```bash
cd web
npm ci
npm run lint
npm run typecheck
npm run test
npm run generate:api-types
npm run build
```

The Vite development server proxies relative `/api/v1` requests to the local Go server. Production assets are emitted to `web/dist`, copied to `internal/frontend/dist` during the image build, and embedded by Go. The production image has no Node runtime.

Run Playwright only against deterministic application/provider fixtures:

```bash
cd web
npx playwright install --with-deps chromium
npm run test:e2e
```

## Database migrations

Migrations are monotonic SQL files embedded from `internal/migrations`. Never edit a migration that may have shipped. Add the next numbered migration, make it transactional/idempotent where practical, and add upgrade/restart/backup tests. Schema changes require corresponding repository, API schema, OpenAPI, and documentation updates.

Temporary SQLite tests must create isolated directories and close connections. Exercise WAL-aware online backup instead of copying the main file during writes.

## Provider changes

Every adapter declares capabilities. Keep CLI arguments in typed builders, invoke subprocesses without a shell, bind discovery and validation to the configured path, bound stdout/stderr, sanitize stored output, and test malformed JSON, exit codes, timeouts, cancellation, and process-group termination.

Do not add Ookla downloads or binaries anywhere in source, tests, release assets, or images. LibreSpeed upgrades require a pinned source tag, license/source notice update, compatibility tests, and Docker provenance update.

## Full checks

```bash
make check
make e2e
make docker-build
make docker-smoke
```

`make api-lint` runs the version-pinned Redocly CLI with telemetry disabled in CI to validate the OpenAPI 3.1 contract.

The Docker smoke test must verify persistence across restart and must never contact a public speed-test provider.

## Build metadata

Release builds inject semantic version, Git commit, and UTC build timestamp through Go linker variables. Local builds may report development values. Do not make builds depend on a writable source tree or embed secrets from the environment.
