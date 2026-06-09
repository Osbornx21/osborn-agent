# Verification Gates

## Universal Gate

Before any completion claim:

```bash
make control-check
git status --short
```

This proves only that control docs exist, plan placeholders are absent, and no obvious secret pattern is present. It does not prove service code works.

## Documentation Gate

For documentation-only changes:

```bash
make control-check
```

Pass criteria:

- Exit code `0`.
- No placeholder scan hits.
- No obvious secret scan hits.
- Changed files are listed in final summary.

## Service Code Gate

For any Go service change after `server/go.mod` exists:

```bash
cd server
go test ./...
```

Additional package-specific tests must be run when touching:

| Area | Command |
|---|---|
| config | `go test ./internal/config -v` |
| xiaozhi protocol | `go test ./internal/protocol/xiaozhi -v` |
| stackchan app/avatar protocol | `go test ./internal/protocol/stackchanapp -v` |
| session/turn | `go test ./internal/session -v` |
| transport/session concurrency | `go test -race ./internal/session ./internal/protocol/xiaozhi` |
| LLM tool-call orchestration | `go test ./internal/config ./internal/agent ./internal/tools ./internal/httpapi ./internal/app ./internal/session ./internal/mcp ./internal/stackchan -run 'TestLoadExampleConfigPasses|TestValidateToolFollowUpPolicyRejects(InvalidBounds|UnsafeRecursiveToolCalls)|TestMemoryLookupTool|TestAppRegistersMemoryLookupServiceTool|TestRegistry|TestAdminServiceToolCatalog|TestPublicRouterDoesNotMountAdminServiceToolCatalog|TestNewWiresAdminServiceToolCatalog|TestVoiceLoop.*ToolCall|TestVoiceLoopExecutesServiceTool|TestVoiceLoop(Hides|Exposes)V21ServiceToolDefinition|TestLLMProviderToolAlias|TestVoiceLoopRunsToolResultFollowUp|TestVoiceLoopCanDisableToolResultFollowUp|TestVoiceLoopAllowsOneFollowUpToolCallWhenConfigured|TestBuildToolFollowUpPrompt|TestToolResultFollowUpPolicyFromConfig|TestMCPToolOrchestrator|TestBroker|TestBodyScheduler|TestScene|TestToolOutcomeDisplayEventsAreRecognized|TestDomainToolDisplayEventsAreRecognized|TestVoiceLoopSendsDisplayEventSceneForTool(Success|Failure)|TestVoiceLoopSendsDisplayEventSceneFor(HomeAssistantStateTool|HomeAssistantActionTool|SearchWebTool)|TestExpression(ForCue|SequenceToolInputSchemaBoundsCueList|SequencePresetToolInputSchemaListsConfiguredPresets)' -count=1` |
| LLM tool schema/provider parser | `go test ./internal/providers ./internal/providers/siliconflow ./internal/providers/dashscope ./internal/providers/deepseek ./internal/providers/moonshot ./internal/providers/minimax ./internal/providers/doubao ./internal/providers/stepfun ./internal/session -run 'TestToolCallDeltaAccumulator|TestLLMBuilds.*StreamingRequest|TestLLMParsesStreamingToolCall|TestVoiceLoopPasses(ServiceToolDefinitions|StackChan(Expression|DisplayCard)ToolDefinition)ToLLMRequest|TestStackChan(ExpressionToolRequiresMatchingGatewayCapability|DisplayCardToolRequiresConfiguredCardsAndScreen)|TestLLMProviderToolAlias' -count=1` |
| Home Assistant service tool | `go test ./internal/config ./internal/homeassistant ./internal/app ./internal/tools -run 'TestLoadExampleConfigPasses|TestValidateHomeAssistant|TestRegisterGetStateTool|TestRegisterCallActionTool|TestClientErrorsDoNotLeakToken|TestNewClientRequiresURLAndToken|TestAgentServiceToolsRegistersHomeAssistant(State|Action)ToolWhen(Enabled|Configured)|TestRegistry' -count=1` |
| search service tool | `go test ./internal/config ./internal/search ./internal/app ./internal/tools -run 'TestLoadExampleConfigPasses|TestValidateSearch|TestRegisterWebSearchTool|TestSearchClient|TestNewSearchClient|TestAgentServiceToolsRegistersSearchWebToolWhenEnabled|TestRegistry' -count=1` |
| Feishu service tool and smoke command | `go test ./internal/config ./internal/feishu ./internal/app ./internal/tools ./cmd/stackchan-gateway -run 'TestLoadExampleConfigPasses|TestValidateFeishu|TestFeishuClient|TestNewFeishuClient|TestRegisterFeishu|TestAgentServiceToolsRegistersFeishuToolsWhenEnabled|TestFeishuSmokeCommand|TestRegistry' -count=1` |
| reminder service/display tool | `go test ./internal/config ./internal/reminder ./internal/app ./internal/session -run 'TestLoadExampleConfigPasses|TestValidateReminderToolBounds|TestRegisterAnnounceTool|TestAgentServiceToolsRegistersReminderAnnounceToolWhenEnabled|TestVoiceLoopSendsDisplayEventSceneForReminderTool|TestVoiceLoopDoesNotSendReminderEventSceneForRejectedReminderTool' -count=1` |
| agent bridges | `go test ./internal/agents ./internal/httpapi ./internal/app ./internal/config ./internal/session -run 'TestLoadExampleConfigPasses|TestParseModeCommand|TestModeStore|TestBridgeCatalog|TestRuntimeStatus|TestRouter.*V21|TestV21|TestHermes|TestOpenClaw|TestClaudeAndHermes|Test.*ToolIntents(FilterUnsafeNamesAndCapCalls|RespectConfiguredAllowedTools|RespectConfiguredMaxToolIntents)|TestAdminAgentMode|TestXiaozhiDeviceMode|TestAdminAgentBridgeCatalog|TestAdminAgentRuntimeStatus|TestPublicRouterDoesNotMountAdminAgent(ModeRoutes|BridgeCatalog|RuntimeStatus)|TestAgentServiceToolsRegistersGatedV21ToolWhenEnabled|TestNewWiresAdminAgentModeController|TestNewWiresPublicDeviceModeSelectWithDeviceAuth|TestNewWiresAdminAgentBridgeCatalog|TestNewWiresAdminAgentRuntimeStatus|TestAgentModeCommandHandlerUpdatesRuntimeModeStore|TestAgentModeReaderUsesRuntimeModeStore|TestAgentRuntimeRouterRoutesRoleplayModeToHermes|TestAgentRuntimeRouterRoutesToolModeToOpenClaw|TestAgentRuntimeRouterRestrictsOpenClawToolIntentsFromConfig|TestAgentRuntimeRouterRateLimitsOpenClawRuntimeRoutes|TestAgentRuntimeRouterSkipsOpenClawWhenInputTooLong|TestAgentRuntimeRouterCoolsDownOpenClawAfterErrors|TestAgentRuntimeRouterIgnoresBlankOpenClawResponseWithoutToolCalls|TestValidateV21|TestValidateHermes|TestValidateOpenClaw|TestValidateAgentBridgeRejects(UnsafeAllowedToolIntents|InvalidMaxToolIntents|InvalidRuntimeRouteRateLimits|InvalidRuntimeInputLimits|InvalidRuntimeErrorCooldowns)|TestValidateAgentDefaultMode|TestVoiceLoopHandlesAgentModeCommandBeforeLLM|TestVoiceLoopRoutesAgentRuntimeBeforeLLM|TestVoiceLoopTracesAgentRuntimeSkippedReason|TestVoiceLoopFallsBackToLLMWhenAgentRuntime(ReturnsBlankResponse|Errors)|TestVoiceLoop(Hides|Exposes)V21ServiceToolDefinition' -count=1` |
| agent persona/memory | `go test ./internal/agent ./internal/app -run 'Test.*Memory|TestMemoryCompactor|TestPromptBuilderIncludesRecentConversation|TestInMemoryRecentTurnStore|TestSQLiteMemoryStore.*Recent|TestAgentPromptBuilderUsesConfiguredSQLiteMemoryStore' -count=1` |
| admin memory API | `go test ./internal/agent ./internal/httpapi ./internal/app -run 'Test.*Memory|TestAdminMemory|TestAdminRecentTurns|TestMemoryCompactor|TestPublicRouterDoesNotMountAdminRecentTurns' -count=1` |
| LLM provider request shape | `go test ./internal/providers/dashscope ./internal/providers/minimax ./internal/providers/deepseek ./internal/providers/doubao ./internal/providers/stepfun ./internal/providers/siliconflow ./internal/providers/moonshot -run 'TestLLM(BuildsOpenAICompatibleStreamingRequest|BuildsStructuredMessagesWhenProvided|BuildsOpenAICompatibleStreamingRequestWithVoiceSafeDefaults|BuildsArkOpenAICompatibleStreamingRequest|BuildsStepFunOpenAICompatibleStreamingRequest)' -count=1` |
| provider router/admin profile control | `go test ./internal/providerrouter ./internal/httpapi ./internal/app -run 'TestRouter.*Profile|TestAdminProviderProfile|TestReadyz' -count=1` |
| ECS runtime package | `go test ./cmd/stackchan-gateway -run 'TestECSPackage(Command|Validate)' -count=1` |
| mcp | `go test ./internal/mcp -v` |
| stackchan body/display | `go test ./internal/config ./internal/httpapi ./internal/app ./internal/stackchan ./internal/session -run 'TestLoadExampleConfigPasses|TestValidateFailsWhen(Display(Lifecycle|Event)ScenePolicy|DisplayCardPolicy|ExpressionCuePolicy|ExpressionSequencePolicy|BodyLifecycleLEDPolicy)IsInvalid|TestStackChan(ExpressionPolicies|LifecycleExpressionCues|EventExpressionCues|ExpressionSequences|DisplayCards|LifecycleLEDs)FromConfig|TestExpression(ForCue|SequenceToolInputSchemaBoundsCueList|SequencePresetToolInputSchemaListsConfiguredPresets|SequenceCatalogRedactsCueListsAndReportsDeviceAvailability|CatalogRedactsCaptionsAndReportsDeviceAvailability)|TestAgentRouteSkippedDisplayEventIsRecognized|TestAdminStackChanExpression(Cue|Sequence)Catalog|TestPublicRouterDoesNotMountAdminStackChanExpression(Cue|Sequence)Catalog|TestNewWiresAdminStackChanExpression(Cue|Sequence)Catalog|TestWebSocketMCPDeviceReceivesDisplaySceneOnListenStart|TestBodyScheduler|TestScene|TestDisplaySceneCatalogRedactsCaptionsAndReportsDeviceAvailability|TestAdminStackChanDisplaySceneCatalog|TestPublicRouterDoesNotMountAdminStackChanDisplaySceneCatalog|TestNewWiresAdminStackChanDisplaySceneCatalog|TestDisplayCard(ToolInputSchemaListsConfiguredCards|CatalogRedactsCaptionsAndReportsDeviceAvailability)|TestAdminStackChanDisplayCardCatalog|TestPublicRouterDoesNotMountAdminStackChanDisplayCardCatalog|TestNewWiresAdminStackChanDisplayCardCatalog|TestMCPToolOrchestrator.*StackChan(Expression|DisplayCard)|TestVoiceLoopSendsConfiguredExpressionCueFor(LifecycleScene|AgentRouteEvent|AgentRuntimeSkippedRoute)|TestVoiceLoop(TracesListenStartBodyDispatchesSafely|DispatchesLifecycleLEDsWithoutLeakingRawColorsToTrace|NewIdleListenStartResetsBodySchedulerTurnLimit|CloseSendsIdleLEDWithoutOldGeneration)|TestVoiceLoopPassesStackChan(DisplayCard|Expression)ToolDefinitionToLLMRequest|TestStackChan(ExpressionToolRequiresMatchingGatewayCapability|DisplayCardToolRequiresConfiguredCardsAndScreen)|TestVoiceLoop.*Display(Lifecycle|Event)' -count=1` |
| runtime readiness/provider wiring | `go test ./internal/app -run TestReadyz -count=1` |

