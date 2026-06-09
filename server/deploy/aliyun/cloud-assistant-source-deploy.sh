#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

REGION="cn-shanghai"
INSTANCE_ID="i-uf63f4ymqc2dxtljxz2n"
SOURCE_REF="HEAD"
CHUNK_SIZE="20000"
REMOTE_ROOT="/tmp"
INSTALL_PATH="/opt/a21-air/stackchan-gateway/stackchan-gateway"
CONFIG_PATH="/etc/a21-air/stackchan-gateway.yaml"
ENV_FILE="/etc/a21-air/gateway.env"
SERVICE_NAME="stackchan-gateway"
SERVICE_PORT="21082"
PUBLIC_BASE_URL=""
PROXY_URL="${HTTPS_PROXY:-${ALL_PROXY:-}}"
ALLOW_DIRTY=0
DRY_RUN=0

usage() {
  cat <<'EOF'
usage: server/deploy/aliyun/cloud-assistant-source-deploy.sh [options]

Deploy the current A21 Air source snapshot to the Aliyun ECS gateway through
Cloud Assistant SendFile + RunCommand. This is for emergency/source hotfixes
when the fixed runtime package flow is not the right fit.

Options:
  --region <id>              Aliyun region. Default: cn-shanghai
  --instance <id>            ECS instance id.
  --source-ref <ref>         Git ref to archive. Default: HEAD
  --chunk-size <bytes>       Base64 chunk width for SendFile. Default: 20000
  --proxy <url>              Proxy for aliyun CLI, e.g. socks5://127.0.0.1:18080
  --remote-root <path>       Remote temp root. Default: /tmp
  --install-path <path>      Gateway binary path on ECS.
  --config <path>            Runtime gateway config path on ECS.
  --env-file <path>          Private env file sourced before voice-profile-check.
  --service <name>           systemd service name. Default: stackchan-gateway
  --service-port <port>      ECS-local gateway port. Default: 21082
  --public-base-url <url>    Optional public base URL for post-deploy health checks.
  --allow-dirty             Allow archiving a dirty local worktree.
  --dry-run                 Build archive/chunks and print safe summary only.
  -h, --help                Show this help.

Required local tools: git, aliyun, jq, base64, fold, split.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --region) REGION="${2:?missing --region value}"; shift 2 ;;
    --instance) INSTANCE_ID="${2:?missing --instance value}"; shift 2 ;;
    --source-ref) SOURCE_REF="${2:?missing --source-ref value}"; shift 2 ;;
    --chunk-size) CHUNK_SIZE="${2:?missing --chunk-size value}"; shift 2 ;;
    --proxy) PROXY_URL="${2:?missing --proxy value}"; shift 2 ;;
    --remote-root) REMOTE_ROOT="${2:?missing --remote-root value}"; shift 2 ;;
    --install-path) INSTALL_PATH="${2:?missing --install-path value}"; shift 2 ;;
    --config) CONFIG_PATH="${2:?missing --config value}"; shift 2 ;;
    --env-file) ENV_FILE="${2:?missing --env-file value}"; shift 2 ;;
    --service) SERVICE_NAME="${2:?missing --service value}"; shift 2 ;;
    --service-port) SERVICE_PORT="${2:?missing --service-port value}"; shift 2 ;;
    --public-base-url) PUBLIC_BASE_URL="${2:?missing --public-base-url value}"; shift 2 ;;
    --allow-dirty) ALLOW_DIRTY=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

require_tool() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 2
  }
}

for tool in git jq base64 fold split; do
  require_tool "$tool"
done
if [ "$DRY_RUN" -eq 0 ]; then
  require_tool aliyun
fi

if ! [[ "$CHUNK_SIZE" =~ ^[0-9]+$ ]] || [ "$CHUNK_SIZE" -lt 8000 ] || [ "$CHUNK_SIZE" -gt 20000 ]; then
  echo "--chunk-size must be an integer between 8000 and 20000" >&2
  exit 2
fi

cd "$REPO_ROOT"
git rev-parse --verify "$SOURCE_REF" >/dev/null
SOURCE_COMMIT="$(git rev-parse --short=12 "$SOURCE_REF")"

if [ "$ALLOW_DIRTY" -eq 0 ] && [ -n "$(git status --porcelain)" ]; then
  echo "worktree is dirty; commit first or pass --allow-dirty" >&2
  git status --short >&2
  exit 2
