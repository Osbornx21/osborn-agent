#!/usr/bin/env bash
set -euo pipefail

REGION="cn-shanghai"
INSTANCE_ID="i-uf63f4ymqc2dxtljxz2n"
DEVICE_ID="44:1b:f6:e2:74:50"
SERVICE_NAME="stackchan-gateway"
SERVICE_PORT="21082"
GATEWAY_BIN="/opt/a21-air/stackchan-gateway/stackchan-gateway"
CONFIG_PATH="/etc/a21-air/stackchan-gateway.yaml"
ENV_FILE="/etc/a21-air/gateway.env"
OUTPUT_PATH="/var/lib/a21-air/fixtures/asr/spoken-opus.json"
ADVERTISE_URL="ws://47.103.57.217/xiaozhi/v1/ws"
MAX_FRAMES="200"
TIMEOUT_MS="60000"
PROXY_URL="${HTTPS_PROXY:-${ALL_PROXY:-}}"
EXECUTE=0

usage() {
  cat <<'EOF'
usage: server/deploy/aliyun/physical-asr-fixture-capture.sh [options]

Capture a real spoken Xiaozhi Opus ASR fixture from the physical A21 Air
StackChan through the ECS public gateway path. Default mode is dry-run and
does not call Aliyun APIs or stop the gateway.

Options:
  --region <id>            Aliyun region. Default: cn-shanghai
  --instance <id>          ECS instance id.
  --device <id>            Runtime Xiaozhi Device-Id/MAC.
  --service <name>         systemd service. Default: stackchan-gateway
  --service-port <port>    ECS-local gateway port behind Caddy. Default: 21082
  --gateway-bin <path>     Installed gateway binary path.
  --config <path>          Runtime gateway config path.
  --env-file <path>        Private gateway env file sourced on ECS.
  --output <path>          Durable fixture path. Default:
                           /var/lib/a21-air/fixtures/asr/spoken-opus.json
  --advertise-url <url>    Public ws/wss URL printed for capture. Default:
                           ws://47.103.57.217/xiaozhi/v1/ws
  --max-frames <n>         Max Opus frames to capture. Default: 200
  --timeout-ms <n>         Capture timeout. Default: 60000
  --proxy <url>            Proxy for aliyun CLI.
  --execute                Actually run Cloud Assistant and temporarily stop
                           stackchan-gateway while capture owns its port.
  -h, --help               Show this help.

During --execute, ask the operator to wake/tap StackChan and speak during the
capture window. The remote command prints no tokens, no audio payloads and no
transcripts. It always attempts to restart stackchan-gateway on exit.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --region) REGION="${2:?missing --region value}"; shift 2 ;;
    --instance) INSTANCE_ID="${2:?missing --instance value}"; shift 2 ;;
    --device) DEVICE_ID="${2:?missing --device value}"; shift 2 ;;
    --service) SERVICE_NAME="${2:?missing --service value}"; shift 2 ;;
    --service-port) SERVICE_PORT="${2:?missing --service-port value}"; shift 2 ;;
    --gateway-bin) GATEWAY_BIN="${2:?missing --gateway-bin value}"; shift 2 ;;
    --config) CONFIG_PATH="${2:?missing --config value}"; shift 2 ;;
    --env-file) ENV_FILE="${2:?missing --env-file value}"; shift 2 ;;
    --output) OUTPUT_PATH="${2:?missing --output value}"; shift 2 ;;
    --advertise-url) ADVERTISE_URL="${2:?missing --advertise-url value}"; shift 2 ;;
    --max-frames) MAX_FRAMES="${2:?missing --max-frames value}"; shift 2 ;;
    --timeout-ms) TIMEOUT_MS="${2:?missing --timeout-ms value}"; shift 2 ;;
    --proxy) PROXY_URL="${2:?missing --proxy value}"; shift 2 ;;
    --execute) EXECUTE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

is_positive_integer() {
  [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -gt 0 ]
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 2
  }
}

if ! is_positive_integer "$SERVICE_PORT"; then
  echo "--service-port must be a positive integer" >&2
  exit 2
fi
if ! is_positive_integer "$MAX_FRAMES" || [ "$MAX_FRAMES" -lt 10 ] || [ "$MAX_FRAMES" -gt 400 ]; then
  echo "--max-frames must be an integer between 10 and 400" >&2
  exit 2
