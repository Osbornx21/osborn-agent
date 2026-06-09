#!/usr/bin/env bash
set -euo pipefail

REGION="cn-shanghai"
INSTANCE_ID="i-uf63f4ymqc2dxtljxz2n"
DEVICE_ID="44:1b:f6:e2:74:50"
SERVICE_NAME="stackchan-gateway"
SERVICE_PORT="21082"
GATEWAY_BIN="/opt/a21-air/stackchan-gateway/stackchan-gateway"
TRACE_FILE="/var/lib/a21-air/traces/turns.jsonl"
REPORT_DIR="/var/lib/a21-air/acceptance"
WAIT_SECONDS="30"
PROXY_URL="${HTTPS_PROXY:-${ALL_PROXY:-}}"
EXECUTE=0

usage() {
  cat <<'EOF'
usage: server/deploy/aliyun/physical-postflash-retest.sh [options]

Run the fixed ECS-side reconnect retest after the A21 Air StackChan firmware
overlay has been flashed and the device has rebooted. Default mode is dry-run
and does not call Aliyun APIs.

Options:
  --region <id>            Aliyun region. Default: cn-shanghai
  --instance <id>          ECS instance id.
  --device <id>            Runtime Xiaozhi Device-Id/MAC.
  --service <name>         systemd service. Default: stackchan-gateway
  --service-port <port>    ECS-local gateway port. Default: 21082
  --gateway-bin <path>     Installed gateway binary path.
  --trace-file <path>      ECS trace JSONL path.
  --report-dir <path>      ECS report directory.
  --wait-seconds <n>       Wait after gateway restart before retest. Default: 30
  --proxy <url>            Proxy for aliyun CLI.
  --execute                Actually run Cloud Assistant and restart gateway.
  -h, --help               Show this help.

The remote command prints no tokens and does not call private admin APIs. It:
  1. records a restart-start timestamp,
  2. restarts stackchan-gateway,
  3. verifies ECS-local /healthz and /readyz,
  4. waits for the flashed device to open a Xiaozhi WebSocket,
  5. runs physical-reconnect-retest into REPORT_DIR.
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
    --trace-file) TRACE_FILE="${2:?missing --trace-file value}"; shift 2 ;;
    --report-dir) REPORT_DIR="${2:?missing --report-dir value}"; shift 2 ;;
    --wait-seconds) WAIT_SECONDS="${2:?missing --wait-seconds value}"; shift 2 ;;
    --proxy) PROXY_URL="${2:?missing --proxy value}"; shift 2 ;;
    --execute) EXECUTE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if ! [[ "$WAIT_SECONDS" =~ ^[0-9]+$ ]] || [ "$WAIT_SECONDS" -lt 5 ] || [ "$WAIT_SECONDS" -gt 300 ]; then
  echo "--wait-seconds must be an integer between 5 and 300" >&2
  exit 2
fi

require_tool() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 2
  }
}

remote_script() {
  cat <<'REMOTE'
set -euo pipefail
DEVICE_ID="__DEVICE_ID__"
SERVICE_NAME="__SERVICE_NAME__"
SERVICE_PORT="__SERVICE_PORT__"
GATEWAY_BIN="__GATEWAY_BIN__"
TRACE_FILE="__TRACE_FILE__"
REPORT_DIR="__REPORT_DIR__"
WAIT_SECONDS="__WAIT_SECONDS__"

run_id=$(date -u +%Y%m%dT%H%M%SZ)
report="$REPORT_DIR/a21-air-postflash-reconnect-$run_id.json"

echo "phase=postflash-reconnect"
echo "device=$DEVICE_ID"
echo "wait_seconds=$WAIT_SECONDS"
if [ ! -x "$GATEWAY_BIN" ]; then
  echo "gateway binary missing or not executable: $GATEWAY_BIN" >&2
  exit 1
fi
command -v jq >/dev/null 2>&1 || {
  echo "remote jq missing" >&2
  exit 1
}
mkdir -p "$REPORT_DIR"
restart_start=$(date -Is)
echo "restart_start=$restart_start"

systemctl restart "$SERVICE_NAME"
sleep 2
systemctl is-active --quiet "$SERVICE_NAME"
curl -fsS "http://127.0.0.1:$SERVICE_PORT/healthz"
printf '\n'
curl -fsS "http://127.0.0.1:$SERVICE_PORT/readyz"
printf '\n'

sleep "$WAIT_SECONDS"

set +e
"$GATEWAY_BIN" physical-reconnect-retest \
  --trace-file "$TRACE_FILE" \
  --device "$DEVICE_ID" \
  --restart-start "$restart_start" \
  --report "$report"
code=$?
set -e

echo "report_path=$report"
if [ -f "$report" ]; then
  jq -c '{schema_version,device_id,restart_start,device_hello_after_restart_ok,listen_start_after_restart_ok,gateway_restart_reconnect_ok,hello_event,listen_event_after_restart}' "$report"
fi
exit "$code"
REMOTE
}

render_remote_script() {
  remote_script |
    sed \
      -e "s#__DEVICE_ID__#$DEVICE_ID#g" \
      -e "s#__SERVICE_NAME__#$SERVICE_NAME#g" \
      -e "s#__SERVICE_PORT__#$SERVICE_PORT#g" \
      -e "s#__GATEWAY_BIN__#$GATEWAY_BIN#g" \
      -e "s#__TRACE_FILE__#$TRACE_FILE#g" \
      -e "s#__REPORT_DIR__#$REPORT_DIR#g" \
      -e "s#__WAIT_SECONDS__#$WAIT_SECONDS#g"
}

echo "region=$REGION"
echo "instance=$INSTANCE_ID"
echo "device=$DEVICE_ID"
echo "service=$SERVICE_NAME"
echo "service_port=$SERVICE_PORT"
echo "gateway_bin=$GATEWAY_BIN"
echo "trace_file=$TRACE_FILE"
echo "report_dir=$REPORT_DIR"
echo "wait_seconds=$WAIT_SECONDS"
echo "execute=$([ "$EXECUTE" -eq 1 ] && echo true || echo false)"

REMOTE_SCRIPT="$(render_remote_script)"
if [ "$EXECUTE" -eq 0 ]; then
  echo "dry_run=true"
  printf '%s\n' "$REMOTE_SCRIPT" | sed -n '1,120p'
  exit 0
fi

for tool in aliyun jq base64; do
  require_tool "$tool"
done

if [ -n "$PROXY_URL" ]; then
  export HTTPS_PROXY="$PROXY_URL"
  export ALL_PROXY="$PROXY_URL"
fi

payload="$(printf "bash <<'A21AIR_POSTFLASH_RETEST'\n%s\nA21AIR_POSTFLASH_RETEST\n" "$REMOTE_SCRIPT" | base64 | tr -d '\n')"
run_json="$(aliyun ecs RunCommand --RegionId "$REGION" --Type RunShellScript --InstanceId.1 "$INSTANCE_ID" --CommandContent "$payload" --ContentEncoding Base64 --Timeout 420 --Name "a21-air-postflash-reconnect")"
invoke_id="$(printf '%s' "$run_json" | jq -r '.InvokeId // .InvocationId // empty')"
if [ -z "$invoke_id" ]; then
  echo "RunCommand returned no invoke id" >&2
  exit 1
fi
echo "invoke=$invoke_id"

for _ in $(seq 1 210); do
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
