# Provider Matrix

Date checked: 2026-06-08

Status: Task 13 provider burn-down is active with hardware, usable firmware and ECS available as of 2026-06-07. The committed runtime default profile is now `siliconflow-dashscope-voice`: DashScope ASR, SiliconFlow LLM, and DashScope TTS. DashScope LLM/ASR/TTS, Doubao LLM/ASR/TTS, StepFun LLM, SiliconFlow LLM, Moonshot LLM, MiniMax LLM/TTS WebSocket, DeepSeek LLM, and Anthropic Claude LLM adapters have fixture/parser/redaction/cancellation tests. The private admin provider probe endpoint plus `provider-probe-matrix` report command now exist for registry-backed ASR/LLM/TTS smoke checks, and the app's authenticated xiaozhi WebSocket path now creates a runtime `session.VoiceLoop` from the configured default provider profile instead of only authenticating the device. `asr-fixture-capture` captured real StackChan xiaozhi WebSocket Opus frames into an ignored fixture JSON for ASR probes, `provider-probe-gate` turns validated report summaries into an explicit production-selection gate, and `provider-probe-package` generates a safe 5080lab/ECS execution package. Current clean 5080lab LLM reports show SiliconFlow is stable and fast; Moonshot has formal package evidence as the first passing current-Go LLM fallback. ECS now also has formal LLM-only fallback evidence for SiliconFlow plus DashScope `qwen3.6-flash` from source ref `aa00ec56d781`; this supports a voice-path LLM rollback policy but does not replace full ASR/LLM/TTS fallback profile evidence. The 2026-06-07 ECS default-profile 20-run ASR/LLM/TTS gate passes with the real StackChan fixture, and actual-host release-package dry-run passes from source ref `42037f3efb21` / `clean`. The 2026-06-07 physical no-audio root cause was DashScope TTS provider bytes being passed through as Xiaozhi raw Opus; the gateway now requests provider PCM, encodes 60 ms chunks through libopus into Xiaozhi raw Opus packets, and the user confirmed real device audio playback recovered. Physical StackChan acceptance and any full fallback ASR/LLM/TTS policy evidence remain active gates before a final production claim.

Lab evidence: 5080lab was inspected on 2026-06-06. Relevant older full-matrix reports live under `C:\Users\21\Desktop\a21-mainland-latency-lab`; latest copied smoke reports live under `/Users/jiyurun/Documents/New project/reports/5080lab-provider` and `/Users/jiyurun/Documents/New project/reports/a21-5080lab-provider-evidence-20260605-225138.tgz`. The copied `.tgz` is a legacy A21 smoke bundle and is intentionally rejected by current `provider-probe-evidence-validate` because it contains `a21-provider-smoke-*.json`, not `provider-probe-*.json`; it is reference-only and cannot be promoted into production provider selection. Raw reports must not be committed because some historical artifacts contain API keys or signed audio URLs.

## Integration Rules

- Use only official provider documentation or official SDK/OpenAPI pages as adapter authority.
- Every adapter must expose `ProviderID`, `ModelID`, `SourceDocURL`, and `SourceDocCheckedAt`.
- Every request test must assert endpoint, method, required headers, content type, body shape, and no raw secret in logs.
- Every streaming adapter must have parser tests for first chunk, delta chunk, final chunk, error chunk, and cancellation.
- Voice provider output is not trusted as xiaozhi-ready Opus. Each TTS adapter must report codec, sample rate, channels, frame timing, and whether decode/resample/Opus encode is required.
- Probe output may log provider id, model id, safe provider HTTP status, safe provider error code, latency, transcript/token/audio lengths, codec, sample rate, frame/byte counts, and error class only.
- Provider probe APIs must stay on the private admin listener, require `STACKCHAN_ADMIN_TOKEN`, and never return prompt text, generated text, raw payloads, API keys, Authorization headers, or signed provider URLs.
- Admin provider probe failures may return safe provider diagnostics: `provider_error`, provider HTTP status, and sanitized provider error code. They must not return provider message text, request/response payloads, prompt text, generated text, or token values.
- Runtime xiaozhi WebSocket traffic must enter `session.VoiceLoop` through the configured default profile; provider probes and runtime gateway routing should share the same registry semantics so probe evidence remains relevant to actual device sessions.
- The runtime default profile is `siliconflow-dashscope-voice`; `/readyz` must check `dashscope-asr`, `siliconflow-llm`, and `dashscope-tts` through the same environment-backed registry before the gateway is treated as ready. For DashScope ASR, readiness includes the local Xiaozhi raw Opus -> PCM decoder factory required by Fun-ASR's provider-side PCM path. For DashScope TTS, readiness includes the local PCM -> Xiaozhi raw Opus encoder factory required by StackChan downlink playback.
- Voice hot-path model selection must prefer current official model-list entries that are Instruct, Flash, Turbo, highspeed, or explicitly thinking-disabled. Thinking, Reasoner, R1, Pro-reasoning, `*-think`, and multimodal/vision/coder models are not default voice models unless current provider-probe evidence proves p50/p95 first token and total latency fit the voice budget.
- Legacy A21 env keys may be bridged for credentials, but legacy model variables such as `A21_SILICONFLOW_MODEL` must not silently override the StackChan gateway default. Model overrides must use current gateway env names such as `SILICONFLOW_LLM_MODEL` and must be backed by fresh 5080lab/ECS provider-probe evidence.
- The private admin provider probe endpoint must use the same environment-backed provider registry as runtime VoiceLoop creation; a registered provider with missing credentials should surface as a safe provider configuration/probe failure, not as `PROVIDER_NOT_FOUND`.
- Runtime `listen stop` handling must not block the WebSocket read loop while ASR/LLM/TTS streaming is in progress; `abort` and new `listen start` messages are part of the low-latency voice contract and must remain readable during speaking.
- Runtime accepts only one active WebSocket binding per device id; reconnect replaces the previous binding and cancels its context so stale provider streams cannot keep running after a device reconnects.
- Runtime session lifecycle must keep `stackchan_sessions_active` truthful: the metric increments only after server hello is sent and decrements exactly once when the VoiceLoop is closed by disconnect or replacement.
- Runtime shutdown must explicitly close upgraded xiaozhi WebSocket bindings before server shutdown completes; HTTP listener shutdown alone is not enough evidence that active device voice turns have stopped.
- After runtime shutdown begins, new WebSocket upgrades must be rejected by the voice runtime even if an outer listener is still draining, so no post-shutdown provider turn or active session can be created.
- `provider-probe` CLI reports are written as `server/var/reports/provider-probe-YYYYMMDD-HHMMSS.json` and store transcript/prompt/generated text lengths only, never transcript text, prompt text or generated text.
- Standalone `provider-probe` and batch `provider-probe-matrix` both run report schema validation before a generated report can be promoted into summary, gate, evidence or docs.
- Every report must pass `provider-probe-validate --report <path>` before its summary enters this matrix or any control evidence log.
- Evidence tables should be generated with `provider-probe-summary` from validated reports, not hand-copied from raw JSON.
- Production provider selection must pass `provider-probe-gate` over the same validated reports, with minimum run count, success percent, required profile/modality coverage and LLM fallback checks.
- When `providers.auto_fallback.enabled=true`, every configured fallback profile must define ASR, LLM and TTS at config validation time. LLM-only evidence can support the LLM-provider rollback decision, but it cannot by itself enable automatic full voice fallback.
- `provider-probe-gate` failure output may include only validated safe error labels, such as `provider_error:http_402:invalid_request_error`, so account/auth/rate-limit blockers are visible without exposing provider messages or payloads.
- Passing `provider-probe-gate` output must include the exact gate parameters and source provenance used for evidence promotion: minimum runs, success percent, required profiles, required modalities, fallback modality, `source_ref` and `source_state`.
- LLM-only profiles such as `siliconflow-llm`, `moonshot-llm`, `stepfun-llm`, `minimax-llm`, `deepseek-llm`, `dashscope-llm` and `doubao-llm` are burn-down/isolation profiles. They can prove LLM fallback readiness, but they do not replace the full ASR/LLM/TTS production gate.
- Evidence tarballs from 5080lab/ECS must pass `provider-probe-evidence-validate` before any summary or gate result is promoted; the validator rejects passing gate text that omits or empties the required profile, modality, fallback, `source_ref` or `source_state` parameters, then recomputes the gate from the embedded reports using those parameters and checks the row/profile/provider counts match.
- Remote evidence promotion should use `provider-probe-evidence-summary` so rows are regenerated from validated JSON reports and tied to archive SHA256.
- `provider-probe-package` writes the safe promotion Markdown as `provider-probe-evidence-summary.md` in the run report directory; the tarball remains limited to raw validated reports plus summary/gate text.
- 5080lab/ECS runs should start from `provider-probe-package` so the execution scripts, README and manifest are reproducible; package generation validates selected profiles exist in the gateway config, the manifest records safe `config_path`, `source_ref`, `source_state`, `run_delay_ms` and `requires_asr_fixture`, runners pass config into `provider-probe-matrix`, copy provenance into `provider-probe-gate.txt`, the command rejects dirty package directories, and the package does not carry secrets, provider env files, spoken fixtures, raw audio, transcripts or generated text.
- When `provider-probe-package` is generated from `git archive` or another source snapshot without `.git`, operators must pass explicit `--source-ref <commit>` and `--source-state clean`; evidence with `source_ref=unavailable` is diagnostic-only and must be rerun before promotion.
- The package includes `run-provider-probes.sh` for Linux/ECS and `run-provider-probes.ps1` for Windows 5080lab; both runners must preserve the same matrix, summary, gate, archive validation and promotion-summary flow.
- Package runners default unset Go dependency mirrors to `GOPROXY=https://goproxy.cn,direct` and `GOSUMDB=sum.golang.google.cn` so 5080lab does not stall on `proxy.golang.org`; explicit remote env values may override them.
- Package runners use `provider-probe-matrix --allow-failed-profiles` so failed providers still leave validated reports, safe summaries and a non-empty `provider-probe-gate.txt` diagnostic before `provider-probe-gate` rejects production selection.
- Gate-failure diagnostic archives are named `provider-probe-diagnostics-<RUN_ID>.tgz`, must pass `provider-probe-diagnostics-validate`, and are troubleshooting artifacts only; production promotion still requires a passing `provider-probe-evidence-<RUN_ID>.tgz` validated by `provider-probe-evidence-validate`.
- Package runners execute a small Go self-test by default; `PROVIDER_PROBE_SKIP_SELF_TEST=1` is only for automation that has already run equivalent tests in the same source tree.
- `provider-probe-matrix` is the preferred 5080lab/ECS entrypoint for current reports because it runs multiple profiles, validates every report, and bridges old `A21_LAB_*` env-file names to current gateway provider env names.
- `device-provisioning-check` is the preferred first gate for new physical StackChan units. It listens on the xiaozhi WebSocket path, requires the configured device token, avoids VoiceLoop, reports URL/auth/hello/identity-match readiness, and prints only identity hashes unless `--show-device-identity` is explicitly passed for local pairing. It must pass before a new unit's fixture capture can be promoted.
- `asr-fixture-capture` is the preferred spoken-ASR fixture entrypoint. It refuses to start when `--output` is neither ignored by git nor under durable ECS `/var/lib/a21-air/fixtures/asr/`, listens on the configured xiaozhi WebSocket path, prints a safe ready line with `connect_url`, configured `device_id`, configured `client_id` and the auth environment variable name, reuses gateway device auth and binary Opus parsing, writes `xiaozhi_opus_frames_v1` JSON under ignored `server/var/fixtures/asr/` or durable ECS `/var/lib/a21-air/fixtures/asr/`, refuses captures that fail the semantic fixture gate, and does not decode, resample, re-encode or log the audio payload or token value. Device Authorization may be either the raw configured token or `Bearer <token>`, matching xiaozhi-esp32's default Bearer prefix behavior; device `Device-Id` and `Client-Id` headers must both match configuration before the device is accepted. Auth failures print only HTTP status, fixed error code and header presence booleans. It does not start the admin listener, so capture requires the device auth token but not `STACKCHAN_ADMIN_TOKEN`; normal gateway runtime and provider probe admin APIs still require admin auth. When it listens on `0.0.0.0` or `::` for a physical StackChan, `--advertise-url ws://<reachable-host>:<port>/<path>` or `wss://...` is required before the capture server starts; advertised URL paths must match the capture WebSocket path and must not include user info, query parameters or fragments.
- Real ASR semantic probes must pass `asr-fixture-validate` before either standalone `provider-probe` or batch `provider-probe-matrix`; the validator reports only counts and rejects short, tiny, low-diversity or repeated placeholder fixtures.
- ASR semantic probes should use `--asr-opus-fixture` with `xiaozhi_opus_frames_v1` JSON: 16 kHz, 60 ms Opus frames, each encoded as `payload_hex` or `payload_base64`. Frame payloads are input-only and must not be copied into reports or logs.
- Transport failures are recorded as `network_error`, not `provider_error`, so network reachability issues can be separated from HTTP provider failures without logging raw URLs, API keys or provider payloads.

