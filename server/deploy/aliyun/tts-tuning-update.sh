#!/usr/bin/env bash
set -euo pipefail

REGION="cn-shanghai"
INSTANCE_ID="i-uf63f4ymqc2dxtljxz2n"
CONFIG_PATH="/etc/a21-air/stackchan-gateway.yaml"
ENV_FILE="/etc/a21-air/gateway.env"
SERVICE_NAME="stackchan-gateway"
SERVICE_PORT="21082"
PROXY_URL="${HTTPS_PROXY:-${ALL_PROXY:-}}"
VOLUME=""
RATE=""
PITCH=""
VOICE=""
MODEL=""
OPUS_BITRATE_BPS=""
OPUS_COMPLEXITY=""
STATUS_ONLY=0
EXECUTE=0

usage() {
  cat <<'EOF'
usage: server/deploy/aliyun/tts-tuning-update.sh [options]

Safely inspect or update non-secret DashScope TTS tuning values on the A21 Air
Aliyun ECS runtime. By default this is a dry-run and does not call Aliyun APIs.

Options:
  --region <id>          Aliyun region. Default: cn-shanghai
  --instance <id>        ECS instance id.
  --config <path>        Gateway config path on ECS.
  --env-file <path>      Private env file on ECS.
  --service <name>       systemd service name. Default: stackchan-gateway
  --service-port <port>  ECS-local gateway port. Default: 21082
  --proxy <url>          Proxy for aliyun CLI.
  --status               Inspect current tuning only; no env writes or restart.
  --volume <0-100>       Set DASHSCOPE_TTS_VOLUME.
  --rate <0.5-2.0>       Set DASHSCOPE_TTS_RATE.
  --pitch <0.5-2.0>      Set DASHSCOPE_TTS_PITCH.
  --voice <id>           Set DASHSCOPE_TTS_VOICE.
  --model <id>           Set DASHSCOPE_TTS_MODEL.
  --opus-bitrate-bps <24000-96000>
                         Set A21_OPUS_DOWNLINK_BITRATE_BPS.
  --opus-complexity <1-10>
                         Set A21_OPUS_DOWNLINK_COMPLEXITY.
  --execute              Actually run Cloud Assistant. Without this, dry-run.
  -h, --help             Show this help.

The command prints only safe provider/model/voice/tuning values and never prints
provider tokens or device auth tokens.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --region) REGION="${2:?missing --region value}"; shift 2 ;;
    --instance) INSTANCE_ID="${2:?missing --instance value}"; shift 2 ;;
    --config) CONFIG_PATH="${2:?missing --config value}"; shift 2 ;;
    --env-file) ENV_FILE="${2:?missing --env-file value}"; shift 2 ;;
    --service) SERVICE_NAME="${2:?missing --service value}"; shift 2 ;;
    --service-port) SERVICE_PORT="${2:?missing --service-port value}"; shift 2 ;;
    --proxy) PROXY_URL="${2:?missing --proxy value}"; shift 2 ;;
    --status) STATUS_ONLY=1; shift ;;
    --volume) VOLUME="${2:?missing --volume value}"; shift 2 ;;
    --rate) RATE="${2:?missing --rate value}"; shift 2 ;;
    --pitch) PITCH="${2:?missing --pitch value}"; shift 2 ;;
    --voice) VOICE="${2:?missing --voice value}"; shift 2 ;;
    --model) MODEL="${2:?missing --model value}"; shift 2 ;;
    --opus-bitrate-bps) OPUS_BITRATE_BPS="${2:?missing --opus-bitrate-bps value}"; shift 2 ;;
    --opus-complexity) OPUS_COMPLEXITY="${2:?missing --opus-complexity value}"; shift 2 ;;
    --execute) EXECUTE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

validate_int_range() {
  local name="$1" value="$2" min="$3" max="$4"
  [ -z "$value" ] && return 0
  if ! [[ "$value" =~ ^[0-9]+$ ]]; then
    echo "$name must be an integer" >&2
    exit 2
  fi
  if [ "$value" -lt "$min" ] || [ "$value" -gt "$max" ]; then
    echo "$name must be between $min and $max" >&2
    exit 2
  fi
}

validate_float_range() {
  local name="$1" value="$2" min="$3" max="$4"
  [ -z "$value" ] && return 0
  python3 - "$name" "$value" "$min" "$max" <<'PY'
import sys
name, raw, low, high = sys.argv[1], sys.argv[2], float(sys.argv[3]), float(sys.argv[4])
try:
    value = float(raw)
except ValueError:
    print(f"{name} must be a number", file=sys.stderr)
    raise SystemExit(2)
if value < low or value > high:
    print(f"{name} must be between {low:g} and {high:g}", file=sys.stderr)
    raise SystemExit(2)
PY
}

