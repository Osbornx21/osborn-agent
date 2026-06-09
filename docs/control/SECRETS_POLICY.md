# Secrets Policy

## 原则

用户授权内部明文使用敏感数据，但仓库仍不自动收集、不提交真实密钥。

允许明文存在于：

- `.local/stackchan-secrets.plaintext.md`
- `/Users/jiyurun/Documents/a21-mainland-latency-lab/secrets`
- `/Users/jiyurun/Documents/v21-knowledge-platform/.env.stackchan.local`
- `/Users/jiyurun/Documents/v21-knowledge-platform/.env.providers.local`
- ECS root-readable env file
- 云厂商 secret manager

禁止提交到 Git：

- API key
- access token
- SSH private key
- Wi-Fi 密码
- TLS private key
- provider raw authorization header
- V21/Hermes/OpenClaw bearer token
- real spoken ASR Opus fixture or user voice recording

## 本仓库密钥策略

`.gitignore` 已忽略：

- `.local/`
- `.env*`
- `*.pem`
- `*.key`
- `*.secret`
- `*.secrets`
- `server/var/fixtures/**` except `.gitkeep`

## 服务端 env 命名

服务端实现时优先使用这些 env 名：

| Env | 用途 |
|---|---|
| `STACKCHAN_MAIN_AUTH_TOKEN` | 设备 WebSocket auth |
| `STACKCHAN_ADMIN_TOKEN` | 内部 admin API auth |
| `DASHSCOPE_API_KEY` | DashScope |
| `DOUBAO_API_KEY` | Doubao |
| `MINIMAX_API_KEY` | MiniMax |
| `OPENAI_API_KEY` | OpenAI |
| `ANTHROPIC_API_KEY` | Claude |
| `V21_ADAPTER_URL` | V21 endpoint |
| `V21_ADAPTER_TOKEN` | V21 auth |
| `HERMES_AGENT_URL` | Hermes endpoint |
| `HERMES_AGENT_KEY` | Hermes auth |
| `OPENCLAW_WS_URL` | OpenClaw endpoint |
| `OPENCLAW_AGENT_TOKEN` | OpenClaw auth |
| `HOME_ASSISTANT_TOKEN` | Home Assistant long-lived access token |
| `SEARCH_ADAPTER_URL` | internal search adapter endpoint |
| `SEARCH_ADAPTER_TOKEN` | internal search adapter bearer token |
| `FEISHU_APP_ID` | Feishu self-built app id |
| `FEISHU_APP_SECRET` | Feishu self-built app secret |
| `FEISHU_<TARGET>_RECEIVE_ID` | Feishu allowlisted target receive id, for example chat_id/open_id/user_id |

## 日志规则

- 可以记录 env 名。
- 不记录 env 值。
- 可以记录 provider id 和 model id。
- 默认不记录完整 transcript。
- provider raw payload 默认不落盘。
