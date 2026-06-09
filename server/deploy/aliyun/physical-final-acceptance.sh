#!/usr/bin/env bash
set -euo pipefail

REGION="cn-shanghai"
INSTANCE_ID="i-uf63f4ymqc2dxtljxz2n"
DEVICE_ID="44:1b:f6:e2:74:50"
CLIENT_ID="36d53c70-30e7-41e9-9720-6a5000e40a3c"
FIRMWARE_BUILD_ID="StackChan-UserDemo V1.4.1 + A21 Air 1445 speaking-only wake barge-in"
FIRMWARE_VERSION="V1.4.1"
GATEWAY_BIN="/opt/a21-air/stackchan-gateway/stackchan-gateway"
TRACE_FILE="/var/lib/a21-air/traces/turns.jsonl"
REPORT_DIR="/var/lib/a21-air/acceptance"
PROVIDER_PROFILE="siliconflow-dashscope-voice"
MIN_TURNS="20"
LATEST_TURNS="20"
SINCE=""
GATEWAY_COMMIT=""
LED_RETEST_REPORT=""
RECONNECT_REPORT=""
NOTES="operator confirmed 1445 speaking-only wake barge-in physical acceptance"
PROXY_URL="${HTTPS_PROXY:-${ALL_PROXY:-}}"
EXECUTE=0

AUDIO_PLAYBACK_OK=0
SCREEN_TEXT_OK=0
HEAD_CONTROL_OK=0
LED_LIFECYCLE_OK=0
NO_UNEXPECTED_CAMERA_TRIGGER=0
WIFI_RECONNECT_OK=0

usage() {
  cat <<'EOF'
usage: server/deploy/aliyun/physical-final-acceptance.sh [options]

Run the final ECS-side physical acceptance chain after the flashed StackChan
has completed the operator-observed 20-turn acceptance window. Default mode is
dry-run and does not call Aliyun APIs.

Required with --execute:
  --since <rfc3339>                  Acceptance window start timestamp.
  --gateway-commit <sha>             Deployed gateway commit.
  --led-retest-report <ecs-path>     Existing trace-bound LED retest report.
  --reconnect-report <ecs-path>      Existing post-flash reconnect report.
  --audio-playback-ok                Operator confirmed real audio playback.
  --screen-text-ok                   Operator confirmed official screen text.
  --head-control-ok                  Operator confirmed head movement/control.
  --led-lifecycle-ok                 Operator confirmed lifecycle LEDs.
  --no-unexpected-camera-trigger     Operator confirmed no surprise camera.
  --wifi-reconnect-ok                Operator confirmed Wi-Fi reconnect.

Options:
  --region <id>                      Aliyun region. Default: cn-shanghai
  --instance <id>                    ECS instance id.
  --device <id>                      Runtime Xiaozhi Device-Id/MAC.
  --client-id <id>                   Runtime Xiaozhi Client-Id/UUID.
  --firmware-build-id <text>         Firmware build id for report.
  --firmware-version <text>          Firmware version for report.
  --gateway-bin <path>               Installed gateway binary path.
  --trace-file <path>                ECS trace JSONL path.
  --report-dir <path>                ECS report directory.
  --provider-profile <id>            Provider profile used for the run.
  --min-turns <n>                    Required completed turns. Default: 20
  --latest-turns <n>                 Latest completed-turn window. Default: 20
  --notes <safe text>                Short safe notes, no transcript/secrets.
  --proxy <url>                      Proxy for aliyun CLI.
  --execute                          Actually run Cloud Assistant.
  -h, --help                         Show this help.

The remote command prints no tokens and does not call private admin APIs. It:
  1. derives metrics with physical-acceptance-metrics,
  2. requires the reconnect report schema/device/pass state to match this run,
  3. generates stackchan_physical_acceptance_v2,
  4. validates it with the acceptance subcommand.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --region) REGION="${2:?missing --region value}"; shift 2 ;;
    --instance) INSTANCE_ID="${2:?missing --instance value}"; shift 2 ;;
    --device) DEVICE_ID="${2:?missing --device value}"; shift 2 ;;
    --client-id) CLIENT_ID="${2:?missing --client-id value}"; shift 2 ;;
    --firmware-build-id) FIRMWARE_BUILD_ID="${2:?missing --firmware-build-id value}"; shift 2 ;;
    --firmware-version) FIRMWARE_VERSION="${2:?missing --firmware-version value}"; shift 2 ;;
    --gateway-bin) GATEWAY_BIN="${2:?missing --gateway-bin value}"; shift 2 ;;
    --trace-file) TRACE_FILE="${2:?missing --trace-file value}"; shift 2 ;;
    --report-dir) REPORT_DIR="${2:?missing --report-dir value}"; shift 2 ;;
    --provider-profile) PROVIDER_PROFILE="${2:?missing --provider-profile value}"; shift 2 ;;
    --min-turns) MIN_TURNS="${2:?missing --min-turns value}"; shift 2 ;;
    --latest-turns) LATEST_TURNS="${2:?missing --latest-turns value}"; shift 2 ;;
    --since) SINCE="${2:?missing --since value}"; shift 2 ;;
    --gateway-commit) GATEWAY_COMMIT="${2:?missing --gateway-commit value}"; shift 2 ;;
    --led-retest-report) LED_RETEST_REPORT="${2:?missing --led-retest-report value}"; shift 2 ;;
    --reconnect-report) RECONNECT_REPORT="${2:?missing --reconnect-report value}"; shift 2 ;;
    --notes) NOTES="${2:?missing --notes value}"; shift 2 ;;
    --proxy) PROXY_URL="${2:?missing --proxy value}"; shift 2 ;;
    --audio-playback-ok) AUDIO_PLAYBACK_OK=1; shift ;;
    --screen-text-ok) SCREEN_TEXT_OK=1; shift ;;
    --head-control-ok) HEAD_CONTROL_OK=1; shift ;;
    --led-lifecycle-ok) LED_LIFECYCLE_OK=1; shift ;;
    --no-unexpected-camera-trigger) NO_UNEXPECTED_CAMERA_TRIGGER=1; shift ;;
    --wifi-reconnect-ok) WIFI_RECONNECT_OK=1; shift ;;
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

