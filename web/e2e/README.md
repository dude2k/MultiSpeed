# Real-backend browser tests

`npm run test:e2e` starts two local processes:

1. The Vite development server, which proxies `/api` to the test backend.
2. A Go test backend assembled from MultiSpeed's production API, SQLite store,
   scheduler, execution manager, interface discovery, and Ookla/LibreSpeed
   adapters.

The only injected backend dependency is route validation. It returns a fixed,
successful snapshot so the suite never contacts a public IP-discovery or speed
test endpoint. The provider adapters execute the shell fixtures in
`e2e/fixtures/`, whose discovery and measurement JSON is deterministic.
Cloudflare is presented by the real provider registry but is not executed.

On Linux, the launcher builds `./web/e2e/backend` with the `go` command found on
`PATH`. When Go is unavailable, it builds and runs `e2e/Dockerfile` instead.
Useful overrides:

- `MULTISPEED_E2E_BACKEND_COMMAND`: complete backend start command. The launcher
  supplies `APP_LISTEN_ADDR`, an isolated `APP_DATA_DIR`, and fixture paths.
- `MULTISPEED_E2E_GO_COMMAND`: Go executable to use for the test-backend build.
- `MULTISPEED_E2E_DOCKER_COMMAND`: Docker executable to use for fallback.
- `MULTISPEED_E2E_DOCKER_IMAGE`: prebuilt compatible test image; skips build.
- `MULTISPEED_E2E_BACKEND_PORT`: backend port, default `18787`.

The command/image override must expose the same test-backend composition if the
measurement assertions are expected to pass. Every run receives a fresh SQLite
database, and the launcher removes its temporary directory or container on exit.
