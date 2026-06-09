# Osborn Agent

Osborn Agent 是一个面向 M5Stack StackChan 的云端语音 Agent 项目。它复用 Xiaozhi/小智风格的低延迟语音链路，把 StackChan 当作桌面具身语音终端：设备端负责麦克风、喇叭、屏幕、唤醒词、动作和本地状态，云端网关负责 ASR、LLM、TTS、记忆、工具、模式切换和 provider 编排。

这个仓库对应 A21 Air / StackChan Xiaozhi gateway 的内测包 1。

## 项目定位

StackChan 不适合把完整“大脑”塞进 ESP32-S3。Osborn Agent 的主线设计是：

- 设备端保持轻量：音频采集/播放、Xiaozhi WebSocket、屏幕文本、舵机、LED、触摸、唤醒词和必要的 MCP 能力。
- 服务端掌握语音链路：DashScope ASR/TTS、SiliconFlow/百炼/火山/DeepSeek/Kimi/MiniMax 等 LLM provider、会话状态机、打断、追踪和验收。
- Agent 能力云端化：人格、短期上下文、长期记忆、工具调用、Feishu/Home Assistant/V21/OpenClaw/Hermes 等桥接能力都在网关侧受控执行。
- 体验目标优先：低延迟、可打断、能连续对话、状态灯和身体反馈稳定，不为了功能堆叠牺牲语音主链路。

## 内测包 1 状态

当前内测包 1 已通过真实 StackChan 硬件 20 轮物理验收。

- 默认语音链路：`siliconflow-dashscope-voice`
- ASR/LLM/TTS：DashScope ASR + SiliconFlow LLM + DashScope TTS
- 硬件基线：StackChan V1.4.1 + A21 Air overlays
- 自定义唤醒词：`大头`
- 首音 p50/p95：`834/1090 ms`
- 连续对话上下文：20/20 LLM 请求携带 recent context
- 打断停止延迟：`1 ms`
- 摄像头安全：验收窗口内无意外触发
- 自动 provider fallback：内测包 1 暂停开启，等待完整 ASR/LLM/TTS fallback 证据

详细发布说明见 [docs/releases/INNER_TEST1.md](./docs/releases/INNER_TEST1.md)。

## 目录结构

| 路径 | 说明 |
|---|---|
| `server/` | Go 语音网关、provider adapters、Xiaozhi 协议、session/VoiceLoop、MCP/tool 编排 |
| `server/configs/` | 示例网关配置和 StackChan persona 配置 |
| `server/deploy/aliyun/` | 阿里云 ECS 部署、物理验收、TTS 调参和 fixture 捕获脚本 |
| `firmware/stackchan-a21air/` | StackChan V1.4.1 的 A21 Air overlay patch、构建和刷机辅助脚本 |
| `docs/control/` | 项目控制文档、任务看板、验证门和 provider 接入门 |
| `docs/releases/` | 公开发布摘要 |

## 快速开始

基础检查：

```bash
make control-check
git status --short
```

运行 Go 测试：

```bash
cd server
go test ./...
```

检查默认 voice profile 配置：

```bash
cd server
go run ./cmd/stackchan-gateway voice-profile-check \
  --config ./configs/stackchan-gateway.example.yaml \
  --profile siliconflow-dashscope-voice
```

本地模拟器可用于协议和状态机回归，真实硬件验收仍以 StackChan 实机和 ECS trace 为准。

## Provider 策略

内测包 1 的默认链路是 DashScope ASR/TTS + SiliconFlow LLM。项目已实现或验证过的主要 provider 包括：

- 阿里云百炼 / DashScope
- 火山方舟 / Doubao
- SiliconFlow
- DeepSeek
- Moonshot / Kimi
- MiniMax
- StepFun
- Anthropic Claude

内测包 1 保留手动/admin provider hot-switch 能力，但不启用自动 fallback。自动 fallback 需要先捕获 durable spoken Opus fixture，并通过完整 ASR/LLM/TTS fallback matrix。

## 安全边界

公开仓库不包含：

- provider key、生产 `.env`、ECS 私有配置
- 原始音频、转写文本、prompt、模型输出
- 固件二进制、设备 NVS dump
- 用户私有运行数据库

真实密钥只应放在 `.local/`、系统环境变量、ECS secret 或用户明确指定的私有位置，不进入 Git。

## 关键文档

| 文档 | 用途 |
|---|---|
| [docs/releases/INNER_TEST1.md](./docs/releases/INNER_TEST1.md) | 内测包 1 发布摘要 |
| [docs/control/README.md](./docs/control/README.md) | 控制文档索引 |
| [docs/control/MAINLINE_STATUS.md](./docs/control/MAINLINE_STATUS.md) | 当前主线状态 |
| [docs/control/TASK_BOARD.md](./docs/control/TASK_BOARD.md) | 任务看板和验收记录 |
| [docs/control/VERIFICATION_GATES.md](./docs/control/VERIFICATION_GATES.md) | 验证门和通过标准 |
| [docs/control/PROVIDER_INTEGRATION_GATES.md](./docs/control/PROVIDER_INTEGRATION_GATES.md) | 大陆 provider 接入门 |
| [firmware/stackchan-a21air/README.md](./firmware/stackchan-a21air/README.md) | 固件 overlay、构建和刷机说明 |

## 开发原则

- 语音链路优先，尽量复用 Xiaozhi 协议和成熟 ASR/LLM/TTS provider，不手搓低层音频。
- 服务端是 source of truth，设备端不持有 admin token。
- 功能上线前必须能被测试、trace、实机报告或 release artifact 证明。
- A21/X21 只做工具层参考，不继承失控的端侧架构。
