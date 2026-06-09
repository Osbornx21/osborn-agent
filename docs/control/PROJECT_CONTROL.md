# Project Control

## 项目身份

项目名：`stackchan-gateway`

目标：为内部 StackChan S3 设备构建一个低延迟、可验证、可恢复的 Xiaozhi 兼容服务端。

本项目不是 A21/X21 的延续。A21 只提供 provider、benchmark、V21 adapter、经验教训参考。

## 当前开发主线

当前主线负责人：Codex control tower。

职责：

- 维护项目顺序、任务边界、验证门和风险记录。
- 将跨线程同步的架构判断固化到控制文档。
- 防止 A21/X21 既有架构污染 StackChan 主线。
- 在没有 fresh verification 前，不声称任务完成。
- 在需要 subagent 时先划清文件权属，再派工和审查。

当前主线判断：

- StackChan 是具身 voice satellite / 桌面 Agent 终端。
- ESP32-S3 设备端负责音频、屏幕、传感器、动作、本地状态和设备侧 MCP 工具。
- ASR/LLM/TTS、记忆、工具、权限、观测、provider 切换放在自建网关或主机侧。
- Agent/Skill 系统作为服务和消息层接入，不塞进固件。
- 语音链路完全遵循 xiaozhi：设备协议、Opus 帧、状态语义、listen/tts/abort/MCP 流程都以 xiaozhi 为准。
- 我们只自建并掌握服务端，不重新发明设备端音频链路。
- 生产服务端全部上云，默认部署在阿里云 ECS 或等价云端环境。
- 5080lab 用于国内镜像拉取、依赖缓存、容器/模型下载、provider burn-down 和本地模型实验。
- 局域网链路只允许作为实验室压测/对照，不作为产品主线。
- 串口只允许用于开发、管理、刷机、恢复、诊断，不作为产品运行链路。

## 权威顺序

当文档冲突时，按以下顺序判断：

1. 用户最新明确指令。
2. `docs/control/*` 控制规则。
3. `docs/superpowers/plans/2026-06-06-stackchan-xiaozhi-server.md` 服务端计划。
4. `STACKCHAN_XIAOZHI_TOOLING_INVENTORY.md` 资料清单。
5. A21/V21/官方源码参考。

## 红线

- 不继承 A21/X21 端侧固件架构。
- 不在 P0 做 full-duplex realtime。
- 不手搓替代 xiaozhi 的音频采集、播放、Opus、状态机协议。
- 不在 WebSocket mock loop 跑通前接真实 provider。
- 不在 simulator 跑通前上物理 StackChan。
- 不把局域网服务端作为生产主线。
- 不把串口作为产品运行链路。
- 不在 ECS 上直接拉取大模型/大镜像作为默认流程；大件优先由 5080lab 通过国内镜像准备。
- 不凭记忆、第三方博客或 A21 旧代码实现真实 provider 调用；百炼、火山、MiniMax、StepFun、DeepSeek 必须按官网最新文档复核。
- 不把真实 API key、SSH private key、Wi-Fi 密码提交进 Git。
- 不让 subagent 并行改同一组文件。
- 不在没有 fresh verification 的情况下说完成。
- 不用临时 ECS `RunCommand` 脚本绕过已建立的 deployment/preflight/env 纪律。必须优先复用固定 package/preflight/systemd 流程；如果为了 hotfix 使用 Cloud Assistant 源码分片部署，必须优先使用 `server/deploy/aliyun/cloud-assistant-source-deploy.sh` 或保持等价证据边界：显式进入 Bash、source `/etc/a21-air/gateway.env` 后再运行 `voice-profile-check`，并记录分片状态、SHA、测试、构建、profile-check、restart、health/ready 和公网/5080lab 证据。
- 不把 V21 接入 casual/roleplay 模式。
- 硬件、固件和 ECS 可用后，不再以资源缺席推迟真实 fixture、物理验收、ECS preflight、刷机/NVS 诊断或最终 provider evidence；但这些动作仍必须按 gate 产出 fresh verification，不能把串口/LAN 当生产链路，也不能把 ECS 可用等同于生产部署完成。
- A21 air 不从旧 A21/New project/X21 固件工程执行实机刷写；旧工程只能作为参考资料。若需要刷机/NVS/URL 写入，必须先在 A21 air 控制文档中记录 A21 air 专用入口或把外部固件交付件作为已配置对象验收，不能用旧项目的 guard 或 artifact 代替。

## 默认推进节奏

一个工作批次只做一个明确范围。

产品优先级：

1. P0：可靠低延迟语音伴侣，先半双工/可打断。
2. P1：身体工具化，通过 MCP 控制头部、灯、屏幕、提醒、音量、摄像头，并限速、限幅、去抖。
3. P2：桌面智能家居中枢，用自然语言控制 Home Assistant 设备，并用表情/动作反馈状态。
4. P3：人格与记忆，作为网关/服务侧能力。
5. P4：视觉和具身行为，摄像头以工具式调用为主。
6. P5：真正 realtime 全双工，等音频链路、AEC、VAD、打断稳定后再做。

第一批允许范围：

1. `server/` Go 服务骨架。
2. `internal/config` 配置和密钥校验。
3. `internal/protocol/xiaozhi` JSON 消息契约。
4. `internal/protocol/xiaozhi` binary audio framing。

第一批禁止范围：

- provider 实网调用。
- MCP broker 实现。
- StackChan body scheduler。
- ECS 部署。
- 物理设备刷机或 NVS 写入。
- 局域网生产链路设计。
- 串口产品链路设计。
- ESPHome 主路线替代官方 StackChan/xiaozhi 固件。
- speech-to-speech Realtime API 作为唯一大脑。
- 官方 App/社交后端改造。

## 任务关闭标准

任务关闭必须同时满足：

- 代码或文档改动范围与任务匹配。
- 有对应测试或检查命令。
- 验证命令在当前轮新鲜运行。
- 失败项明确记录。
- 任务看板更新。

## 状态汇报格式

每个阶段结束时用这个格式：

```text
本轮完成：
- ...

验证：
- command: result

未验证：
- ...

下一步：
- ...
```