## Current Default Voice Evidence

ECS default-profile evidence on 2026-06-07 used the real StackChan `xiaozhi_opus_frames_v1` fixture captured from the configured hardware, source ref `44b1440`, source state `clean`, profile `siliconflow-dashscope-voice`, 20 runs, and gate parameters `--min-runs 20 --min-success-percent 95 --require-profiles siliconflow-dashscope-voice --require-modalities asr,llm,tts`. Report path on ECS: `/tmp/a21-air-provider-evidence-202606071845/provider-probe-20260607-183109.json`.

| Profile | Provider | Modality | Runs | Successes | First p50 / p95 | Total p50 / p95 | Gate implication |
|---|---|---|---:|---:|---:|---:|---|
| `siliconflow-dashscope-voice` | `dashscope-asr` | ASR | 20 | 20 | 303 / 429 ms transcript | 1016 / 1732 ms | Default ASR path passes with Xiaozhi raw Opus decoded to provider PCM. |
| `siliconflow-dashscope-voice` | `siliconflow-llm` | LLM | 20 | 20 | 124 / 258 ms token | 139 / 285 ms | Default hot-path LLM remains within voice latency budget. |
| `siliconflow-dashscope-voice` | `dashscope-tts` | TTS | 20 | 20 | 489 / 649 ms audio | 1041 / 1478 ms | Default TTS path passes provider evidence. Physical no-audio root cause was provider Opus-like passthrough; current gateway requests PCM and encodes Xiaozhi raw Opus, with real device audio playback confirmed on 2026-06-07. |

## Current LLM Fallback Evidence

ECS LLM-only fallback evidence on 2026-06-08 used source ref `aa00ec56d781`, source state `clean`, profiles `siliconflow-llm,dashscope-llm`, 20 runs, timeout 8000 ms, gate parameters `--min-runs 20 --min-success-percent 80 --require-profiles siliconflow-llm,dashscope-llm --require-modalities llm --require-fallback-modality llm`, and evidence archive `/var/lib/a21-air/provider-probes/provider-probe-evidence-ecs-llm-fallback-siliconflow-dashscope-flash-aa00ec5-20260607T161712Z.tgz`. Archive SHA256: `aa0b2e61da56f5112abd0abcc8ac324b5c54b2df197fb3616d61ef8e27fc4bdf`.

| Profile | Provider | Model | Runs | Successes | First token p50 / p95 | Total p50 / p95 | Gate implication |
|---|---|---|---:|---:|---:|---:|---|
| `siliconflow-llm` | `siliconflow-llm` | `Qwen/Qwen3.5-35B-A3B` | 20 | 20 | 177 / 240 ms | 204 / 258 ms | Current default hot-path LLM remains fast on ECS. |
| `dashscope-llm` | `dashscope-llm` | `qwen3.6-flash` | 20 | 20 | 177 / 261 ms | 217 / 313 ms | DashScope can be used as the low-latency LLM rollback candidate after the ECS env change from `qwen3.6-plus` to `qwen3.6-flash`. |

Earlier ECS diagnostic on the same source showed `qwen3.6-plus` was not acceptable for the voice fallback path: DashScope LLM passed only 19/20, with first token p50/p95 794/3388 ms and one timeout. Keep `qwen3.6-flash` as the DashScope voice fallback model unless newer evidence supersedes it.

This section is LLM-only fallback evidence. Full automatic voice fallback still needs either a fresh full ASR/LLM/TTS fallback-profile evidence package or an explicit policy decision that reuses the already-passed default DashScope ASR/TTS evidence because the configured fallback profile shares those providers. The admin provider profile catalog now exposes safe `shared_default_voice_io` and `llm_only_fallback` booleans so that policy decision can be audited from config without exposing credentials or enabling automatic fallback by itself.

## LLM Providers