validate_token() {
  local name="$1" value="$2"
  [ -z "$value" ] && return 0
  if [ "${#value}" -gt 128 ] || ! [[ "$value" =~ ^[A-Za-z0-9._:/+-]+$ ]]; then
    echo "$name contains unsupported characters" >&2
    exit 2
  fi
}

validate_int_range "--volume" "$VOLUME" 0 100
validate_float_range "--rate" "$RATE" 0.5 2.0
validate_float_range "--pitch" "$PITCH" 0.5 2.0
validate_token "--voice" "$VOICE"
validate_token "--model" "$MODEL"
validate_int_range "--opus-bitrate-bps" "$OPUS_BITRATE_BPS" 24000 96000
validate_int_range "--opus-complexity" "$OPUS_COMPLEXITY" 1 10

if [ "$STATUS_ONLY" -eq 1 ] && { [ -n "$VOLUME" ] || [ -n "$RATE" ] || [ -n "$PITCH" ] || [ -n "$VOICE" ] || [ -n "$MODEL" ] || [ -n "$OPUS_BITRATE_BPS" ] || [ -n "$OPUS_COMPLEXITY" ]; }; then
  echo "--status cannot be combined with tuning updates" >&2
  exit 2
fi

if [ "$STATUS_ONLY" -eq 0 ] && [ -z "$VOLUME$RATE$PITCH$VOICE$MODEL$OPUS_BITRATE_BPS$OPUS_COMPLEXITY" ]; then
  STATUS_ONLY=1
fi

echo "region=$REGION"
echo "instance=$INSTANCE_ID"
echo "config=$CONFIG_PATH"
echo "env_file=$ENV_FILE"
echo "service=$SERVICE_NAME"
echo "status_only=$([ "$STATUS_ONLY" -eq 1 ] && echo true || echo false)"
echo "execute=$([ "$EXECUTE" -eq 1 ] && echo true || echo false)"
echo "requested_tuning={volume:${VOLUME:-unchanged},rate:${RATE:-unchanged},pitch:${PITCH:-unchanged},voice:${VOICE:-unchanged},model:${MODEL:-unchanged},opus_bitrate_bps:${OPUS_BITRATE_BPS:-unchanged},opus_complexity:${OPUS_COMPLEXITY:-unchanged}}"
echo "proxy_configured=$([ -n "$PROXY_URL" ] && echo true || echo false)"

if [ "$EXECUTE" -eq 0 ]; then
  echo "dry_run=true"
  exit 0
fi

for tool in aliyun jq base64 python3; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "missing required tool: $tool" >&2
    exit 2
  }
done

if [ -n "$PROXY_URL" ]; then
  export HTTPS_PROXY="$PROXY_URL"
  export ALL_PROXY="$PROXY_URL"
fi

sanitize_aliyun_stderr() {
  sed -E \
    -e 's/(AccessKeyId=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(Signature=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(Content=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(SecurityToken=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(SignatureNonce=)[^&" ]+/\1[REDACTED]/g'
}

remote_script="$(cat <<EOF
set -euo pipefail
CFG="$CONFIG_PATH"
ENV_FILE="$ENV_FILE"
SERVICE="$SERVICE_NAME"
PORT="$SERVICE_PORT"
STATUS_ONLY="$STATUS_ONLY"
VOLUME="$VOLUME"
RATE="$RATE"
PITCH="$PITCH"
VOICE="$VOICE"
MODEL="$MODEL"
OPUS_BITRATE_BPS="$OPUS_BITRATE_BPS"
OPUS_COMPLEXITY="$OPUS_COMPLEXITY"

if [ ! -f "\$ENV_FILE" ]; then
  echo "env file missing: \$ENV_FILE" >&2
  exit 1
fi

if [ "\$STATUS_ONLY" != "1" ]; then
  backup="\${ENV_FILE}.backup-\$(date +%Y%m%d-%H%M%S)-tts-tuning"
  cp -a "\$ENV_FILE" "\$backup"
  export ENV_FILE VOLUME RATE PITCH VOICE MODEL OPUS_BITRATE_BPS OPUS_COMPLEXITY
  python3 - <<'PY'
from pathlib import Path
import os

