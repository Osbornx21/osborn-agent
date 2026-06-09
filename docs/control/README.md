# Control Docs

这个目录是项目控制台。之后任何实现、subagent 分工、验收声明、密钥处理、任务切换，都先按这里的规则走。

## 文件索引

| 文档 | 作用 |
|---|---|
| [MAINLINE_STATUS.md](./MAINLINE_STATUS.md) | 当前主线状态、分支、推进批次 |
| [PROJECT_CONTROL.md](./PROJECT_CONTROL.md) | 总治理规则和红线 |
| [TASK_BOARD.md](./TASK_BOARD.md) | 当前任务状态、下一步允许范围 |
| [VERIFICATION_GATES.md](./VERIFICATION_GATES.md) | 验证命令和验收门 |
| [PROVIDER_INTEGRATION_GATES.md](./PROVIDER_INTEGRATION_GATES.md) | 大陆 provider 精确接入门和官方文档源 |
| [SUBAGENT_POLICY.md](./SUBAGENT_POLICY.md) | 多 agent 使用边界 |
| [SECRETS_POLICY.md](./SECRETS_POLICY.md) | 明文敏感数据处理边界 |
| [CHANGE_PROTOCOL.md](./CHANGE_PROTOCOL.md) | 每次改动流程 |
| [ARCHITECTURE_DECISIONS.md](./ARCHITECTURE_DECISIONS.md) | 架构决策记录 |

## 控制命令

```bash
make control-check
```

通过标准：

- 核心控制文档存在。
- 服务端实施计划没有占位词。
- 仓库没有明显真实密钥模式。
- `git status --short` 可读，方便确认改动范围。
