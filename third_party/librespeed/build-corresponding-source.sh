#!/bin/sh
set -eu

# This script is shipped inside MultiSpeed's complete corresponding-source
# archive. Run it from the root of the extracted patched LibreSpeed tree.
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

version="${LIBRESPEED_VERSION:-v1.0.13}"
patch_version="${LIBRESPEED_PATCH_VERSION:-multispeed.dns2.xnet056}"
build_date="${BUILD_DATE:-unknown}"
output="${OUTPUT:-./librespeed-cli}"

go test -mod=vendor -count=1 ./speedtest
go build -mod=vendor -trimpath -buildvcs=false \
  -ldflags "-s -w -X github.com/librespeed/speedtest-cli/defs.ProgName=librespeed-cli -X github.com/librespeed/speedtest-cli/defs.ProgVersion=${version}+${patch_version} -X github.com/librespeed/speedtest-cli/defs.BuildDate=${build_date}" \
  -o "${output}" \
  ./main.go