Runtime `/readyz` must check the configured default voice profile through the same environment-backed provider registry used by the xiaozhi WebSocket runtime. A missing provider, missing provider credential, or missing required local converter/decoder must return HTTP 503 with only safe check values such as `provider_not_found` or `provider_config_error`; it must not print provider messages, prompts, generated text, Authorization headers or token values.

Provider profile admin controls must stay on the private admin listener and require `STACKCHAN_ADMIN_TOKEN` bearer auth. The catalog route may expose only configured profile names, ASR/LLM/TTS provider ids, voice-runtime/default/fallback booleans and effective device profile statuses; it must not expose provider env var names, token values, prompts, generated text, transcripts, raw provider config details or provider error bodies. PUT overrides must still validate that the target profile can build a full ASR/LLM/TTS voice runtime before the next turn can use it; LLM-only probe profiles may appear in the catalog as `voice_runtime:false` but must not become device voice overrides. When automatic fallback is enabled, startup config validation must reject every `providers.auto_fallback.profiles[]` entry that is missing ASR, LLM or TTS, even if that profile is useful as an LLM-only probe profile.

Voice-path LLM request builders must keep provider-specific short-output controls and structured message shape under test. Persona/memory/continuity instructions should reach Chat APIs as a `system` message, bounded recent turns should be sent as alternating `user`/`assistant` chat-history messages when available, and the current ASR final transcript should remain the final current `user` message; legacy `LLMRequest.Text` may remain only for fallback and safe length metadata. DashScope/Qwen must send `enable_thinking:false` and a bounded `max_completion_tokens`; MiniMax M3 and Doubao must send bounded `max_completion_tokens` and `thinking.type:"disabled"` by default; DeepSeek must send `thinking.type:"disabled"` and bounded `max_tokens`; StepFun must send bounded `max_tokens`; SiliconFlow must keep `enable_thinking:false` and bounded `max_tokens`; Moonshot/Kimi must use bounded `max_completion_tokens` and must not send deprecated `max_tokens`. Adapter tests must assert the exact field names because these are provider-specific and cannot be inferred from generic OpenAI compatibility.

Memory management endpoints must stay on the private admin listener, require `STACKCHAN_ADMIN_TOKEN` bearer auth, use the configured gateway memory repository, and never expose repository error details or token values. The public xiaozhi WebSocket router must not mount memory management routes. Memory compaction must not delete source memories by default and must not call an LLM summarizer unless a separate gate is added for prompt/data leakage, latency and rollback. Recent-turn inspection must also stay private-admin only, require an explicit `device_id`, read the same bounded `recent_turns` store used by prompt construction, respect a small limit cap, and sanitize repository errors; it may expose bounded recent user/assistant text only on the private admin listener for debugging continuity.