fi
if ! is_positive_integer "$TIMEOUT_MS" || [ "$TIMEOUT_MS" -lt 5000 ] || [ "$TIMEOUT_MS" -gt 300000 ]; then
  echo "--timeout-ms must be an integer between 5000 and 300000" >&2
  exit 2
fi
case "$OUTPUT_PATH" in
  *'/../'*|'../'*|*'/..'|*'/./'*)
    echo "--output must not contain path traversal" >&2
    exit 2
    ;;
esac
case "$OUTPUT_PATH" in
  /var/lib/a21-air/fixtures/asr/*.json|*/server/var/fixtures/asr/*.json|server/var/fixtures/asr/*.json|./server/var/fixtures/asr/*.json|var/fixtures/asr/*.json|./var/fixtures/asr/*.json) ;;
  *) echo "--output must be under /var/lib/a21-air/fixtures/asr/ or server/var/fixtures/asr/" >&2; exit 2 ;;
esac
case "$ADVERTISE_URL" in
  ws://*/xiaozhi/*|wss://*/xiaozhi/*) ;;
  *) echo "--advertise-url must be a ws/wss Xiaozhi URL" >&2; exit 2 ;;
esac

remote_script() {
  cat <<'REMOTE'
set -euo pipefail
DEVICE_ID="__DEVICE_ID__"
SERVICE_NAME="__SERVICE_NAME__"
SERVICE_PORT="__SERVICE_PORT__"
GATEWAY_BIN="__GATEWAY_BIN__"
CONFIG_PATH="__CONFIG_PATH__"
ENV_FILE="__ENV_FILE__"
OUTPUT_PATH="__OUTPUT_PATH__"
ADVERTISE_URL="__ADVERTISE_URL__"
MAX_FRAMES="__MAX_FRAMES__"
TIMEOUT_MS="__TIMEOUT_MS__"

echo "phase=physical-asr-fixture-capture"
echo "device=$DEVICE_ID"
echo "capture_listen=127.0.0.1:$SERVICE_PORT"
echo "advertise_url=$ADVERTISE_URL"
echo "output=$OUTPUT_PATH"
echo "max_frames=$MAX_FRAMES"
echo "timeout_ms=$TIMEOUT_MS"
echo "ecs_time=$(date -Is)"

if [ ! -x "$GATEWAY_BIN" ]; then
  echo "gateway binary missing: $GATEWAY_BIN" >&2
  exit 1
fi
if [ ! -f "$CONFIG_PATH" ]; then
  echo "gateway config missing: $CONFIG_PATH" >&2
  exit 1
fi
if [ ! -f "$ENV_FILE" ]; then
  echo "gateway env missing: $ENV_FILE" >&2
  exit 1
fi
mkdir -p "$(dirname "$OUTPUT_PATH")"

restart_needed=0
cleanup() {
  if [ "$restart_needed" -eq 1 ]; then
    systemctl start "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

set -a
. "$ENV_FILE"
set +a

systemctl stop "$SERVICE_NAME"
restart_needed=1

set +e
"$GATEWAY_BIN" asr-fixture-capture \
  --config "$CONFIG_PATH" \
  --listen "127.0.0.1:$SERVICE_PORT" \
  --advertise-url "$ADVERTISE_URL" \
  --output "$OUTPUT_PATH" \
  --max-frames "$MAX_FRAMES" \
  --timeout-ms "$TIMEOUT_MS"
capture_code=$?
set -e

systemctl start "$SERVICE_NAME"
restart_needed=0
sleep 2
systemctl is-active --quiet "$SERVICE_NAME"

if [ "$capture_code" -ne 0 ]; then
  echo "capture_failed=true code=$capture_code" >&2
  exit "$capture_code"
fi

"$GATEWAY_BIN" asr-fixture-validate --fixture "$OUTPUT_PATH"
fixture_sha="$(sha256sum "$OUTPUT_PATH" | awk '{print $1}')"
fixture_size="$(stat -c %s "$OUTPUT_PATH")"
fixture_mtime="$(stat -c %y "$OUTPUT_PATH")"
echo "fixture_path=$OUTPUT_PATH"
echo "fixture_sha256=$fixture_sha"
echo "fixture_size=$fixture_size"
echo "fixture_mtime=$fixture_mtime"
curl -fsS "http://127.0.0.1:$SERVICE_PORT/healthz"
printf '\n'
curl -fsS "http://127.0.0.1:$SERVICE_PORT/readyz"
printf '\n'
REMOTE
}

render_remote_script() {
  remote_script |
    sed \
      -e "s#__DEVICE_ID__#$DEVICE_ID#g" \
      -e "s#__SERVICE_NAME__#$SERVICE_NAME#g" \
      -e "s#__SERVICE_PORT__#$SERVICE_PORT#g" \
      -e "s#__GATEWAY_BIN__#$GATEWAY_BIN#g" \
      -e "s#__CONFIG_PATH__#$CONFIG_PATH#g" \
      -e "s#__ENV_FILE__#$ENV_FILE#g" \
      -e "s#__OUTPUT_PATH__#$OUTPUT_PATH#g" \
      -e "s#__ADVERTISE_URL__#$ADVERTISE_URL#g" \
      -e "s#__MAX_FRAMES__#$MAX_FRAMES#g" \
      -e "s#__TIMEOUT_MS__#$TIMEOUT_MS#g"
}

echo "region=$REGION"
echo "instance=$INSTANCE_ID"
echo "device=$DEVICE_ID"
echo "service=$SERVICE_NAME"
echo "service_port=$SERVICE_PORT"
echo "gateway_bin=$GATEWAY_BIN"
echo "config=$CONFIG_PATH"
echo "env_file=$ENV_FILE"
echo "output=$OUTPUT_PATH"
echo "advertise_url=$ADVERTISE_URL"
echo "max_frames=$MAX_FRAMES"
echo "timeout_ms=$TIMEOUT_MS"
echo "execute=$([ "$EXECUTE" -eq 1 ] && echo true || echo false)"

REMOTE_SCRIPT="$(render_remote_script)"
if [ "$EXECUTE" -eq 0 ]; then
  echo "dry_run=true"
  printf '%s\n' "$REMOTE_SCRIPT" | sed -n '1,180p'
  exit 0
fi

for tool in aliyun jq base64; do
  require_tool "$tool"
done

if [ -n "$PROXY_URL" ]; then
  export HTTPS_PROXY="$PROXY_URL"
  export ALL_PROXY="$PROXY_URL"
fi

payload="$(printf "bash <<'A21AIR_ASR_FIXTURE_CAPTURE'\n%s\nA21AIR_ASR_FIXTURE_CAPTURE\n" "$REMOTE_SCRIPT" | base64 | tr -d '\n')"
run_json="$(aliyun ecs RunCommand --RegionId "$REGION" --Type RunShellScript --InstanceId.1 "$INSTANCE_ID" --CommandContent "$payload" --ContentEncoding Base64 --Timeout 900 --Name "a21-air-asr-fixture-capture")"
invoke_id="$(printf '%s' "$run_json" | jq -r '.InvokeId // .InvocationId // empty')"
if [ -z "$invoke_id" ]; then
  echo "RunCommand returned no invoke id" >&2
  exit 1
fi
echo "invoke=$invoke_id"

for _ in $(seq 1 450); do
  result_json="$(aliyun ecs DescribeInvocationResults --RegionId "$REGION" --InvokeId "$invoke_id" --InstanceId "$INSTANCE_ID" --ContentEncoding Base64 --MaxResults 10)"
  state="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("InvocationStatus")) | .InvocationStatus' | head -1)"
  exit_code="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("ExitCode")) | .ExitCode' | head -1)"
  if [ "$state" = "Success" ] || [ "$state" = "Failed" ] || [ "$state" = "PartialFailed" ] || { [ -n "$exit_code" ] && [ "$exit_code" != "null" ] && [ "$exit_code" != "0" ]; }; then
    output_b64="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("Output")) | .Output' | head -1)"
    if [ -n "$output_b64" ] && [ "$output_b64" != "null" ]; then
      printf '%s' "$output_b64" | base64 -d || true
    fi
    echo "invoke_status=$state exit_code=$exit_code"
    [ "$state" = "Success" ] && [ "${exit_code:-0}" = "0" ]
    exit $?
  fi
  sleep 2
done

echo "invoke timed out: $invoke_id" >&2
exit 1
