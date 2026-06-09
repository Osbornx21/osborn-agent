# Physical StackChan Acceptance

Physical execution is active now that the StackChan hardware and usable firmware are available as of 2026-06-07. The software gate exists so the hardware run produces a validated report instead of ad hoc notes.

## Report Command

Run the one-turn LED lifecycle retest immediately after the operator observes a real hardware turn. This command correlates the visual observation with ECS trace evidence; it does not infer color from the trace alone.

```bash
cd server
go run ./cmd/stackchan-gateway physical-led-retest \
  --trace-file /var/lib/a21-air/traces/turns.jsonl \
  --report ./var/acceptance/stackchan-s3-led-YYYYMMDD-HHMMSS.json \
  --device 44:1b:f6:e2:74:50 \
  --trace-id <observed-trace-id> \
  --listen-start-sequence <observed-listen-start-sequence> \
  --gateway-commit <deployed-gateway-commit> \
  --visual-green-confirmed
```

The LED retest passes only when the selected observed turn has `listen_start`, `first_uplink_audio` before `speech_final`, listen-start LED dispatch before `speech_final`, no LED overwrite before `speech_final`, and explicit physical green confirmation from the operator. When the official firmware immediately enters another auto-listen cycle, bind the report to the operator-observed segment with `--trace-id` plus `--listen-start-sequence`; otherwise the default latest-trace selector is intentionally strict and may reject an incomplete tail. For the current lifecycle gate, the same operator observation must also confirm that TTS speaking switches visibly blue and the device settles/off after TTS stop; capture those as short safe notes until the CLI grows dedicated fields. Optional notes are kept short and must not include transcripts, prompts, token values, raw audio, generated text or raw LED values.

Then derive the turn, first-audible, continuity, barge-in and camera-safety metrics from the trace. The first-audible basis is `first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms`, so it measures gateway time from ASR final to the first Xiaozhi downlink audio packet. Continuity is derived only from safe `llm_request.fields.recent_turn_count` numbers: the report must show `llm_request_turns`, `llm_recent_context_turns`, `max_recent_turn_count` and `continuity_context_ok`, but it must not expose prompts, transcripts, recent-turn text or generated text. Barge-in stop latency is derived from `tts_stop_sent.fields.stop_latency_ms` for `listen_start`/`abort` interruptions when the trace contains an interruption segment. Camera safety is derived from `llm_tool_call.fields.tool_name=self.camera.take_photo` inside the same acceptance window; even a rejected/default-blocked camera tool attempt makes final acceptance fail. Use `--barge-in-stop-latency-ms` only as a fallback override when the trace predates that field or the operator intentionally measured the interruption out of band:

```bash
cd server
go run ./cmd/stackchan-gateway physical-acceptance-metrics \
  --trace-file /var/lib/a21-air/traces/turns.jsonl \
  --device 44:1b:f6:e2:74:50 \
  --min-turns 20 \
  --since <acceptance-start-rfc3339> \
  --latest-turns 20 \
  > ./var/acceptance/stackchan-s3-metrics-YYYYMMDD-HHMMSS.json
```

Gateway restart reconnect is verified from the trace with parsed RFC3339 timestamps. Do not compare timestamp strings: a `+08:00` event before a UTC restart can sort later as text while still being earlier in time.

After flashing the A21 Air `大头` + hot Xiaozhi WebSocket overlay, use the Cloud Assistant helper instead of hand-writing a restart/retest command. It defaults to dry-run and calls no Aliyun API:

```bash
server/deploy/aliyun/physical-postflash-retest.sh
```

Once the operator has confirmed the overlay was flashed and the device has rebooted, run:

```bash
server/deploy/aliyun/physical-postflash-retest.sh --execute
```

The helper first verifies that the installed gateway binary is executable, `jq` is present and the report directory can be created. It then restarts `stackchan-gateway`, checks ECS-local `/healthz` and `/readyz`, waits for the flashed device to open the Xiaozhi WebSocket, then writes an ECS-side `a21-air-postflash-reconnect-*.json` report through `physical-reconnect-retest`. It prints no tokens and does not call private admin APIs.

```bash
cd server
go run ./cmd/stackchan-gateway physical-reconnect-retest \
  --trace-file /var/lib/a21-air/traces/turns.jsonl \
  --device 44:1b:f6:e2:74:50 \
  --restart-start <gateway-restart-start-rfc3339> \
  --report ./var/acceptance/stackchan-s3-reconnect-YYYYMMDD-HHMMSS.json
```

