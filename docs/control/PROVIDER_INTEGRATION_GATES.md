# Provider Integration Gates

Date: 2026-06-07

## Rule

真实 provider adapter 不能凭记忆、旧项目代码或第三方博客实现。

任何大陆 provider 代码进入仓库前，必须满足以下门槛：

- 当轮重新打开官网文档，只使用官方文档、官方 OpenAPI/SDK 说明或官方控制台给出的接入信息。
- 在 provider matrix 中记录官网 URL、检索日期、endpoint、auth header、model id 规则、stream 格式、错误格式、取消方式和输出音频格式。
- 先写 request/response fixture 测试，再写 adapter；fixture 必须覆盖非流式、流式首包、流式 delta、流式结束包、错误包和取消行为。
- 流式 provider 必须测试 SSE 或 WebSocket parser，不能只测 happy-path JSON。
- 语音 provider 必须记录输入/输出 codec、sample rate、channel、容器格式；不能假设 provider 输出就是 xiaozhi 可播放 Opus。
- 所有真实调用必须经过 `provider-probe`，并记录 p50/p95 first transcript、first token 或 first audio。
- 语音热路径模型必须从当前官方文档或 `/models` 模型清单里选择，优先 Instruct、Flash、Turbo、highspeed 或显式关闭 thinking 的模型；Thinking、Reasoner、R1、Pro-reasoning、coder、VL/Omni 和 `*-think` 模型不得作为默认语音模型，除非当前 5080lab/ECS `provider-probe` 证据证明 p50/p95 首 token 和总时延达标。
- 旧 A21 env 变量只能作为凭据兼容来源；旧模型变量不能静默覆盖 StackChan gateway 默认模型。模型覆盖必须使用当前 gateway env 名并留下 provider-probe 证据。
- 真实调用的运行位置优先为 5080lab 或 ECS；大依赖、SDK、容器、模型文件由 5080lab 国内镜像通道准备。
- provider probe 入口必须挂在私有 admin listener，使用 `STACKCHAN_ADMIN_TOKEN` bearer auth；公网 xiaozhi WebSocket 入口不得暴露 provider probe 或 provider 管理能力。
- `provider-probe` CLI 必须生成 JSON report artifact，且 report 只允许包含 provider id、model/voice id、耗时、transcript/token/audio 长度、frame/byte counts 和 safe error class；transcript 文本、prompt 文本、生成文本、raw payload、Authorization header 和 API key 不得写入。
- 5080lab/ECS 批量跑测必须优先从当前 Go 命令 `provider-probe-package` 生成的执行包开始，再由包内脚本调用 `provider-probe-matrix`、`provider-probe-summary` 和 `provider-probe-gate`；旧 A21 Python latency report 只能作为历史参考，不能替代当前 report artifact。
- `provider-probe-package` 产物只允许包含 `run-provider-probes.sh`、`run-provider-probes.ps1`、`README.md` 和 `manifest.json`；生成命令必须先读取 gateway `config_path` 并校验所有 selected profiles 存在，不能把缺 profile 的错误留给 5080lab/ECS runner；`manifest.json` 必须记录安全的 `config_path`、`source_ref`、`source_state`、`run_delay_ms` 和 `requires_asr_fixture`，包内 runner 必须把 config path 传给 `provider-probe-matrix`、把 provenance 写入 `provider-probe-gate.txt`、把 run delay 传给 matrix，并在 `requires_asr_fixture=true` 时先验证 spoken fixture；生成命令必须拒绝含未知文件的脏目录，不得包含 API key、provider env file、spoken fixture、raw audio、transcript、prompt 或 generated text。
- 从 `git archive`、手工压缩包或其他没有 `.git` 的源码快照生成 package 时，必须显式传 `provider-probe-package --source-ref <commit> --source-state clean`；不得让 production evidence gate 只记录 `source_ref=unavailable`。
- 任何为了避免 provider burst throttling 的节奏控制，都必须显式使用 `--run-delay-ms` 并进入 report/package artifact；在命令外手工 sleep 但 report 不记录 cadence 的结果，不得作为 production selection evidence。
- 5080lab 是 Windows 时必须使用 `run-provider-probes.ps1`，ECS/Linux 使用 `run-provider-probes.sh`；两者必须保持同一套 matrix、summary、gate、evidence tarball validation 和 promotion-summary 输出流程。
- 包内 runner 必须在未显式设置时默认使用 `GOPROXY=https://goproxy.cn,direct` 和 `GOSUMDB=sum.golang.google.cn`，保证 5080lab 走国内 Go 依赖镜像；允许远端环境显式覆盖。
- 包内 runner 必须用 `provider-probe-matrix --allow-failed-profiles`；某个 profile 0 成功时仍要写出已验证 report、summary 和非空 `provider-probe-gate.txt` 诊断，再由 `provider-probe-gate` 给出安全、可复盘的失败原因。
- Windows 包内 runner 必须把 `provider-probe-summary.md`、`provider-probe-gate.txt` 和 `provider-probe-evidence-summary.md` 写成 UTF-8 文本，不能使用 PowerShell 5.1 默认 UTF-16；同时必须在 `provider-probe-gate` 返回非零并写 stderr 时继续进入 diagnostics tarball validation，不得被 `$ErrorActionPreference='Stop'` 提前中断。
- `provider-probe-gate` 失败时，包内 runner 可以生成 `provider-probe-diagnostics-<RUN_ID>.tgz`，但它只允许包含已验证 `provider-probe-*.json`、`provider-probe-summary.md` 和失败的 `provider-probe-gate.txt`；该 tarball 必须通过 `provider-probe-diagnostics-validate`，只用于排障，绝不能作为 production provider evidence 晋升。
- 包内 runner 默认在 provider 调用前执行小范围 Go self-test；只有已经在同一源码树跑过等价测试的自动化才允许设置 `PROVIDER_PROBE_SKIP_SELF_TEST=1`。
- 每份真实 `provider-probe` report 在进入 evidence log 或 provider matrix 摘要前，必须先通过 `provider-probe-validate --report <path>`；该校验器只允许 schema 化延迟/计数字段，并拒绝 prompt/transcript/generated text/raw payload/API key/Authorization/signed URL 等字段或值。
- 进入 provider matrix 或 evidence log 的表格必须由 `provider-probe-summary` 从已验证 report 生成；不要手工从 raw JSON 摘字段。
- 生产 provider profile 选择前必须用 `provider-probe-gate` 对同一批已验证 report 执行最小 run 数、成功率、profile/modality 覆盖和 fallback provider 检查；未过 gate 的报告只能作为观察数据。
- 启用 `providers.auto_fallback.enabled=true` 时，配置校验必须拒绝任何不完整的 fallback profile；LLM-only profile 可以作为探测/rollback 证据候选，但不能成为自动语音 fallback 目标，除非它同时定义 ASR、LLM 和 TTS。
- 从 5080lab/ECS 回传的 evidence tarball 必须通过 `provider-probe-evidence-validate --archive <tgz>`；tarball 只允许包含 `provider-probe-*.json`、`provider-probe-summary.md` 和 `provider-probe-gate.txt`，`provider-probe-gate.txt` 必须记录非空的 profile、modality、fallback、`source_ref` 和 `source_state` 门槛/溯源参数，并且校验器必须用同包 report 重新计算 gate，确认 row/profile/provider 计数一致，不得包含 env、fixture、raw audio、transcript、prompt、generated text 或任意路径穿越条目。
- 从远端 tarball 晋升到 provider matrix 或 evidence log 的表格必须由 `provider-probe-evidence-summary --archive <tgz>` 生成；该命令从已验证 JSON 重新生成 Markdown，并记录 archive SHA256，不信任远端自带 summary 文本。
- `provider-probe-package` 必须将晋升用 Markdown 落盘为 `provider-probe-evidence-summary.md`，但该文件不进入 evidence tarball；如果缺失，必须在本地重新运行 `provider-probe-evidence-summary --archive <tgz>` 生成。
- 传输层失败必须记录为 `network_error`，不要混成 `provider_error`；但仍不得记录原始 URL、Authorization、API key、raw HTTP body 或 provider message。
- 单次 `provider-probe` 和批量 `provider-probe-matrix` 生成 report 后都必须立即执行同一套 report schema 校验；校验失败的 report 不得进入 summary、gate、evidence 或控制文档。
- ASR 语义 provider-probe，包括单次 `provider-probe` 和批量 `provider-probe-matrix`，必须使用 `--asr-opus-fixture` 指向 `xiaozhi_opus_frames_v1` fixture，并且该 fixture 必须先通过 `asr-fixture-validate --fixture <path>`；fixture payload 只能作为输入进入 provider，不能复制进 report、日志或控制文档。provider adapter 必须按官方输入格式显式转换 fixture 音频，不能因为 fixture 是 Xiaozhi Opus 就默认裸传给 ASR provider。
- `asr-fixture-capture` 必须在开始服务前拒绝未被 git ignore 的 `--output` 路径，启动后打印带 `connect_url` 的安全 ready 行，并在写出 fixture 前拒绝过短、过小、低多样性或重复占位帧主导的采集；监听 `0.0.0.0` 或 `::` 给实体 StackChan 采集时必须传 `--advertise-url ws://<reachable-host>:<port>/<path>` 或 `wss://...`，缺失时必须在开始服务前失败，且 advertised URL path 必须匹配 capture WebSocket path、不得包含 userinfo、query 参数或 fragment；捕获 ready、进度和成功输出只能包含路径、`connect_url`、配置中的 `device_id`、`client_id`、认证环境变量名、帧数、字节数、时长、unique payload 计数、最大帧数和超时；认证失败输出只能包含 HTTP status、固定 auth error code 和 header presence 布尔值；不能包含音频 payload、transcript、认证环境变量值、调用方传入的 Authorization、`Device-Id` 或 `Client-Id` 值。
- 实体 StackChan 采集前，采集主机必须把 `STACKCHAN_MAIN_AUTH_TOKEN` 设置为 token 值；网关接受设备端 Authorization 为 raw token 或 `Bearer <token>`，对齐 xiaozhi-esp32 默认 Bearer 前缀行为；设备端 `Device-Id` 与 `Client-Id` header 必须匹配配置中的 `device_id=stackchan-s3-main`、`client_id=stackchan-s3-main-client`；`asr-fixture-capture` 不启动 admin listener，采集命令不得要求 `STACKCHAN_ADMIN_TOKEN`。token 绝不能写进 `connect_url`、日志、report 或控制文档。
- 日志、trace、report 只能记录 provider id、model id、状态码、耗时、transcript/token/audio 长度；不能记录 raw API key、Authorization header、完整 raw payload。