fi

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/a21-air-ecs-deploy.XXXXXX")"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

ARCHIVE="$WORK_DIR/source-$SOURCE_COMMIT.tgz"
PART_DIR="$WORK_DIR/parts"
mkdir -p "$PART_DIR"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

base64_file_one_line() {
  if base64 -w 0 /dev/null >/dev/null 2>&1; then
    base64 -w 0 "$1"
  else
    base64 -i "$1" | tr -d '\n'
  fi
}

git archive --format=tar.gz --output "$ARCHIVE" "$SOURCE_REF"
SOURCE_SHA="$(sha256_file "$ARCHIVE")"
ARCHIVE_SIZE="$(wc -c < "$ARCHIVE" | tr -d ' ')"

base64_file_one_line "$ARCHIVE" | fold -w "$CHUNK_SIZE" > "$PART_DIR/parts-lines.txt"
split -l 1 -a 3 "$PART_DIR/parts-lines.txt" "$PART_DIR/part-"
rm -f "$PART_DIR/parts-lines.txt"
PART_COUNT="$(find "$PART_DIR" -type f -name 'part-*' | wc -l | tr -d ' ')"
if [ "$PART_COUNT" -eq 0 ]; then
  echo "archive split produced no parts" >&2
  exit 1
fi

REMOTE_DIR="$REMOTE_ROOT/a21-air-deploy-$SOURCE_COMMIT"

remote_script_template() {
  cat <<'REMOTE_SCRIPT'
set -euo pipefail
COMMIT="__SOURCE_COMMIT__"
EXPECTED_SHA="__SOURCE_SHA__"
EXPECTED_PARTS="__PART_COUNT__"
REMOTE_DIR="__REMOTE_DIR__"
ARCHIVE="__REMOTE_ROOT__/a21-air-source-$COMMIT.tgz"
SRC="__REMOTE_ROOT__/a21-air-source-$COMMIT"
BIN="__REMOTE_ROOT__/stackchan-gateway-$COMMIT"
INSTALL_PATH="__INSTALL_PATH__"
CONFIG_PATH="__CONFIG_PATH__"
ENV_FILE="__ENV_FILE__"
SERVICE_NAME="__SERVICE_NAME__"
SERVICE_PORT="__SERVICE_PORT__"

cd "$REMOTE_DIR"
actual_parts=$(find . -maxdepth 1 -type f -name "source-$COMMIT-part-*.b64" | wc -l | tr -d ' ')
echo "parts=$actual_parts expected=$EXPECTED_PARTS"
[ "$actual_parts" = "$EXPECTED_PARTS" ]
cat source-$COMMIT-part-*.b64 | base64 -d > "$ARCHIVE"
actual_sha=$(sha256sum "$ARCHIVE" | awk '{print $1}')
echo "source_sha=$actual_sha expected=$EXPECTED_SHA"
[ "$actual_sha" = "$EXPECTED_SHA" ]

rm -rf "$SRC"
mkdir -p "$SRC"
tar -xzf "$ARCHIVE" -C "$SRC"
cd "$SRC/server"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOSUMDB="${GOSUMDB:-sum.golang.google.cn}"

go test ./cmd/stackchan-gateway -run 'TestPhysicalAcceptance(Metrics|Report)Command|TestPhysicalLEDRetestCommand|TestPhysicalReconnectRetestCommand|TestECS(Package(Command|Validate)|PreflightDryRunCommand)|TestVoiceProfileCheck' -count=1
go test ./internal/observability -count=1
go test ./internal/session ./internal/stackchan ./internal/app ./internal/simulator -count=1
go build -o "$BIN" ./cmd/stackchan-gateway
ldd "$BIN" | grep -q 'libopus.so.0'

set -a
. "$ENV_FILE"
set +a
"$BIN" voice-profile-check --config "$CONFIG_PATH"
install -o a21air -g a21air -m 0755 "$BIN" "$INSTALL_PATH"
systemctl restart "$SERVICE_NAME"
sleep 2
systemctl is-active --quiet "$SERVICE_NAME"
curl -fsS "http://127.0.0.1:$SERVICE_PORT/healthz"
curl -fsS "http://127.0.0.1:$SERVICE_PORT/readyz"

set +e
led_probe=$("$INSTALL_PATH" physical-led-retest --trace-file /dev/null --visual-green-confirmed 2>&1)
led_code=$?
metrics_probe=$("$INSTALL_PATH" physical-acceptance-metrics 2>&1)
metrics_code=$?
acceptance_report_probe=$("$INSTALL_PATH" physical-acceptance-report 2>&1)
acceptance_report_code=$?
reconnect_probe=$("$INSTALL_PATH" physical-reconnect-retest 2>&1)
reconnect_code=$?
set -e
printf '%s\n' "$led_probe" | grep -q 'trace file has no events'
[ "$led_code" -ne 0 ]
printf '%s\n' "$metrics_probe" | grep -q -- '--trace-file is required'
[ "$metrics_code" -ne 0 ]
printf '%s\n' "$acceptance_report_probe" | grep -q -- '--report is required'
[ "$acceptance_report_code" -ne 0 ]
printf '%s\n' "$reconnect_probe" | grep -q -- '--trace-file is required'
[ "$reconnect_code" -ne 0 ]

stat -c 'binary_mtime=%y binary_size=%s owner=%U:%G' "$INSTALL_PATH"
echo "deploy_ok commit=$COMMIT"
REMOTE_SCRIPT
}

