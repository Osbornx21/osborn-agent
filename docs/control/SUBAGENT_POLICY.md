# Subagent Policy

## Default

不要默认派 subagent。只有用户明确授权“使用 subagent / 多 agent / 并行 agent”时才派。

## 适合 subagent 的任务

- provider 适配器，且文件权属独立。
- 部署文档和 systemd/Caddy 配置。
- simulator 场景，且不改 session 核心。
- 独立 code review。
- 对官方源码的特定问题核查。

## 不适合早期委派的任务

第一批 Task 1-4 默认由主控线程完成：

- 服务骨架。
- config contract。
- Xiaozhi JSON contract。
- binary audio framing。

原因：这些是后续所有任务的边界，一旦漂移，项目会失控。

## 分工规则

每个 subagent prompt 必须包含：

- 任务名。
- 允许修改的文件或目录。
- 禁止修改的文件或目录。
- 需要运行的验证命令。
- 输出必须列出改动文件。
- 明确说明不要 revert 他人改动。

## 文件权属示例

| Worker | 可写范围 | 禁止范围 |
|---|---|---|
| provider worker | `server/internal/providers/dashscope/*` | `server/internal/session/*` |
| MCP worker | `server/internal/mcp/*` | `server/internal/providers/*` |
| body worker | `server/internal/stackchan/*` | `server/internal/protocol/*` |
| deploy worker | `server/deploy/*`, `server/docs/*` | `server/internal/*` |

## 审查规则

每个 subagent 完成后必须两段审查：

1. Spec compliance review：是否严格完成任务，没有多做。
2. Code quality review：边界、并发、错误处理、测试质量。

发现问题必须回同一个 worker 修，不直接让下一个 worker 接烂摊子。

