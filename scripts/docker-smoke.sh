#!/usr/bin/env bash
set -Eeuo pipefail

image="${1:-multispeed:local}"
port="${MULTISPEED_SMOKE_PORT:-18787}"
name="multispeed-smoke-${RANDOM}-$$"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/multispeed-smoke.XXXXXX")"
data_dir="${work_dir}/data"
fake_cli="${work_dir}/librespeed-cli"
provider_volume=""

cleanup() {
  docker rm --force "${name}" >/dev/null 2>&1 || true
  if [[ -n "${provider_volume}" ]]; then
    docker volume rm --force "${provider_volume}" >/dev/null 2>&1 || true
  fi
  if [[ -d "${work_dir}" && "${work_dir}" == */multispeed-smoke.* && "${work_dir}" != "/" ]]; then
    rm -rf -- "${work_dir}"
  fi
}
trap cleanup EXIT INT TERM

mkdir -p "${data_dir}"

cat >"${fake_cli}" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'librespeed-cli v1.0.13+multispeed.dns2.xnet056 smoke fixture'
  exit 0
fi
printf '%s\n' '[{"timestamp":"2026-01-01T00:00:00Z","server":{"name":"Local smoke fixture","url":"http://127.0.0.1"},"client":{"ip":"203.0.113.10"},"bytes_sent":62500000,"bytes_received":125000000,"ping":8.25,"jitter":0.75,"upload":50,"download":100,"share":""}]'
EOF
chmod 0755 "${fake_cli}"

listen_address="127.0.0.1:${port}"
network_args=(--network host)
expected_network_mode="host"
curl_null_device="/dev/null"
data_dir_docker="${data_dir}"
fake_cli_docker="${fake_cli}"
provider_mount=(--mount "type=bind,src=${fake_cli_docker},dst=/opt/multispeed/providers/librespeed-cli,readonly")
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    # Docker Desktop does not expose a container's host-network loopback to
    # the Windows host. Publish the smoke port explicitly instead.
    listen_address="0.0.0.0:${port}"
    network_args=(--network bridge --publish "127.0.0.1:${port}:${port}")
    expected_network_mode="bridge"
    data_dir_docker="$(cygpath -w "${data_dir}")"
    fake_cli_docker="$(cygpath -w "${fake_cli}")"
    # Stop MSYS from rewriting Linux container paths such as /data and /bin/sh.
    export MSYS_NO_PATHCONV=1
    curl_null_device="NUL"
    # Desktop bind mounts do not preserve the executable bit. Stage the fixture
    # into a short-lived Docker volume with explicit mode instead.
    provider_volume="${name}-provider"
    docker volume create "${provider_volume}" >/dev/null
    docker run --rm \
      --user 0:0 \
      --mount "type=bind,src=${fake_cli_docker},dst=/fixture/librespeed-cli,readonly" \
      --mount "type=volume,src=${provider_volume},dst=/providers" \
      --entrypoint /bin/sh "${image}" -c 'cp /fixture/librespeed-cli /providers/librespeed-cli && chmod 0755 /providers/librespeed-cli'
    provider_mount=(--mount "type=volume,src=${provider_volume},dst=/opt/multispeed/providers,readonly")
    ;;
esac

docker run --detach \
  --name "${name}" \
  --init \
  --platform linux/amd64 \
  "${network_args[@]}" \
  --user "$(id -u):$(id -g)" \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  --mount "type=bind,src=${data_dir_docker},dst=/data" \
  "${provider_mount[@]}" \
  --env "APP_LISTEN_ADDR=${listen_address}" \
  --env APP_DATA_DIR=/data \
  --env LIBRESPEED_BINARY=/opt/multispeed/providers/librespeed-cli \
  --env ACCEPT_OOKLA_EULA=false \
  "${image}" >/dev/null

for attempt in $(seq 1 60); do
  if curl --silent --fail "http://127.0.0.1:${port}/api/v1/readyz" >/dev/null; then
    break
  fi
  if [ "${attempt}" -eq 60 ]; then
    docker logs "${name}" >&2
    exit 1
  fi
  sleep 1
done

# Resolve the exact path from the running container. This works for native
# Linux host networking and Docker Desktop's bridged fallback alike.
route_line="$(docker exec "${name}" ip -4 route get 1.1.1.1 | head -n 1)"
interface_name="$(awk '{for (i=1; i<=NF; i++) if ($i == "dev") {print $(i+1); exit}}' <<<"${route_line}")"
source_ip="$(awk '{for (i=1; i<=NF; i++) if ($i == "src") {print $(i+1); exit}}' <<<"${route_line}")"
if [ -z "${interface_name}" ] || [ -z "${source_ip}" ]; then
  printf '%s\n' 'Could not determine the default IPv4 interface and source address.' >&2
  exit 1
fi

curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/healthz" >/dev/null
curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/readyz" >/dev/null
frontend_html="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/")"
grep -q 'MultiSpeed' <<<"${frontend_html}"

settings_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/settings")"
grep -q '"ooklaEulaAccepted":false' <<<"${settings_json}"
grep -q '"ooklaEulaEffectiveAccepted":false' <<<"${settings_json}"
grep -q '"ooklaEulaAcceptanceSource":"none"' <<<"${settings_json}"
grep -q '"ooklaEulaCurrentVersion":"ookla-eula-terms-privacy-review-2026-08-11"' <<<"${settings_json}"
system_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/system")"
grep -q '"schemaVersion":6' <<<"${system_json}"

unconfirmed_eula_status="$(curl --silent --show-error \
  --output "${curl_null_device}" \
  --write-out '%{http_code}' \
  --request PUT \
  --header 'Content-Type: application/json' \
  --data '{"accepted":true,"confirmed":false}' \
  "http://127.0.0.1:${port}/api/v1/settings/ookla-eula")"
