# A21 Air StackChan Firmware Overlay

This directory tracks the A21 Air overlay for the official M5Stack StackChan firmware source.

Baseline:
- Official StackChan clean source: `/Users/jiyurun/Documents/stackchan-xiaozhi-endpoint-lab/workspace/official-stackchan-clean`
- Firmware line: StackChan V1.4.1 / xiaozhi-esp32 v2.2.4
- Hardware target: M5Stack StackChan CoreS3, ESP32-S3, 16 MB flash

## Da Tou Wake Word

`stackchan-v1.4.1-da-tou-wake.patch` switches the official firmware from the built-in HiStackChan wake model to the custom MultiNet wake-word path:

- display wake name: `大头`
- command phrase: `da tou`
- custom wake threshold: `20`
- wake-word audio upload: enabled
- wake detection while already listening: disabled

The preferred path is the committed build helper. Default mode is dry-run, so
it checks source cleanliness, patch applicability, ignored artifact output and
ESP-IDF discovery without copying source, fetching dependencies or writing
artifacts:

```bash
bash firmware/stackchan-a21air/build-a21air-stackchan.sh
```

To produce a new archived artifact directory:

```bash
bash firmware/stackchan-a21air/build-a21air-stackchan.sh --execute
```

For a final flash candidate, prefer the official README ESP-IDF target and make
the version requirement explicit:

```bash
bash firmware/stackchan-a21air/build-a21air-stackchan.sh \
  --execute \
  --require-idf-version v5.5.4
```

If ESP-IDF v5.5.4 is not installed locally, do not silently substitute an old
A21/X21 artifact. Either install the target toolchain or use 5080lab/mainland
mirror infrastructure to build from this StackChan overlay source and bring
back the generated artifact directory.

The helper writes `manifest.md` and `a21air-firmware-checksums.env` into each
artifact directory. The flash helper reads that checksum file when present, so
new reproducible builds do not depend on hard-coded hashes from the older smoke
artifact.

Manual patch application, kept here only for inspection/debugging:

```bash
cd "/Users/jiyurun/Documents/stackchan-xiaozhi-endpoint-lab/workspace/official-stackchan-clean"
git apply "/Users/jiyurun/Documents/A21 air/firmware/stackchan-a21air/stackchan-v1.4.1-da-tou-wake.patch"
git apply "/Users/jiyurun/Documents/A21 air/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-head-touch-debounce.patch"
git apply --unidiff-zero "/Users/jiyurun/Documents/A21 air/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-calm-motion.patch"
cd firmware
python3 ./fetch_repos.py
cd ..
git apply --unidiff-zero "/Users/jiyurun/Documents/A21 air/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-hot-websocket.patch"
git apply "/Users/jiyurun/Documents/A21 air/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-hot-channel-wake.patch"
git apply "/Users/jiyurun/Documents/A21 air/firmware/stackchan-a21air/stackchan-v1.4.1-a21air-voice-always-awake.patch"
cd firmware
idf.py build
```

If GitHub or ESP-IDF dependency pulls stall from the Mac network, use 5080lab mainland mirrors for the dependency fetch step. Do not replace this with old A21/X21 firmware artifacts.

Build evidence:

