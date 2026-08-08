# Contributing to MultiSpeed

Thank you for improving MultiSpeed. Changes should preserve its core invariants: one-container Linux deployment, independent persisted tasks, explicit source-path binding, fail-closed route validation, no host-network mutation, no authentication subsystem, and no redistributed Ookla executable.

## Before opening a change

1. Search existing issues and describe the user or operational problem.
2. Keep changes focused and original; do not copy branding, layouts, assets, or substantial code from reference projects.
3. For new dependencies, explain need, maintenance health, license, supply-chain impact, and runtime cost.
4. Update tests, OpenAPI, documentation, changelog, and third-party notices together when applicable.

Use conventional, imperative commit subjects where practical. Never commit `.env`, databases, backups, public/provider test results, credentials, provider binaries, build output, or dependency caches.

## Development checks

Backend:

```bash
go fmt ./...
go vet ./...
golangci-lint run
go test ./...
go build ./cmd/multispeed
```

Frontend:

```bash
cd web
npm ci
npm run lint
npm run typecheck
npm run test
npm run build
npm run test:e2e
```

Container:

```bash
docker build --platform linux/amd64 -t multispeed:local .
bash scripts/docker-smoke.sh multispeed:local
```

Do not run real public speed tests in CI. Use fake CLI executables, temporary SQLite databases, and local deterministic HTTP fixtures.

## Backend expectations

- Use contexts, explicit timeouts, bounded queues/output, structured logging, and sanitized errors.
- Keep provider-specific behavior inside adapters.
- Invoke subprocesses with argument arrays; never pass user data to a shell.
- Ensure cancellation terminates the full process group.
- Never fall back from a configured interface/source address.
- Add migrations rather than modifying released migration files.
- Keep all persisted timestamps UTC and aggregate in the requested reporting timezone.

## Frontend expectations

- Maintain strict TypeScript and schema validation.
- Preserve keyboard operation, visible focus, semantic HTML, WCAG AA contrast, responsive layouts, and useful loading/error/empty states.
- Use relative same-origin API URLs and local assets only.
- Do not add login, user, token, session, or role interfaces.
- Label Cloudflare as automatic edge selection and show provider-method differences.

## Documentation and licensing

Provider/dependency upgrades require exact versions, lockfile updates, license review, and reproducible build information. Never add an Ookla download step or binary. LibreSpeed changes must preserve its LGPL notice, source availability, and replaceable subprocess boundary.

## Pull requests

Complete the pull-request checklist, include verification results actually run, and identify anything not run. Reviewers may request focused Linux tests for routing, subprocess lifecycle, SQLite backup, container permissions, and host networking.
