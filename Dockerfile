# syntax=docker/dockerfile:1.7

ARG NODE_VERSION=24.19.0
ARG GO_VERSION=1.26.5
ARG DEBIAN_RELEASE=bookworm

FROM node:${NODE_VERSION}-${DEBIAN_RELEASE}-slim@sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03 AS frontend-build
WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY web/ ./
RUN npm run build

FROM golang:${GO_VERSION}-${DEBIAN_RELEASE}@sha256:8d36439c36258ba98de1bf2b316eda72905f9d743117119f6db9705c49245644 AS librespeed-build
ARG LIBRESPEED_VERSION=v1.0.13
ARG LIBRESPEED_PATCH_VERSION=multispeed.dns2.xnet055
ARG LIBRESPEED_X_NET_VERSION=v0.55.0
ARG BUILD_DATE=unknown
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64
WORKDIR /build

# LibreSpeed is built from its immutable LGPL tag plus the documented
# MultiSpeed source-bound DNS overlay. The upstream archive, complete overlay,
# integration test, license, and module metadata ship in the runtime image.
COPY third_party/librespeed/ /overlay/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    go mod download "github.com/librespeed/speedtest-cli@${LIBRESPEED_VERSION}"; \
    module_dir="/go/pkg/mod/github.com/librespeed/speedtest-cli@${LIBRESPEED_VERSION}"; \
    mkdir -p /build/librespeed; \
    cp -a "${module_dir}/." /build/librespeed/; \
    chmod -R u+w /build/librespeed; \
    cd /build/librespeed; \
    go get "golang.org/x/net@${LIBRESPEED_X_NET_VERSION}"; \
    test "$(go list -m -f '{{.Version}}' golang.org/x/net)" = "${LIBRESPEED_X_NET_VERSION}"; \
    test "$(grep -Fc 'defaultDialer.LocalAddr = localTCPAddr' speedtest/speedtest.go)" = 1; \
    sed -i '/defaultDialer.LocalAddr = localTCPAddr/a\	defaultDialer.Resolver = newSourceBoundResolver(addr.IP)\n\tif err := restrictDialerToAllowedServerEndpoints(defaultDialer); err != nil { return nil, err }' speedtest/speedtest.go; \
    install -m 0644 /overlay/source_bound_resolver.go speedtest/source_bound_resolver.go; \
    install -m 0644 /overlay/source_bound_resolver_test.go speedtest/source_bound_resolver_test.go; \
    go test -count=1 -run '^(TestSourceBoundResolverUsesSelectedSourceForUDPAndTCPFallback|TestCustomServerDestinationGuard.*)$' ./speedtest; \
    rm speedtest/source_bound_resolver_test.go; \
    go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X github.com/librespeed/speedtest-cli/defs.ProgName=librespeed-cli -X github.com/librespeed/speedtest-cli/defs.ProgVersion=${LIBRESPEED_VERSION}+${LIBRESPEED_PATCH_VERSION} -X github.com/librespeed/speedtest-cli/defs.BuildDate=${BUILD_DATE}" \
      -o /out/librespeed-cli \
      ./main.go; \
    test -x /out/librespeed-cli; \
    install -D -m 0644 LICENSE /out/notices/LICENSE; \
    install -D -m 0644 go.mod /out/notices/go.mod; \
    if [ -f go.sum ]; then install -m 0644 go.sum /out/notices/go.sum; fi; \
    install -D -m 0644 /overlay/README.md /out/notices/multispeed-source-bound-dns/README.md; \
    install -m 0644 /overlay/source_bound_resolver.go /out/notices/multispeed-source-bound-dns/source_bound_resolver.go; \
    install -m 0644 /overlay/source_bound_resolver_test.go /out/notices/multispeed-source-bound-dns/source_bound_resolver_test.go; \
    install -D -m 0644 "/go/pkg/mod/cache/download/github.com/librespeed/speedtest-cli/@v/${LIBRESPEED_VERSION}.zip" "/out/notices/librespeed-speedtest-cli-${LIBRESPEED_VERSION}-source.zip"

FROM golang:${GO_VERSION}-${DEBIAN_RELEASE}@sha256:8d36439c36258ba98de1bf2b316eda72905f9d743117119f6db9705c49245644 AS backend-build
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN rm -rf internal/frontend/dist && mkdir -p internal/frontend/dist
COPY --from=frontend-build /src/web/dist/ ./internal/frontend/dist/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    test "${TARGETOS}" = linux; \
    test "${TARGETARCH}" = amd64; \
    GOOS=linux GOARCH=amd64 go build \
      -trimpath \
      -buildvcs=false \
      -ldflags "-s -w -X main.version=${VERSION} -X main.gitCommit=${VCS_REF} -X main.buildTime=${BUILD_DATE}" \
      -o /out/multispeed \
      ./cmd/multispeed

FROM debian:${DEBIAN_RELEASE}-slim@sha256:abd67ffcfa541b485a3dff59865ab629aa048a6c613e639d36e7456b0b229241 AS runtime
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG LIBRESPEED_VERSION=v1.0.13
ARG LIBRESPEED_PATCH_VERSION=multispeed.dns2.xnet055
ARG LIBRESPEED_X_NET_VERSION=v0.55.0

LABEL org.opencontainers.image.title="MultiSpeed" \
      org.opencontainers.image.description="Production-ready multi-WAN speed-test monitor" \
      org.opencontainers.image.source="https://github.com/dude2k/MultiSpeed" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      io.multispeed.librespeed.version="${LIBRESPEED_VERSION}" \
      io.multispeed.librespeed.patch="${LIBRESPEED_PATCH_VERSION}" \
      io.multispeed.librespeed.x-net.version="${LIBRESPEED_X_NET_VERSION}" \
      io.multispeed.ookla.included="false"

RUN set -eux; \
    apt-get update; \
    apt-get install --yes --no-install-recommends ca-certificates iproute2 tzdata; \
    rm -rf /var/lib/apt/lists/*; \
    groupadd --system --gid 10001 multispeed; \
    useradd --system --uid 10001 --gid multispeed --home-dir /nonexistent --shell /usr/sbin/nologin multispeed; \
    install -d -o multispeed -g multispeed -m 0750 /data; \
    install -d -o root -g root -m 0755 /opt/multispeed/providers; \
    install -d -o root -g root -m 0755 /usr/share/doc/librespeed-cli

COPY --from=backend-build --chown=root:root /out/multispeed /usr/local/bin/multispeed
COPY --from=librespeed-build --chown=root:root /out/librespeed-cli /usr/local/bin/librespeed-cli
COPY --from=librespeed-build --chown=root:root /out/notices/ /usr/share/doc/librespeed-cli/
COPY --chown=root:root LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/multispeed/

RUN chmod 0755 /usr/local/bin/multispeed /usr/local/bin/librespeed-cli && \
    chmod -R a-w /usr/local/bin /usr/share/doc/multispeed /usr/share/doc/librespeed-cli

ENV APP_LISTEN_ADDR=127.0.0.1:8787 \
    APP_DATA_DIR=/data \
    APP_LOG_LEVEL=INFO \
    APP_SHUTDOWN_TIMEOUT=20s \
    LIBRESPEED_BINARY=/usr/local/bin/librespeed-cli \
    OOKLA_BINARY=/opt/multispeed/providers/speedtest \
    ACCEPT_OOKLA_EULA=false

USER 10001:10001
WORKDIR /data
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/multispeed"]
