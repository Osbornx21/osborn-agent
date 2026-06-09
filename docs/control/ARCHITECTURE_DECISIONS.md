# Architecture Decisions

## ADR-0001: New Gateway, Not A21/X21

Decision: 服务端项目命名为 `stackchan-gateway`，不继承 A21/X21 端侧固件架构。

Reason: A21/X21 已经出现架构失控；本项目需要以官方 StackChan + Xiaozhi 协议为地基。

## ADR-0002: WebSocket First

Decision: P0 只做 Xiaozhi WebSocket transport。

Reason: 官方 firmware 已支持 WebSocket；MQTT + UDP 更复杂，放到 WebSocket 实机验收后。

## ADR-0003: Half-Duplex First

Decision: P0 使用 `listen` auto / half-duplex 模式。

Reason: StackChan/CoreS3 AEC 未证明稳定，full-duplex 会把问题混在音频、网络、provider、回声里。

## ADR-0004: Gateway Owns Turn Lifecycle

Decision: gateway 统一拥有 session、turn、generation、abort、TTS queue。

Reason: 低延迟体验的关键是取消旧 generation 和清空 stale audio，不能交给 provider 或 agent 后端。

## ADR-0005: Cascade Voice Is Production Default

Decision: ASR -> streaming LLM -> streaming TTS 是默认生产链路。

Reason: 更容易测量、fallback、打断和接入 V21/工具；speech-to-speech realtime 只做实验模式。

## ADR-0006: StackChan Hardware Through MCP And Scene DSL

Decision: head/LED/camera/screen 通过 MCP broker、body scheduler、semantic scene DSL 调度。

Reason: 设备保留渲染权，云端只发送语义和安全动作，避免 raw UI 和 servo spam。

## ADR-0007: V21 Professional Only

Decision: V21 只能由 professional/evidence 模式调用。

Reason: casual/roleplay 模式不能静默进入知识检索，否则用户体验和隐私边界会混乱。

## ADR-0008: ECS Only Runs Gateway Hot Path

Decision: 4c/8G Aliyun ECS 运行 gateway、TLS、metrics、session control，不跑重模型。

Reason: 重模型放 5080 lab 或云 API，ECS 保持稳定低延迟控制面。

## ADR-0009: Secrets Outside Git

Decision: 即使内部授权明文使用，真实密钥也不自动写入 Git 工作区。

Reason: 防止后续 commit、备份、分享、agent 上下文传播造成事故。

## ADR-0010: Verification Before Completion

Decision: 没有 fresh verification，就不能声称完成。

Reason: 这是防止项目失控最硬的一条。

## ADR-0011: StackChan Is A Bodily Voice Satellite

Decision: StackChan 的产品定位是具身 voice satellite / 桌面 Agent 终端，而不是在 ESP32-S3 中塞完整大脑。

Reason: Home Assistant/ESPHome/Wyoming、Mycroft/OpenVoiceOS、xiaozhi-esp32 等成熟路线的共识是小设备负责音频、屏幕、传感器、动作和本地状态；ASR/LLM/TTS、记忆、工具、权限、观测和 provider 切换由网关/主机负责。

Consequences:

- 保留官方 StackChan 固件、身体层和 xiaozhi 兼容协议。
- 自建 xiaozhi 兼容网关负责会话、provider、打断、记忆、工具、权限、指标。
- Agent/Skill 系统以服务和消息方式接入。
- ESPHome 不作为主路线，因为它会压扁 StackChan 的表情、舵机、Avatar 和 MCP 身体优势。
- speech-to-speech Realtime API 只作为实验路径，不能替代 gateway 主控。

## ADR-0012: Development Mainline Control Tower

Decision: 当前线程中的 Codex 作为 A21 Air / StackChan Xiaozhi 项目的开发主线和 control tower。

Reason: 项目已经明确经历过 A21/X21 架构失控，需要一个持续维护边界、顺序、验证和风险记录的主线角色。

Consequences:

- 主线先维护控制文档和任务顺序，再推进实现。
- 每次跨线程同步都要落到控制文档或任务看板。
- 代码实现默认按 `docs/superpowers/plans/2026-06-06-stackchan-xiaozhi-server.md` 的 Task 顺序推进。
- 未通过验证门的功能不能提升到下一阶段。

## ADR-0013: Cloud-Only Production Server

Decision: 服务端生产链路全部上云，默认以阿里云 ECS 为第一运行环境。

Reason: 之前项目在局域网链路上吃过亏；LAN 会把可达性、延迟、服务发现、机器状态、网络穿透和 provider 调用混在一起，导致问题难定位。StackChan 的生产体验应该由稳定云端 gateway 负责。

Consequences:

- P0 voice loop 目标是 StackChan -> cloud gateway -> provider -> cloud gateway -> StackChan。
- 5080 本地实验室只做 benchmark、provider burn-down、local model 实验和 simulator。
- LAN 服务不能作为 production fallback，除非未来新增 ADR 并完成独立验收。
- ECS 部署不再是后期可选项，而是服务端主线验收的一部分。

## ADR-0014: Serial Is Development And Admin Only

Decision: 串口只用于开发、管理、刷机、恢复、诊断和验收 instrumentation，不作为产品运行链路。

Reason: 串口适合救援和低层观察，但不适合作为用户级 voice loop 或正常控制通道。产品通道必须走 xiaozhi-compatible 网络协议。

Consequences:

- 串口日志可以辅助诊断设备端问题。
- 串口可以用于刷机、NVS 检查、紧急配置。
- 不能通过串口承载正常对话、provider 桥接、云端工具调用或用户控制链路。
- 任何依赖串口才能工作的功能不能标记为产品可用。

## ADR-0015: Xiaozhi Voice Semantics Are Authoritative

Decision: 语音链路完全遵循 xiaozhi 的设备协议、Opus 帧、状态语义、`listen`/`tts`/`abort`/MCP 流程；本项目只自建并掌握服务端。

Reason: 之前手搓音频效果很差，音频链路不应该重新发明。成熟路径是复用 xiaozhi 设备侧采集、播放、Opus 和协议语义，在服务端重新掌握 provider、会话、打断、监控和工具调度。

Consequences:

- 不做自定义 PCM bridge 作为产品主链路。
- 不替换官方 xiaozhi 设备端 audio service。
- 服务端实现必须以 xiaozhi WebSocket JSON 和 binary Opus contract 为第一兼容目标。
- 所有 TTS downlink 都经过 gateway generation/cancel/pacing，不允许 provider 直接控制设备播放。
- 音频问题优先对标 xiaozhi 行为，而不是扩大自研音频面。

## ADR-0016: 5080lab Is The Domestic Mirror And Artifact Lane

Decision: 5080lab 作为国内镜像、依赖缓存、容器镜像、大模型文件和 provider burn-down 的优先拉取/准备环境。

Reason: 本地和 ECS 直接拉大依赖、大镜像、大模型容易被网络、代理和跨境链路拖垮；5080lab 具备大陆环境和高性能资源，更适合做下载、缓存、验证和 artifact 准备。

Consequences:

- 生产运行仍然在云端 ECS。
- 5080lab 不成为产品 voice loop 的运行主链路。
- 大镜像、大模型、容器和依赖缓存优先在 5080lab 通过国内镜像完成。
- 经过验证的 artifact 再同步到 ECS 或发布存储。
- 小型 Go module 依赖可在开发机直接拉取；失败或大件时切 5080lab 镜像路径。