LLM tool-call orchestration must remain gateway-owned. Model/provider tool requests may be executed only through the orchestrator, MCP allowlist or service-tool registry permission boundary, per-turn limit and timeout. StackChan head/LED/display calls must pass through the existing scheduler/composer safety helpers; rejected tools must not reach the device or service executor, and trace events must not contain arguments, schemas, tool results, prompts, transcripts, generated text or token values. Raw MCP tool schemas exposed to providers must be filtered by both the configured per-device allowlist and the current discovered device tool set; `self.camera.take_photo` must not be exposed to or executed from the default voice hot path without a future explicit camera mode/tool gate. Gateway-owned service tools must use the current runtime identity for scoping; `memory.lookup` must not trust model-supplied `device_id`/`user_id` arguments or expose memory `metadata_json`. Mode-gated service tools must also be gated before provider schema exposure: `v21.voice_query` may be exposed in `LLMRequest.Tools` only when the current device agent mode is `professional`; other modes or unknown mode must hide its schema, while execution still retains the service-tool professional-mode guard. The service-tool catalog admin route must stay on the private admin listener, require bearer auth, remain read-only, execute no service tool, remain unavailable on the public xiaozhi router, and expose only safe registered-tool metadata: name, description, permission, allow/permission status and filtered top-level schema property names. It must not expose token env names, token values, executor details, tool arguments, tool results, prompts, transcripts, generated text or raw schemas. Provider-facing tool names must be OpenAI-compatible safe aliases, and aliases must be mapped back to internal MCP/service tool names before execution. StackChan display lifecycle/event scenes must be gateway-composed from configured policies, not model-supplied captions or arbitrary screen parameters, and must be skipped when the current device has not discovered the required screen MCP method. The private admin StackChan display-scene catalog must require bearer auth, stay off the public xiaozhi router, remain read-only, and expose only safe lifecycle/event ids, scene/emotion/accent, caption-presence booleans, motion metadata, display bounds and per-device screen availability; it must not expose static captions, MCP method names, token env names, token values, prompts, transcripts, generated text, tool arguments or tool results. `stackchan.express`, `stackchan.expression_sequence` and `stackchan.play_expression_sequence` must be exposed only as provider-safe semantic expression tools, must not accept model-supplied yaw/pitch/colors/captions, and must route through the body scheduler, scene composer and MCP broker; unsupported cues or preset ids must be rejected before any device call. `stackchan.expression_sequence` may accept only one to three fixed enum cues, must validate the entire cue list before dispatching the first cue, and must respect the scheduler min-gap between cue dispatches. `stackchan.play_expression_sequence` may accept only configured preset ids; provider-facing schema must not expose preset cue lists, motion values, LED values or captions. The private admin StackChan expression-sequence catalog must require bearer auth, stay off the public xiaozhi router, remain read-only, and expose only safe preset ids, cue counts and per-device body/screen availability; it must not expose cue lists, static captions, MCP method names, token env names, token values, prompts, transcripts, generated text, tool arguments or tool results. The private admin StackChan expression-cue catalog must require bearer auth, stay off the public xiaozhi router, remain read-only, and expose only safe cue ids, configured flags, motion/LED/scene metadata, lifecycle/event cue mappings and per-device body/screen availability; it must not expose static captions, MCP method names, token env names, token values, prompts, transcripts, generated text, tool arguments or tool results. `stackchan.show_card` may be exposed only when `stackchan.display.cards` is configured and current device MCP includes `self.screen.set_scene`; it may accept only configured card ids, optional caption text must require `allow_caption=true`, captions must be bounded by card/global policy before display, and rejected card ids must not reach the device. The private admin StackChan display-card catalog must require bearer auth, stay off the public xiaozhi router, remain read-only, and expose only safe card ids, scene/emotion/accent, caption-policy booleans/bounds, motion metadata and per-device screen availability; it must not expose static captions, model captions, MCP method names, token env names, token values, prompts, transcripts, generated text, tool arguments or tool results. Operator-owned `stackchan.expression.cues` policy may override only those fixed cue names, and configured motion, LED and scene fields must be bounded by startup validation before the cue reaches runtime; duplicate cue keys after trimming/lowercasing must be rejected instead of leaving runtime policy selection to map iteration order. Operator-owned `stackchan.expression.sequences` may define only safe sequence ids and one to three fixed cue names; duplicate sequence ids after trimming/lowercasing must be rejected. Optional `stackchan.expression.lifecycle_cues` may map only `listening`, `thinking`, `speaking` or `idle` to fixed cue names; it is operator-configured, disabled when absent, and lifecycle dispatch uses only the cue motion/LED path so ordered screen lifecycle remains owned by `stackchan.display.lifecycle_scenes`. Optional `stackchan.expression.event_cues` may map only known display event ids to fixed cue names; it is operator-configured, disabled when absent, rejects duplicate normalized event ids, and event dispatch uses only the cue motion/LED path so event screen scenes remain owned by `stackchan.display.event_scenes`. Policy-skipped agent bridge turns may dispatch only the generic `agent_route.skipped` display/expression event, must still fall back to the normal LLM path, and must not send skip reasons, transcripts, prompts, generated text, provider bodies, URLs or token values to StackChan display/body payloads. Streaming `tool_calls` parsers must aggregate split `function.arguments` JSON before emitting a provider `ToolCall`; partial arguments must never reach the orchestrator. Tool result display feedback may use only configured `stackchan.display.event_scenes.tool.succeeded` / `tool.failed` policies based on safe outcome metadata; domain-specific service-tool feedback for Home Assistant state/action and web search may use only configured `homeassistant.state`, `homeassistant.action` and `search.web` event scenes after successful execution, and must not expose tool arguments/results. `tools.tool_followup` may disable the second LLM request, bound its result count/bytes with positive limits, and restrict prompt inputs to safe `allowed_tools`; non-allowlisted tool results must not enter the follow-up prompt. Recursive follow-up tool calls are disabled by default. If `allow_tool_calls=true`, `allowed_tools` must be non-empty, only already-exposed allowed schemas may be sent to the second LLM request, aliases must be resolved and checked again before execution, `max_tool_calls` must be capped at 1-2, disallowed or overflow calls must be recorded as skipped, and the final answer request must carry no tool schemas.

Home Assistant tools must be disabled by default, require `tools.home_assistant.enabled=true`, `base_url`, `token_env`, a non-empty entity allowlist and an environment token before registration. `homeassistant.get_state` is read-only and may call only allowlisted entity IDs; it must not expose raw Home Assistant attributes, token values or provider error bodies. `homeassistant.call_action` may be registered only when `allowed_actions` is configured, must require write permission, and may accept only `action_id` plus operator-defined slots for that action. Model-supplied domain, service, entity IDs, target or raw service data must never reach Home Assistant. Each configured action must map to static domain/service/entity IDs/data, and every action entity must also be in the global HA entity allowlist. Slots may set only validated string, number, integer or boolean service-data keys configured by the operator; out-of-range values and slots not configured for the chosen action must be rejected before HTTP. Arbitrary HA service passthrough requires a separate future gate.

Search must be disabled by default and may register only when `tools.search.enabled=true`, `base_url_env`, `token_env` and non-empty env values are configured. `search.web` must call the operator-owned internal adapter at `/internal/v1/search/web`, never a model-selected raw URL, and may accept only a short query plus bounded `max_results`. Returned payloads must include only bounded title, URL, snippet, source domain and published time, optionally filtered by configured bare-domain allowlist; raw adapter bodies, token values and unbounded provider output must not reach traces or tool payloads.

Feishu tools must be disabled by default and may register only when `tools.feishu.enabled=true`, `base_url`, `app_id_env`, `app_secret_env`, at least one `allowed_targets[]` entry and all referenced env values are configured. The gateway uses Feishu self-built-app `tenant_access_token`; model-supplied `receive_id`, `receive_id_type`, app credentials, raw message IDs from unrelated responses, card JSON, file uploads, mentions and arbitrary OpenAPI methods must not be accepted. `feishu.list_targets` may expose only target id, description and receive-id type, never the real chat/open/user ID. Voice-runtime `feishu.send_text` must bound text length, neutralize Feishu mention markup, and return only a safe confirmation request without contacting Feishu; it must not leak text, app secret, tenant token, receive ID or provider error bodies. `feishu-smoke` must reuse the same gateway config/env/allowlist boundary, reject unknown targets before HTTP, never print the message text or receive ID, and print only a safe `target=<id> message_id=<id>` success summary. Real Feishu live smoke evidence must record command shape and message-id only, not text, token, tenant token, receive ID, app secret or provider body. A future user-confirmed voice send flow requires a separate gate before runtime conversation may perform real Feishu writes.

Reminder tools must be disabled by default and may register only when `tools.reminder.enabled=true`. `reminder.announce` is only a currently-due local announcement primitive: it must not create schedules, write calendars, contact external services, or send Feishu/messages. It may accept only bounded title/message strings and a small urgency enum, must require device-control permission, and must not echo the full reminder message in the service-tool result. StackChan reminder screen feedback must use the configured `stackchan.display.event_scenes.reminder.due` policy instead of model-supplied screen text, and the reminder-specific display event must not fire when the tool call is rejected, skipped or unavailable.

