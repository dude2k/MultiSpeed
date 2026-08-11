#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 ]]; then
  echo "usage: validate-spdx-frontend.sh <spdx-json> <license-bundle.tar.gz>" >&2
  exit 2
fi

sbom="$1"
license_bundle="$2"
work_directory="$(mktemp -d)"
trap 'rm -rf "${work_directory}"' EXIT

tar -xOzf "${license_bundle}" multispeed/third-party/npm/manifest.json \
  | jq -r '.packages[] | "\(.name)@\(.version)"' \
  | LC_ALL=C sort -u > "${work_directory}/expected"
jq -r '.packages[] | select(.name != null and .versionInfo != null) | "\(.name)@\(.versionInfo)"' "${sbom}" \
  | LC_ALL=C sort -u > "${work_directory}/actual"

test -s "${work_directory}/expected"
comm -23 "${work_directory}/expected" "${work_directory}/actual" > "${work_directory}/missing"
if [[ -s "${work_directory}/missing" ]]; then
  echo "SPDX SBOM is missing frontend components from the shipped npm manifest:" >&2
  sed 's/^/  - /' "${work_directory}/missing" >&2
  exit 1
fi

for required in react echarts vite rolldown tailwindcss; do
  grep -Eq "^${required}@" "${work_directory}/expected"
done