| Provider ID | Scope | Endpoint / Base URL | Auth | Model Rule | Stream Shape | Error Shape | Checked Source |
|---|---|---|---|---|---|---|---|
| `dashscope-llm` | 百炼 / DashScope OpenAI-compatible chat | `POST https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions` or SDK base URL `https://dashscope.aliyuncs.com/compatible-mode/v1` | `Authorization: Bearer $DASHSCOPE_API_KEY` | Use official model ids. ECS voice fallback currently uses `qwen3.6-flash`; `qwen3.6-plus` is not selected for voice fallback because current ECS evidence showed p95 first token 3388 ms and one timeout. | OpenAI-compatible chat completion chunks when `stream:true`; usage-only chunks may appear when usage is requested and must be ignored for text output. | DashScope error docs include request format, auth, arrearage/balance, model not found, rate/parameter errors; map by HTTP status plus `code`/`message` when present. | [Aliyun OpenAI compatibility](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope), [Aliyun text generation](https://help.aliyun.com/zh/model-studio/text-generation), [Aliyun error codes](https://help.aliyun.com/zh/model-studio/error-code) |
| `doubao-llm` | 火山方舟 / Ark chat | `POST https://ark.cn-beijing.volces.com/api/v3/chat/completions`; SDK base URL `https://ark.cn-beijing.volces.com/api/v3` | `Authorization: Bearer $ARK_API_KEY` | Adapter accepts either official Model ID or `ep-*` Endpoint ID; official examples include `doubao-seed-2-0-lite-260215` and `doubao-seed-1-6-251015`, while 5080lab used endpoint id `ep-20260530213814-r9mhz`. | OpenAI-compatible `stream:true` chat completion chunks; parser emits `delta.content` text and final chunks, ignores usage-only/no-choice chunks. | Error docs list HTTP status plus `code`/`message`; adapter maps status and sanitized `code`/`message`. | [Ark Base URL/auth](https://www.volcengine.com/docs/82379/1298459), [Ark Chat API](https://www.volcengine.com/docs/82379/1302010), [Ark streaming](https://www.volcengine.com/docs/82379/2123275), [Ark error codes](https://www.volcengine.com/docs/82379/1282849) |
| `minimax-llm` | MiniMax OpenAI-compatible chat | `POST https://api.minimaxi.com/v1/chat/completions` | `Authorization: Bearer <token>` | Official page lists `MiniMax-M3`, `MiniMax-M2.7`, `MiniMax-M2.7-highspeed`, `MiniMax-M2.5`, `MiniMax-M2.5-highspeed`, `MiniMax-M2.1`, `MiniMax-M2.1-highspeed`, `MiniMax-M2`. Adapter default is `MiniMax-M3`, sends configurable `max_completion_tokens`, and defaults `thinking.type` to `disabled` for voice latency. The exact off switch is inferred from the official API `thinking` object plus the official M3 release note that thinking can be toggled off; provider-probe must verify this before hot routing. | `stream:true` data-only SSE; parser emits `choices[0].delta.content`, emits final on non-empty `finish_reason`, ignores no-choice chunks, and terminates on `[DONE]`. | Response includes `base_resp.status_code/status_msg`; adapter maps `base_resp.status_code` to string error code, sanitizes `status_msg`, and also supports OpenAI-style `code/message` fallbacks. | [MiniMax chat completions](https://platform.minimaxi.com/docs/api-reference/text-chat-openai), [MiniMax error codes](https://platform.minimaxi.com/docs/api-reference/errorcode), [MiniMax M3 release note](https://www.minimax.io/blog/minimax-m3) |
| `stepfun-llm` | StepFun chat | `POST https://api.stepfun.com/v1/chat/completions`; SDK base URL `https://api.stepfun.com/v1` | `Authorization: Bearer $STEPFUN_API_KEY` or `$STEP_API_KEY` in examples | Examples include `step-3.7-flash`, `step-3.5-flash`, and `step-audio-2` for multimodal audio; adapter default is `step-3.7-flash` and accepts optional `reasoning_format` plus `reasoning_effort`. | OpenAI-compatible SSE when `stream:true`; parser handles `data:` chunks, `delta.content`, reasoning-only `delta.reasoning` / `delta.reasoning_content` chunks, finish reasons, and `[DONE]`. | Official error classes include 400, 401, 402, 404, 429, 451, 500, 503 and developer guide also mentions 504; adapter maps HTTP status plus sanitized `code`/`message` when present. | [StepFun chat completions](https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create), [StepFun streaming chunk](https://platform.stepfun.com/docs/zh/api-reference/chat/streaming), [StepFun stream guide](https://platform.stepfun.com/docs/zh/guides/developer/stream), [StepFun error codes](https://platform.stepfun.com/docs/zh/api-reference/error-codes), [StepFun exception guide](https://platform.stepfun.com/docs/zh/guides/developer/exception) |
| `siliconflow-llm` | SiliconFlow OpenAI-compatible chat | `POST https://api.siliconflow.cn/v1/chat/completions`; SDK/API base URL `https://api.siliconflow.cn/v1`; API Reference defines request/auth fields, and official CN stream-mode docs confirm the mainland endpoint. | `Authorization: Bearer $SILICONFLOW_API_KEY` | Adapter default is `Qwen/Qwen3.5-35B-A3B`: it appears in the corrected 2026-06-06 5080lab `/v1/models` exact-match scan and has the strongest clean 20-run/formal package latency evidence. The adapter sends `max_tokens:160` and `enable_thinking:false` by default for voice fallback. Do not use SiliconFlow `Thinking`, `R1`, `Pro/deepseek-*`, coder, VL, or Omni models as defaults without fresh provider-probe evidence. | OpenAI-compatible data-only SSE when `stream:true`; parser emits `choices[0].delta.content`, skips reasoning-only and usage-only chunks, emits final on non-empty `finish_reason`, and terminates on `[DONE]`. | Official error docs expose numeric `code` and `message`; adapter maps HTTP status plus sanitized `code/message`, and redacts API key material from provider messages. | [SiliconFlow chat completions](https://docs.siliconflow.com/en/api-reference/chat-completions/chat-completions_copy), [SiliconFlow stream-mode](https://docs.siliconflow.cn/en/userguide/capabilities/stream-mode), [SiliconFlow Qwen3/use thinking](https://docs.siliconflow.cn/en/usercases/use-qwen3), [SiliconFlow error codes](https://docs.siliconflow.cn/en/faqs/error-code) |
| `moonshot-llm` | Moonshot / Kimi OpenAI-compatible chat | `POST https://api.moonshot.cn/v1/chat/completions`; SDK/API base URL `https://api.moonshot.cn/v1`. | `Authorization: Bearer $MOONSHOT_API_KEY` | Adapter default is `moonshot-v1-8k` because older 5080lab 30-run evidence showed usable fallback latency and official Moonshot V1 pricing/FAQ still document the 8k model. It sends `max_completion_tokens:160`, never sends deprecated `max_tokens`, and does not send Kimi `thinking` by default; `thinking` is only for explicit future experiments. | OpenAI-compatible data-only SSE when `stream:true`; parser emits `choices[0].delta.content`, skips reasoning-only and usage-only/no-choice chunks, emits final on non-empty `finish_reason`, and terminates on `[DONE]`. | Official errors use JSON `error.type` plus `error.message`; docs list 400, 401, 403, 404, 429 and 500 classes including auth, quota, rate-limit and overload. Adapter maps HTTP status plus sanitized `type/code/message` and redacts API key material from provider messages. | [Kimi API overview](https://platform.kimi.com/docs/api/overview), [Kimi chat completions](https://platform.kimi.com/docs/api/chat), [Kimi error docs](https://platform.kimi.com/docs/api/errors), [Moonshot V1 pricing](https://platform.kimi.com/docs/pricing/chat-v1) |
| `deepseek-llm` | DeepSeek chat | `POST https://api.deepseek.com/chat/completions`; OpenAI base URL `https://api.deepseek.com` | `Authorization: Bearer ${DEEPSEEK_API_KEY}` | Current models: `deepseek-v4-flash`, `deepseek-v4-pro`; `deepseek-chat` and `deepseek-reasoner` are marked for deprecation on 2026-07-24 15:59 UTC. Adapter default is `deepseek-v4-flash`, sends configurable `max_tokens`, and defaults `thinking.type` to `disabled` for voice latency because official docs say thinking defaults to enabled. Optional `reasoning_effort` is only sent when thinking is enabled. | Data-only SSE when `stream:true`; terminates with `data: [DONE]`; parser emits `delta.content`, skips `delta.reasoning_content` and empty-choices usage chunks, and emits final on non-empty `finish_reason`. | Official HTTP classes: 400 invalid format, 401 auth, 402 balance, 422 invalid params, 429 rate limit, 500 server, 503 overload; adapter maps nested `error.code/type/message` and sanitized HTTP fallback text. | [DeepSeek first call](https://api-docs.deepseek.com/), [DeepSeek chat completion](https://api-docs.deepseek.com/api/create-chat-completion), [DeepSeek errors](https://api-docs.deepseek.com/quick_start/error_codes), [DeepSeek models](https://api-docs.deepseek.com/api/list-models) |
| `anthropic-llm` | Claude Messages API | `POST https://api.anthropic.com/v1/messages`; SDK/API base URL `https://api.anthropic.com` | `x-api-key: $ANTHROPIC_API_KEY`, `anthropic-version: 2023-06-01`, `Content-Type: application/json`; never `Authorization: Bearer` for direct Claude API | Official models overview lists Claude Opus 4.8, Sonnet 4.6, and Haiku 4.5; adapter default is `claude-sonnet-4-6` as the best speed/intelligence balance, with configurable `max_tokens`. Sampling params are intentionally omitted because current Opus 4.7/4.8 docs reject non-default sampling values. | Anthropic typed SSE when `stream:true`: `event:` plus JSON `data:`; parser emits only `content_block_delta` with `delta.type:"text_delta"`, skips ping/thinking/tool-json deltas, emits final on `message_stop`, and maps stream `event:error` to `ProviderError`. It must not reuse OpenAI SSE parser. | Official error JSON has top-level `type:error`, nested `error.type/message`, and `request_id`; HTTP classes include 400, 401, 402, 403, 404, 413, 429, 500, 504, 529. Adapter maps sanitized `error.type/message/request_id`. | [Claude messages](https://platform.claude.com/docs/claude/reference/messages_post), [Claude streaming](https://platform.claude.com/docs/en/build-with-claude/streaming), [Claude errors](https://platform.claude.com/docs/en/api/errors), [Claude models](https://platform.claude.com/docs/en/docs/about-claude/models), [Opus 4.8 notes](https://platform.claude.com/docs/en/about-claude/models/whats-new-claude-4-8) |

### 5080lab LLM Latency Evidence

Latest StepFun smoke, measured 2026-06-05 from copied 5080lab reports:

| Source | Provider | Protocol | Runs | First byte p50 / p95 | First content p50 / p95 | Total p50 / p95 | Mainline implication |
|---|---|---|---:|---:|---:|---:|---|
| `a21-provider-smoke-20260605-225111-334580400.json` | StepFun | OpenAI-compatible chat completions, streaming | 3/3 | 133.165 / 251.357 ms | 177.277 / 304.399 ms | 314.249 / 406.981 ms | Promote `stepfun-llm` ahead of slower text providers for adapter implementation, but still require current official-doc verification and in-repo `provider-probe` before production routing. |

Legacy tarball audit on 2026-06-06: `provider-probe-evidence-validate --archive "/Users/jiyurun/Documents/New project/reports/a21-5080lab-provider-evidence-20260605-225138.tgz"` failed with an actionable legacy-smoke rejection. This is expected and preserves the rule that only current Go `provider-probe-package` output can enter the production gate.

Current Go 5080lab LLM-only smoke, measured 2026-06-06 with `provider-probe-package` on the Windows 5080lab host and copied into ignored `server/var/reports/5080lab-llm-20260605T234656Z/`:

| Source | Profile | Provider | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-074709.json` | `stepfun-llm` | `stepfun-llm` | 3/3 | 1889 / 1948 ms | 1891 / 1948 ms | Passed `provider-probe-validate` | Current Go adapter works from 5080lab, but first-token latency is much slower than the old Python smoke; keep StepFun as implementation lead, not production-selected. |
| `provider-probe-20260606-074710.json` | `deepseek-llm` | `deepseek-llm` | 0/3 | n/a | n/a | Passed `provider-probe-validate`; earlier schema reported only `provider_error` | Current Go DeepSeek path is not viable for fallback until provider account state is fixed and rerun. |

5080lab LLM-only gate status: `provider-probe-gate --min-runs 3 --min-success-percent 80 --require-profiles stepfun-llm,deepseek-llm --require-modalities llm --require-fallback-modality llm` failed because `deepseek-llm` had no successful probes. This is observation evidence only; it does not select a production provider profile and does not satisfy the full ASR/LLM/TTS gate.

DeepSeek follow-up diagnostic, measured 2026-06-06 after adding safe provider HTTP status/code metadata and copied into ignored `server/var/reports/5080lab-deepseek-diagnostic-20260606-075926/`:

| Source | Profile | Provider | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-075928.json` | `deepseek-llm` | `deepseek-llm` | 0/1 | n/a | n/a | Passed `provider-probe-validate`; `provider-probe-summary` reports `provider_error:http_402:invalid_request_error` | DeepSeek official error docs map HTTP 402 to insufficient balance, so this blocks DeepSeek as fallback until the API account balance/billing state is fixed and the current Go probe is rerun. |

Additional 5080lab LLM fallback diagnostics, measured 2026-06-06 after adding LLM-only profiles and copied into ignored `server/var/reports/5080lab-*-llm-diagnostic-*/`:

| Source | Profile | Provider | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-081443.json` | `doubao-llm` | `doubao-llm` | 3/3 | 5976 / 7314 ms | 6138 / 7452 ms | Passed `provider-probe-validate` | Doubao is a working current-Go fallback candidate but too slow for P0 hot path unless endpoint/model provisioning improves. |
| `provider-probe-20260606-081405.json` | `dashscope-llm` | `dashscope-llm` | 0/3 | n/a | n/a | Passed `provider-probe-validate`; `provider-probe-summary` reports `timeout` | DashScope LLM did not finish within the 8000 ms probe timeout and should not be selected for the hot LLM fallback without a faster model/config. |
| `provider-probe-20260606-081214.json` | `minimax-llm` | `minimax-llm` | 0/3 | n/a | n/a | Passed `provider-probe-validate`; earlier schema reported only `provider_error`; 5080lab/local provider env names show no MiniMax API key present | MiniMax remains a configured LLM-only burn-down profile, but cannot be judged until `MINIMAX_API_KEY` / `A21_LAB_MINIMAX_API_KEY` is supplied. |
| `provider-probe-20260606-082304.json` | `minimax-llm` | `minimax-llm` | 0/1 | n/a | n/a | Passed `provider-probe-validate`; `provider-probe-summary` reports `provider_config_error` | New probe classification separates missing provider credentials/local transcoders from provider HTTP failures without exposing env values. |

LLM-only fallback gate status: `provider-probe-gate --min-runs 3 --min-success-percent 80 --require-profiles stepfun-llm,doubao-llm --require-modalities llm --require-fallback-modality llm` previously passed using current Go 5080lab StepFun and Doubao reports. Later package runs failed the same gate, so this earlier pass is historical observation evidence only, not a stable fallback selection. Full production selection still requires ASR/LLM/TTS coverage, a real spoken `xiaozhi_opus_frames_v1` fixture, evidence tarball validation, and a passing full provider gate.

Latest current-Go PowerShell package observation on 5080lab, measured 2026-06-06 09:40 CST from a working-tree snapshot that fixed Windows diagnostics handling and copied into ignored `server/var/reports/5080lab-current-psfix3-20260606T0941/`:

| Source | Profile | Provider | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-094027.json` | `stepfun-llm` | `stepfun-llm` | 3/3 | 863 / 2824 ms | 863 / 2826 ms | Diagnostics tarball passed remote and local `provider-probe-diagnostics-validate` | StepFun remains the leading implemented LLM path, but p95 jitter still needs more runs before production routing. |
| `provider-probe-20260606-094049.json` | `doubao-llm` | `doubao-llm` | 1/3 | 5794 / 5794 ms | 5951 / 5951 ms | Diagnostics tarball passed remote and local `provider-probe-diagnostics-validate`; gate failed with `timeout` | Doubao is a working but unstable/slow fallback candidate; do not treat it as P0-selected until a faster endpoint/model/config passes the current gate. |

The 09:40 LLM-only gate failed because `doubao-llm` success rate was 1/3 below 80%. A separate MiniMax config-failure package on the same 5080lab snapshot produced a validated diagnostics tarball with `provider_config_error`, proving the Windows package runner now reaches diagnostics validation on expected gate failures.

Latest clean-source LLM-only package observation on 5080lab, measured 2026-06-06 10:38 CST from package source ref `a355609b88df` and copied into ignored `server/var/reports/5080lab/provider-probe-diagnostics-llm-a355609-20260606T1038.tgz`:

| Source | Profile | Provider | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-103859.json` | `stepfun-llm` | `stepfun-llm` | 5/5 | 2572 / 3461 ms | 2572 / 3463 ms | Diagnostics tarball passed remote and local `provider-probe-diagnostics-validate` | StepFun still works through the current Go gateway on 5080lab, but this 5-run p95 is too high to call P0 low-latency without more tuning or a faster model/config. |
| `provider-probe-20260606-103924.json` | `doubao-llm` | `doubao-llm` | 0/5 | n/a | n/a | Diagnostics tarball passed remote and local `provider-probe-diagnostics-validate`; gate failed with `timeout` | Doubao is not a reliable P0 fallback in the current 5080lab provider configuration. Keep it in burn-down only until a faster endpoint/model/config passes current Go gate runs. |

The latest read-only 5080lab Desktop scan at 2026-06-06 10:36 CST still found no real `spoken-opus.json` fixture and no passing `provider-probe-evidence-*.tgz`; only diagnostics tarballs were present. A key-name-only scan of the private 5080lab provider env found StepFun, DeepSeek, DashScope, Volcengine/Ark, Moonshot and SiliconFlow keys, but no MiniMax or Anthropic key names; no secret values were printed. No full ASR/LLM/TTS evidence tarball is available yet.

Current model-list and voice-default check, measured 2026-06-06 17:09 CST on 5080lab using provider `/models` APIs and current Go `provider-probe`:

| Provider | Model-list observation | Small probe result | Mainline decision |
|---|---|---|---|
| SiliconFlow | `/v1/models` returned 93 ids. Corrected exact-match checks confirmed `Qwen/Qwen3.5-35B-A3B`, `Qwen/Qwen3-30B-A3B-Instruct-2507`, `Qwen/Qwen3-Coder-30B-A3B-Instruct`, `deepseek-ai/DeepSeek-V4-Flash`, and `Pro/deepseek-ai/DeepSeek-V3` are all current ids; the list also includes Thinking, VL/Omni, R1 and Pro variants. | The exploratory 3-run sweep found `Qwen/Qwen3-30B-A3B-Instruct-2507` at 321/1688 ms, `deepseek-ai/DeepSeek-V4-Flash` at 714/2074 ms, `Pro/deepseek-ai/DeepSeek-V3` at 1085/7316 ms, and coder at 268/8485 ms. A follow-up default probe with legacy `A21_SILICONFLOW_MODEL` present confirmed the gateway still uses `Qwen/Qwen3.5-35B-A3B`, 3/3 success, first token 127/140/218 ms. | Keep the StackChan default SiliconFlow model as `Qwen/Qwen3.5-35B-A3B`, because it is current in `/models`, has stronger clean 20-run/formal package evidence, and remains fast in the final default probe. Do not let legacy `A21_SILICONFLOW_MODEL=Pro/deepseek-ai/DeepSeek-V3` override the voice path; it is a useful tool-layer model but too slow and bursty for default speech. |
| DashScope | OpenAI-compatible `/models` returned 212 ids including `qwen-flash`, `qwen-turbo`, `deepseek-v4-flash`, and `deepseek-v4-pro`. | Current direct gateway TTS probe on 5080lab succeeded with DashScope `cosyvoice-v3-flash` first audio 438 ms and total 936 ms for a short text. | Keep DashScope TTS in the default profile. Real fixture/ECS provider evidence and physical audio retest now pass, but final acceptance still requires the validated physical report. |
| DeepSeek | `/models` returned `deepseek-v4-flash` and `deepseek-v4-pro`. | Previous 5080lab current-Go DeepSeek diagnostic failed with `provider_error:http_402:invalid_request_error`. | Treat DeepSeek as account/billing blocked and not a default/fallback voice model until billing state and fresh probe evidence are fixed. |
| StepFun | `/v1/models` returned `step-3.5-flash`, `step-3.7-flash`, `stepaudio-2.5-*`, `step-audio-2-mini`, and `step-audio-2-think`. | Prior clean StepFun current-Go runs had high p95 and/or 429 rate limits. | Prefer explicit flash/audio non-think candidates for future burn-down; do not select `*-think` or reasoning-heavy configs for default voice. |

Initial SiliconFlow adapter burn-down, measured 2026-06-06 10:53 CST on 5080lab from a dirty SiliconFlow implementation snapshot and copied into ignored `server/var/reports/5080lab-siliconflow-20260606T1053/`:

| Source | Profile | Provider | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-105330.json` | `stepfun-llm` | `stepfun-llm` | 5/5 | 1404 / 2113 ms | 1406 / 2113 ms | Local and remote report validation passed; historical 5-run LLM gate passed with SiliconFlow | Superseded by clean 20-run evidence; do not promote StepFun until rate-limit/config issues are fixed. |
| `provider-probe-20260606-105331.json` | `siliconflow-llm` | `siliconflow-llm` | 5/5 | 129 / 199 ms | 145 / 217 ms | Local and remote report validation passed; LLM fallback gate passed with StepFun | SiliconFlow is the strongest current mainland LLM hot-path candidate; keep it in the next default package and rerun from a clean commit before production evidence. |

Historical dirty-snapshot LLM-only gate status: `provider-probe-gate --min-runs 5 --min-success-percent 80 --require-profiles stepfun-llm,siliconflow-llm --require-modalities llm --require-fallback-modality llm` passed locally and on 5080lab for the same reports. This proved the adapter shape quickly, but it is superseded by the clean 20-run evidence below and does not satisfy the full ASR/LLM/TTS production gate.

Clean-source SiliconFlow burn-down, copied into ignored `server/var/reports/5080lab-siliconflow-clean-af64bf5-20260606T1105/` and `server/var/reports/5080lab-siliconflow-clean-9270f9d-delay1000-20260606T1111/`:

| Source | Source ref / state | Cadence | Profile | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-110545.json` | `af64bf5313e3` / `clean` | 20-run burst, 5000 ms timeout, no explicit run delay | `stepfun-llm` | 9/20 | 2062 / 4177 ms | 2062 / 4177 ms | Local and remote validation passed; LLM gate failed with `provider_error:http_429:rate_limited,timeout` | StepFun cannot be promoted as current P0 fallback under burst evidence. |
| `provider-probe-20260606-110548.json` | `af64bf5313e3` / `clean` | 20-run burst, 5000 ms timeout, no explicit run delay | `siliconflow-llm` | 20/20 | 134 / 195 ms | 150 / 211 ms | Local and remote validation passed | SiliconFlow remains the strongest current mainland LLM hot-path candidate. |
| `provider-probe-20260606-111146.json` | `9270f9dc9644` / `clean` | 20 runs, 8000 ms timeout, `run_delay_ms=1000` | `stepfun-llm` | 10/20 | 1375 / 6129 ms | 1385 / 6129 ms | Local and remote validation passed; LLM gate failed with `provider_error:http_429:rate_limited` | Even paced low-QPS evidence does not clear StepFun's current rate-limit/account state; downgrade to observation candidate until rate limit/config is fixed. |
| `provider-probe-20260606-111209.json` | `9270f9dc9644` / `clean` | 20 runs, 8000 ms timeout, `run_delay_ms=1000` | `siliconflow-llm` | 20/20 | 132 / 326 ms | 148 / 341 ms | Local and remote validation passed | SiliconFlow is stable across burst and paced clean-source runs, but it still needs a second passing LLM provider before fallback gate can pass. |

Clean-source SiliconFlow+Moonshot LLM fallback gate, measured 2026-06-06 11:37 CST on 5080lab from source ref `102d54ea21b2`, copied into ignored `server/var/reports/5080lab-moonshot-clean-102d54e-20260606T113615/`, and locally reproduced with `provider-probe-validate`, `provider-probe-summary` and `provider-probe-gate`:

| Source | Source ref / state | Cadence | Profile | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-113641.json` | `102d54ea21b2` / `clean` | 20 runs, 8000 ms timeout, `run_delay_ms=1000` | `siliconflow-llm` | 20/20 | 144 / 209 ms | 159 / 225 ms | Local and remote validation passed; LLM fallback gate passed with Moonshot | SiliconFlow is the current P0 LLM hot-path candidate. |
| `provider-probe-20260606-113708.json` | `102d54ea21b2` / `clean` | 20 runs, 8000 ms timeout, `run_delay_ms=1000` | `moonshot-llm` | 20/20 | 339 / 691 ms | 339 / 691 ms | Local and remote validation passed; LLM fallback gate passed with SiliconFlow | Moonshot is now the first passing current-Go LLM fallback candidate. |

Manual LLM-only gate status: the clean 20-run gate passed for `siliconflow-llm,moonshot-llm` with `--min-runs 20 --min-success-percent 80 --require-modalities llm --require-fallback-modality llm --source-ref 102d54ea21b2 --source-state clean`. Treat this as report/gate evidence superseded by the formal package evidence below.

Formal LLM-only package evidence, measured 2026-06-06 11:45 CST on 5080lab from source ref `b9155e60c073`, generated with explicit `provider-probe-package --source-ref b9155e60c073 --source-state clean`, copied into ignored `server/var/reports/5080lab-llm-evidence-b9155e-20260606T114449/`, and locally reproduced with `provider-probe-evidence-validate` plus `provider-probe-evidence-summary`:

Archive: `provider-probe-evidence-llm-siliconflow-moonshot-b9155e60c073-20260606T114449.tgz`

SHA256: `f0588f172703630ed242a8b81fc584ff6f0216ff6e4a857a5245eca5b4320927`

| Source | Source ref / state | Cadence | Profile | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---|---:|---:|---:|---|---|
| `provider-probe-20260606-114521.json` | `b9155e60c073` / `clean` | 20 runs, 8000 ms timeout, `run_delay_ms=1000` | `siliconflow-llm` | 20/20 | 141 / 218 ms | 158 / 228 ms | Remote and local evidence tarball validation passed | SiliconFlow remains the current P0 LLM hot-path candidate. |
| `provider-probe-20260606-114548.json` | `b9155e60c073` / `clean` | 20 runs, 8000 ms timeout, `run_delay_ms=1000` | `moonshot-llm` | 20/20 | 347 / 491 ms | 347 / 491 ms | Remote and local evidence tarball validation passed | Moonshot remains the first passing current-Go LLM fallback candidate. |

Current formal LLM-only evidence status: the package evidence tarball passes for `siliconflow-llm,moonshot-llm` with `--min-runs 20 --min-success-percent 80 --require-modalities llm --require-fallback-modality llm --source-ref b9155e60c073 --source-state clean`. This promotes the LLM-only fallback evidence from manual reports to a validated package artifact. It is now superseded for the default voice profile by the 2026-06-07 ECS ASR/LLM/TTS gate, but it remains useful as fallback-strategy evidence; formal physical StackChan acceptance still needs a validated report.

Latest full-profile blocker checks, measured 2026-06-06 11:50-12:34 CST on 5080lab before hardware arrival:

| Source | Scope | Result | Mainline implication |
|---|---|---|---|
| Read-only artifact scan | `C:\Users\21\Desktop` and `C:\Users\21\Documents` | Found the formal LLM-only evidence tarballs and older diagnostics tarballs; found no `spoken-opus.json`, `asr*opus*.json`, or full ASR/LLM/TTS evidence tarball | Historical blocker before hardware capture; superseded for the default profile by the 2026-06-07 ECS fixture and ASR/LLM/TTS gate. |
| Clean source `173f9c072988` default full-profile package | `siliconflow-dashscope-voice,siliconflow-llm,moonshot-llm,stepfun-llm,doubao-llm,dashscope-cosyvoice`, required modalities `asr,llm,tts` | Package runner exited before provider calls with `ASR fixture not found: ./var/fixtures/asr/spoken-opus.json` and printed the safe `asr-fixture-capture` command | Historical proof that full provider selection was correctly gated on spoken fixture capture; no fake ASR/TTS result was promoted before the fixture existed. |
| Read-only artifact rescan at 12:03-12:04 CST | `spoken-opus.json`, `*asr*.json`, `*opus*.json`, `provider-probe-evidence-*.tgz`, `provider-probe-*.json`, and package manifests under `Desktop`/`Documents` | Still found no `spoken-opus.json`, no ASR/Opus JSON fixture, and no full ASR/LLM/TTS evidence tarball; latest provider reports remain LLM-only package reports, and latest full-profile package artifact is only the fail-fast package manifest | Historical blocker before hardware capture; no longer the current default-profile gate status. |
| Count-only artifact rescan at 12:33 CST | `Desktop`, `Downloads`, `Documents` and temp roots | Found two `provider-probe-evidence-*.tgz` archives, latest `provider-probe-evidence-llm-siliconflow-moonshot-b9155e60c073-20260606T114449.tgz`; found five diagnostics archives; `FIXTURE_CANDIDATE_COUNT=0` | Historical scan result only; later ECS fixture capture and default-profile evidence supersede it. |
| Clean source `9eb65efd340b` default full-profile package at 12:34 CST | Same full profile set, required modalities `asr,llm,tts`, explicit `source_ref=9eb65efd340b`, `source_state=clean` | Package generation succeeded; generated README includes raw-token/`Bearer <token>` device Authorization guidance; runner stopped before reports/provider calls with `ASR fixture not found: ./var/fixtures/asr/spoken-opus.json`; report dir contained only `.gitkeep` | Historical proof that source, Bearer auth alignment, package generation and Go mirrors were no longer the blocker before hardware fixture capture. |

Current active gate as of 2026-06-07: physical `AI.Agent` / screen-tap retest on the current StackChan with the deployed ASR no-stop fix, physical acceptance report capture, and fallback/rollback provider evidence where a production policy needs more than the default `siliconflow-dashscope-voice` profile. The current unit is new hardware; keep using its proven configured WebSocket URL, device/client identity and token pairing, and do not reuse previous A21/X21 firmware or identity assumptions.

Source file: `results\llm_stream_30r.jsonl` from 5080lab, measured 2026-05-30 on mainland broadband with 30 successful streaming runs per provider.

| Provider | Model | Runs | First content p50 | First content p95 | Total p50 | Total p95 | Mainline implication |
|---|---|---:|---:|---:|---:|---:|---|
| SiliconFlow | `Qwen/Qwen3.5-35B-A3B` | 30/30 | 217.3 ms | 573.0 ms | 765.9 ms | 1167.4 ms | Excellent historical mainland latency; corrected 2026-06-06 5080lab `/v1/models` exact-match scan confirms the model id is still current. Keep as default until stronger current evidence displaces it. |
| StepFun | `step-3.5-flash` | 30/30 | 307.3 ms | 2939.0 ms | 751.1 ms | 3446.7 ms | Strong first-token candidate; tail jitter must be handled with timeout/fallback. Lab parser had to read `delta.reasoning` in addition to normal `delta.content`. |
| DeepSeek | `deepseek-v4-flash` | 30/30 | 635.3 ms | 831.8 ms | 1790.3 ms | 2060.4 ms | Good mainland LLM fallback with stable first token; update model ids to current `v4` names. |
| DashScope | `qwen3.6-plus` | 30/30 | 778.7 ms | 1003.8 ms | 30602.1 ms | 40158.6 ms | First token acceptable after warm-up, but verbose completion total was large; segmenting and max token limits matter. |
| Moonshot | `moonshot-v1-8k` | 30/30 | 1114.1 ms | 1522.4 ms | 1225.7 ms | 1623.1 ms | Usable fallback; now promoted into current Task 13 LLM burn-down via `moonshot-llm`, pending clean current-Go 5080lab gate evidence before routing. |
| Volcengine Ark | `ep-20260530213814-r9mhz` | 30/30 | 2953.9 ms | 4733.2 ms | 13287.4 ms | 17608.7 ms | Do not choose current Ark endpoint for P0 hot voice path without warm endpoint/provisioning proof. |

Lab configuration notes:

- `configs\llm_providers.json` used StepFun `step-3.5-flash`, DeepSeek `deepseek-v4-flash`, DashScope `qwen3.6-plus`, and Volcengine endpoint id `ep-20260530213814-r9mhz`.
- Lab report notes that Volcengine Ark required an `ep-*` endpoint id in that setup, not a bare model name.
- Lab probe `scripts\openai_chat_probe.py` included parser compatibility for `delta.content`, `delta.reasoning_content`, and `delta.reasoning`.

### Current In-Repo Local Provider-Probe Evidence

These reports were generated on the local Codex Mac, not 5080lab/ECS. They prove current Go gateway adapter/report wiring, but they are not production provider-selection evidence. Raw reports remain ignored under `server/var/reports/`.

| Source | Profile | Provider | Runs | First token p50 / p95 | Total p50 / p95 | Result | Mainline implication |
|---|---|---|---:|---:|---:|---|---|
| `server/var/reports/provider-probe-20260606-062921.json` | `stepfun-llm` | `stepfun-llm` | 3/3 | 2226 / 2575 ms | 2228 / 2576 ms | Passed `provider-probe-validate` | Go adapter and report path work with real StepFun env, but local first-token latency is much slower than prior 5080lab evidence; rerun on 5080lab/ECS before routing. |
| `server/var/reports/provider-probe-20260606-063114.json` | `deepseek-llm` | `deepseek-llm` | 0/1 | n/a | n/a | Passed `provider-probe-validate`; failure class `network_error` | Official DeepSeek docs still list `deepseek-v4-flash`; current Mac could not complete TLS/API transport, so rerun on 5080lab/ECS before judging provider quality. |

Local gate status: these two local reports fail `provider-probe-gate` because `deepseek-llm` has no successful probes. They are wiring/root-cause evidence only, not provider-selection evidence.

## ASR Providers

| Provider ID | Scope | Endpoint / Connection | Auth | Audio In | Stream Shape | Error Shape | Checked Source |
|---|---|---|---|---|---|---|---|
| `dashscope-asr` | 百炼 / Fun-ASR realtime | WebSocket Beijing: `wss://dashscope.aliyuncs.com/api-ws/v1/inference`; Singapore: `wss://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com/api-ws/v1/inference` | `Authorization: Bearer <your_api_key>`; optional `X-DashScope-WorkSpace`; auth checked during WebSocket handshake. | Official docs list `opus` but the audio stream spec requires Ogg container for opus/speex. Xiaozhi uplink is raw 16 kHz mono Opus packets, so the adapter decodes with an explicit libopus-backed Xiaozhi Opus -> PCM dependency and sends `parameters.format:"pcm"` binary audio. Raw Xiaozhi Opus passthrough to DashScope is forbidden. | Client `run-task` with `streaming:"duplex"`, binary PCM audio, `finish-task`; server `task-started`, `result-generated`, `task-finished`; `sentence_end:true` maps to ASR final. | Handshake 401/403 for missing/invalid API key; missing local decoder fails readiness/probe as `provider_config_error`; `task-failed` maps to sanitized `ProviderError`. | [DashScope Fun-ASR WebSocket](https://help.aliyun.com/zh/model-studio/fun-asr-realtime-websocket-api), [DashScope Fun-ASR client events](https://help.aliyun.com/zh/model-studio/fun-asr-client-events), [DashScope Fun-ASR server events](https://help.aliyun.com/zh/model-studio/fun-asr-server-events) |
| `doubao-asr` | 火山边缘大模型网关 / Doubao ASR realtime | WebSocket `wss://ai-gateway.vei.volces.com/v1/realtime?model=<your_model_name>`; platform preset model query is `?model=bigmodel` | `Authorization: Bearer $YOUR_API_KEY`; optional `X-Api-Resource-Id` for third-party/custom channel; resource ids include `volc.bigasr.sauc.duration` and `volc.bigasr.sauc.concurrent`. | Realtime event API currently requires PCM config; example session uses raw PCM, 16 kHz, 16-bit, 1 channel; audio payload is base64 in JSON events. Adapter requires an injected xiaozhi Opus -> PCM decoder and rejects startup without it; it never sends Opus as PCM. | Client `transcription_session.update`, `input_audio_buffer.append`, `input_audio_buffer.commit`; server `transcription_session.updated`, `conversation.item.input_audio_transcription.result`, `conversation.item.input_audio_transcription.completed`. | Realtime `type:"error"` maps sanitized `code`/`message`; historical resource mismatch code `55000000` is covered by fixture; handshake failures are sanitized. | [Doubao ASR Realtime](https://www.volcengine.com/docs/6893/1527759) |

## TTS Providers

| Provider ID | Scope | Endpoint / Connection | Auth | Text In | Audio Out | Stream Shape | Checked Source |
|---|---|---|---|---|---|---|---|
| `dashscope-tts` | 百炼 / CosyVoice realtime | WebSocket Beijing: `wss://dashscope.aliyuncs.com/api-ws/v1/inference`; Singapore: `wss://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com/api-ws/v1/inference` | `Authorization: Bearer <your_api_key>`; optional `X-DashScope-WorkSpace`; auth checked during WebSocket handshake. | Client `run-task` with `task:"tts"`, `function:"SpeechSynthesizer"`, `streaming:"duplex"`, `continue-task` text, then mandatory `finish-task`; all events in a task must use the same `task_id`. | Adapter requests provider `format:"pcm"`, `sample_rate:24000`, mono binary audio, buffers it into 60 ms PCM frames, and uses the injected libopus encoder to emit one Xiaozhi raw Opus packet per downlink frame. Provider Opus passthrough is forbidden because physical StackChan rejected those bytes with ESP Opus decode error `-4`. Env tuning supports official `volume` `[0,100]`, `rate` `[0.5,2.0]` and `pitch` `[0.5,2.0]`. | Server returns `task-started`, `result-generated` subevents; `sentence-synthesis` is immediately followed by one binary audio frame; `task-finished` closes the stream; `task-failed` maps to sanitized `ProviderError`. | [DashScope CosyVoice WebSocket](https://help.aliyun.com/zh/model-studio/cosyvoice-websocket-api), [DashScope CosyVoice client events](https://help.aliyun.com/zh/model-studio/cosyvoice-client-events), [DashScope CosyVoice server events](https://help.aliyun.com/zh/model-studio/cosyvoice-server-events) |
| `doubao-tts` | 火山边缘大模型网关 / Doubao TTS realtime | WebSocket `wss://ai-gateway.vei.volces.com/v1/realtime?model=<your_model_name>`; platform preset model query is `?model=doubao-tts` | `Authorization: Bearer $YOUR_API_KEY`; optional `X-Api-Resource-Id`; resource ids include `volc.service_type.10029`, `volc.megatts.default`, `volc.megatts.concurr`. | Client sends `tts_session.update`, `input_text.append`, `input_text.done`. | `output_audio_format` may be `mp3`, `ogg_opus`, or `pcm`; `output_audio_sample_rate` is explicit. `response.audio.delta` carries base64 audio, not raw xiaozhi frame bytes. Adapter requires an injected provider-audio -> xiaozhi Opus converter before startup and defaults to provider PCM 16 kHz. | Server returns `tts_session.updated`, `response.audio.delta`, `response.audio.done`; realtime `type:"error"` maps sanitized `code`/`message`, with historical `55000000` resource mismatch covered. | [Doubao TTS Realtime](https://www.volcengine.com/docs/6893/1527770) |
| `minimax-tts-http` | MiniMax synchronous TTS HTTP | `POST https://api.minimaxi.com/v1/t2a_v2`; backup `https://api-bj.minimaxi.com/v1/t2a_v2` | `Authorization: Bearer <token>` | Request includes `model`, `text`, `stream`, `voice_setting`, optional `pronunciation_dict`, `audio_setting`, `subtitle_enable`. | Example output defaults to mp3, sample rate 32000, bitrate 128000, channel 1; non-stream output `audio` is hex by default; `output_format` may be `url` or `hex` for non-stream. | HTTP response includes `data.audio`, `extra_info`, `trace_id`, `base_resp`; streaming shape needs fixture capture if `stream:true` is used. | [MiniMax T2A HTTP](https://platform.minimaxi.com/docs/api-reference/speech-t2a-http) |
| `minimax-tts-ws` | MiniMax synchronous TTS WebSocket | WebSocket `wss://api.minimaxi.com/ws/v1/t2a_v2` | `Authorization: Bearer <token>` | Client waits for `connected_success`, then sends `task_start`, `task_continue`, and `task_finish`; adapter default is `speech-2.8-turbo` with voice `male-qn-qingse`. | Adapter requests mp3, 32000 Hz, 128000 bitrate, mono because official examples use that shape. Server `task_continued` carries hex `data.audio` plus `extra_info`; adapter requires an explicit provider-audio -> xiaozhi Opus converter before dialing and never forwards mp3 bytes to the device. | Server events include `connected_success`, `task_started`, `task_continued`, `task_finished`, and `task_failed`; adapter maps `base_resp.status_code/status_msg`, handshake status, cancellation close and secret redaction. | [MiniMax T2A WebSocket](https://platform.minimaxi.com/docs/api-reference/speech-t2a-websocket), [MiniMax T2A WebSocket guide](https://platform.minimaxi.com/docs/guides/speech-t2a-websocket) |

### 5080lab Voice Latency Evidence

Source files: `results\cloud_tts_final_10r.jsonl`, `results\asr_10r.jsonl`, and `results\tts_10r.jsonl` from 5080lab.

| Provider / Component | Runs | First audio / inference p50 | p95 | Complete p50 | Output note | Mainline implication |
|---|---:|---:|---:|---:|---|---|
| Doubao TTS V1 HTTP | 10/10 | 1064.1 ms | 1298.6 ms | 1128.2 ms | Base64 audio, median duration about 3985.5 ms. | HTTP/non-stream shape is too slow for P0 first audible target; keep as fallback or compare against realtime WebSocket. |
| Qwen TTS HTTP | 10/10 | 1094.5 ms | 1132.4 ms | 1094.7 ms | Returns signed audio URL. | Non-stream TTS is too slow for realtime turn-taking; must test CosyVoice/Qwen realtime WebSocket before choosing DashScope TTS. |
| Local sherpa ASR | 10/10 | 24.2 ms inference | 26.3 ms | n/a | First load about 914.6 ms. | Useful lab fallback and benchmark, but production ASR remains cloud provider unless explicitly promoted. |
| Local sherpa TTS | 10/10 | 75.0 ms inference | 81.4 ms | n/a | First load about 1416.7 ms, output 22050 Hz WAV-like audio. | Excellent local benchmark; not production hot path on ECS unless architecture changes. |

Lab voice notes:

- `REPORT_CLOUD_VOICE.md` says old Ark `ark-*` key was not valid for Doubao voice; voice APIs required a separate Doubao voice key or AppID/access token path at that time.
- `cloud_tts_probe.py` tested Qwen HTTP `qwen3-tts-flash` through DashScope multimodal generation and a Doubao HTTP path using `X-Api-Key`.
- Earlier Doubao runs hit code `55000000` with message `resource ID is mismatched with speaker related resource`; fixture tests must cover resource/voice mismatch as a first-class error.
- These measurements do not prove realtime WebSocket first-audio latency. Task 13 must add realtime provider probes before production provider choice.

## Fixture Plan

Each provider package must start with fixtures before adapter code. Fixture files are now checked by `server/internal/providers/provider_fixtures_test.go`.

LLM fixture set:

- `http_headers.json`: required headers with redacted placeholder values.
- `http_request_nonstream.json` and `http_response_nonstream.json`: canonical non-streaming request/response.
- `http_request_stream.json`: canonical streaming request.
- `sse_first_chunk.sse`, `sse_delta_chunk.sse`, `sse_finish_chunk.sse`: raw SSE text, including `data:` lines and provider-specific finish marker.
- `http_error_auth_401.json`: auth failure fixture.
- `http_error_rate_limit_or_overload.json`: rate-limit, overload or quota fixture.
- `cancel_expected.md`: provider-specific cancellation and close behavior.

Realtime ASR fixture set:

- `ws_headers.json`.
- `ws_client_start_or_session_update.json`.
- `ws_client_audio_append.json`.
- `ws_client_finish_or_commit.json`.
- `ws_server_started_or_session_updated.json`.
- `ws_server_first_result.json`.
- `ws_server_finished_or_completed.json`.
- `ws_error_event.json`.
- `http_error_handshake_auth_401.json`.
- `audio_format.json`.
- `cancel_expected.md`.

Realtime/WebSocket TTS fixture set:

- `ws_headers.json`.
- `ws_client_start_or_session_update.json`.
- `ws_client_text_append_or_continue.json`.
- `ws_client_finish_or_text_done.json`.
- `ws_server_started_or_session_updated.json`.
- `ws_server_first_audio_delta.json`.
- `ws_server_audio_done_or_task_finished.json`.
- `ws_error_event.json`.
- `http_error_handshake_auth_401.json`.
- `audio_format.json`.
- `cancel_expected.md`.

Current fixture paths:

- `server/internal/providers/dashscope/testdata/llm/`
- `server/internal/providers/dashscope/testdata/asr/`
- `server/internal/providers/dashscope/testdata/tts/`
- `server/internal/providers/doubao/testdata/llm/`
- `server/internal/providers/doubao/testdata/asr/`
- `server/internal/providers/doubao/testdata/tts/`
- `server/internal/providers/minimax/testdata/llm/`
- `server/internal/providers/minimax/testdata/tts_http/`
- `server/internal/providers/minimax/testdata/tts_ws/`
- `server/internal/providers/stepfun/testdata/llm/`
- `server/internal/providers/deepseek/testdata/llm/`
- `server/internal/providers/anthropic/testdata/llm/`

Provider-specific additions:

- DashScope LLM includes `sse_usage_before_done_chunk.sse` for usage-only stream chunk coverage.
- DashScope ASR uses Fun-ASR `run-task` fixtures with model `fun-asr-realtime`, audio `format:"pcm"`, explicit Xiaozhi raw Opus -> PCM decoder injection, PCM binary audio, `result-generated` sentence parsing, `task-failed` redaction, and `finish-task` close semantics.
- DashScope TTS uses CosyVoice `run-task` fixtures with model `cosyvoice-v3-flash`, voice `longanyang`, provider output `format:"pcm"`, `sample_rate:24000`, `sentence-synthesis` plus binary PCM parsing, required PCM -> Xiaozhi raw Opus encoder injection, context cancel close, `task-failed` redaction, `finish-task` close semantics, and env-driven volume/rate/pitch tuning.
- Doubao LLM uses Ark OpenAI-compatible `chat/completions` fixtures with `ep-*` endpoint-id coverage, `stream:true`, data-only SSE parsing, auth/rate-limit fixtures, response close on cancellation, and secret redaction tests.
- Doubao ASR uses Edge AI Gateway Realtime fixtures with model query `bigmodel`, `transcription_session.update`, base64 PCM `input_audio_buffer.append`, `input_audio_buffer.commit`, partial/result/completed parsing, explicit Opus decoder injection, context cancel close, and secret redaction tests.
- Doubao TTS uses Edge AI Gateway Realtime fixtures with model query `doubao-tts`, `tts_session.update`, `input_text.append`, `input_text.done`, base64 `response.audio.delta`, `response.audio.done`, explicit provider-audio to xiaozhi Opus converter injection, context cancel close, and secret redaction tests.
- StepFun LLM includes `sse_reasoning_delta_chunk.sse` and `sse_reasoning_content_delta_chunk.sse` for `delta.reasoning` / `delta.reasoning_content` parser coverage; reasoning-only chunks are not emitted as user-visible voice text.
- MiniMax LLM uses OpenAI-compatible `chat/completions`, defaults to `MiniMax-M3`, sends `thinking.type:"disabled"` by default for the StackChan voice path, supports configurable `max_completion_tokens`, maps `base_resp.status_code/status_msg`, closes the HTTP stream on context cancellation, and redacts bearer material from provider errors.
- MiniMax TTS WebSocket uses `connected_success` -> `task_start` -> `task_started` -> `task_continue` -> `task_finish`; it parses hex `data.audio` from `task_continued`, records mp3/32000 Hz/mono metadata from `extra_info`, requires an explicit provider-audio to xiaozhi Opus converter, closes on context cancellation, maps `base_resp.status_code/status_msg`, and redacts bearer material from handshake/task errors.
- DeepSeek LLM uses OpenAI-compatible `chat/completions`, defaults to `deepseek-v4-flash`, sends `thinking.type:"disabled"` by default for StackChan voice, supports configurable `max_tokens`, skips reasoning-only chunks and `sse_usage_before_done_chunk.sse`, closes the HTTP stream on context cancellation, and redacts bearer material from provider errors.
- Anthropic LLM uses Claude Messages `POST /v1/messages`, `x-api-key`, `anthropic-version:2023-06-01`, default `claude-sonnet-4-6`, typed SSE parser coverage for `content_block_delta` text, ping/thinking skip, `message_stop` final, `event:error`, request cancellation close, and secret redaction. It intentionally omits sampling params for current Claude 4.x compatibility.
- Doubao ASR/TTS include `ws_error_resource_mismatch_55000000.json`.
- MiniMax TTS is split into `tts_http` fixtures for fallback documentation and a `tts_ws` adapter for the current realtime candidate.

## Open Verification Items

- Volcengine Ark pages were reopened on 2026-06-06; public pages are front-end rendered, so adapter authority is limited to official URLs plus official search-index snippets for endpoint, auth, stream and error shape. Re-run provider probes before choosing Ark for the hot voice path.
- Claude Messages, streaming, errors and models pages were reopened before `anthropic-llm` code; still run provider-probe before routing Claude into any realtime voice profile because cross-border latency may be high.
- MiniMax WebSocket TTS auth/header, event and hex-audio parsing are now fixture-tested, but provider-probe must verify realtime first-audio latency and converter quality before hot routing.
- Private admin probe endpoint and CLI report command exist for ASR/LLM/TTS mock and registry-backed smoke checks, including ASR Opus fixture input. The real StackChan/xiaozhi Opus fixture and ECS default-profile 20-run provider gate now pass; physical device audio acceptance remains open.
- Local default-profile readiness passed on 2026-06-06 with DashScope and SiliconFlow env mapped from existing lab names. The 2026-06-07 ECS no-stop simulator now measures one real-fixture first downlink audio turn at 1532 ms, while simulator-generated fake Opus frames remain rejected as non-evidence.
- Re-run 5080lab provider probes after current env refresh; 2026-05-30/31 lab data is useful ranking evidence but not a substitute for current Task 13 probe artifacts.
- Confirm every selected TTS codec can be decoded/resampled and encoded to xiaozhi downlink Opus before physical StackChan acceptance.
- Decide provider probe run location per profile: 5080lab for mainland burn-down, ECS only for cloud-path latency confirmation.