test "${unconfirmed_eula_status}" = 422
settings_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/settings")"
grep -q '"ooklaEulaAccepted":false' <<<"${settings_json}"

incomplete_eula_status="$(curl --silent --show-error \
  --output "${curl_null_device}" \
  --write-out '%{http_code}' \
  --request PUT \
  --header 'Content-Type: application/json' \
  --data '{}' \
  "http://127.0.0.1:${port}/api/v1/settings/ookla-eula")"
test "${incomplete_eula_status}" = 422

settings_json="$(curl --silent --show-error --fail \
  --request PUT \
  --header 'Content-Type: application/json' \
  --data '{"accepted":true,"confirmed":true}' \
  "http://127.0.0.1:${port}/api/v1/settings/ookla-eula")"
grep -q '"ooklaEulaAccepted":true' <<<"${settings_json}"
grep -q '"ooklaEulaVersion":"ookla-eula-terms-privacy-review-2026-08-11"' <<<"${settings_json}"
grep -q '"ooklaEulaEffectiveAccepted":true' <<<"${settings_json}"
grep -q '"ooklaEulaAcceptanceSource":"persisted"' <<<"${settings_json}"

test "$(docker inspect --format '{{.HostConfig.NetworkMode}}' "${name}")" = "${expected_network_mode}"
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "${name}")" = true
test "$(docker inspect --format '{{.Config.User}}' "${name}")" != 0
docker inspect --format '{{json .HostConfig.CapDrop}}' "${name}" | grep -q 'ALL'
docker inspect --format '{{json .HostConfig.SecurityOpt}}' "${name}" | grep -q 'no-new-privileges'

task_payload="$(printf '{"name":"Docker smoke LibreSpeed","description":"Deterministic local provider fixture","enabled":false,"provider":"librespeed","cronExpression":"0 * * * *","timezone":"UTC","serverSelectionMode":"automatic","interfaceName":"%s","sourceIp":"%s","ipFamily":"ipv4","timeoutSeconds":30,"routeValidation":"required"}' "${interface_name}" "${source_ip}")"
task_json="$(curl --silent --show-error --fail \
  --request POST \
  --header 'Content-Type: application/json' \
  --data "${task_payload}" \
  "http://127.0.0.1:${port}/api/v1/tasks")"
task_id="$(sed -n 's/.*"id":"\([^"]*\)".*/\1/p' <<<"${task_json}")"
test -n "${task_id}"

run_json="$(curl --silent --show-error --fail \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{}' \
  "http://127.0.0.1:${port}/api/v1/tasks/${task_id}/run")"
result_id="$(sed -n 's/.*"id":"\([^"]*\)".*/\1/p' <<<"${run_json}")"
test -n "${result_id}"

for attempt in $(seq 1 90); do
  result_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/results/${result_id}")"
  if grep -q '"status":"succeeded"' <<<"${result_json}"; then
    break
  fi
  if grep -Eq '"status":"(failed|skipped|cancelled)"' <<<"${result_json}"; then
    printf '%s\n' "${result_json}" >&2
    exit 1
  fi
  if [ "${attempt}" -eq 90 ]; then
    printf '%s\n' "${result_json}" >&2
    exit 1
  fi
  sleep 1
done

grep -q '"downloadBitsPerSecond":100000000' <<<"${result_json}"
grep -q '"uploadBitsPerSecond":50000000' <<<"${result_json}"
grep -q 'multispeed-bps-v2' <<<"${result_json}"

configuration_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/config/export")"
grep -q '"format":"multispeed-config"' <<<"${configuration_json}"
grep -q "${task_id}" <<<"${configuration_json}"
if grep -q 'ooklaEula' <<<"${configuration_json}"; then
  printf '%s\n' 'Configuration export leaked Ookla EULA state.' >&2
  exit 1
fi
configuration_json="${configuration_json/Docker smoke LibreSpeed/Docker smoke restored}"
import_json="$(curl --silent --show-error --fail \
  --request POST \
  --header 'Content-Type: application/json' \
  --data-binary "${configuration_json}" \
  "http://127.0.0.1:${port}/api/v1/config/import")"
grep -q '"taskCount":1' <<<"${import_json}"
grep -q '"routeProfileCount":0' <<<"${import_json}"
grep -q '"settingsUpdated":true' <<<"${import_json}"
task_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/tasks/${task_id}")"
grep -q '"name":"Docker smoke restored"' <<<"${task_json}"
settings_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/settings")"
grep -q '"ooklaEulaAccepted":true' <<<"${settings_json}"

test -s "${data_dir}/multispeed.db"
docker restart "${name}" >/dev/null

for attempt in $(seq 1 60); do
  if curl --silent --fail "http://127.0.0.1:${port}/api/v1/readyz" >/dev/null; then
    break
  fi
  if [ "${attempt}" -eq 60 ]; then
    docker logs "${name}" >&2
    exit 1
  fi
  sleep 1
done

task_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/tasks/${task_id}")"
result_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/results/${result_id}")"
settings_json="$(curl --silent --show-error --fail "http://127.0.0.1:${port}/api/v1/settings")"
grep -q "${task_id}" <<<"${task_json}"
grep -q '"status":"succeeded"' <<<"${result_json}"
grep -q '"ooklaEulaAccepted":true' <<<"${settings_json}"
grep -q '"ooklaEulaEffectiveAccepted":true' <<<"${settings_json}"
grep -q '"ooklaEulaAcceptanceSource":"persisted"' <<<"${settings_json}"

printf '%s\n' 'Docker smoke test passed: schema 6, health, readiness, frontend, hardened runtime, config roundtrip, versioned EULA acceptance, normalized fake provider, and restart persistence.'
