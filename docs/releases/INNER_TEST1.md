# A21 Air Inner Test 1

Date: 2026-06-09

This is the first public inner-test baseline for the A21 Air / StackChan Xiaozhi gateway. It is a cloud-first voice satellite runtime for M5Stack StackChan hardware: the device keeps the microphone, speaker, screen, wake word and body feedback local, while ASR, LLM, TTS, memory, tools and provider routing run through the gateway.

## Baseline

- Device target: M5Stack StackChan S3
- Device identity used in acceptance: `44:1b:f6:e2:74:50`
- Gateway profile: `siliconflow-dashscope-voice`
- Voice chain: DashScope ASR, SiliconFlow LLM, DashScope TTS
- Firmware baseline: StackChan V1.4.1 with A21 Air overlays for `大头` wake, hot Xiaozhi WebSocket, speaking-time barge-in, calmer motion and A21 mode launcher
- Firmware artifact checksum: `f76a7ec913dcf51edd11d980238a54853366998e375f6e0dfcc1858d3b00fc45`
- Automatic provider fallback: disabled for Inner Test 1
- Manual/admin rollback candidate: `dashscope-cosyvoice`

## Acceptance

The current physical acceptance window passed with 20 completed real hardware voice turns.

- Final report: `/var/lib/a21-air/acceptance/a21-air-final-physical-acceptance-20260609T072422Z.json`
- Completed/audio turns: `20/20`
- First audible p50/p95: `834/1090 ms`
- Barge-in stop latency: `1 ms`
- Body MCP success rate: `1`
- Recent-dialogue context: `20/20` LLM requests carried recent context, max recent turn count `8`
- Camera safety: `camera_tool_call_count=0`, `unexpected_camera_triggered=false`
- TTS aggregate quality: no clipping, near-zero DC offset

## Release Assets

The GitHub release for `inner-test1` attaches sanitized operational artifacts:

- `a21-air-inner-test1-runtime-20260609.tgz`
- `a21-air-inner-test1-evidence-20260609.tgz`
- `INNER_TEST1_RELEASE.md`

The assets intentionally do not include provider keys, production env files, raw audio, transcripts, prompts, generated text, firmware source archives or device NVS dumps.

## Known Boundaries

- Full ASR/LLM/TTS automatic fallback is not enabled in Inner Test 1. The durable spoken Opus fixture is not present on ECS, so the full fallback matrix must be captured and passed before automatic fallback is turned on again.
- MiniMax is implemented as a provider adapter but was not ECS-smoked in this baseline because the cloud host does not currently have a MiniMax key installed.
- StackChan custom screen scene MCP remains optional. Official Xiaozhi screen text is the accepted display path for this firmware baseline.
- Firmware binaries are not committed to the public repository. Build and flash through the tracked A21 Air firmware helpers and checksums.
