#!/bin/bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SOURCE_DIR="/Users/jiyurun/Documents/stackchan-xiaozhi-endpoint-lab/workspace/official-stackchan-clean"
OUTPUT_ROOT="$REPO_ROOT/server/var/runtime/hardware/firmware"
BUILD_LABEL=""
IDF_EXPORT="${IDF_EXPORT:-}"
REQUIRE_IDF_VERSION=""
KEEP_WORKDIR=0
EXECUTE=0

WAKE_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-da-tou-wake.patch"
HEAD_TOUCH_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-head-touch-debounce.patch"
HOTWS_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-hot-websocket.patch"
HOT_CHANNEL_WAKE_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-hot-channel-wake.patch"
CALM_MOTION_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-calm-motion.patch"
VOICE_ALWAYS_AWAKE_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-voice-always-awake.patch"
LAUNCHER_AI_AGENT_OPEN_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-launcher-ai-agent-open.patch"
MODE_LAUNCHER_PATCH="$REPO_ROOT/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-mode-launcher.patch"
CHECKSUM_FILE_NAME="a21air-firmware-checksums.env"

usage() {
  cat <<'EOF'
usage: firmware/stackchan-a21air/build-a21air-stackchan.sh [options]

Rebuild the A21 Air StackChan V1.4.1 "Da Tou" + hot Xiaozhi WebSocket
firmware overlay from official StackChan source. Default mode is dry-run and
does not copy source, fetch dependencies, run idf.py or write artifacts.

Options:
  --source-dir <path>          Official clean StackChan source tree.
  --output-root <path>         Ignored artifact root. Default:
                               server/var/runtime/hardware/firmware
  --build-label <label>        Artifact directory suffix. Default:
                               stackchan-v1.4.1-da-tou-hotws-YYYYMMDD-HHMM
  --idf-export <path>          ESP-IDF export.sh. Default prefers local
                               ~/esp/esp-idf-v5.5.4/export.sh, then v5.5.2.
  --require-idf-version <text> Fail if `idf.py --version` does not contain
                               this text, e.g. v5.5.4.
  --keep-workdir               Keep the temporary copied source after build.
  --execute                    Actually copy, fetch, patch, build and archive.
  -h, --help                   Show this help.

If GitHub or ESP-IDF dependency pulls stall from the Mac network, use 5080lab
or another mainland mirror lane for the dependency fetch/build, then bring back
the artifact directory. Do not use old A21/X21 firmware artifacts.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --source-dir) SOURCE_DIR="${2:?missing --source-dir value}"; shift 2 ;;
    --output-root) OUTPUT_ROOT="${2:?missing --output-root value}"; shift 2 ;;
    --build-label) BUILD_LABEL="${2:?missing --build-label value}"; shift 2 ;;
    --idf-export) IDF_EXPORT="${2:?missing --idf-export value}"; shift 2 ;;
    --require-idf-version) REQUIRE_IDF_VERSION="${2:?missing --require-idf-version value}"; shift 2 ;;
    --keep-workdir) KEEP_WORKDIR=1; shift ;;
    --execute) EXECUTE=1; shift ;;
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

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

file_size() {
  wc -c < "$1" | tr -d ' '
}

safe_label() {
  local value="$1"
  if [ -z "$value" ]; then
    date '+stackchan-v1.4.1-da-tou-hotws-%Y%m%d-%H%M'
    return 0
  fi
  if ! [[ "$value" =~ ^[A-Za-z0-9._-]+$ ]]; then
    echo "--build-label may contain only letters, numbers, dot, underscore and dash" >&2
    exit 2
  fi
  printf '%s\n' "$value"
}

find_idf_export() {
  if [ -n "$IDF_EXPORT" ]; then
    printf '%s\n' "$IDF_EXPORT"
    return 0
  fi
  if [ -f "$HOME/esp/esp-idf-v5.5.4/export.sh" ]; then
    printf '%s\n' "$HOME/esp/esp-idf-v5.5.4/export.sh"
    return 0
  fi
  if [ -f "$HOME/esp/esp-idf-v5.5.2/export.sh" ]; then
    printf '%s\n' "$HOME/esp/esp-idf-v5.5.2/export.sh"
    return 0
  fi
  return 1
}