Then generate and validate the broader physical acceptance report. Keep operator confirmations short and safe:

```bash
cd server
go run ./cmd/stackchan-gateway physical-acceptance-report \
  --report ./var/acceptance/stackchan-s3-YYYYMMDD-HHMMSS.json \
  --metrics-file ./var/acceptance/stackchan-s3-metrics-YYYYMMDD-HHMMSS.json \
  --device 44:1b:f6:e2:74:50 \
  --hardware-device-id 44:1b:f6:e2:74:50 \
  --client-id 36d53c70-30e7-41e9-9720-6a5000e40a3c \
  --firmware-build-id "StackChan-UserDemo V1.4.1" \
  --firmware-version V1.4.1 \
  --gateway-commit <deployed-gateway-commit> \
  --provider-profile siliconflow-dashscope-voice \
  --audio-playback-ok \
  --screen-text-ok \
  --head-control-ok \
  --led-lifecycle-ok \
  --led-retest-report ./var/acceptance/stackchan-s3-led-YYYYMMDD-HHMMSS.json \
  --no-unexpected-camera-trigger \
  --wifi-reconnect-ok \
  --gateway-restart-reconnect-ok \
  --notes "safe hardware observation notes"
```

The generator validates the report before writing it, including the referenced LED retest report. Then run the validator again as the final gate:

After the operator-observed 20-turn window, LED retest report, post-flash reconnect report and operator confirmations are all available, prefer the ECS helper over manual report generation. It defaults to dry-run:

```bash
server/deploy/aliyun/physical-final-acceptance.sh
```

Execution requires the acceptance start timestamp, deployed gateway commit, LED retest report path, passing reconnect report path and explicit operator confirmation flags:

```bash
server/deploy/aliyun/physical-final-acceptance.sh \
  --execute \
  --since <acceptance-start-rfc3339> \
  --gateway-commit <deployed-gateway-commit> \
  --led-retest-report /var/lib/a21-air/acceptance/a21-air-led-retest-YYYYMMDD.json \
  --reconnect-report /var/lib/a21-air/acceptance/a21-air-postflash-reconnect-YYYYMMDD.json \
  --audio-playback-ok \
  --screen-text-ok \
  --head-control-ok \
  --led-lifecycle-ok \
  --no-unexpected-camera-trigger \
  --wifi-reconnect-ok
```

The helper first verifies that the installed gateway binary is executable, `jq` is present and the report directory can be created. It then runs `physical-acceptance-metrics`, requires `gateway_restart_reconnect_ok=true` in the reconnect report, generates `stackchan_physical_acceptance_v2`, validates it with `acceptance`, and prints only safe summary fields, including continuity counts, `camera_tool_call_count` and aggregate `tts_audio_quality` stats.

```bash
cd server
go run ./cmd/stackchan-gateway acceptance \
  --report ./var/acceptance/stackchan-s3-YYYYMMDD-HHMMSS.json \
  --device 44:1b:f6:e2:74:50 \
  --turns 20
```

## Required JSON

```json
{
  "schema_version": "stackchan_physical_acceptance_v2",
  "device_id": "44:1b:f6:e2:74:50",
  "hardware_device_id": "44:1b:f6:e2:74:50",
  "client_id": "36d53c70-30e7-41e9-9720-6a5000e40a3c",
  "firmware_build_id": "StackChan-UserDemo V1.4.1",
  "firmware_version": "V1.4.1",
  "gateway_commit": "gateway-commit",
  "provider_profile": "provider-profile-id",
  "completed_turns": 20,
  "p50_first_audible_ms": 980,
  "p95_first_audible_ms": 1280,
  "barge_in_stop_latency_ms": 180,
  "body_mcp_tool_success_rate": 1.0,
  "llm_request_turns": 20,
  "llm_recent_context_turns": 19,
  "max_recent_turn_count": 8,
  "continuity_context_ok": true,
  "continuity_basis": "llm_request.fields.recent_turn_count > 0",
  "tts_audio_quality": {
    "event_count": 20,
    "sample_count": 240000,
    "duration_ms": 10000,
    "peak_dbfs_max": -1.5,
    "rms_dbfs_p50": -22.8,
    "rms_dbfs_p95": -18.4,
    "clipped_percent_max": 0.1,
    "silence_percent_max": 18.5,
    "dc_offset_max_abs": 0.004
  },
  "audio_playback_ok": true,
  "screen_text_ok": true,
  "head_control_ok": true,
  "led_lifecycle_ok": true,
  "led_retest_report": "./var/acceptance/stackchan-s3-led-YYYYMMDD-HHMMSS.json",
  "custom_screen_scene_mcp_required": false,
  "camera_tool_call_count": 0,
  "unexpected_camera_triggered": false,
  "wifi_reconnect_ok": true,
  "gateway_restart_reconnect_ok": true,
  "notes": "safe hardware observation notes"
}
```