render_remote_script() {
  remote_script_template |
    sed \
      -e "s#__SOURCE_COMMIT__#$SOURCE_COMMIT#g" \
      -e "s#__SOURCE_SHA__#$SOURCE_SHA#g" \
      -e "s#__PART_COUNT__#$PART_COUNT#g" \
      -e "s#__REMOTE_DIR__#$REMOTE_DIR#g" \
      -e "s#__REMOTE_ROOT__#$REMOTE_ROOT#g" \
      -e "s#__INSTALL_PATH__#$INSTALL_PATH#g" \
      -e "s#__CONFIG_PATH__#$CONFIG_PATH#g" \
      -e "s#__ENV_FILE__#$ENV_FILE#g" \
      -e "s#__SERVICE_NAME__#$SERVICE_NAME#g" \
      -e "s#__SERVICE_PORT__#$SERVICE_PORT#g"
}

REMOTE_SCRIPT="$WORK_DIR/remote-deploy.sh"
render_remote_script > "$REMOTE_SCRIPT"
REMOTE_SCRIPT_B64_BYTES="$(base64 < "$REMOTE_SCRIPT" | tr -d '\n' | wc -c | tr -d ' ')"

echo "source_ref=$SOURCE_REF"
echo "source_commit=$SOURCE_COMMIT"
echo "source_sha=$SOURCE_SHA"
echo "archive_bytes=$ARCHIVE_SIZE"
echo "chunk_size=$CHUNK_SIZE"
echo "parts=$PART_COUNT"
echo "remote_dir=$REMOTE_DIR"
echo "remote_script_base64_bytes=$REMOTE_SCRIPT_B64_BYTES"
echo "proxy_configured=$([ -n "$PROXY_URL" ] && echo true || echo false)"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry_run=true"
  exit 0
fi

if [ -n "$PROXY_URL" ]; then
  export HTTPS_PROXY="$PROXY_URL"
  export ALL_PROXY="$PROXY_URL"
fi

aliyun_ecs() {
  local stderr_file status
  stderr_file="$(mktemp "$WORK_DIR/aliyun-stderr.XXXXXX")"
  set +e
  aliyun ecs "$@" 2>"$stderr_file"
  status=$?
  set -e
  if [ "$status" -ne 0 ]; then
    sanitize_aliyun_stderr < "$stderr_file" >&2
    rm -f "$stderr_file"
    return "$status"
  fi
  rm -f "$stderr_file"
  return 0
}

sanitize_aliyun_stderr() {
  sed -E \
    -e 's/(AccessKeyId=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(Signature=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(Content=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(SecurityToken=)[^&" ]+/\1[REDACTED]/g' \
    -e 's/(SignatureNonce=)[^&" ]+/\1[REDACTED]/g'
}

