#!/bin/bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARTIFACT_DIR=""
PORT="/dev/cu.usbmodem1101"
BAUD="460800"
DO_FLASH=0
CONFIRM_DEVICE_ID=""
ESPTOOL_PYTHON="${ESPTOOL_PYTHON:-}"
BEFORE_RESET="default_reset"
ALLOW_HISTORICAL_ARTIFACT=0

EXPECTED_DEVICE_ID="44:1b:f6:e2:74:50"
EXPECTED_CLIENT_ID="36d53c70-30e7-41e9-9720-6a5000e40a3c"
EXPECTED_APP_SHA=""
EXPECTED_BOOTLOADER_SHA=""
EXPECTED_PARTITION_SHA=""
EXPECTED_OTA_SHA=""
EXPECTED_ASSETS_SHA=""
CHECKSUM_FILE_NAME="a21air-firmware-checksums.env"

usage() {
  cat <<'EOF'
usage: firmware/stackchan-a21air/flash-a21air-stackchan.sh [options]

Preflight or flash the A21 Air StackChan V1.4.1 "Da Tou" + hot Xiaozhi
WebSocket artifact. Default mode is preflight only and writes nothing.

Required:
  --artifact-dir <path>       Artifact directory to preflight or flash. There
                              is intentionally no default, so rollback or
                              historical builds cannot be selected by accident.

Options:
  --port <path>               Serial port. Default: /dev/cu.usbmodem1101
  --baud <value>              Flash baud. Default: 460800
  --python <path>             Python interpreter with esptool installed.
  --before <mode>             esptool reset mode before connect. Default:
                              default_reset. Use no_reset after manually
                              entering CoreS3 download mode.
  --allow-historical-artifact Allow explicitly selected historical/reverted
                              artifact directories. Never use for routine
                              preflight or flashing.
  --flash                     Execute write_flash after all checks pass.
  --confirm-device-id <id>    Required with --flash; must equal the current
                              A21 Air StackChan Device-Id.
  -h, --help                  Show this help.

This script never erases NVS and never writes the old A21 firmware. It writes
only the checked A21 Air StackChan artifact offsets:
  0x0 bootloader.bin
  0x8000 partition-table.bin
  0xd000 ota_data_initial.bin
  0x20000 stack-chan.bin
  0xa00000 generated_assets.bin
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --artifact-dir) ARTIFACT_DIR="${2:?missing --artifact-dir value}"; shift 2 ;;
    --port) PORT="${2:?missing --port value}"; shift 2 ;;
    --baud) BAUD="${2:?missing --baud value}"; shift 2 ;;
    --python) ESPTOOL_PYTHON="${2:?missing --python value}"; shift 2 ;;
    --before) BEFORE_RESET="${2:?missing --before value}"; shift 2 ;;
    --allow-historical-artifact) ALLOW_HISTORICAL_ARTIFACT=1; shift ;;
    --flash) DO_FLASH=1; shift ;;
    --confirm-device-id) CONFIRM_DEVICE_ID="${2:?missing --confirm-device-id value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

python_has_esptool() {
  local python_dir
  python_dir="$(cd "$(dirname "$1")" 2>/dev/null && pwd)" || return 1
  if [ -x "$python_dir/esptool.py" ]; then
    return 0
  fi
  if command -v perl >/dev/null 2>&1; then
    perl -e 'alarm 3; exec @ARGV' "$1" -c 'import esptool' >/dev/null 2>&1
  else
    "$1" -c 'import esptool' >/dev/null 2>&1
  fi
}