env_path = Path(os.environ["ENV_FILE"])
updates = {
    "DASHSCOPE_TTS_VOLUME": os.environ.get("VOLUME", "").strip(),
    "DASHSCOPE_TTS_RATE": os.environ.get("RATE", "").strip(),
    "DASHSCOPE_TTS_PITCH": os.environ.get("PITCH", "").strip(),
    "DASHSCOPE_TTS_VOICE": os.environ.get("VOICE", "").strip(),
    "DASHSCOPE_TTS_MODEL": os.environ.get("MODEL", "").strip(),
    "A21_OPUS_DOWNLINK_BITRATE_BPS": os.environ.get("OPUS_BITRATE_BPS", "").strip(),
    "A21_OPUS_DOWNLINK_COMPLEXITY": os.environ.get("OPUS_COMPLEXITY", "").strip(),
}
updates = {k: v for k, v in updates.items() if v}
lines = env_path.read_text().splitlines()
seen = set()
out = []
for line in lines:
    stripped = line.strip()
    if not stripped or stripped.startswith("#") or "=" not in line:
        out.append(line)
        continue
    key = line.split("=", 1)[0].strip()
    if key in updates:
        out.append(f"{key}={updates[key]}")
        seen.add(key)
    else:
        out.append(line)
for key, value in updates.items():
    if key not in seen:
        out.append(f"{key}={value}")
env_path.write_text("\\n".join(out) + "\\n")
PY
  chmod 600 "\$ENV_FILE"
  echo "backup=\$backup"
fi

set -a
. "\$ENV_FILE"
set +a
/opt/a21-air/stackchan-gateway/stackchan-gateway voice-profile-check --config "\$CFG"
if [ "\$STATUS_ONLY" != "1" ]; then
  systemctl restart "\$SERVICE"
  sleep 2
fi
systemctl is-active --quiet "\$SERVICE"
curl -fsS "http://127.0.0.1:\${PORT}/healthz"
curl -fsS "http://127.0.0.1:\${PORT}/readyz"
python3 - <<'PY'
import os, json
keys = [
    "DASHSCOPE_TTS_MODEL",
    "DASHSCOPE_TTS_VOICE",
    "DASHSCOPE_TTS_VOLUME",
    "DASHSCOPE_TTS_RATE",
    "DASHSCOPE_TTS_PITCH",
    "A21_OPUS_DOWNLINK_BITRATE_BPS",
    "A21_OPUS_DOWNLINK_COMPLEXITY",
]
print(json.dumps({k: (os.environ.get(k) or "<default>") for k in keys}, indent=2, ensure_ascii=False))
PY
EOF
)"

payload="$(printf "bash <<'A21AIR_TTS_TUNING'\n%s\nA21AIR_TTS_TUNING\n" "$remote_script" | base64 | tr -d '\n')"
run_json="$(aliyun ecs RunCommand --RegionId "$REGION" --Type RunShellScript --InstanceId.1 "$INSTANCE_ID" --CommandContent "$payload" --ContentEncoding Base64 --Timeout 180 --Name "a21-air-tts-tuning" 2> >(sanitize_aliyun_stderr >&2))"
invoke_id="$(printf '%s' "$run_json" | jq -r '.InvokeId // .InvocationId // empty')"
if [ -z "$invoke_id" ]; then
  echo "RunCommand returned no invoke id" >&2
  exit 1
fi
echo "invoke=$invoke_id"

for _ in $(seq 1 90); do
  result_json="$(aliyun ecs DescribeInvocationResults --RegionId "$REGION" --InvokeId "$invoke_id" --InstanceId "$INSTANCE_ID" --ContentEncoding Base64 --MaxResults 10 2> >(sanitize_aliyun_stderr >&2))"
  state="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("InvocationStatus")) | .InvocationStatus' | head -1)"
  code="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("ExitCode")) | .ExitCode' | head -1)"
  if [ "$state" = "Success" ]; then
    output_b64="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("Output")) | .Output' | head -1)"
    if [ -n "$output_b64" ] && [ "$output_b64" != "null" ]; then
      printf '%s' "$output_b64" | base64 -d || true
    fi
    echo "invoke_status=$state exit_code=$code"
    exit 0
  fi
  if [ "$state" = "Failed" ] || [ "$state" = "PartialFailed" ] || { [ -n "$code" ] && [ "$code" != "null" ] && [ "$code" != "0" ]; }; then
    output_b64="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("Output")) | .Output' | head -1)"
    if [ -n "$output_b64" ] && [ "$output_b64" != "null" ]; then
      printf '%s' "$output_b64" | base64 -d || true
    fi
    echo "invoke failed state=$state code=$code" >&2
    exit 1
  fi
  sleep 2
done

echo "invoke timed out" >&2
exit 1