`screen_text_ok` means the official Xiaozhi ASR/LLM/TTS screen text path was visibly normal. It does not require `self.screen.set_scene`, because official StackChan V1.4.1 does not expose that MCP tool. Custom scene display support must stay outside official physical acceptance unless a future custom-firmware or `/stackChan/ws` gate proves it separately. `unexpected_camera_triggered` must be `false` and `camera_tool_call_count` must be `0`; any camera tool attempt in the acceptance trace window fails acceptance until camera mode and user-visible confirmation semantics exist.

For the active A21 Air unit, `--device` is required and must be the runtime Xiaozhi `Device-Id` / MAC `44:1b:f6:e2:74:50`, because ECS trace events use that value. The example config's `stackchan-s3-main` id is not valid for this physical acceptance trace. `physical-acceptance-metrics` derives active `body_mcp_tool_success_rate` from acceptance-relevant `stackchan_body_dispatch` trace events and derives continuity from `llm_request.fields.recent_turn_count`; the report command copies both from `--metrics-file` when values are not supplied explicitly. Use `--since <acceptance-start-rfc3339>` for the final run so historical turns from earlier gateway versions or pre-fix tests cannot enter the acceptance window. Use `--latest-turns 20` unless the trace file has already been trimmed to the acceptance run, so old pre-fix trace failures do not pollute the current physical acceptance window. `led/idle_start` remains in trace diagnostics but is excluded from the active body success rate because official auto-listen can supersede idle settle between turns. The final acceptance window should include at least one real interruption after the gateway version that records `stop_latency_ms`, otherwise barge-in remains unproven even if the rest of the 20-turn voice window passes.

For sound-quality investigations, the same metrics JSON and generated acceptance report include a nested `tts_audio_quality` summary when the selected trace window contains TTS PCM quality events. It reports only safe aggregate numbers such as event count, sample count, duration, peak/RMS dBFS, clipped percent, silence percent and DC offset. The final helper prints those aggregate fields explicitly. It does not store or print raw PCM, Opus bytes, transcripts or generated text.

Runtime reports stay under `server/var/acceptance/` and are ignored by git unless deliberately sanitized.

## USB Acoustic Capture

The ECM USB measurement microphone can be used as a repeatable acoustic witness for future acceptance runs. First confirm the macOS-visible device name:

```bash
cd server
go run ./cmd/stackchan-gateway acoustic-devices
```

Then capture a short WAV plus manifest under `server/var/runtime/acoustic/`:

```bash
cd server
go run ./cmd/stackchan-gateway acoustic-capture \
  --ffmpeg /opt/homebrew/bin/ffmpeg \
  --device-name ECM999U \
  --duration-ms 10000 \
  --scenario quiet_1m_front \
  --label ecm999u
```

The manifest records sample rate, channel count, duration, peak dBFS, RMS dBFS, clipped percent, silence percent and DC offset. The same PCM16 metric definition is used by server-side `tts_audio_quality` trace diagnostics, so acoustic captures and DashScope TTS source-PCM checks are comparable without storing raw audio in traces. Use scenario names that describe the physical setup, for example `quiet_1m_front`, `fan_noise_1m_front`, or `tts_50cm_front`. The command refuses to write raw WAV output into a repository path unless that path is ignored by git; use `server/var/runtime/acoustic/`, another ignored `server/var/runtime/*` child, or an external temp directory. If live capture times out, grant microphone permission to the app or terminal process running Codex, then retry; if avfoundation cannot resolve the device by name, pass the discovered input explicitly with `--ffmpeg-input`, such as `--ffmpeg-input :0`.

For offline checks or fixture-driven tests, analyze an existing PCM 16-bit WAV without touching hardware:

```bash
cd server
go run ./cmd/stackchan-gateway acoustic-capture \
  --input-wav ./var/fixtures/acoustic/sample.wav \
  --scenario fixture_baseline \
  --label fixture
```