find_esptool_python() {
  if [ -n "$ESPTOOL_PYTHON" ]; then
    if python_has_esptool "$ESPTOOL_PYTHON"; then
      printf '%s\n' "$ESPTOOL_PYTHON"
      return 0
    fi
    echo "configured Python has no esptool: $ESPTOOL_PYTHON" >&2
    return 1
  fi

  local candidate
  local candidates=(
    "$HOME"/.espressif/python_env/idf5.5_py3.14_env/bin/python
    "$HOME"/.espressif/python_env/idf5.5_py3.13_env/bin/python
    "$HOME"/.espressif/python_env/*/bin/python
  )
  for candidate in "${candidates[@]}"; do
    [ -x "$candidate" ] || continue
    if python_has_esptool "$candidate"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  if command -v python3 >/dev/null 2>&1 && python_has_esptool "$(command -v python3)"; then
    command -v python3
    return 0
  fi

  return 1
}

require_file_sha() {
  local path="$1"
  local expected="$2"
  if [ ! -f "$path" ]; then
    echo "missing artifact file: $path" >&2
    exit 1
  fi
  local actual
  actual="$(sha256_file "$path")"
  if [ "$actual" != "$expected" ]; then
    echo "artifact sha mismatch: $path" >&2
    echo "expected=$expected" >&2
    echo "actual=$actual" >&2
    exit 1
  fi
}

read_checksum_var() {
  local file="$1"
  local name="$2"
  local fallback="$3"
  if [ ! -f "$file" ]; then
    printf '%s\n' "$fallback"
    return 0
  fi
  local line
  line="$(grep -E "^${name}=[0-9a-f]{64}$" "$file" || true)"
  if [ -z "$line" ]; then
    echo "checksum file is missing valid $name: $file" >&2
    exit 1
  fi
  printf '%s\n' "${line#*=}"
}

if [ -z "$ARTIFACT_DIR" ]; then
  echo "--artifact-dir is required; no firmware artifact is selected by default" >&2
  usage >&2
  exit 2
fi

guard_historical_artifact() {
  local artifact_base
  artifact_base="$(basename "$ARTIFACT_DIR")"
  case "$artifact_base" in
    stackchan-v1.4.1-da-tou-20260608-0146|\
    stackchan-v1.4.1-da-tou-hotws-20260608-0508|\
    stackchan-v1.4.1-da-tou-hotws-v554-20260608-0835|\
    stackchan-v1.4.1-da-tou-hotws-v554-autostart-assets-20260608-1000|\
    stackchan-v1.4.1-da-tou-hotws-v554-assets-noautostart-20260608-1022|\
    stackchan-v1.4.1-da-tou-hotws-v554-assets-rescue-20260608-1024|\
    stackchan-v1.4.1-da-tou-hotws-v554-assets-calm-motion-20260608-1410|\
    stackchan-v1.4.1-da-tou-hotws-v554-wakefix-calm-motion-20260608-2005|\
    stackchan-v1.4.1-da-tou-hotws-v554-listening-wake-calm-motion-20260608-2115)
      if [ "$ALLOW_HISTORICAL_ARTIFACT" -ne 1 ]; then
        echo "artifact is historical/reverted and blocked by default: $artifact_base" >&2
        echo "pass --allow-historical-artifact only for deliberate rollback/debug after updating control docs" >&2
        exit 2
      fi
      ;;
  esac
}

if [ ! -d "$ARTIFACT_DIR" ]; then
  echo "artifact directory not found: $ARTIFACT_DIR" >&2
  exit 1
fi
guard_historical_artifact

CHECKSUM_FILE="$ARTIFACT_DIR/$CHECKSUM_FILE_NAME"
if [ ! -f "$CHECKSUM_FILE" ]; then
  echo "checksum file is required for selected artifact: $CHECKSUM_FILE" >&2
  exit 1
fi
if ! grep -Fqx 'A21AIR_ARTIFACT_KIND=stackchan-v1.4.1-da-tou-hotws' "$CHECKSUM_FILE"; then
  echo "checksum file has wrong or missing artifact kind: $CHECKSUM_FILE" >&2
  exit 1
fi
EXPECTED_BOOTLOADER_SHA="$(read_checksum_var "$CHECKSUM_FILE" A21AIR_BOOTLOADER_SHA256 "$EXPECTED_BOOTLOADER_SHA")"
EXPECTED_PARTITION_SHA="$(read_checksum_var "$CHECKSUM_FILE" A21AIR_PARTITION_TABLE_SHA256 "$EXPECTED_PARTITION_SHA")"
EXPECTED_OTA_SHA="$(read_checksum_var "$CHECKSUM_FILE" A21AIR_OTA_DATA_INITIAL_SHA256 "$EXPECTED_OTA_SHA")"
EXPECTED_APP_SHA="$(read_checksum_var "$CHECKSUM_FILE" A21AIR_STACK_CHAN_SHA256 "$EXPECTED_APP_SHA")"
EXPECTED_ASSETS_SHA="$(read_checksum_var "$CHECKSUM_FILE" A21AIR_GENERATED_ASSETS_SHA256 "$EXPECTED_ASSETS_SHA")"
echo "artifact_checksum_file=$CHECKSUM_FILE"

require_file_sha "$ARTIFACT_DIR/bootloader.bin" "$EXPECTED_BOOTLOADER_SHA"
require_file_sha "$ARTIFACT_DIR/partition-table.bin" "$EXPECTED_PARTITION_SHA"
require_file_sha "$ARTIFACT_DIR/ota_data_initial.bin" "$EXPECTED_OTA_SHA"
require_file_sha "$ARTIFACT_DIR/stack-chan.bin" "$EXPECTED_APP_SHA"
require_file_sha "$ARTIFACT_DIR/generated_assets.bin" "$EXPECTED_ASSETS_SHA"

if [ ! -c "$PORT" ]; then
  echo "serial port is not present or not a character device: $PORT" >&2
  exit 1
fi

echo "artifact_ok=$ARTIFACT_DIR"
echo "target_device_id=$EXPECTED_DEVICE_ID"
echo "target_client_id=$EXPECTED_CLIENT_ID"
echo "serial_port=$PORT"
echo "baud=$BAUD"
echo "before_reset=$BEFORE_RESET"
echo "flash_mode=$([ "$DO_FLASH" -eq 1 ] && echo true || echo false)"

ESPTOOL_PYTHON_RESOLVED=""
if ESPTOOL_PYTHON_RESOLVED="$(find_esptool_python 2>/tmp/a21air-esptool-python.err)"; then
  echo "esptool_python=$ESPTOOL_PYTHON_RESOLVED"
else
  echo "esptool_python=missing"
  cat /tmp/a21air-esptool-python.err >&2 || true
fi
rm -f /tmp/a21air-esptool-python.err

FLASH_ARGS=(
  --chip esp32s3
  -p "$PORT"
  -b "$BAUD"
  --before "$BEFORE_RESET"
  --after hard_reset
  write_flash
  --flash_mode dio
  --flash_size 16MB
  --flash_freq 80m
  0x0 bootloader.bin
  0x8000 partition-table.bin
  0xd000 ota_data_initial.bin
  0x20000 stack-chan.bin
  0xa00000 generated_assets.bin
)

if [ "$DO_FLASH" -eq 0 ]; then
  printf 'preflight_only=true\n'
  printf 'flash_command=(cd %q && %q -m esptool' "$ARTIFACT_DIR" "${ESPTOOL_PYTHON_RESOLVED:-python3}"
  printf ' %q' "${FLASH_ARGS[@]}"
  printf ')\n'
  exit 0
fi

if [ "$CONFIRM_DEVICE_ID" != "$EXPECTED_DEVICE_ID" ]; then
  echo "--flash requires --confirm-device-id $EXPECTED_DEVICE_ID" >&2
  exit 2
fi

if [ -z "$ESPTOOL_PYTHON_RESOLVED" ]; then
  echo "no Python interpreter with esptool is available; source ESP-IDF export.sh or pass --python" >&2
  exit 2
fi

cd "$ARTIFACT_DIR"
"$ESPTOOL_PYTHON_RESOLVED" -m esptool "${FLASH_ARGS[@]}"