- 2026-06-08 smoke build passed from a temporary official-source copy with local ESP-IDF v5.5.2.
- `custom_wake_word.cc` participated in the build, and generated `sdkconfig` contains `CONFIG_CUSTOM_WAKE_WORD_DISPLAY="大头"` plus `CONFIG_SR_MN_CN_MULTINET7_QUANT=y`.
- Saved ignored artifacts and manifest under `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-20260608-0146/`.
- `stack-chan.bin` SHA256: `3c353e98d35e2e32ac381032d32df8ccfd6941f64210a9e144746a8f92bb0742`.
- App partition free space: 27%.
- 2026-06-08 hot-WebSocket + `大头` smoke artifact is archived under `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-20260608-0508/`, with flash-helper preflight passing against the connected A21 Air StackChan serial port.
- 2026-06-08 official-target ESP-IDF v5.5.4 build passed through the reproducible helper. The earlier artifact under `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-20260608-0835/` is retained as root-cause evidence only because it still used the old `4M` assets partition.
- v5.5.4 `stack-chan.bin` SHA256: `22053e2e88786fdb1c1013b30120885ead58b4c049e96899cc7a3921c055f440`.
- v5.5.4 app size: `0x39a720`, with 27% app partition free.
- Flash-helper preflight passed for the v5.5.4 artifact against `/dev/cu.usbmodem1101`, target `44:1b:f6:e2:74:50`, without writing.
- 2026-06-08 physical flash of the v5.5.4 artifact above proved the StackChan official icon loss was an assets partition sizing bug: `generated_assets.bin` is `4688622` bytes while the old official partition table declared only `4M` for `assets`.
- The hot-WebSocket overlay now grows the `assets` partition to `5M`; serial boot evidence after the rescue flash shows `Assets: The partition size is 5120 KB`, no `icon_* failed` lines, and the official launcher/home/mode icon assets load again.
- A temporary `CONFIG_A21_AIR_AUTO_START_AI_AGENT=y` build is explicitly rejected: it boot-looped with `Guru Meditation Error: Core 0 panic'ed (LoadProhibited)` immediately after `[AI.AGENT] on create`. Do not enable AI.Agent autostart again without a dedicated App lifecycle design/test gate.
- Formal no-autostart v5.5.4 build evidence now exists under `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-assets-noautostart-20260608-1022/`; its `stack-chan.bin` SHA256 is `c0c4e75f13c9c92dc425cf81e01ba9f3bd4df5cd9fbd461144e0d29f7556f2c0`, `assets` partition is `5M`, `大头` custom wake remains configured, and flash-helper preflight passed.
- The currently restored physical unit was recovered with the ignored rescue artifact `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-assets-rescue-20260608-1024/`, which combines the previously bootable app with the fixed `5M` partition table. Serial evidence shows no panic, no icon missing lines, and hot Xiaozhi WebSocket connection after opening AI.Agent.
- 2026-06-08 21:29 rollback: user requested returning to the pre-19:44 baseline after selecting the wrong path/model. ECS runs revert commit `2bfdac8`, whose gateway runtime code matches pre-19:44 commit `d84b377`; local repository HEAD may include later control-document, flash-helper and deploy-runner guard commits, but gateway runtime code must stay at that rollback baseline unless deliberately changed. The physical unit `44:1b:f6:e2:74:50` was flashed back to `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-assets-calm-motion-20260608-1410/`, with `stack-chan.bin` SHA256 `0511ed4f58ea8a9fadd890a8651fe8e7a24346c3db073c0e44d4ab7930f8bd0c`. Serial evidence `server/var/runtime/hardware/serial/stackchan-rollback-1440-boot-20260608-2135.log` shows `Assets: The partition size is 5120 KB`, no panic, no `icon_* failed`, and no repeated `Stop idle motion`/touch-pet churn during the 50 s boot capture. This rollback intentionally removed post-19:44 wakefix, pure-`大头` server control-turn, audio/continuity polish and listening-wake changes.
- `flash-a21air-stackchan.sh` intentionally has no default artifact. Every preflight or flash must pass `--artifact-dir`, and known historical/reverted artifact directory names are blocked by default, including the v5.5.2 smokes, old 4M-assets build, autostart boot-loop build, rescue/no-autostart candidates, the `1410` rollback build, wakefix build and listening-wake build. `--allow-historical-artifact` exists only for deliberate rollback/debug after the control docs have been updated.
- 2026-06-08 head-touch debounce build evidence exists under `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-headtouch-threshold2-20260608-verify/`; it preserves the official top-touch feature while requiring touch intensity `>=2`, 3 consecutive press frames, 2 release frames and 2 same-direction swipe frames. `stack-chan.bin` SHA256: `79e1e3d82f234d6a71b789e93dda4fde1a0657dd76a1fe83c291dab5894b4f36`. Not flashed.
- 2026-06-09 09:18 recovery flash: the physical unit is now back on `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-singlewake-calm-motion-kconfigfix-20260608-2330/`, `stack-chan.bin` SHA256 `2a045c5e94dc970453753a929f28909657ca9a76d92079a1702129eba6929b19`. Esptool verified bootloader, partition table, OTA data, app and generated assets hashes for target MAC `44:1b:f6:e2:74:50`. Serial log `server/var/runtime/hardware/serial/stackchan-rollback-2330-boot-20260609-0920.log` shows normal launcher boot, `Assets: The partition size is 5120 KB`, no panic/Guru and no missing icon lines in the capture.
- 2026-06-09 rejected runtime-start attempt: `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-runtime-start-kconfigfix-calm-motion-20260609-0925/` boot-looped on the real unit. Serial log `server/var/runtime/hardware/serial/stackchan-runtime-start-kconfigfix-boot-20260609-0912.log` shows repeated `A21.AIR request Xiaozhi runtime start after launcher settle` followed immediately by `Guru Meditation Error ... LoadProhibited`. Do not reintroduce launcher-loop `requestXiaozhiStart()`; automatic runtime entry must use the official app lifecycle and fresh serial proof before flashing.
- 2026-06-09 official-lifecycle launcher entry artifact is now flashed on the physical unit: `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-launcher-ai-agent-open-calm-motion-20260609-0945/`, `stack-chan.bin` SHA256 `f9c41946e97f21e4131aa249e3b933ec8fb9fb48deeb53e15b175012ab162ba3`. Serial boot evidence `server/var/runtime/hardware/serial/stackchan-launcher-ai-agent-open-boot-20260609-0950.log` shows `Assets: The partition size is 5120 KB`, no panic/Guru, no missing icon lines, `[Launcher] A21 Air auto open AI.AGENT`, `[AI.AGENT] on open`, `[HAL] start xiaozhi`, `CustomWakeWord: Command: da tou, Text: 大头`, and a hot Xiaozhi WebSocket session. Wake retest evidence `server/var/runtime/hardware/serial/stackchan-da-tou-mac-say-retest-20260609-0952.log` shows `Custom wake word detected ... da tou` and `Application: Wake word detected: 大头`; the same retest exposed an idle `Channel timeout 201 seconds`, so the gateway now keeps idle Xiaozhi channels warm with a neutral LLM emotion frame before the final long-idle physical retest.
- 2026-06-09 hot-channel wake artifact is now flashed on the physical unit: `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-launcher-ai-agent-open-hotwake-calm-motion-20260609-1020/`, `stack-chan.bin` SHA256 `2bda5be7736020ab2ab269640473c10a82a8e306777d19cff5bf3d3420797381`. It adds `stackchan-v1.4.1-a21air-hot-channel-wake.patch`, which fixes the official hot-channel wake path by switching Idle to Connecting before `ContinueWakeWordInvoke()` when the Xiaozhi audio channel is already open. Serial boot `server/var/runtime/hardware/serial/stackchan-hot-channel-wake-boot-20260609-102027.log` showed hot WebSocket connection with no panic/Guru, and immediate wake retest `server/var/runtime/hardware/serial/stackchan-hot-channel-wake-immediate-20260609-102157.log` showed `大头` detection, `State: idle -> connecting -> listening`, ASR transcript `今天天气怎么样？`, and no `Channel timeout`.
- 2026-06-09 voice-always-awake artifact is now flashed on the physical unit: `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-launcher-ai-agent-open-hotwake-calm-motion-voice-always-awake-20260609-1220/`, `stack-chan.bin` SHA256 `a56495be9ec481ee45b0bd0aa79a7165eeefc3d33a9e1514229d9767819ac6a3`. It adds `stackchan-v1.4.1-a21air-voice-always-awake.patch`, which keeps wake-word detection and mic input alive by preventing the generic xiaozhi `PowerSaveTimer` from entering sleep while `AudioService::IsWakeWordRunning()` is true. Flash helper verified hashes for bootloader, partition table, OTA data, app and assets on target `44:1b:f6:e2:74:50`. Long-idle retest `server/var/runtime/hardware/serial/stackchan-voice-always-awake-longidle-retest-20260609-1230.log` waited 55 seconds without reset or screen tap, then detected `大头`, entered `State: idle -> connecting -> listening`, spoke through TTS, recorded zero touch events, and did not show power-save entry. Acoustic TTS capture `server/var/runtime/acoustic/acoustic-20260609T040539Z-ecm999u-stackchan_tts_50cm_voice_awake/manifest.json` recorded peak `-21.89 dBFS`, RMS `-44.48 dBFS`, `clipped_percent=0` and `dc_offset=-0.000002`.
- 2026-06-09 A21.MODE launcher artifact is now flashed on the physical unit: `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-launcher-ai-agent-open-hotwake-calm-motion-voice-always-awake-mode-launcher-20260609-1245/`, `stack-chan.bin` SHA256 `7516495f652f30d62ca162d2f02c447a7ed72400b3db70ded2969be94947e9cb`. It adds `stackchan-v1.4.1-a21air-mode-launcher.patch`, registering a top-level `A21.MODE` app next to the official StackChan apps without replacing the launcher, icons, AI.Agent lifecycle or Xiaozhi voice runtime. Serial boot evidence `server/var/runtime/hardware/serial/stackchan-mode-launcher-boot-20260609-1245.log` shows `Assets: The partition size is 5120 KB`, `[A21.MODE] on create`, `[Launcher] A21 Air auto open AI.AGENT`, `[AI.AGENT] on open`, hot Xiaozhi WebSocket connection and no panic/Guru. ECS-local device-mode status and select loop passed through Cloud Assistant invokes `t-sh06ne83qz49khs` and `t-sh06ne8ag9u64u8`; selecting `tool` returned `bridge_disabled` as expected, then selecting `casual` restored the default mode.
- 2026-06-09 speaking-only wake/barge-in artifact is now flashed on the physical unit: `server/var/runtime/hardware/firmware/stackchan-v1.4.1-da-tou-hotws-v554-wake-bargein-speakingonly-20260609-1445/`, `stack-chan.bin` SHA256 `f76a7ec913dcf51edd11d980238a54853366998e375f6e0dfcc1858d3b00fc45`. It keeps the long-idle wake guard and permits the custom `大头` wake word during speaking so speech can be interrupted cleanly, but intentionally does not force custom wake detection while already listening/ASR is active. The discarded `stackchan-v1.4.1-da-tou-hotws-v554-wake-bargein-20260609-1435/` artifact forced wake detection in listening and produced `audio_input` task watchdog resets plus a stuck green listening state, so it is rejected and must not be promoted. Accepted evidence: `server/var/runtime/hardware/serial/stackchan-speakingonly-boot-20260609-1445.log`, `server/var/runtime/hardware/serial/stackchan-speakingonly-deepseek-bargein-live-20260609-1448.log`, and user-confirmed physical acceptance.