## Official Source Snapshot

以下只是截至 2026-06-07 的接入源快照。实现当天仍要重新打开官网文档复核。协议细节以 `server/docs/provider-matrix.md` 为当前 Task 13 预适配矩阵。

| Provider | Scope | Current Official Endpoint / Shape | Required Auth | Official Source |
|---|---|---|---|---|
| 阿里云百炼 / DashScope | LLM chat | `https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions` under OpenAI-compatible base URL `https://dashscope.aliyuncs.com/compatible-mode/v1`; stream uses OpenAI-compatible chat completion chunks and may include usage-only chunks; voice adapter sends `enable_thinking:false` and `max_completion_tokens:160` by default | `Authorization: Bearer $DASHSCOPE_API_KEY` | `https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions` |
| 阿里云百炼 / DashScope | ASR realtime | WebSocket `wss://dashscope.aliyuncs.com/api-ws/v1/inference`; official docs accept `parameters.format:"opus"` only under the documented audio stream constraints, including Ogg container for opus/speex. Xiaozhi sends raw 16 kHz mono Opus packets, so the gateway adapter must decode Xiaozhi Opus to 16 kHz mono PCM and send `parameters.format:"pcm"` binary audio -> `result-generated` -> `finish-task` -> `task-finished` | `Authorization: Bearer <api_key>`; optional `X-DashScope-WorkSpace` | `https://help.aliyun.com/zh/model-studio/fun-asr-realtime-websocket-api`, `https://help.aliyun.com/zh/model-studio/fun-asr-client-events`, `https://help.aliyun.com/zh/model-studio/fun-asr-server-events` |
| 阿里云百炼 / CosyVoice | TTS realtime | WebSocket `wss://dashscope.aliyuncs.com/api-ws/v1/inference`; `run-task` with `streaming:"duplex"`, provider `format:"pcm"`, `sample_rate:24000` -> `task-started` -> `continue-task` text -> `sentence-synthesis` text event followed by binary PCM audio -> mandatory `finish-task` -> `task-finished`; gateway must encode 60 ms PCM chunks into Xiaozhi raw Opus packets before device downlink | `Authorization: Bearer <api_key>` plus optional `X-DashScope-WorkSpace` | `https://help.aliyun.com/zh/model-studio/cosyvoice-websocket-api`, `https://help.aliyun.com/zh/model-studio/cosyvoice-client-events`, `https://help.aliyun.com/zh/model-studio/cosyvoice-server-events` |
| 火山方舟 / Doubao | LLM chat | `https://ark.cn-beijing.volces.com/api/v3/chat/completions`; model id or `ep-*` endpoint id; `stream:true` uses OpenAI-compatible chat completion chunks; voice adapter sends `thinking.type:"disabled"` and `max_completion_tokens:160` by default and must not also send `max_tokens` | `Authorization: Bearer $ARK_API_KEY` | `https://www.volcengine.com/docs/82379/1302010`, `https://www.volcengine.com/docs/82379/2123288` |
| 火山引擎 / Doubao voice | ASR realtime | Realtime WebSocket `wss://ai-gateway.vei.volces.com/v1/realtime?model=<model>`; official events include `transcription_session.update`, `input_audio_buffer.append`, `input_audio_buffer.commit`, `conversation.item.input_audio_transcription.completed` | `Authorization: Bearer $YOUR_API_KEY` | `https://www.volcengine.com/docs/6893/1527759` |
| 火山引擎 / Doubao voice | TTS realtime | Realtime WebSocket `wss://ai-gateway.vei.volces.com/v1/realtime?model=<model>`; platform preset query can be `?model=doubao-tts`; official events include `tts_session.update`, `input_text.append`, `input_text.done`, `response.audio.delta`, `response.audio.done` | `Authorization: Bearer $YOUR_API_KEY` | `https://www.volcengine.com/docs/6893/1527770` |
| MiniMax | LLM chat | OpenAI-compatible `https://api.minimaxi.com/v1/chat/completions`; legacy text endpoint exists but is not the default for new LLM adapters. M3 `thinking` is explicitly controlled for voice latency; current adapter defaults to `thinking.type:"disabled"` and `max_completion_tokens:160` and must be provider-probed before hot routing. | `Authorization: Bearer <token>` | `https://platform.minimaxi.com/docs/api-reference/text-chat-openai`, `https://www.minimax.io/blog/minimax-m3` |
| MiniMax | TTS | HTTP `https://api.minimaxi.com/v1/t2a_v2` remains fixture-only fallback; WebSocket adapter uses `wss://api.minimaxi.com/ws/v1/t2a_v2`, waits for `connected_success`, sends `task_start`/`task_continue`/`task_finish`, parses hex `data.audio`, and requires provider-audio -> xiaozhi Opus conversion before any device downlink | `Authorization: Bearer <token>` | `https://platform.minimaxi.com/docs/api-reference/speech-t2a-http`, `https://platform.minimaxi.com/docs/api-reference/speech-t2a-websocket`, `https://platform.minimaxi.com/docs/guides/speech-t2a-websocket` |
| StepFun | LLM chat | `https://api.stepfun.com/v1/chat/completions`; stream is SSE `data: {...}` ending with `data: [DONE]`; reasoning chunks can use `delta.reasoning` or `delta.reasoning_content` depending on `reasoning_format`; voice adapter sends `max_tokens:160` by default | OpenAI-compatible bearer key | `https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create`, `https://platform.stepfun.com/docs/zh/api-reference/chat/streaming`, `https://platform.stepfun.com/docs/zh/api-reference/error-codes` |
| SiliconFlow | LLM chat | OpenAI-compatible `https://api.siliconflow.cn/v1/chat/completions`; API Reference defines chat body/auth fields, and official CN stream-mode docs confirm the `.cn` endpoint plus SSE `data: {...}` / `[DONE]`; voice adapter sends `enable_thinking:false` and `max_tokens:160` by default, skips reasoning-only and usage-only chunks | `Authorization: Bearer $SILICONFLOW_API_KEY` | `https://docs.siliconflow.com/en/api-reference/chat-completions/chat-completions_copy`, `https://docs.siliconflow.cn/en/userguide/capabilities/stream-mode`, `https://docs.siliconflow.cn/en/usercases/use-qwen3`, `https://docs.siliconflow.cn/en/faqs/error-code` |
| Moonshot / Kimi | LLM chat | OpenAI-compatible `https://api.moonshot.cn/v1/chat/completions`; SDK/API base URL `https://api.moonshot.cn/v1`; stream is SSE `data: {...}` ending with `data: [DONE]`; voice adapter defaults to `moonshot-v1-8k`, sends `max_completion_tokens:160`, never sends deprecated `max_tokens`, and does not send `thinking` unless explicitly configured | `Authorization: Bearer $MOONSHOT_API_KEY` | `https://platform.kimi.com/docs/api/overview`, `https://platform.kimi.com/docs/api/chat`, `https://platform.kimi.com/docs/api/errors`, `https://platform.kimi.com/docs/pricing/chat-v1` |
| DeepSeek | LLM chat | OpenAI-compatible base URL `https://api.deepseek.com`; chat path `/chat/completions`; stream is data-only SSE ending with `[DONE]`; current model ids include `deepseek-v4-flash` and `deepseek-v4-pro`; official docs say `thinking` defaults enabled, so the voice adapter defaults to `thinking.type:"disabled"` and `max_tokens:160`, and only sends `reasoning_effort` when thinking is enabled | `Authorization: Bearer ${DEEPSEEK_API_KEY}` | `https://api-docs.deepseek.com/`, `https://api-docs.deepseek.com/api/create-chat-completion`, `https://api-docs.deepseek.com/quick_start/error_codes` |
| Anthropic / Claude | LLM chat / agent bridge | Claude Messages `https://api.anthropic.com/v1/messages`; typed SSE uses `event:` names such as `message_start`, `content_block_delta`, `message_delta`, `message_stop`, and stream `error`; parser must not reuse OpenAI `data:` chunk handling | `x-api-key: $ANTHROPIC_API_KEY`, `anthropic-version: 2023-06-01`, `Content-Type: application/json` | `https://platform.claude.com/docs/claude/reference/messages_post`, `https://platform.claude.com/docs/en/build-with-claude/streaming`, `https://platform.claude.com/docs/en/api/errors`, `https://platform.claude.com/docs/en/docs/about-claude/models` |