Agent bridges must never own the realtime xiaozhi audio session, provider streaming loop, TTS queue or device MCP transport. V21 calls must be blocked outside professional mode with reason `v21_requires_professional_mode`, use the V21 internal service contract `/internal/v1/knowledge/voice-query`, and return only a bounded spoken answer/source-count payload to the LLM follow-up; V21 `full_answer`, citation excerpts and evidence excerpts must not be included in the service-tool payload. Agent mode state must be device-scoped, configured with a safe default, and controlled only from the private admin API, explicit natural-language professional/roleplay/tool mode entry/exit commands, or the public device-auth `/xiaozhi/device-mode/status|select` endpoint for the current device. The device-auth mode endpoint must authenticate with the same configured device `Device-Id`, `Client-Id` and raw/Bearer auth token boundary as xiaozhi provisioning, must not accept admin tokens, must not allow a model/body-supplied `device_id`, and must return only safe `active_mode`, `requested_mode`, `available` and `reason` state. Selecting the configured default mode through this endpoint must clear the per-device override rather than leaving a sticky default override. For non-casual modes, `available/reason` must be derived from the same runtime status source used by VoiceLoop/admin runtime status: `roleplay` maps to Hermes, `tool` maps to OpenClaw, and `professional` maps to V21. An unavailable bridge must not reject the mode selection, but it must return a safe reason such as `bridge_disabled`, `runtime_rate_limited`, `runtime_input_too_long` or `runtime_error_cooldown` so device UI cannot imply a bridge is healthy when the gateway would fall back. Agent mode and agent bridge admin HTTP routes, including catalog routes, must remain unavailable on the public xiaozhi router. Agent mode catalog responses may expose only safe mode names and effective device statuses. Agent bridge catalog responses may expose only safe bridge ids, enabled booleans, required modes, invocation type, service-tool name, runtime-route/tool-intent booleans, bounded-output booleans, normalized bridge-safe `allowed_tool_intents`, bounded `max_tool_intents`, bounded `max_runtime_routes_per_minute`, bounded `max_runtime_input_chars`, bounded `max_runtime_errors_before_cooldown` / `runtime_error_cooldown_ms` and boolean fallback policy flags; they must not expose prompts, persona text, provider config, bridge URLs, token env names, token values, raw collection ids, generated text, provider response bodies or provider error bodies. Agent runtime-status responses may additionally expose only safe dynamic runtime policy reasons such as `runtime_rate_limited`, `runtime_input_too_long` or `runtime_error_cooldown`, and must compute them from the shared runtime policy state without executing bridge calls. Natural-language mode commands must run after ASR final, skip LLM requests and memory writeback, speak only a short TTS confirmation, and avoid broad mentions such as "专业模式是什么" being treated as control commands. OpenClaw must be disabled by default, require URL/token env configuration when enabled, and route only when the device is in `tool` mode. Hermes must be disabled by default, require URL/token env configuration when enabled, and route only when the device is in `roleplay` mode. OpenClaw/Hermes runtime routes may be capped per bridge/device with `max_runtime_routes_per_minute`; a capped route must not call the external bridge and must fall back to the normal LLM path. OpenClaw/Hermes runtime input may be capped with `max_runtime_input_chars`; an over-limit transcript must not call the external bridge and must fall back to the normal LLM path. OpenClaw/Hermes runtime errors may enter per bridge/device cooldown after `max_runtime_errors_before_cooldown` consecutive errors for `runtime_error_cooldown_ms`; a cooled-down route must not call the external bridge and must fall back to the normal LLM path. Policy-skipped OpenClaw/Hermes runtime routes may record only a safe `agent_route_skipped` trace event containing `reason`, `mode` and `destination`; skipped-route traces must not contain transcripts, prompts, generated text, provider bodies, URLs, tool arguments/results or token values. OpenClaw/Hermes responses must return bounded text for gateway TTS or safe gateway tool calls; an empty response with no safe tool calls must not mark the turn handled and must fall back to the normal LLM path. OpenClaw/Hermes runtime errors must record only a safe `agent_route_error` code and must fall back to the normal LLM path unless the turn context itself was canceled; trace events must not contain provider error bodies, URLs, prompts, transcripts, generated text or token values. OpenClaw/Hermes tool intents must first pass the global bridge-safe tool set, then any configured per-bridge `allowed_tool_intents`, then the per-bridge `max_tool_intents` cap, then convert into gateway-owned tool calls that still pass through the MCP/service-tool orchestrator before any device or external side effect. Claude/Hermes/OpenClaw tool intents must be filtered at the bridge boundary before VoiceLoop sees them: empty or unknown tool names are dropped, nested `v21.voice_query` bridge calls are not accepted from external agent intents, and forwarded calls must respect the configured per-bridge cap without exceeding the hard limit of two safe gateway tool calls per routed turn. Forwarded tool calls must still pass through the same orchestrator before side effects.

New Go dependency pulls must use the smallest reliable dependency that fits the service boundary. If local pulls stall or fail on large packages, pure-Go replacements, containers or model artifacts, route the pull through 5080lab/domestic mirrors before promoting the dependency into the service path.

## ECS Runtime Package Gate

Before claiming cloud runtime packaging is ready:

```bash
cd server
go test ./cmd/stackchan-gateway -run 'TestECS(Package(Command|Validate)|PreflightDryRunCommand)' -count=1
go run ./cmd/stackchan-gateway ecs-package \
  --config ./configs/stackchan-gateway.example.yaml \
  --output-dir ./var/ecs-packages/stackchan-gateway-runtime
go run ./cmd/stackchan-gateway ecs-package-validate \
  --package-dir ./var/ecs-packages/stackchan-gateway-runtime
tmp_env="$(mktemp "${TMPDIR:-/tmp}/stackchan-gateway.ecs.env.XXXXXX")"
chmod 600 "$tmp_env"
cat > "$tmp_env" <<'ENV'
STACKCHAN_MAIN_AUTH_TOKEN=<placeholder-main-token>
STACKCHAN_ADMIN_TOKEN=<placeholder-admin-token>
DASHSCOPE_API_KEY=<placeholder-dashscope-key>
SILICONFLOW_API_KEY=<placeholder-siliconflow-key>
ENV
go run ./cmd/stackchan-gateway ecs-preflight-dry-run \
  --package-dir ./var/ecs-packages/stackchan-gateway-runtime \
  --config ./configs/stackchan-gateway.example.yaml \
  --env-file "$tmp_env"
rm -f "$tmp_env"
```

Pass criteria:

- `ecs-package` writes exactly `stackchan-gateway.service`, `preflight.sh`, `gateway.env.example`, `README.md` and `manifest.json`.
- The generated package must not contain the gateway binary, production config copy, provider env file, API key values, spoken fixtures, raw audio, transcripts, prompts or generated text.
- `manifest.json` must record safe source provenance, runtime paths, default profile and env names only.
- `gateway.env.example` may list required env variable names such as `STACKCHAN_MAIN_AUTH_TOKEN`, `STACKCHAN_ADMIN_TOKEN`, `DASHSCOPE_API_KEY` and `SILICONFLOW_API_KEY`, but must leave values empty.
- `preflight.sh` must source the private ECS env file and run `voice-profile-check` before systemd starts the gateway.
- `ecs-package-validate` must accept freshly generated packages and reject unknown files, non-empty env-template values, manifest/unit/preflight mismatches and secret-like content patterns without printing the secret-like value.
- `ecs-preflight-dry-run` must validate the package, private env file, runtime config and default voice profile without starting systemd or running provider network probes. It must use the supplied env file rather than ambient shell env, reject missing required env names and reject group/world-readable env files without printing env values.
- The command must reject output directories containing unknown files, especially `gateway.env` or provider env files.
- The ECS host is now available, so this gate should be run on the actual ECS host with the private cloud env file before any deployment claim.
- This gate proves packaging/preflight software only. It does not prove ECS production deployment, provider production selection, full ASR/LLM/TTS evidence or physical StackChan acceptance.

## ECS Cloud Dev Runtime Smoke Gate

Before claiming the A21 Air cloud dev runtime is reachable by StackChan firmware:

```bash
curl --noproxy '*' -fsS http://47.103.57.217/healthz
curl --noproxy '*' -fsS http://47.103.57.217/readyz
curl --noproxy '*' -fsS -X POST \
  -H 'Device-Id: 44:1b:f6:e2:74:50' \
  -H 'Client-Id: 36d53c70-30e7-41e9-9720-6a5000e40a3c' \
  http://47.103.57.217/xiaozhi/ota/
cd server
STACKCHAN_MAIN_AUTH_TOKEN=<from-private-env> \
  go run ./cmd/stackchan-sim \
    --scenario hello_only \
    --gateway ws://47.103.57.217/xiaozhi/v1/ws \
    --device '44:1b:f6:e2:74:50' \
    --client '36d53c70-30e7-41e9-9720-6a5000e40a3c' \
    --auth-token-env STACKCHAN_MAIN_AUTH_TOKEN \
    --timeout-ms 5000
```

Pass criteria:

- `/healthz` returns OK from a domestic egress that can actually reach the ECS public IP.
- `/readyz` returns `providers:ok` for the configured default voice profile.
- Configured-device OTA returns official-shape `server_time` and `websocket` fields; token values must be redacted in logs and reports.
- Unknown-device OTA returns fixed `403 OTA_DEVICE_NOT_CONFIGURED`.
- `hello_only` reports `passed:true` with one successful handshake and zero first-audio metrics.
- This proves public Caddy/OTA/WebSocket/token plumbing only. It does not prove ASR, LLM, TTS, real first audio, physical microphone capture, display/body MCP behavior or production provider selection.
- If local Mac direct/proxy egress returns `Empty reply from server`, SOCKS errors or `403` while 5080lab/ECS-local/real-device checks pass, record it as a local egress/path issue and use 5080lab, ECS-local or real device evidence for cloud readiness. Do not downgrade the gateway unless ECS-local, 5080lab domestic checks or the real device path also fail.

## ECS Source Hotfix Deploy Helper Gate

Before using or modifying the Cloud Assistant source hotfix helper:

```bash
bash -n server/deploy/aliyun/cloud-assistant-source-deploy.sh
server/deploy/aliyun/cloud-assistant-source-deploy.sh \
  --source-ref HEAD \
  --allow-dirty \
  --dry-run
```

Pass criteria:

- Dry-run prints only safe source ref, short commit, archive SHA256, archive size, chunk size, part count, remote temp dir, rendered remote-script size and proxy-configured boolean.
- Dry-run does not call Aliyun APIs, does not read provider credentials and does not require `aliyun` CLI.
- Default chunks are no larger than the Cloud Assistant-safe 20 KB width.
- Aliyun CLI stderr must be sanitized before printing. Signed request query parameters such as `AccessKeyId`, `Signature`, `Content`, `SecurityToken` and `SignatureNonce` must be redacted while preserving the high-level error class for troubleshooting.
- The rendered remote command runs under Bash, verifies part count and archive SHA before extraction, uses domestic Go mirrors by default, runs source-archive-safe tests, builds `cmd/stackchan-gateway`, checks `libopus.so.0`, sources `/etc/a21-air/gateway.env`, runs `voice-profile-check`, installs the binary, restarts systemd, checks ECS-local `/healthz` and `/readyz`, and verifies `physical-led-retest`, `physical-reconnect-retest`, `physical-acceptance-metrics` and `physical-acceptance-report` command availability without producing false physical acceptance reports.
- A real deployment still requires Cloud Assistant `SendFile`/`RunCommand` invoke IDs, source SHA, ECS tests, build, profile-check, restart, local/public health/readiness and binary mtime/size/owner evidence in `TASK_BOARD.md`.