The official StackChan README targets ESP-IDF v5.5.4. Treat the v5.5.2 artifact, the earlier v5.5.4 `0835` artifact, the post-19:44 `wakefix/listening-wake` artifacts, the 2026-06-09 launcher-loop runtime-start artifact and the older `2330` recovery artifact as historical/rejected-or-superseded evidence only. Current hardware is on the `1445` speaking-only wake/barge-in artifact above; it supersedes `1245` while preserving hot WebSocket, hot-channel wake, voice-always-awake power-save protection, calm motion and mode launcher behavior.

Physical flashing is a separate operator step and must be done only after confirming the target serial port and that the connected unit is the current A21 Air StackChan hardware.

Use the committed helper for flash preflight instead of copying `flash_args` from the saved build directory. The saved `flash_args` contains ESP-IDF build-directory paths, while the archived artifact files are stored at the artifact root.

```bash
bash firmware/stackchan-a21air/flash-a21air-stackchan.sh \
  --artifact-dir "/Users/jiyurun/Documents/A21 air/server/var/runtime/hardware/firmware/<artifact>"
```

The helper defaults to preflight only after an artifact is explicitly selected: it checks the A21 Air artifact hashes, confirms the serial device exists, prints the exact esptool command, and writes nothing. To actually flash the current A21 Air StackChan unit, the command must be explicit:

```bash
bash firmware/stackchan-a21air/flash-a21air-stackchan.sh \
  --artifact-dir "/Users/jiyurun/Documents/A21 air/server/var/runtime/hardware/firmware/<artifact>" \
  --flash \
  --confirm-device-id 44:1b:f6:e2:74:50
```

CoreS3 sometimes does not enter the ROM downloader through automatic esptool reset. If auto-reset fails with `No serial data received`, put the unit into download mode by long-pressing `RST/RESET` for about 2-3 seconds until the internal green LED turns on, then release it and flash with:

```bash
bash firmware/stackchan-a21air/flash-a21air-stackchan.sh \
  --artifact-dir "/Users/jiyurun/Documents/A21 air/server/var/runtime/hardware/firmware/<artifact>" \
  --before no_reset \
  --flash \
  --confirm-device-id 44:1b:f6:e2:74:50
```

The helper prefers an ESP-IDF Python under `~/.espressif/python_env` and falls back to `python3` only when it already has `esptool`. You can also pass `--python <path>` after sourcing an ESP-IDF environment.

Do not run `--flash` unless the connected port is the current A21 Air StackChan unit and the operator has confirmed physical flashing.

## Head Touch Debounce

`stackchan-v1.4.1-a21air-head-touch-debounce.patch` keeps the official top-touch/head-pet feature enabled, but makes the gesture recognizer less sensitive to weak or single-frame SI12T noise:

- press starts only after intensity `>=2` for 3 consecutive 50 ms frames
- release requires 2 consecutive released frames
- swipe requires 2 consecutive frames in the same direction

This patch does not touch AI.Agent autostart, official launcher assets, Xiaozhi ASR/LLM/TTS code, provider selection or gateway voice runtime.

## Calm Speaking Motion

`stackchan-v1.4.1-a21air-calm-motion.patch` keeps the official StackChan speaking mouth/expression animation, but disables the random speaking head micro-motion through `CONFIG_A21_AIR_DISABLE_STACKCHAN_SPEAKING_MOTION=y`. Gateway-owned semantic body cues stay cloud-paced, and this patch does not touch AI.Agent autostart, launcher assets, wake detection or Xiaozhi audio framing.

## A21 Air Hot Xiaozhi WebSocket

`stackchan-v1.4.1-a21air-hot-websocket.patch` adds a narrow A21 Air runtime overlay for the official Xiaozhi WebSocket lifecycle:

- keep the Xiaozhi WebSocket audio channel warm while the device is idle
- retry every 5 seconds after a gateway/network close
- send only the normal Xiaozhi `hello` while idle
- do not start listening and do not upload microphone audio until the official wake/touch listening path runs
- keep the ASR/LLM/TTS protocol and Opus audio framing unchanged

This is meant to fix the real StackChan behavior observed on 2026-06-08: after a gateway restart, ECS health and readiness recovered but the device did not produce a post-restart `hello_received` until the voice app opened a new audio channel. The server-side report `/var/lib/a21-air/acceptance/a21-air-physical-reconnect-after-user-click-rfc3339-20260607T210249Z.json` still showed `gateway_restart_reconnect_ok=false`, with the latest device trace event before the service restart.

The overlay is firmware-only and must be rebuilt/flashed before the gateway restart reconnect gate can pass. It does not replace the server-side final physical acceptance metrics.

The hot-WebSocket patch is intentionally stored as a zero-context patch because it touches fetched `xiaozhi-esp32` sources that are absent before `fetch_repos.py`. Apply it with `git apply --unidiff-zero`. The calm-motion patch is a normal-context patch and is applied after wake/head-touch but before the fetched hot-WebSocket overlay.

## Hot-Channel Wake

`stackchan-v1.4.1-a21air-hot-channel-wake.patch` is a narrow companion to the hot WebSocket overlay. Official xiaozhi-esp32 already handles wake-word upload when the audio channel is closed by entering `kDeviceStateConnecting` before continuing the wake invocation. With A21 Air's idle hot WebSocket, the channel is already open while the app is still Idle; without the patch the direct continuation returns immediately because `ContinueWakeWordInvoke()` requires Connecting. The patch sets Connecting before that direct continuation so the same official wake-word upload and listening transition runs on warm channels.

## Voice Always Awake

`stackchan-v1.4.1-a21air-voice-always-awake.patch` is the long-idle companion to hot WebSocket and hot-channel wake. Official xiaozhi `PowerSaveTimer` can disable wake-word detection and audio input after the standby timeout; on A21 Air this made `大头` work immediately after reset but fail after the device sat idle. The patch adds `CONFIG_A21_AIR_KEEP_WAKE_WORD_AWAKE=y`, resets the power-save ticks while the wake-word engine is running, and keeps the custom `大头` wake word available during speaking for barge-in. It deliberately does not force custom wake detection while the device is already listening/ASR-active, because the rejected `1435` artifact proved that double-running that path can trip the `audio_input` watchdog. This keeps the device usable as a desktop voice satellite without changing the xiaozhi ASR/LLM/TTS protocol or Opus framing.