## 5080lab Evidence Snapshot

5080lab evidence is latency and risk evidence, not protocol authority. Raw reports may contain historical API keys or signed audio URLs and must not be committed.

Latest inspected path:

```text
C:\Users\21\Desktop\a21-mainland-latency-lab
```

Relevant 2026-06-05 finding imported into `server/docs/provider-matrix.md`:

- Latest copied StepFun smoke report `a21-provider-smoke-20260605-225111-334580400.json` executed 3 streaming runs successfully against `api.stepfun.com`; first content p50/p95 was 177.277/304.399 ms and total p50/p95 was 314.249/406.981 ms.

Relevant 2026-05-30/31 findings imported into `server/docs/provider-matrix.md`:

- LLM streaming first content p50: SiliconFlow 217.3 ms, StepFun 307.3 ms, DeepSeek 635.3 ms, DashScope 778.7 ms, Volcengine Ark endpoint 2953.9 ms.
- StepFun stream parser must handle `delta.reasoning` / `delta.reasoning_content`, not only `delta.content`.
- Volcengine Ark lab setup used an `ep-*` endpoint id; current endpoint was too cold for P0 realtime voice.
- HTTP/non-stream TTS first-byte p50 was about 1.06-1.10 s for Doubao and Qwen, so realtime WebSocket TTS must be probed before production choice.
- Doubao voice key/resource/voice mismatch produced code `55000000`; fixtures must include this class.
- Doubao ASR accepts provider-side PCM only. Gateway code must use an explicit xiaozhi Opus -> PCM decoder dependency before this profile can be routed; sending raw Opus bytes as `input_audio_buffer.append.audio` is forbidden.
- DashScope Fun-ASR accepts PCM directly; its documented `opus` path is not Xiaozhi raw Opus passthrough. Gateway code must use an explicit xiaozhi Opus -> PCM decoder dependency before this profile can be routed; sending raw Xiaozhi Opus bytes while declaring `format:"opus"` is forbidden.
- Doubao TTS returns base64 provider audio deltas. Gateway code must use an explicit provider-audio -> xiaozhi Opus converter dependency before this profile can be routed; sending provider `pcm`, `mp3`, or `ogg_opus` bytes directly as xiaozhi downlink frames is forbidden.

