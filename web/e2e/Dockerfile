# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.5
ARG DEBIAN_RELEASE=bookworm

FROM golang:${GO_VERSION}-${DEBIAN_RELEASE}@sha256:8d36439c36258ba98de1bf2b316eda72905f9d743117119f6db9705c49245644 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY internal ./internal
COPY web/e2e/backend ./web/e2e/backend
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -o /out/multispeed-e2e ./web/e2e/backend

FROM debian:${DEBIAN_RELEASE}-slim@sha256:abd67ffcfa541b485a3dff59865ab629aa048a6c613e639d36e7456b0b229241
RUN groupadd --system --gid 10001 multispeed && \
    useradd --system --uid 10001 --gid multispeed --home-dir /nonexistent --shell /usr/sbin/nologin multispeed && \
    install -d -o multispeed -g multispeed -m 0750 /data && \
    install -d -m 0755 /opt/multispeed/providers
COPY --from=build /out/multispeed-e2e /usr/local/bin/multispeed-e2e
COPY web/e2e/fixtures/ookla-speedtest /opt/multispeed/providers/ookla-speedtest
COPY web/e2e/fixtures/librespeed-cli /opt/multispeed/providers/librespeed-cli
RUN chmod 0755 /usr/local/bin/multispeed-e2e /opt/multispeed/providers/ookla-speedtest /opt/multispeed/providers/librespeed-cli

ENV APP_LISTEN_ADDR=0.0.0.0:18787 \
    APP_DATA_DIR=/data \
    OOKLA_BINARY=/opt/multispeed/providers/ookla-speedtest \
    LIBRESPEED_BINARY=/opt/multispeed/providers/librespeed-cli \
    ACCEPT_OOKLA_EULA=false

EXPOSE 18787
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/multispeed-e2e"]
