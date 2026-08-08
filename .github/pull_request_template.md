## Summary

Describe the operator-facing outcome and why this change is needed.

## Verification

List commands actually run and their results. Mark commands not run and explain why.

- [ ] `go fmt ./...`
- [ ] `go vet ./...`
- [ ] `golangci-lint run`
- [ ] `go test ./...`
- [ ] `go build ./cmd/multispeed`
- [ ] `npm ci && npm run lint && npm run typecheck && npm run test && npm run build` in `web/`
- [ ] `npm run test:e2e` in `web/`
- [ ] `docker build --platform linux/amd64 -t multispeed:local .`
- [ ] `bash scripts/docker-smoke.sh multispeed:local`

## Safety and compatibility

- [ ] No public speed test runs in CI or tests.
- [ ] Configured network paths fail closed; there is no fallback to another WAN.
- [ ] No code modifies host routes, rules, addresses, gateways, or firewall state.
- [ ] No authentication/session/user/token code or permissive CORS was added.
- [ ] The one-container, host-networked, non-root, capability-free deployment still works.
- [ ] No Ookla executable or download/install step was added.
- [ ] Provider output, subprocess arguments, response sizes, and error details remain bounded and sanitized.

## Data, API, and documentation

- [ ] Migrations are additive and upgrade/restart/backup behavior is tested, or no schema changed.
- [ ] `openapi.yaml`, API models, and frontend types agree, or no API changed.
- [ ] User/operations documentation and `CHANGELOG.md` are updated.
- [ ] Dependency versions, lockfiles, SBOM impact, and `THIRD_PARTY_NOTICES.md` are updated where needed.

## Screenshots

Include screenshots for visible UI changes in both relevant themes and at desktop/mobile widths. Remove network identifiers and result metadata first.