check_source_tree() {
  if [ ! -d "$SOURCE_DIR" ]; then
    echo "source dir not found: $SOURCE_DIR" >&2
    exit 1
  fi
  if [ ! -f "$SOURCE_DIR/firmware/fetch_repos.py" ]; then
    echo "source dir is not the expected StackChan tree: missing firmware/fetch_repos.py" >&2
    exit 1
  fi
  if [ ! -f "$WAKE_PATCH" ] || [ ! -f "$HEAD_TOUCH_PATCH" ] || [ ! -f "$HOTWS_PATCH" ] || [ ! -f "$HOT_CHANNEL_WAKE_PATCH" ] || [ ! -f "$CALM_MOTION_PATCH" ] || [ ! -f "$VOICE_ALWAYS_AWAKE_PATCH" ] || [ ! -f "$LAUNCHER_AI_AGENT_OPEN_PATCH" ] || [ ! -f "$MODE_LAUNCHER_PATCH" ]; then
    echo "missing A21 Air overlay patch files" >&2
    exit 1
  fi
  if git -C "$SOURCE_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if [ -n "$(git -C "$SOURCE_DIR" status --porcelain)" ]; then
      echo "source dir is dirty; use a clean official StackChan tree" >&2
      git -C "$SOURCE_DIR" status --short >&2
      exit 1
    fi
    local check_dir
    check_dir="$(mktemp -d "${TMPDIR:-/tmp}/a21air-stackchan-patch-check.XXXXXX")"
    trap 'rm -rf "$check_dir"' RETURN
    rsync -a --delete --exclude .git --exclude firmware/build "$SOURCE_DIR"/ "$check_dir/source"/
    (
      cd "$check_dir/source"
      git apply "$WAKE_PATCH"
      git apply "$HEAD_TOUCH_PATCH"
      git apply "$CALM_MOTION_PATCH"
      git apply "$LAUNCHER_AI_AGENT_OPEN_PATCH"
      git apply "$MODE_LAUNCHER_PATCH"
      if [ -d "firmware/xiaozhi-esp32" ]; then
        git apply --unidiff-zero "$HOTWS_PATCH"
        git apply "$HOT_CHANNEL_WAKE_PATCH"
        git apply "$VOICE_ALWAYS_AWAKE_PATCH"
      fi
    )
    rm -rf "$check_dir"
    trap - RETURN
  fi
}

check_output_root() {
  mkdir -p "$OUTPUT_ROOT"
  if git -C "$REPO_ROOT" check-ignore -q "$OUTPUT_ROOT/probe"; then
    return 0
  fi
  if git -C "$REPO_ROOT" check-ignore -q "$OUTPUT_ROOT"; then
    return 0
  fi
  echo "output root must be git-ignored: $OUTPUT_ROOT" >&2
  exit 1
}

copy_artifact() {
  local source="$1"
  local dest="$2"
  if [ ! -f "$source" ]; then
    echo "missing build artifact: $source" >&2
    exit 1
  fi
  cp "$source" "$dest"
}

write_checksums() {
  local output_dir="$1"
  {
    printf 'A21AIR_ARTIFACT_KIND=stackchan-v1.4.1-da-tou-hotws\n'
    printf 'A21AIR_BOOTLOADER_SHA256=%s\n' "$(sha256_file "$output_dir/bootloader.bin")"
    printf 'A21AIR_PARTITION_TABLE_SHA256=%s\n' "$(sha256_file "$output_dir/partition-table.bin")"
    printf 'A21AIR_OTA_DATA_INITIAL_SHA256=%s\n' "$(sha256_file "$output_dir/ota_data_initial.bin")"
    printf 'A21AIR_STACK_CHAN_SHA256=%s\n' "$(sha256_file "$output_dir/stack-chan.bin")"
    printf 'A21AIR_GENERATED_ASSETS_SHA256=%s\n' "$(sha256_file "$output_dir/generated_assets.bin")"
  } > "$output_dir/$CHECKSUM_FILE_NAME"
}