shell_quote() {
  printf '%q' "$1"
}

require_execute_value() {
  local name="$1"
  local value="$2"
  if [ "$EXECUTE" -eq 1 ] && [ -z "$value" ]; then
    echo "$name is required with --execute" >&2
    exit 2
  fi
}

require_execute_flag() {
  local name="$1"
  local value="$2"
  if [ "$EXECUTE" -eq 1 ] && [ "$value" -ne 1 ]; then
    echo "$name is required with --execute" >&2
    exit 2
  fi
}

if ! is_positive_integer "$MIN_TURNS"; then
  echo "--min-turns must be a positive integer" >&2
  exit 2
fi
if ! is_positive_integer "$LATEST_TURNS"; then
  echo "--latest-turns must be a positive integer" >&2
  exit 2
fi
if [ "$LATEST_TURNS" -lt "$MIN_TURNS" ]; then
  echo "--latest-turns must be >= --min-turns" >&2
  exit 2
fi

require_execute_value "--since" "$SINCE"
require_execute_value "--gateway-commit" "$GATEWAY_COMMIT"
require_execute_value "--led-retest-report" "$LED_RETEST_REPORT"
require_execute_value "--reconnect-report" "$RECONNECT_REPORT"
require_execute_flag "--audio-playback-ok" "$AUDIO_PLAYBACK_OK"
require_execute_flag "--screen-text-ok" "$SCREEN_TEXT_OK"
require_execute_flag "--head-control-ok" "$HEAD_CONTROL_OK"
require_execute_flag "--led-lifecycle-ok" "$LED_LIFECYCLE_OK"
require_execute_flag "--no-unexpected-camera-trigger" "$NO_UNEXPECTED_CAMERA_TRIGGER"
require_execute_flag "--wifi-reconnect-ok" "$WIFI_RECONNECT_OK"