## ECS TTS Tuning Helper Gate

Before modifying DashScope TTS physical tuning values on ECS:

```bash
bash -n server/deploy/aliyun/tts-tuning-update.sh
server/deploy/aliyun/tts-tuning-update.sh --status
server/deploy/aliyun/tts-tuning-update.sh --volume 42
server/deploy/aliyun/tts-tuning-update.sh --opus-bitrate-bps 48000 --opus-complexity 8
```

Pass criteria:

- The helper must default to dry-run and print only safe region, instance, config path, env-file path, service name, requested tuning, execute flag and proxy-configured state.
- `--status` must not write the private env file, restart systemd or call provider APIs.
- Tuning updates must be bounded before Cloud Assistant execution: volume 0-100, rate/pitch 0.5-2.0, and model/voice ids limited to simple token characters.
- When executed, the helper must back up `/etc/a21-air/gateway.env`, update only `DASHSCOPE_TTS_MODEL`, `DASHSCOPE_TTS_VOICE`, `DASHSCOPE_TTS_VOLUME`, `DASHSCOPE_TTS_RATE`, `DASHSCOPE_TTS_PITCH`, `A21_OPUS_DOWNLINK_BITRATE_BPS` or `A21_OPUS_DOWNLINK_COMPLEXITY`, preserve file mode 0600, source the env, run `voice-profile-check`, restart `stackchan-gateway`, and verify ECS-local `/healthz` plus `/readyz`.
- Output must never print provider tokens, device auth tokens, Authorization headers, transcripts, prompts, generated text or raw audio.
- TTS/Opus tuning must not change Xiaozhi sample rates, provider PCM format, binary frame version or the ASR/LLM/TTS state machine. Opus tuning is bounded to downlink bitrate `24000..96000` bps and complexity `1..10`; DTX/FEC stay off and VBR/voice-signal semantics stay on unless a separate audio gate is added.

## Mock Simulator Gate

Before more provider burn-down polishing or physical device work, prove the non-hardware xiaozhi gateway path with a temporary mock profile override. The committed example config now defaults to the real `siliconflow-dashscope-voice` profile, so this gate must not accidentally use that file unchanged.

Terminal A:

```bash
cd server
mock_config="$(mktemp "${TMPDIR:-/tmp}/stackchan-gateway.mock.XXXXXX")"
perl -0pe '
  s/providers:\n  default_profile:/providers:\n  mock:\n    asr_auto_final_on_audio: true\n  default_profile:/;
  s/default_profile: "siliconflow-dashscope-voice"/default_profile: "cn-low-latency-cascade"/;
  s/agent:\n  default_mode: "casual"/agent:\n  default_mode: "tool"/;
  s/  openclaw:\n    enabled: false/  openclaw:\n    enabled: true/;
  s/(  openclaw:.*?max_runtime_input_chars:) 360/$1 1/s;
  s/min_command_gap_ms: 160/min_command_gap_ms: 1/;
' \
  ./configs/stackchan-gateway.example.yaml > "$mock_config"
STACKCHAN_MAIN_AUTH_TOKEN=dev-test-token STACKCHAN_ADMIN_TOKEN=admin-token \
OPENCLAW_WS_URL=https://openclaw.example.internal OPENCLAW_AGENT_TOKEN=dev-openclaw-token \
  go run ./cmd/stackchan-gateway --config "$mock_config"
```

Terminal B:

```bash
cd server
STACKCHAN_MAIN_AUTH_TOKEN=dev-test-token \
  go run ./cmd/stackchan-sim \
    --scenario mock_gateway_suite \
    --gateway ws://127.0.0.1:8080/xiaozhi/v1/ws \
    --trace-file ./var/traces/turns.jsonl
```

Pass criteria:

- `mock_gateway_suite` reports `passed:true` and includes child summaries for `happy_path_20_turns`, `asr_final_without_listen_stop`, `abort_during_tts`, `provider_slow_first_audio`, `ws_reconnect`, `mcp_head_motion`, `mcp_display_scene`, `mcp_led_feedback`, and `mcp_agent_bridge_skip_feedback`.
- All child turns complete.
- The `asr_final_without_listen_stop` child completes through provider ASR final without client `listen.stop`, matching StackChan/Xiaozhi auto-listening behavior and preventing a regression that would require a second screen tap.
- The `abort_during_tts` child drops old generation audio.
- The `provider_slow_first_audio` child enforces the configured `--max-first-audio-ms` budget, defaulting to 1500 ms.
- The `ws_reconnect` child opens a second same-device connection, observes the old connection close, and completes a turn on the replacement connection.
- The MCP children advertise device MCP support, complete initialize/tools-list JSON-RPC, receive and respond to the allowlisted head, display and LED calls, verify the bridge-skip child observes configured `agent_route.skipped` screen plus `settle` head/LED feedback after policy fallback, and still complete the voice turn.
- When `--trace-file` is provided, the suite applies the required trace-event checks for each child scenario.
- This is protocol/session evidence only. It is not real provider latency evidence.
- The `mcp_display_scene` child is custom/mock MCP evidence only. It must not be used as physical display acceptance evidence while official StackChan V1.4.1 tools/list does not expose `self.screen.set_scene`.
- LED evidence can use the official simulator profile only when the scenario is run with `--firmware-profile official-v1.4.1`; this profile omits `self.screen.set_scene`, keeps official head/LED tools, and rejects custom screen-scene calls instead of producing a mock false positive.

## Real First-Audio Simulator Gate

Before claiming true provider first-audio latency for the default runtime profile, run the same simulator through the committed `siliconflow-dashscope-voice` profile with real DashScope ASR/TTS, real SiliconFlow LLM, and a real spoken `xiaozhi_opus_frames_v1` fixture. Fake simulator Opus frames are not valid ASR evidence.

Terminal A:

```bash
cd server
set -a
source /path/to/provider.env
set +a
export DASHSCOPE_API_KEY="${DASHSCOPE_API_KEY:-${A21_LAB_DASHSCOPE_API_KEY:-${A21_DASHSCOPE_API_KEY:-}}}"
export SILICONFLOW_API_KEY="${SILICONFLOW_API_KEY:-${A21_LAB_SILICONFLOW_API_KEY:-${A21_SILICONFLOW_API_KEY:-}}}"
export STACKCHAN_MAIN_AUTH_TOKEN="${STACKCHAN_MAIN_AUTH_TOKEN:-dev-test-token}"
export STACKCHAN_ADMIN_TOKEN="${STACKCHAN_ADMIN_TOKEN:-admin-token}"
go run ./cmd/stackchan-gateway voice-profile-check \
  --config ./configs/stackchan-gateway.example.yaml
go run ./cmd/stackchan-gateway --config ./configs/stackchan-gateway.example.yaml
```

Terminal B:

```bash
cd server
curl -fsS http://127.0.0.1:8080/readyz
STACKCHAN_MAIN_AUTH_TOKEN=dev-test-token \
  go run ./cmd/stackchan-sim \
    --scenario provider_slow_first_audio \
    --gateway ws://127.0.0.1:8080/xiaozhi/v1/ws \
    --trace-file ./var/traces/turns.jsonl \
    --asr-opus-fixture ./var/fixtures/asr/spoken-opus.json \
    --max-first-audio-ms 1500 \
    --timeout-ms 15000 \
    --require-trace-events hello_received,listen_start,first_uplink_audio,speech_final,first_downlink_audio_sent,turn_complete
```

Pass criteria:

- `/readyz` returns `ready:true` with safe `config` and `providers` checks.
- `voice-profile-check` reports the committed default `siliconflow-dashscope-voice` profile with non-mock ASR/LLM/TTS providers and rejects explicit realtime voice LLM model overrides that contain reasoning/code/vision/pro-class markers such as `r1`, `think`, `reasoning`, `coder`, `vl`, `omni` or `pro`.
- `./var/fixtures/asr/spoken-opus.json` exists and is ignored by git, or the ECS fixture exists under `/var/lib/a21-air/fixtures/asr/`; either path must pass `asr-fixture-validate`.
- Simulator output reports p50/p95 first audio from a run that used the fixture via `--asr-opus-fixture`, and every turn stays within `--max-first-audio-ms`.
- A missing fixture, invalid fixture, or fake-frame timeout is a failed/unmeasured real first-audio gate, not partial success.

## Provider Gate

Before writing any real provider adapter:

- Re-open the official provider docs in the current implementation turn.
- Update or confirm `docs/control/PROVIDER_INTEGRATION_GATES.md` and `server/docs/provider-matrix.md`.
- Add golden fixtures for request shape, stream chunks, finish chunks and error payloads.
- Add parser tests before any real API call is accepted.
- Prove secret redaction with tests.

Before choosing a production provider profile:

```bash
cd server
go run ./cmd/stackchan-gateway provider-probe-package \
  --output-dir ./var/probe-packages/provider-probe-package \
  --profiles siliconflow-dashscope-voice,siliconflow-llm,moonshot-llm,stepfun-llm,doubao-llm,dashscope-cosyvoice \
  --runs 20 \
  --timeout-ms 5000 \
  --run-delay-ms 0
STACKCHAN_WEBSOCKET_URL=ws://<ecs-or-lab-host>:8080/xiaozhi/v1/ws \
go test ./internal/httpapi ./internal/config ./internal/app \
  -run 'TestRouterMountsOTAPath|TestXiaozhiOTAHandler|TestNewWiresPublicXiaozhiOTAConfig' \
  -count=1
go run ./cmd/stackchan-gateway device-provisioning-check \
  --config ./configs/stackchan-gateway.example.yaml \
  --listen 0.0.0.0:8080 \
  --advertise-url ws://<ecs-or-lab-host>:8080/xiaozhi/v1/ws \
  --timeout-ms 30000
go run ./cmd/stackchan-gateway asr-fixture-capture \
  --config ./configs/stackchan-gateway.example.yaml \
  --listen 0.0.0.0:8080 \
  --advertise-url ws://<ecs-or-lab-host>:8080/xiaozhi/v1/ws \
  --output ./var/fixtures/asr/spoken-opus.json \
  --max-frames 200 \
  --timeout-ms 30000
git check-ignore -v ./var/fixtures/asr/spoken-opus.json
go run ./cmd/stackchan-gateway asr-fixture-validate \
  --fixture ./var/fixtures/asr/spoken-opus.json
# On ECS, post-flash physical capture may instead use the fixed Cloud
# Assistant helper and durable fixture path:
#   server/deploy/aliyun/physical-asr-fixture-capture.sh --execute
#   ASR_OPUS_FIXTURE=/var/lib/a21-air/fixtures/asr/spoken-opus.json
go run ./cmd/stackchan-gateway provider-probe-matrix \
  --env-file /path/to/provider.env \
  --profiles siliconflow-dashscope-voice,siliconflow-llm,moonshot-llm,stepfun-llm,doubao-llm,dashscope-cosyvoice \
  --runs 20 \
  --timeout-ms 5000 \
  --run-delay-ms 0 \
  --output-dir ./var/reports \
  --asr-opus-fixture ./var/fixtures/asr/spoken-opus.json
go run ./cmd/stackchan-gateway provider-probe-summary ./var/reports/provider-probe-*.json
go run ./cmd/stackchan-gateway provider-probe-gate \
  --min-runs 20 \
  --min-success-percent 80 \
  --require-profiles siliconflow-dashscope-voice,siliconflow-llm,moonshot-llm,stepfun-llm,doubao-llm,dashscope-cosyvoice \
  --require-modalities asr,llm,tts \
  --require-fallback-modality llm \
  ./var/reports/provider-probe-*.json
STACKCHAN_MAIN_AUTH_TOKEN=<from-private-env> \
  go run ./cmd/stackchan-sim \
    --scenario asr_final_without_listen_stop \
    --gateway ws://<ecs-or-lab-host>/xiaozhi/v1/ws \
    --device '44:1b:f6:e2:74:50' \
    --client '36d53c70-30e7-41e9-9720-6a5000e40a3c' \
    --auth-token-env STACKCHAN_MAIN_AUTH_TOKEN \
    --asr-opus-fixture ./var/fixtures/asr/spoken-opus.json \
    --timeout-ms 30000 \
    --require-trace-events hello_received,listen_start,first_uplink_audio,speech_final,first_downlink_audio_sent,turn_complete
go run ./cmd/stackchan-gateway provider-probe-evidence-validate \
  --archive ./var/reports/provider-probe-evidence-<RUN_ID>.tgz
go run ./cmd/stackchan-gateway provider-probe-evidence-summary \
  --archive ./var/reports/provider-probe-evidence-<RUN_ID>.tgz
```

Pass criteria:

- `provider-probe-package` can generate a 5080lab/ECS execution package containing only `run-provider-probes.sh`, `run-provider-probes.ps1`, `README.md`, and `manifest.json`; it must reject dirty output directories with unexpected entries and the package must not contain secrets, provider env files, spoken fixtures, raw audio, transcripts or generated text.
- Linux/ECS runners use `run-provider-probes.sh`; Windows 5080lab runners use `run-provider-probes.ps1`. Both must execute the same matrix, summary, gate, archive validation and promotion-summary flow.
- Package runners default unset `GOPROXY`/`GOSUMDB` to `https://goproxy.cn,direct` and `sum.golang.google.cn` for the 5080lab domestic mirror lane, while still allowing explicit environment overrides.
- Package runners must run `provider-probe-matrix --allow-failed-profiles` so zero-success provider profiles still produce validated reports, safe summaries and a non-empty `provider-probe-gate.txt` diagnostic instead of stopping before evidence is written.
- Any probe pacing used to avoid provider burst throttling must be explicit via `--run-delay-ms`; non-zero values must be recorded as `run_delay_ms` in JSON reports and package manifests. External sleeps around probe commands are not production-selection evidence unless the same cadence is represented in the report.
- Windows package runners must write `provider-probe-summary.md`, `provider-probe-gate.txt` and `provider-probe-evidence-summary.md` as UTF-8 text, not PowerShell 5.1 default UTF-16; they must also capture `provider-probe-gate` stderr/nonzero output without `$ErrorActionPreference='Stop'` terminating before diagnostics validation.
- When `provider-probe-gate` fails, package runners may create `provider-probe-diagnostics-<RUN_ID>.tgz` containing only validated `provider-probe-*.json`, `provider-probe-summary.md` and the failed `provider-probe-gate.txt`; it must pass `provider-probe-diagnostics-validate`, is for troubleshooting only, and must never be promoted as production provider evidence.
- Package runners run a small Go self-test before provider calls by default; `PROVIDER_PROBE_SKIP_SELF_TEST=1` is allowed only for automation that has already run the same tests in the current source tree.
- Package generation must load the gateway `config_path`, verify every selected profile exists before writing runners, record that `config_path` in the manifest, and pass it into `provider-probe-matrix`; `PROVIDER_PROBE_CONFIG` may override that path on the target host. The manifest must also record `requires_asr_fixture=true` when the gate requires ASR or any selected provider profile contains an ASR provider in that config. In that case package runners must fail before any provider call if `ASR_OPUS_FIXTURE` is missing, neither git-ignored nor under the durable ECS `/var/lib/a21-air/fixtures/asr/` path, or rejected by `asr-fixture-validate`; the failure text may mention only the fixture path and next safe capture/check command, not payloads or transcripts.
- The public Xiaozhi OTA config endpoint must be implemented by the A21 Air gateway, mounted only on the configured `server.ota_path`, and must return only the official firmware-readable `websocket.url`, `websocket.token`, `websocket.version` and `server_time.timestamp/timezone_offset` fields. It must select the device token only after request `Device-Id` and `Client-Id` both match a configured `devices[]` entry, return a fixed safe 403 for unknown/unpaired hardware, and return fixed safe 503 errors for incomplete config. Unknown-device and error responses must not include token values, token env names, provider env names, public/private URLs, prompts, transcripts or generated text. Real OTA responses contain the device token by protocol necessity and must not be pasted into docs, reports or issue comments; smoke checks with real secrets must redact `.websocket.token` before output.
- New physical hardware must pass `device-provisioning-check` before `asr-fixture-capture`: the command must report `ready_for_capture=true`, `connected=true`, `hello=true`, `device_id_match=true` and `client_id_match=true` with no token value in output. By default it may print only identity hashes and match booleans; raw Device-Id/Client-Id output requires the explicit local `--show-device-identity` operator flag and must not be copied into control docs or provider evidence.
- `asr-fixture-capture` is run from the same xiaozhi WebSocket binary path as the gateway, refuses to start if `--output` is neither ignored by git nor under `/var/lib/a21-air/fixtures/asr/`, writes only `xiaozhi_opus_frames_v1` fixture JSON to those fixture paths, refuses to write captures that fail the semantic fixture gate, and prints a safe `connect_url` in the ready line.
- When fixture capture listens on `0.0.0.0` or `::`, operators must pass `--advertise-url ws://<reachable-host>:<port>/<path>` or a `wss://...` equivalent for the physical StackChan; capture must fail before serving if this URL is missing, and the advertised URL path must match the capture WebSocket path and must not include user info, query parameters or fragments.
- Physical StackChan capture must set `STACKCHAN_MAIN_AUTH_TOKEN` on the capture host to the token value; the gateway accepts device Authorization as either the raw token or `Bearer <token>`, matching xiaozhi-esp32's default Bearer prefix behavior. Device `Device-Id` and `Client-Id` headers must match the currently configured hardware identity; for the active A21 Air unit this is `44:1b:f6:e2:74:50` / `36d53c70-30e7-41e9-9720-6a5000e40a3c`. `asr-fixture-capture` does not start the admin listener and must not require `STACKCHAN_ADMIN_TOKEN` for capture. The token must never be placed in `connect_url`, logs, reports or docs.
- Fixture capture does not decode, resample, re-encode or log audio payloads; capture ready, progress and success output may include only listen path/address, `connect_url`, configured `device_id`, configured `client_id`, auth environment variable name, fixture path, frame count, byte count, duration, unique-payload count, max frame count and timeout. Auth failure output may include only HTTP status, fixed auth error code and header presence booleans. The auth environment variable value and caller-provided Authorization, `Device-Id` and `Client-Id` values must never be printed.
- `git check-ignore` proves repo-local spoken fixture files are ignored before provider probes run; durable ECS fixture files must live under `/var/lib/a21-air/fixtures/asr/`.
- `asr-fixture-validate` passes before any real ASR semantic probe; it reports only frame/byte/duration/diversity counts and rejects short, tiny, low-diversity or repeated placeholder fixtures.
- Report includes p50/p95 first transcript, first token or first audio for the modalities in the selected profile.
- Report is written to `server/var/reports/provider-probe-YYYYMMDD-HHMMSS.json`.
- `provider-probe` and `provider-probe-matrix` validate every generated report with the same `provider-probe-validate` schema checks before the report path is promoted into summary, gate, evidence or docs.
- Report validation includes safe latency/count invariants, safe provider HTTP status/error code fields and no prompt/transcript/generated text/raw payload/API key fields.
- Evidence tables are generated with `provider-probe-summary` from validated reports; do not hand-copy raw report JSON into docs.
- `provider-probe-gate` passes with the same reports before any production provider profile is selected; when it fails, the failure message may include only safe error labels from validated summaries, such as `provider_config_error` or `provider_error:http_402:invalid_request_error`, never provider messages, response bodies, headers or keys.
- Passing `provider-probe-gate` output written to evidence must include the actual gate parameters and source provenance: `min_runs`, `min_success_percent`, required profiles, required modalities, fallback modality, `source_ref` and `source_state`.
- Provider probe packages generated from source archives without `.git` must pass explicit `--source-ref <commit>` and `--source-state clean` into `provider-probe-package`; `source_ref=unavailable` evidence is diagnostic-only and must be rerun before promotion.
- 5080lab/ECS evidence tarballs pass `provider-probe-evidence-validate`; they may contain only `provider-probe-*.json`, `provider-probe-summary.md` and `provider-probe-gate.txt`, every JSON report inside must pass `provider-probe-validate`, `provider-probe-gate.txt` must include non-empty required profile, modality, fallback, `source_ref` and `source_state` parameters, and the validator recomputes the gate from the embedded reports using those parameters while matching the reported row/profile/provider counts.
- Provider matrix/evidence rows promoted from remote tarballs are generated with `provider-probe-evidence-summary`, which recomputes Markdown from validated JSON reports and includes the archive SHA256.
- `provider-probe-package` writes the promotion Markdown to `provider-probe-evidence-summary.md` outside the tarball; promote from that file or rerun `provider-probe-evidence-summary`, never from raw JSON.
- `provider-probe-matrix --env-file` may read old 5080lab `A21_LAB_*` env names and bridge them to current gateway env names; it must not print secret values.
- ASR reports with semantic claims from either `provider-probe` or `provider-probe-matrix` use a real spoken xiaozhi Opus fixture that passes `asr-fixture-validate`, not the default silence/test frame.
- At least one fallback provider exists.
- Secret values are not printed in logs or reports.
- Provider source docs are recorded with retrieval date and official URL.