write_manifest() {
  local output_dir="$1"
  local source_commit="$2"
  local xiaozhi_commit="$3"
  local idf_version="$4"
  local build_time="$5"
  local app_size_hex
  app_size_hex="$(printf '0x%x' "$(file_size "$output_dir/stack-chan.bin")")"
  {
    printf '# A21 Air StackChan V1.4.1 Da Tou Hot WebSocket Firmware Build\n\n'
    printf 'Build time: %s Asia/Shanghai\n\n' "$build_time"
    printf 'Status:\n'
    printf -- '- Build helper completed.\n'
    printf -- '- Not flashed.\n'
    printf -- '- Physical wake-word and gateway restart reconnect retests pending.\n\n'
    printf 'Source:\n'
    printf -- '- Official StackChan source: `%s`\n' "$SOURCE_DIR"
    printf -- '- Official StackChan source commit: `%s`\n' "$source_commit"
    printf -- '- xiaozhi-esp32 commit after fetch/patch: `%s`\n' "$xiaozhi_commit"
    printf -- '- ESP-IDF: `%s`\n\n' "$idf_version"
    printf 'Source overlays:\n'
    printf -- '- `%s`\n' "$WAKE_PATCH"
    printf -- '- `%s`\n' "$HEAD_TOUCH_PATCH"
    printf -- '- `%s`\n' "$CALM_MOTION_PATCH"
    printf -- '- `%s`\n' "$LAUNCHER_AI_AGENT_OPEN_PATCH"
    printf -- '- `%s`\n\n' "$MODE_LAUNCHER_PATCH"
    printf -- '- `%s`\n' "$HOTWS_PATCH"
    printf -- '- `%s`\n' "$HOT_CHANNEL_WAKE_PATCH"
    printf -- '- `%s`\n\n' "$VOICE_ALWAYS_AWAKE_PATCH"
    printf 'Config verified:\n\n'
    grep -nE 'CONFIG_(USE_CUSTOM_WAKE_WORD|CUSTOM_WAKE_WORD|CUSTOM_WAKE_WORD_DISPLAY|CUSTOM_WAKE_WORD_THRESHOLD|SEND_WAKE_WORD_DATA|A21_AIR_KEEP_WEBSOCKET_CONNECTED|A21_AIR_WEBSOCKET_RECONNECT_INTERVAL_MS|A21_AIR_DISABLE_STACKCHAN_SPEAKING_MOTION|A21_AIR_KEEP_WAKE_WORD_AWAKE|A21_AIR_OPEN_AI_AGENT_FROM_LAUNCHER|A21_AIR_OPEN_AI_AGENT_DELAY_MS|SR_MN_CN_MULTINET7_QUANT)=' "$output_dir/sdkconfig" | sed 's/^/- /'
    printf '\nArtifacts:\n\n'
    printf '| File | Bytes | SHA256 |\n'
    printf '|---|---:|---|\n'
    for file in bootloader.bin partition-table.bin ota_data_initial.bin stack-chan.bin generated_assets.bin flasher_args.json flash_args "$CHECKSUM_FILE_NAME"; do
      printf '| `%s` | %s | `%s` |\n' "$file" "$(file_size "$output_dir/$file")" "$(sha256_file "$output_dir/$file")"
    done
    printf '\nSize evidence:\n'
    printf -- '- stack-chan.bin binary size: `%s` bytes.\n' "$app_size_hex"
    printf '\nFlash command:\n\n'
    printf '```bash\n'
    printf 'bash firmware/stackchan-a21air/flash-a21air-stackchan.sh --artifact-dir %q\n' "$output_dir"
    printf '```\n\n'
    printf 'Do not flash without explicit operator confirmation of the current A21 Air StackChan serial port and target unit.\n'
  } > "$output_dir/manifest.md"
}

BUILD_LABEL="$(safe_label "$BUILD_LABEL")"
OUTPUT_DIR="$OUTPUT_ROOT/$BUILD_LABEL"

check_source_tree
check_output_root

IDF_EXPORT_RESOLVED=""
if IDF_EXPORT_RESOLVED="$(find_idf_export 2>/dev/null)"; then
  :
fi

SOURCE_COMMIT="unavailable"
if git -C "$SOURCE_DIR" rev-parse --verify HEAD >/dev/null 2>&1; then
  SOURCE_COMMIT="$(git -C "$SOURCE_DIR" rev-parse --short=12 HEAD)"
fi

echo "source_dir=$SOURCE_DIR"
echo "source_commit=$SOURCE_COMMIT"
echo "wake_patch=$WAKE_PATCH"
echo "head_touch_patch=$HEAD_TOUCH_PATCH"
echo "hotws_patch=$HOTWS_PATCH"
echo "hot_channel_wake_patch=$HOT_CHANNEL_WAKE_PATCH"
echo "calm_motion_patch=$CALM_MOTION_PATCH"
echo "voice_always_awake_patch=$VOICE_ALWAYS_AWAKE_PATCH"
echo "launcher_ai_agent_open_patch=$LAUNCHER_AI_AGENT_OPEN_PATCH"
echo "mode_launcher_patch=$MODE_LAUNCHER_PATCH"
echo "output_dir=$OUTPUT_DIR"
echo "idf_export=${IDF_EXPORT_RESOLVED:-missing}"
echo "require_idf_version=${REQUIRE_IDF_VERSION:-none}"
echo "execute=$([ "$EXECUTE" -eq 1 ] && echo true || echo false)"