remote_script() {
  cat <<REMOTE_VARS
set -euo pipefail
DEVICE_ID=$(shell_quote "$DEVICE_ID")
CLIENT_ID=$(shell_quote "$CLIENT_ID")
FIRMWARE_BUILD_ID=$(shell_quote "$FIRMWARE_BUILD_ID")
FIRMWARE_VERSION=$(shell_quote "$FIRMWARE_VERSION")
GATEWAY_BIN=$(shell_quote "$GATEWAY_BIN")
TRACE_FILE=$(shell_quote "$TRACE_FILE")
REPORT_DIR=$(shell_quote "$REPORT_DIR")
PROVIDER_PROFILE=$(shell_quote "$PROVIDER_PROFILE")
MIN_TURNS=$(shell_quote "$MIN_TURNS")
LATEST_TURNS=$(shell_quote "$LATEST_TURNS")
SINCE=$(shell_quote "$SINCE")
GATEWAY_COMMIT=$(shell_quote "$GATEWAY_COMMIT")
LED_RETEST_REPORT=$(shell_quote "$LED_RETEST_REPORT")
RECONNECT_REPORT=$(shell_quote "$RECONNECT_REPORT")
NOTES=$(shell_quote "$NOTES")
REMOTE_VARS

  cat <<'REMOTE_BODY'
run_id=$(date -u +%Y%m%dT%H%M%SZ)
metrics_report="$REPORT_DIR/a21-air-final-metrics-$run_id.json"
acceptance_report="$REPORT_DIR/a21-air-final-physical-acceptance-$run_id.json"

echo "phase=final-physical-acceptance"
echo "device=$DEVICE_ID"
echo "since=$SINCE"
echo "metrics_report=$metrics_report"
echo "acceptance_report=$acceptance_report"
if [ ! -x "$GATEWAY_BIN" ]; then
  echo "gateway binary missing or not executable: $GATEWAY_BIN" >&2
  exit 1
fi
command -v jq >/dev/null 2>&1 || {
  echo "remote jq missing" >&2
  exit 1
}
mkdir -p "$REPORT_DIR"

if [ ! -f "$RECONNECT_REPORT" ]; then
  echo "reconnect report not found: $RECONNECT_REPORT" >&2
  exit 1
fi
reconnect_schema="$(jq -r '.schema_version // ""' "$RECONNECT_REPORT")"
if [ "$reconnect_schema" != "a21_air_physical_reconnect_retest_v1" ]; then
  echo "reconnect report schema mismatch: $RECONNECT_REPORT" >&2
  jq -c '{schema_version,device_id,restart_start,device_hello_after_restart_ok,listen_start_after_restart_ok,gateway_restart_reconnect_ok}' "$RECONNECT_REPORT" || true
  exit 1
fi
reconnect_device_id="$(jq -r '.device_id // ""' "$RECONNECT_REPORT")"
if [ "$reconnect_device_id" != "$DEVICE_ID" ]; then
  echo "reconnect report device mismatch: $RECONNECT_REPORT" >&2
  jq -c '{schema_version,device_id,restart_start,device_hello_after_restart_ok,listen_start_after_restart_ok,gateway_restart_reconnect_ok}' "$RECONNECT_REPORT" || true
  exit 1
fi
if [ "$(jq -r '.device_hello_after_restart_ok // false' "$RECONNECT_REPORT")" != "true" ]; then
  echo "reconnect report has no post-restart device hello: $RECONNECT_REPORT" >&2
  jq -c '{schema_version,device_id,restart_start,device_hello_after_restart_ok,listen_start_after_restart_ok,gateway_restart_reconnect_ok}' "$RECONNECT_REPORT" || true
  exit 1
fi
if [ "$(jq -r '.gateway_restart_reconnect_ok // false' "$RECONNECT_REPORT")" != "true" ]; then
  echo "reconnect report is not passing: $RECONNECT_REPORT" >&2
  jq -c '{schema_version,device_id,restart_start,device_hello_after_restart_ok,listen_start_after_restart_ok,gateway_restart_reconnect_ok}' "$RECONNECT_REPORT" || true
  exit 1
fi

"$GATEWAY_BIN" physical-acceptance-metrics \
  --trace-file "$TRACE_FILE" \
  --device "$DEVICE_ID" \
  --min-turns "$MIN_TURNS" \
  --since "$SINCE" \
  --latest-turns "$LATEST_TURNS" \
  > "$metrics_report"

"$GATEWAY_BIN" physical-acceptance-report \
  --report "$acceptance_report" \
  --metrics-file "$metrics_report" \
  --device "$DEVICE_ID" \
  --hardware-device-id "$DEVICE_ID" \
  --client-id "$CLIENT_ID" \
  --firmware-build-id "$FIRMWARE_BUILD_ID" \
  --firmware-version "$FIRMWARE_VERSION" \
  --gateway-commit "$GATEWAY_COMMIT" \
  --provider-profile "$PROVIDER_PROFILE" \
  --audio-playback-ok \
  --screen-text-ok \
  --head-control-ok \
  --led-lifecycle-ok \
  --led-retest-report "$LED_RETEST_REPORT" \
  --no-unexpected-camera-trigger \
  --wifi-reconnect-ok \
  --gateway-restart-reconnect-ok \
  --notes "$NOTES"

"$GATEWAY_BIN" acceptance \
  --report "$acceptance_report" \
  --device "$DEVICE_ID" \
  --turns "$MIN_TURNS"

echo "final_acceptance_ok=true"
jq -c '{schema_version,device_id,firmware_build_id,gateway_commit,provider_profile,completed_turns,p50_first_audible_ms,p95_first_audible_ms,barge_in_stop_latency_ms,body_mcp_tool_success_rate,llm_request_turns,llm_recent_context_turns,max_recent_turn_count,continuity_context_ok,continuity_basis,tts_audio_quality:{event_count:.tts_audio_quality.event_count,sample_count:.tts_audio_quality.sample_count,duration_ms:.tts_audio_quality.duration_ms,peak_dbfs_max:.tts_audio_quality.peak_dbfs_max,rms_dbfs_p50:.tts_audio_quality.rms_dbfs_p50,rms_dbfs_p95:.tts_audio_quality.rms_dbfs_p95,clipped_percent_max:.tts_audio_quality.clipped_percent_max,silence_percent_max:.tts_audio_quality.silence_percent_max,dc_offset_max_abs:.tts_audio_quality.dc_offset_max_abs},audio_playback_ok,screen_text_ok,head_control_ok,led_lifecycle_ok,camera_tool_call_count,unexpected_camera_triggered,wifi_reconnect_ok,gateway_restart_reconnect_ok}' "$acceptance_report"
REMOTE_BODY
}