## Official StackChan V1.4.1 Parity Gate

Before claiming body/display/camera behavior on physical StackChan official firmware:

- Record the firmware/protocol basis in the acceptance artifact: official StackChan firmware version, xiaozhi-esp32 dependency version, live device identity and the active Xiaozhi WebSocket binary version.
- `self.robot.set_led_color` payloads must use the official argument names `red`, `green` and `blue`; simulator expectations and gateway tests must assert those names. Gateway lifecycle LED tests must assert the ordered default sequence listening green, thinking amber, speaking blue and idle off, with RGB policy configured through `stackchan.body.lifecycle_leds` and validated at startup. The gateway trace must record only safe body/expression dispatch metadata such as channel, reason, result, cue and booleans; traces must not include RGB values, yaw/pitch values, raw arguments, transcripts, prompts, generated text or token values.
- Default physical StackChan body policy must stay conservative unless a future custom firmware/app-avatar path disables official autonomous/avatar motion: `stackchan.body.listen_start_motion_enabled` defaults to false, default `min_command_gap_ms` is at least 320, default `max_commands_per_turn` is no more than 6, default `stackchan.expression.event_cues` is empty, the default device MCP allowlist must not expose `self.robot.set_head_angles`, and default Hermes/OpenClaw bridge allowed tool intents must not include `stackchan.express`. Simulator/app tests may explicitly opt in listen-start head motion, head MCP tools or event cues to keep those tool paths covered, but that must not imply the physical default should move on every listen cycle.
- `self.screen.set_scene` must not be counted as official V1.4.1 physical support unless the actual device tools/list exposes it, custom firmware adds it, or a separate StackChan app/avatar WebSocket mapping implements the equivalent display behavior. Official xiaozhi ASR/LLM/TTS text rendering is a separate, valid display path.
- Xiaozhi audio binary protocol versions `1`, `2` and `3` are supported by the gateway parser, downlink encoder and `cmd/stackchan-sim`. The active version must still be recorded in physical acceptance. Any change to `server.websocket_version` must be treated as an OTA/runtime contract change: the device handshake `Protocol-Version` must match the configured version, `cmd/stackchan-sim --protocol-version <1|2|3>` must pass for the target path, and physical fixture/acceptance evidence must be gathered for that active version before promoting it.
- If avatar, JPEG preview, motion, call, dance, heartbeat, camera stream or app-level rendering are in scope, implement and gate `/stackChan/ws?deviceType=StackChan` separately from the xiaozhi voice WebSocket. Do not mix that protocol into the voice simulator.
- The simulator has an explicit `official-v1.4.1` firmware profile. Run `cmd/stackchan-sim --scenario official_stackchan_v1_4_1_tools_list --firmware-profile official-v1.4.1 ...` before accepting official tools-list parity, and run LED scenarios with the same profile before using simulator LED behavior as pre-physical evidence. This profile mirrors the key official V1.4.1 behavior needed for current gates: no fake `self.screen.set_scene`, official head/LED tools, screen brightness/theme tools, camera listed but still gateway-gated, and official `red`/`green`/`blue` LED argument validation. Mock gateway scenarios remain useful regression tests, not official firmware acceptance.
- `self.camera.take_photo` or camera stream behavior may not be re-exposed to the voice hot path without an explicit camera mode/tool gate, rate limits and user-visible confirmation semantics. The config-gated `camera.request_capture` service tool is allowed only as a confirmation request: it must not call device camera MCP, must not return images, must not include the reason text in the tool result, and must return only safe confirmation metadata.

## A21 Air StackChan Firmware Build Gate

Before claiming the A21 Air StackChan firmware overlay is ready for an operator flash:

```bash
bash -n firmware/stackchan-a21air/build-a21air-stackchan.sh \
  firmware/stackchan-a21air/flash-a21air-stackchan.sh
bash firmware/stackchan-a21air/build-a21air-stackchan.sh
bash firmware/stackchan-a21air/flash-a21air-stackchan.sh \
  --artifact-dir "/Users/jiyurun/Documents/A21 air/server/var/runtime/hardware/firmware/<artifact>"
```

Pass criteria:

- The build helper defaults to dry-run: it may check the official StackChan source tree, patch applicability, ignored output root and ESP-IDF export discovery, but it must not copy source, fetch dependencies, run `idf.py` or write artifacts without `--execute`.
- A final flash candidate should be built from the official StackChan V1.4.1 source plus the committed A21 Air patches, preferably with `--require-idf-version v5.5.4` because the official README targets ESP-IDF v5.5.4. A local v5.5.2 build is smoke evidence only unless explicitly accepted for flashing.
- If the Mac network cannot fetch ESP-IDF/components/GitHub dependencies reliably, use 5080lab or another mainland mirror lane to build from the same A21 Air StackChan overlay source. Do not substitute old A21/X21 firmware artifacts.
- Each generated artifact directory must include `manifest.md` and `a21air-firmware-checksums.env`. The flash helper must read that checksum file when present and still verify the expected artifact kind before accepting hashes.
- The flash helper has no default artifact; each preflight or flash must pass `--artifact-dir`. After an artifact is explicitly selected it defaults to preflight only and writes nothing. It must require `a21air-firmware-checksums.env`, verify artifact hashes, the target serial character device and the A21 Air device identity, print the exact esptool command, block known historical/reverted artifact names unless `--allow-historical-artifact` is deliberately supplied, and require `--flash --confirm-device-id 44:1b:f6:e2:74:50` before any write.
- Physical flashing is not complete until the operator explicitly confirms the current A21 Air StackChan target, the flash command succeeds, the device boots the new overlay, `大头` wake is physically observed, gateway-restart reconnect is retested, and the physical acceptance report validates.

## Physical StackChan Gate

Before claiming hardware acceptance:

```bash
cd server
go run ./cmd/stackchan-gateway physical-led-retest \
  --trace-file /var/lib/a21-air/traces/turns.jsonl \
  --report ./var/acceptance/stackchan-s3-led-YYYYMMDD-HHMMSS.json \
  --device 44:1b:f6:e2:74:50 \
  --gateway-commit <deployed-gateway-commit> \
  --visual-green-confirmed
go run ./cmd/stackchan-gateway physical-acceptance-metrics \
  --trace-file /var/lib/a21-air/traces/turns.jsonl \
  --device 44:1b:f6:e2:74:50 \
  --min-turns 20 \
  --since <acceptance-start-rfc3339> \
  --latest-turns 20 \
  > ./var/acceptance/stackchan-s3-metrics-YYYYMMDD-HHMMSS.json
go run ./cmd/stackchan-gateway physical-reconnect-retest \
  --trace-file /var/lib/a21-air/traces/turns.jsonl \
  --device 44:1b:f6:e2:74:50 \
  --restart-start <gateway-restart-start-rfc3339> \
  --report ./var/acceptance/stackchan-s3-reconnect-YYYYMMDD-HHMMSS.json
go run ./cmd/stackchan-gateway physical-acceptance-report \
  --report ./var/acceptance/stackchan-s3-YYYYMMDD-HHMMSS.json \
  --metrics-file ./var/acceptance/stackchan-s3-metrics-YYYYMMDD-HHMMSS.json \
  --device 44:1b:f6:e2:74:50 \
  --hardware-device-id 44:1b:f6:e2:74:50 \
  --client-id 36d53c70-30e7-41e9-9720-6a5000e40a3c \
  --firmware-build-id "StackChan-UserDemo V1.4.1" \
  --firmware-version V1.4.1 \
  --gateway-commit <deployed-gateway-commit> \
  --provider-profile siliconflow-dashscope-voice \
  --audio-playback-ok \
  --screen-text-ok \
  --head-control-ok \
  --led-lifecycle-ok \
  --led-retest-report ./var/acceptance/stackchan-s3-led-YYYYMMDD-HHMMSS.json \
  --no-unexpected-camera-trigger \
  --wifi-reconnect-ok \
  --gateway-restart-reconnect-ok
go run ./cmd/stackchan-gateway acceptance \
  --report ./var/acceptance/stackchan-s3-YYYYMMDD-HHMMSS.json \
  --device 44:1b:f6:e2:74:50 \
  --turns 20
```

Pass criteria:

- 20 half-duplex turns complete.
- Barge-in stops stale audio.
- Head commands work; LED commands are validated with official `red`/`green`/`blue` payload names, listening/ASR is visibly green while the device is actively sending ASR audio, speaking visibly switches blue after TTS starts, and idle/settle does not leave stale lifecycle color after TTS stop. `physical-led-retest` must pass for the latest observed turn: it must find `listen_start`, `first_uplink_audio` before `speech_final`, listen-start `stackchan_body_dispatch` LED result `sent` before `speech_final`, no LED overwrite before `speech_final`, and an explicit operator visual green confirmation. Operator notes for the same turn must also record whether speaking blue and idle/off were observed until the CLI grows first-class fields for those checks. The generated LED retest report must remain sanitized; notes must not include transcripts, prompts, token values, raw audio, generated text or raw LED values.
- `physical-acceptance-metrics` must derive `completed_turns`, `audio_turns`, active `body_mcp_tool_success_rate`, first-audible p50/p95, barge-in stop latency, `llm_request_turns`, `llm_recent_context_turns`, `max_recent_turn_count`, `continuity_context_ok` and `camera_tool_call_count` from the trace. Continuity evidence may use only safe `llm_request.fields.recent_turn_count` counts; metrics and reports must not expose prompts, transcripts, recent-turn text or generated text. Official StackChan can run many listen cycles under one WebSocket `trace_id`, so metrics must segment turns by `listen_start`, not by trace id alone. Use `--since <acceptance-start-rfc3339>` for final physical acceptance so earlier gateway versions and pre-fix tests cannot enter the acceptance window, then use `--latest-turns 20` unless the trace file already contains only the acceptance run. This prevents repaired current behavior from being polluted by older pre-fix trace failures while still failing any body/audio/camera/context problem inside the latest acceptance window. The body MCP success rate counts acceptance-relevant active body cues such as motion/listen, listening LED, thinking LED and speaking LED; `led/idle_start` remains in trace diagnostics but is excluded from this rate because official auto-listen can supersede idle settle between turns. Its first-audible basis is `first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms`, and `audio_turns` must meet the same minimum as completed turns. Barge-in stop latency comes from `tts_stop_sent.fields.stop_latency_ms` for `listen_start`/`abort` interruptions; `--barge-in-stop-latency-ms` is only an override for pre-field traces or explicit operator out-of-band measurement. For sound-quality investigations, current DashScope TTS runs should also emit safe `tts_audio_quality` trace events with PCM16 statistics such as peak/RMS dBFS, clipped percent, silence percent and DC offset, and the metrics/report/final-helper path must carry only those aggregate values inside the selected `--since` / `--latest-turns` acceptance window. These events and summaries must not contain transcripts, prompts, generated text, raw PCM, Opus bytes, audio payloads or token values, and they are diagnostic evidence rather than an acceptance pass/fail substitute.
- `physical-reconnect-retest` must verify the post-gateway-restart device reconnect from trace timestamps using RFC3339 time parsing and `time.Time` comparison, not string comparison. A `+08:00` trace timestamp before the UTC restart must not pass. `gateway_restart_reconnect_ok` requires a real post-restart `hello_received` for the runtime device id; `listen_start_after_restart_ok` is recorded separately because reconnect and a new spoken turn are distinct checks.
- The final physical acceptance runner must reject a reconnect report whose `schema_version` is not `a21_air_physical_reconnect_retest_v1`, whose `device_id` does not match the runtime Device-Id/MAC, whose `device_hello_after_restart_ok` is not `true`, or whose `gateway_restart_reconnect_ok` is not `true`. A passing reconnect report for another device, old schema or stale non-hello state must not be reusable for final acceptance.
- The `stackchan_physical_acceptance_v2` report must validate with `audio_playback_ok=true`, `screen_text_ok=true`, `body_mcp_tool_success_rate>=0.99`, `led_lifecycle_ok=true`, `llm_request_turns>=turns`, `llm_recent_context_turns>0`, `max_recent_turn_count>0`, `continuity_context_ok=true`, and a valid trace-bound `led_retest_report`. Reports generated from `--metrics-file` must persist `metrics_turn_window`, `metrics_trace_since`, `first_audible_basis`, `continuity_basis` and nested `tts_audio_quality` aggregate stats when those values are present. When the metrics file contains `trace_since`, the LED retest `listen_start_timestamp` must be present and must not be earlier than that `trace_since` window, so a stale LED report cannot be reused with a fresh 20-turn acceptance run. `custom_screen_scene_mcp_required` must be `false` for official StackChan V1.4.1 acceptance; absent official `self.screen.set_scene` support must be reported as unsupported, not silently satisfied by simulator evidence.
- `unexpected_camera_triggered` must be `false` and `camera_tool_call_count` must be `0`; even a rejected `self.camera.take_photo` tool call in the acceptance trace window fails default voice acceptance. `camera.request_capture` may only request confirmation and must not count as photo/stream execution. Actual camera photo/stream behavior remains outside default voice acceptance until an explicit capture executor, rate limits and user-visible confirmation semantics exist.
- Wi-Fi reconnect and gateway restart reconnect are tested.
- The physical acceptance JSON is generated by `physical-acceptance-report` and validates against the gateway `acceptance` subcommand.

Hardware execution is active now that the StackChan device and usable firmware are available. A physical acceptance claim still requires the validated report produced by this command, after the real spoken fixture, provider evidence and official V1.4.1 parity gate pass.