## Adapter Requirements

Every real provider adapter must expose:

- `ProviderID`, `ModelID`, `SourceDocURL`, `SourceDocCheckedAt`.
- Exact endpoint and auth header construction in tests.
- A parser test for the provider's actual streaming format.
- Error mapping for auth failure, quota/balance, rate limit, bad request, server error and overload when the official docs publish these categories.
- Context cancellation that stops network reads/writes quickly.
- Redaction tests proving no secret or raw Authorization value appears in logs or reports.

## Voice Output Rule

The device downlink remains xiaozhi-compatible Opus. Provider TTS output is not trusted to be device-ready.

TTS adapters must report codec, sample rate, channel count and frame timing. Any required decode/resample/Opus encode work belongs to the gateway audio pipeline and must be tested separately before physical StackChan acceptance.

DashScope CosyVoice must use provider PCM with an injected libopus encoder in the gateway. Provider `opus` or Opus-like bytes must not be passed directly to StackChan unless a future physical decoder acceptance gate proves byte-for-byte Xiaozhi raw packet compatibility.

## Fixture Gate

Task 13 Step 2 fixture validation lives in:

```text
server/internal/providers/provider_fixtures_test.go
```

Fixture directories are grouped by provider and modality under `server/internal/providers/<provider>/testdata/<modality>/`. MiniMax TTS keeps `tts_http` as fallback documentation and implements the `tts_ws` realtime candidate behind an explicit converter gate.