wait_send() {
  local invoke_id="$1"
  local send_state=""
  for _ in $(seq 1 60); do
    send_state="$(aliyun_ecs DescribeSendFileResults --RegionId "$REGION" --InvokeId "$invoke_id" --MaxResults 50 | jq -r '.. | objects | select(has("InvocationStatus")) | .InvocationStatus' | head -1)"
    if [ "$send_state" = "Success" ]; then
      return 0
    fi
    if [ "$send_state" = "Failed" ] || [ "$send_state" = "PartialFailed" ]; then
      echo "sendfile $invoke_id failed with status=$send_state" >&2
      return 1
    fi
    sleep 1
  done
  echo "sendfile $invoke_id timed out status=$send_state" >&2
  return 1
}

wait_invoke() {
  local invoke_id="$1"
  local result_json invoke_state exit_code output_b64
  for _ in $(seq 1 180); do
    result_json="$(aliyun_ecs DescribeInvocationResults --RegionId "$REGION" --InvokeId "$invoke_id" --InstanceId "$INSTANCE_ID" --ContentEncoding Base64 --MaxResults 10)"
    invoke_state="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("InvocationStatus")) | .InvocationStatus' | head -1)"
    exit_code="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("ExitCode")) | .ExitCode' | head -1)"
    if [ "$invoke_state" = "Success" ]; then
      output_b64="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("Output")) | .Output' | head -1)"
      if [ -n "$output_b64" ] && [ "$output_b64" != "null" ]; then
        printf '%s' "$output_b64" | base64 -d || true
      fi
      echo "invoke_status=$invoke_state exit_code=$exit_code"
      return 0
    fi
    if [ "$invoke_state" = "Failed" ] || [ "$invoke_state" = "PartialFailed" ] || { [ -n "$exit_code" ] && [ "$exit_code" != "null" ] && [ "$exit_code" != "0" ]; }; then
      output_b64="$(printf '%s' "$result_json" | jq -r '.. | objects | select(has("Output")) | .Output' | head -1)"
      if [ -n "$output_b64" ] && [ "$output_b64" != "null" ]; then
        printf '%s' "$output_b64" | base64 -d || true
      fi
      echo "invoke $invoke_id failed status=$invoke_state exit_code=$exit_code" >&2
      return 1
    fi
    sleep 2
  done
  echo "invoke $invoke_id timed out status=$invoke_state exit_code=$exit_code" >&2
  return 1
}

part_index=0
send_invokes=()
for part_path in "$PART_DIR"/part-*; do
  part_name="$(printf 'source-%s-part-%03d.b64' "$SOURCE_COMMIT" "$part_index")"
  part_content="$(cat "$part_path")"
  send_json="$(aliyun_ecs SendFile --RegionId "$REGION" --InstanceId.1 "$INSTANCE_ID" --Name "$part_name" --TargetDir "$REMOTE_DIR" --Content "$part_content" --ContentType PlainText --Overwrite true --FileMode 0600 --Timeout 120)"
  send_invoke="$(printf '%s' "$send_json" | jq -r '.InvokeId // .InvocationId // empty')"
  if [ -z "$send_invoke" ]; then
    echo "SendFile returned no invoke id for $part_name" >&2
    exit 1
  fi
  wait_send "$send_invoke"
  send_invokes+=("$send_invoke")
  echo "send_part=$part_index invoke=$send_invoke status=Success"
  part_index=$((part_index + 1))
done

run_payload="$(printf "bash <<'A21AIR_DEPLOY'\n%s\nA21AIR_DEPLOY\n" "$(cat "$REMOTE_SCRIPT")" | base64 | tr -d '\n')"
run_json="$(aliyun_ecs RunCommand --RegionId "$REGION" --Type RunShellScript --InstanceId.1 "$INSTANCE_ID" --CommandContent "$run_payload" --ContentEncoding Base64 --Timeout 900 --Name "a21-air-deploy-$SOURCE_COMMIT")"
run_invoke="$(printf '%s' "$run_json" | jq -r '.InvokeId // .InvocationId // empty')"
if [ -z "$run_invoke" ]; then
  echo "RunCommand returned no invoke id" >&2
  exit 1
fi
echo "run_invoke=$run_invoke"
wait_invoke "$run_invoke"

if [ -n "$PUBLIC_BASE_URL" ]; then
  curl -fsS "$PUBLIC_BASE_URL/healthz"
  curl -fsS "$PUBLIC_BASE_URL/readyz"
fi

echo "send_invokes=${send_invokes[*]}"