echo "region=$REGION"
echo "instance=$INSTANCE_ID"
echo "device=$DEVICE_ID"
echo "client_id=$CLIENT_ID"
echo "gateway_bin=$GATEWAY_BIN"
echo "trace_file=$TRACE_FILE"
echo "report_dir=$REPORT_DIR"
echo "provider_profile=$PROVIDER_PROFILE"
echo "min_turns=$MIN_TURNS"
echo "latest_turns=$LATEST_TURNS"
echo "since=${SINCE:-<required>}"
echo "gateway_commit=${GATEWAY_COMMIT:-<required>}"
echo "led_retest_report=${LED_RETEST_REPORT:-<required>}"
echo "reconnect_report=${RECONNECT_REPORT:-<required>}"
echo "operator_flags_audio=$AUDIO_PLAYBACK_OK screen=$SCREEN_TEXT_OK head=$HEAD_CONTROL_OK led=$LED_LIFECYCLE_OK camera_clear=$NO_UNEXPECTED_CAMERA_TRIGGER wifi=$WIFI_RECONNECT_OK"
echo "execute=$([ "$EXECUTE" -eq 1 ] && echo true || echo false)"

REMOTE_SCRIPT="$(remote_script)"
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

payload="$(printf "bash <<'A21AIR_FINAL_ACCEPTANCE'\n%s\nA21AIR_FINAL_ACCEPTANCE\n" "$REMOTE_SCRIPT" | base64 | tr -d '\n')"
run_json="$(aliyun ecs RunCommand --RegionId "$REGION" --Type RunShellScript --InstanceId.1 "$INSTANCE_ID" --CommandContent "$payload" --ContentEncoding Base64 --Timeout 600 --Name "a21-air-final-physical-acceptance")"
invoke_id="$(printf '%s' "$run_json" | jq -r '.InvokeId // .InvocationId // empty')"
if [ -z "$invoke_id" ]; then
  echo "RunCommand returned no invoke id" >&2
  exit 1
fi
echo "invoke=$invoke_id"

for _ in $(seq 1 300); do
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