if [ "$EXECUTE" -eq 0 ]; then
  echo "dry_run=true"
  echo "execute_command=bash firmware/stackchan-a21air/build-a21air-stackchan.sh --execute"
  exit 0
fi

for tool in git rsync python3 grep cp awk sed; do
  require_tool "$tool"
done
if [ -z "$IDF_EXPORT_RESOLVED" ] || [ ! -f "$IDF_EXPORT_RESOLVED" ]; then
  echo "ESP-IDF export.sh not found; pass --idf-export or install the target ESP-IDF" >&2
  exit 2
fi
if [ -e "$OUTPUT_DIR" ]; then
  echo "output dir already exists: $OUTPUT_DIR" >&2
  exit 1
fi

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/a21air-stackchan-build.XXXXXX")"
cleanup() {
  if [ "$KEEP_WORKDIR" -eq 1 ]; then
    echo "workdir_kept=$WORK_DIR"
  else
    rm -rf "$WORK_DIR"
  fi
}
trap cleanup EXIT

COPIED_SOURCE="$WORK_DIR/source"
rsync -a --delete --exclude .git --exclude firmware/build "$SOURCE_DIR"/ "$COPIED_SOURCE"/

(
  cd "$COPIED_SOURCE"
  git apply "$WAKE_PATCH"
  git apply "$HEAD_TOUCH_PATCH"
  git apply "$CALM_MOTION_PATCH"
  git apply "$LAUNCHER_AI_AGENT_OPEN_PATCH"
  git apply "$MODE_LAUNCHER_PATCH"
  cd firmware
  python3 ./fetch_repos.py
  cd ..
  git apply --unidiff-zero "$HOTWS_PATCH"
  git apply "$HOT_CHANNEL_WAKE_PATCH"
  git apply "$VOICE_ALWAYS_AWAKE_PATCH"
)

set +u
. "$IDF_EXPORT_RESOLVED"
set -u

IDF_VERSION="$(idf.py --version)"
if [ -n "$REQUIRE_IDF_VERSION" ] && [[ "$IDF_VERSION" != *"$REQUIRE_IDF_VERSION"* ]]; then
  echo "idf version mismatch: got $IDF_VERSION, require text $REQUIRE_IDF_VERSION" >&2
  exit 1
fi

(
  cd "$COPIED_SOURCE/firmware"
  idf.py build
)

mkdir -p "$OUTPUT_DIR"
BUILD_DIR="$COPIED_SOURCE/firmware/build"
copy_artifact "$BUILD_DIR/bootloader/bootloader.bin" "$OUTPUT_DIR/bootloader.bin"
copy_artifact "$BUILD_DIR/partition_table/partition-table.bin" "$OUTPUT_DIR/partition-table.bin"
copy_artifact "$BUILD_DIR/ota_data_initial.bin" "$OUTPUT_DIR/ota_data_initial.bin"
copy_artifact "$BUILD_DIR/stack-chan.bin" "$OUTPUT_DIR/stack-chan.bin"
copy_artifact "$BUILD_DIR/generated_assets.bin" "$OUTPUT_DIR/generated_assets.bin"
copy_artifact "$BUILD_DIR/flasher_args.json" "$OUTPUT_DIR/flasher_args.json"
copy_artifact "$BUILD_DIR/flash_args" "$OUTPUT_DIR/flash_args"
copy_artifact "$COPIED_SOURCE/firmware/sdkconfig" "$OUTPUT_DIR/sdkconfig"

XIAOZHI_COMMIT="unavailable"
if git -C "$COPIED_SOURCE/firmware/xiaozhi-esp32" rev-parse --verify HEAD >/dev/null 2>&1; then
  XIAOZHI_COMMIT="$(git -C "$COPIED_SOURCE/firmware/xiaozhi-esp32" rev-parse --short=12 HEAD)"
fi

write_checksums "$OUTPUT_DIR"
write_manifest "$OUTPUT_DIR" "$SOURCE_COMMIT" "$XIAOZHI_COMMIT" "$IDF_VERSION" "$(date '+%Y-%m-%d %H:%M')"

echo "build_ok=true"
echo "artifact_dir=$OUTPUT_DIR"
echo "artifact_manifest=$OUTPUT_DIR/manifest.md"
echo "artifact_checksums=$OUTPUT_DIR/$CHECKSUM_FILE_NAME"
echo "stack_chan_sha256=$(sha256_file "$OUTPUT_DIR/stack-chan.bin")"
