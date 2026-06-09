package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExampleConfigPasses(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "stackchan-gateway.example.yaml")

	cfg, err := LoadFile(path, mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "plain-secret-value-must-not-appear-in-errors",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret-value-must-not-appear-in-errors",
	}))

	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if cfg.Server.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen addr = %q, want 127.0.0.1:8080", cfg.Server.ListenAddr)
	}
	if cfg.Server.OTAPath != "/xiaozhi/ota/" {
		t.Fatalf("ota path = %q, want /xiaozhi/ota/", cfg.Server.OTAPath)
	}
	if cfg.Server.WebsocketPublicURLEnv != "STACKCHAN_WEBSOCKET_URL" {
		t.Fatalf("websocket public URL env = %q, want STACKCHAN_WEBSOCKET_URL", cfg.Server.WebsocketPublicURLEnv)
	}
	if cfg.Server.WebsocketVersion != 1 {
		t.Fatalf("websocket version = %d, want 1", cfg.Server.WebsocketVersion)
	}
	if len(cfg.Devices) != 1 {
		t.Fatalf("devices len = %d, want 1", len(cfg.Devices))
	}
	allowedDeviceTools := map[string]bool{}
	for _, tool := range cfg.Devices[0].AllowMCPTools {
		allowedDeviceTools[tool] = true
	}
	if allowedDeviceTools["self.robot.set_head_angles"] {
		t.Fatal("example config must not expose head motion in the physical voice hot path by default")
	}
	if allowedDeviceTools["self.screen.set_scene"] {
		t.Fatal("example config must not expose custom screen scenes for official StackChan V1.4.1 by default")
	}
	if !allowedDeviceTools["self.robot.set_led_color"] {
		t.Fatal("example config should keep LED lifecycle feedback available by default")
	}
	if cfg.Audio.FrameDurationMS != 60 {
		t.Fatalf("frame duration = %d, want 60", cfg.Audio.FrameDurationMS)
	}
	if cfg.Providers.DefaultProfile != "siliconflow-dashscope-voice" {
		t.Fatalf("default profile = %q, want siliconflow-dashscope-voice", cfg.Providers.DefaultProfile)
	}
	if cfg.Providers.AutoFallback.Enabled {
		t.Fatal("auto fallback should be disabled in example config until full provider evidence passes")
	}
	if len(cfg.Providers.AutoFallback.Profiles) != 1 || cfg.Providers.AutoFallback.Profiles[0] != "dashscope-cosyvoice" {
		t.Fatalf("auto fallback profiles = %v, want dashscope-cosyvoice", cfg.Providers.AutoFallback.Profiles)
	}
	if profile, ok := cfg.Providers.Profiles["siliconflow-dashscope-voice"]; !ok || profile.ASR != "dashscope-asr" || profile.LLM != "siliconflow-llm" || profile.TTS != "dashscope-tts" {
		t.Fatalf("siliconflow-dashscope-voice profile = %#v/%v, want dashscope-asr/siliconflow-llm/dashscope-tts", profile, ok)
	}
	for profileName, llmProvider := range map[string]string{
		"dashscope-cosyvoice":      "dashscope-llm",
		"doubao-dashscope-voice":   "doubao-llm",
		"minimax-dashscope-voice":  "minimax-llm",
		"deepseek-dashscope-voice": "deepseek-llm",
		"moonshot-dashscope-voice": "moonshot-llm",
	} {
		profile, ok := cfg.Providers.Profiles[profileName]
		if !ok || profile.ASR != "dashscope-asr" || profile.LLM != llmProvider || profile.TTS != "dashscope-tts" {
			t.Fatalf("%s profile = %#v/%v, want dashscope-asr/%s/dashscope-tts", profileName, profile, ok, llmProvider)
		}
	}
	if cfg.Tools.ToolFollowUp.Enabled == nil || !*cfg.Tools.ToolFollowUp.Enabled ||
		cfg.Tools.ToolFollowUp.MaxResults == nil || *cfg.Tools.ToolFollowUp.MaxResults != 3 ||
		cfg.Tools.ToolFollowUp.MaxResultBytes == nil || *cfg.Tools.ToolFollowUp.MaxResultBytes != 2048 ||
		cfg.Tools.ToolFollowUp.AllowToolCalls == nil || *cfg.Tools.ToolFollowUp.AllowToolCalls ||
		cfg.Tools.ToolFollowUp.MaxToolCalls == nil || *cfg.Tools.ToolFollowUp.MaxToolCalls != 1 {
		t.Fatalf("tool follow-up config = %+v, want enabled with bounded defaults", cfg.Tools.ToolFollowUp)
	}
	if len(cfg.Tools.ToolFollowUp.AllowedTools) != 3 ||
		cfg.Tools.ToolFollowUp.AllowedTools[0] != "memory.lookup" ||
		cfg.Tools.ToolFollowUp.AllowedTools[1] != "homeassistant.get_state" ||
		cfg.Tools.ToolFollowUp.AllowedTools[2] != "search.web" {
		t.Fatalf("tool follow-up allowed tools = %+v, want voice-safe read tools", cfg.Tools.ToolFollowUp.AllowedTools)
	}
	if profile, ok := cfg.Providers.Profiles["siliconflow-llm"]; !ok || profile.LLM != "siliconflow-llm" {
		t.Fatalf("siliconflow-llm profile = %#v/%v, want LLM siliconflow-llm", profile, ok)
	}
	if profile, ok := cfg.Providers.Profiles["moonshot-llm"]; !ok || profile.LLM != "moonshot-llm" {
		t.Fatalf("moonshot-llm profile = %#v/%v, want LLM moonshot-llm", profile, ok)
	}
	if cfg.Agent.PersonaPath != "./configs/persona.stackchan.yaml" {
		t.Fatalf("persona path = %q, want ./configs/persona.stackchan.yaml", cfg.Agent.PersonaPath)
	}
	if cfg.Agent.DefaultMode != "casual" {
		t.Fatalf("agent default mode = %q, want casual", cfg.Agent.DefaultMode)
	}
	if cfg.Agent.MemoryDBPath != "./var/memory/stackchan-memory.sqlite3" {
		t.Fatalf("memory db path = %q, want ./var/memory/stackchan-memory.sqlite3", cfg.Agent.MemoryDBPath)
	}
	if cfg.Agent.V21.Enabled {
		t.Fatal("V21 bridge should be disabled in example config")
	}
	if cfg.Agent.V21.BaseURLEnv != "V21_ADAPTER_URL" || cfg.Agent.V21.TokenEnv != "V21_ADAPTER_TOKEN" {
		t.Fatalf("V21 env names = %q/%q, want V21_ADAPTER_URL/V21_ADAPTER_TOKEN", cfg.Agent.V21.BaseURLEnv, cfg.Agent.V21.TokenEnv)
	}
	if cfg.Agent.Hermes.Enabled {
		t.Fatal("Hermes bridge should be disabled in example config")
	}
	if cfg.Agent.Hermes.BaseURLEnv != "HERMES_AGENT_URL" || cfg.Agent.Hermes.TokenEnv != "HERMES_AGENT_KEY" {
		t.Fatalf("Hermes env names = %q/%q, want HERMES_AGENT_URL/HERMES_AGENT_KEY", cfg.Agent.Hermes.BaseURLEnv, cfg.Agent.Hermes.TokenEnv)
	}
	if len(cfg.Agent.Hermes.AllowedToolIntents) != 1 || cfg.Agent.Hermes.AllowedToolIntents[0] != "memory.lookup" {
		t.Fatalf("Hermes allowed tool intents = %+v, want voice-safe non-motion roleplay tools", cfg.Agent.Hermes.AllowedToolIntents)
	}
	if cfg.Agent.Hermes.MaxToolIntents == nil || *cfg.Agent.Hermes.MaxToolIntents != 1 {
		t.Fatalf("Hermes max tool intents = %v, want explicit cap of 1", cfg.Agent.Hermes.MaxToolIntents)
	}
	if cfg.Agent.Hermes.MaxRuntimeRoutesPerMinute != 12 {
		t.Fatalf("Hermes max runtime routes/min = %d, want explicit cap of 12", cfg.Agent.Hermes.MaxRuntimeRoutesPerMinute)
	}
	if cfg.Agent.Hermes.MaxRuntimeInputChars != 360 {
		t.Fatalf("Hermes max runtime input chars = %d, want explicit cap of 360", cfg.Agent.Hermes.MaxRuntimeInputChars)
	}
	if cfg.Agent.Hermes.MaxRuntimeErrorsBeforeCooldown != 2 {
		t.Fatalf("Hermes max runtime errors before cooldown = %d, want explicit cap of 2", cfg.Agent.Hermes.MaxRuntimeErrorsBeforeCooldown)
	}
	if cfg.Agent.Hermes.RuntimeErrorCooldownMS != 30000 {
		t.Fatalf("Hermes runtime error cooldown ms = %d, want explicit cooldown of 30000", cfg.Agent.Hermes.RuntimeErrorCooldownMS)
	}
	if cfg.Agent.OpenClaw.Enabled {
		t.Fatal("OpenClaw bridge should be disabled in example config")
	}
	if cfg.Agent.OpenClaw.BaseURLEnv != "OPENCLAW_WS_URL" || cfg.Agent.OpenClaw.TokenEnv != "OPENCLAW_AGENT_TOKEN" {
		t.Fatalf("OpenClaw env names = %q/%q, want OPENCLAW_WS_URL/OPENCLAW_AGENT_TOKEN", cfg.Agent.OpenClaw.BaseURLEnv, cfg.Agent.OpenClaw.TokenEnv)
	}
	if len(cfg.Agent.OpenClaw.AllowedToolIntents) != 3 || cfg.Agent.OpenClaw.AllowedToolIntents[0] != "memory.lookup" || cfg.Agent.OpenClaw.AllowedToolIntents[1] != "homeassistant.get_state" || cfg.Agent.OpenClaw.AllowedToolIntents[2] != "search.web" {
		t.Fatalf("OpenClaw allowed tool intents = %+v, want voice-safe non-motion tool-mode tools", cfg.Agent.OpenClaw.AllowedToolIntents)
	}
	if cfg.Agent.OpenClaw.MaxToolIntents == nil || *cfg.Agent.OpenClaw.MaxToolIntents != 2 {
		t.Fatalf("OpenClaw max tool intents = %v, want explicit cap of 2", cfg.Agent.OpenClaw.MaxToolIntents)
	}
	if cfg.Agent.OpenClaw.MaxRuntimeRoutesPerMinute != 12 {
		t.Fatalf("OpenClaw max runtime routes/min = %d, want explicit cap of 12", cfg.Agent.OpenClaw.MaxRuntimeRoutesPerMinute)
	}
	if cfg.Agent.OpenClaw.MaxRuntimeInputChars != 360 {
		t.Fatalf("OpenClaw max runtime input chars = %d, want explicit cap of 360", cfg.Agent.OpenClaw.MaxRuntimeInputChars)
	}
	if cfg.Agent.OpenClaw.MaxRuntimeErrorsBeforeCooldown != 2 {
		t.Fatalf("OpenClaw max runtime errors before cooldown = %d, want explicit cap of 2", cfg.Agent.OpenClaw.MaxRuntimeErrorsBeforeCooldown)
	}
	if cfg.Agent.OpenClaw.RuntimeErrorCooldownMS != 30000 {
		t.Fatalf("OpenClaw runtime error cooldown ms = %d, want explicit cooldown of 30000", cfg.Agent.OpenClaw.RuntimeErrorCooldownMS)
	}
	if cfg.Tools.HomeAssistant.Enabled {
		t.Fatal("home assistant tool should be disabled in example config")
	}
	if cfg.Tools.HomeAssistant.TokenEnv != "HOME_ASSISTANT_TOKEN" {
		t.Fatalf("home assistant token env = %q, want HOME_ASSISTANT_TOKEN", cfg.Tools.HomeAssistant.TokenEnv)
	}
	if len(cfg.Tools.HomeAssistant.AllowedActions) != 0 {
		t.Fatalf("home assistant allowed actions = %v, want empty by default", cfg.Tools.HomeAssistant.AllowedActions)
	}
	if cfg.Tools.Search.Enabled {
		t.Fatal("search tool should be disabled in example config")
	}
	if cfg.Tools.Search.BaseURLEnv != "SEARCH_ADAPTER_URL" || cfg.Tools.Search.TokenEnv != "SEARCH_ADAPTER_TOKEN" {
		t.Fatalf("search env names = %q/%q, want SEARCH_ADAPTER_URL/SEARCH_ADAPTER_TOKEN", cfg.Tools.Search.BaseURLEnv, cfg.Tools.Search.TokenEnv)
	}
	if cfg.Tools.Feishu.Enabled {
		t.Fatal("Feishu tool should be disabled in example config")
	}
	if cfg.Tools.Feishu.BaseURL != "https://open.feishu.cn" {
		t.Fatalf("Feishu base URL = %q, want https://open.feishu.cn", cfg.Tools.Feishu.BaseURL)
	}
	if cfg.Tools.Feishu.AppIDEnv != "FEISHU_APP_ID" || cfg.Tools.Feishu.AppSecretEnv != "FEISHU_APP_SECRET" {
		t.Fatalf("Feishu env names = %q/%q, want FEISHU_APP_ID/FEISHU_APP_SECRET", cfg.Tools.Feishu.AppIDEnv, cfg.Tools.Feishu.AppSecretEnv)
	}
	if cfg.Tools.Camera.Enabled {
		t.Fatal("camera tool should be disabled in example config")
	}
	if cfg.Tools.Camera.MaxReasonChars != 120 {
		t.Fatalf("camera max reason chars = %d, want 120", cfg.Tools.Camera.MaxReasonChars)
	}
	if cfg.Tools.Reminder.Enabled {
		t.Fatal("reminder tool should be disabled in example config")
	}
	if cfg.Tools.Reminder.MaxTitleChars != 80 || cfg.Tools.Reminder.MaxMessageChars != 240 {
		t.Fatalf("reminder bounds = %d/%d, want 80/240", cfg.Tools.Reminder.MaxTitleChars, cfg.Tools.Reminder.MaxMessageChars)
	}
	listeningLED := cfg.StackChan.Body.LifecycleLEDs["listening"]
	if listeningLED.R != 0 || listeningLED.G != 168 || listeningLED.B != 0 {
		t.Fatalf("listening lifecycle LED = %+v, want green listen cue", listeningLED)
	}
	speakingLED := cfg.StackChan.Body.LifecycleLEDs["speaking"]
	if speakingLED.R != 0 || speakingLED.G != 0 || speakingLED.B != 168 {
		t.Fatalf("speaking lifecycle LED = %+v, want blue speaking cue", speakingLED)
	}
	thinking := cfg.StackChan.Display.LifecycleScenes["thinking"]
	if thinking.Caption != "我在想。" || thinking.Emotion != "curious" || thinking.Accent != "amber" {
		t.Fatalf("thinking display policy = %+v, want configurable lifecycle scene policy", thinking)
	}
	if thinking.Motion == nil || thinking.Motion.Preset != "attentive" || thinking.Motion.Intensity == nil || *thinking.Motion.Intensity != 0.25 {
		t.Fatalf("thinking motion policy = %+v, want attentive 0.25", thinking.Motion)
	}
	professional := cfg.StackChan.Display.EventScenes["agent_mode.professional"]
	if professional.Scene != "tool" || professional.Caption != "专业模式。" || professional.Emotion != "ready" || professional.Accent != "amber" {
		t.Fatalf("professional event scene policy = %+v, want configured mode display trigger", professional)
	}
	toolRunning := cfg.StackChan.Display.EventScenes["tool.running"]
	if toolRunning.Scene != "tool" || toolRunning.Caption != "我在调用工具。" || toolRunning.Emotion != "ready" {
		t.Fatalf("tool.running event scene policy = %+v, want configured tool display trigger", toolRunning)
	}
	toolSucceeded := cfg.StackChan.Display.EventScenes["tool.succeeded"]
	if toolSucceeded.Scene != "tool" || toolSucceeded.Caption != "工具完成。" || toolSucceeded.Emotion != "happy" {
		t.Fatalf("tool.succeeded event scene policy = %+v, want configured tool success display trigger", toolSucceeded)
	}
	toolFailed := cfg.StackChan.Display.EventScenes["tool.failed"]
	if toolFailed.Scene != "error" || toolFailed.Caption != "工具失败。" || toolFailed.Emotion != "error" {
		t.Fatalf("tool.failed event scene policy = %+v, want configured tool failure display trigger", toolFailed)
	}
	haState := cfg.StackChan.Display.EventScenes["homeassistant.state"]
	if haState.Scene != "tool" || haState.Caption != "我看下设备。" || haState.Emotion != "curious" {
		t.Fatalf("homeassistant.state event scene policy = %+v, want configured HA state display trigger", haState)
	}
	haAction := cfg.StackChan.Display.EventScenes["homeassistant.action"]
	if haAction.Scene != "tool" || haAction.Caption != "我在控制设备。" || haAction.Emotion != "ready" {
		t.Fatalf("homeassistant.action event scene policy = %+v, want configured HA action display trigger", haAction)
	}
	searchWeb := cfg.StackChan.Display.EventScenes["search.web"]
	if searchWeb.Scene != "tool" || searchWeb.Caption != "我去搜索。" || searchWeb.Emotion != "curious" {
		t.Fatalf("search.web event scene policy = %+v, want configured search display trigger", searchWeb)
	}
	memoryUpdated := cfg.StackChan.Display.EventScenes["memory.updated"]
	if memoryUpdated.Scene != "tool" || memoryUpdated.Caption != "我记住了。" || memoryUpdated.Emotion != "happy" {
		t.Fatalf("memory.updated event scene policy = %+v, want configured memory display trigger", memoryUpdated)
	}
	cameraCapturing := cfg.StackChan.Display.EventScenes["camera.capturing"]
	if cameraCapturing.Scene != "tool" || cameraCapturing.Caption != "我看一眼。" || cameraCapturing.Emotion != "curious" {
		t.Fatalf("camera.capturing event scene policy = %+v, want configured camera display trigger", cameraCapturing)
	}
	openClawRoute := cfg.StackChan.Display.EventScenes["agent_route.openclaw"]
	if openClawRoute.Scene != "tool" || openClawRoute.Caption != "我在调度工具脑。" || openClawRoute.Emotion != "ready" {
		t.Fatalf("agent_route.openclaw event scene policy = %+v, want configured OpenClaw route display trigger", openClawRoute)
	}
	v21Route := cfg.StackChan.Display.EventScenes["agent_route.v21"]
	if v21Route.Scene != "tool" || v21Route.Caption != "我去查知识库。" || v21Route.Emotion != "ready" {
		t.Fatalf("agent_route.v21 event scene policy = %+v, want configured V21 route display trigger", v21Route)
	}
	skippedRoute := cfg.StackChan.Display.EventScenes["agent_route.skipped"]
	if skippedRoute.Scene != "tool" || skippedRoute.Caption != "我先用普通对话。" || skippedRoute.Emotion != "curious" {
		t.Fatalf("agent_route.skipped event scene policy = %+v, want configured safe bridge-skip display trigger", skippedRoute)
	}
	reminderDue := cfg.StackChan.Display.EventScenes["reminder.due"]
	if reminderDue.Scene != "tool" || reminderDue.Caption != "提醒到了。" || reminderDue.Emotion != "ready" {
		t.Fatalf("reminder.due event scene policy = %+v, want configured reminder display trigger", reminderDue)
	}
	statusCard := cfg.StackChan.Display.Cards["status.note"]
	if statusCard.Scene != "tool" || statusCard.Emotion != "warm" || statusCard.Accent != "green" || !statusCard.AllowCaption || statusCard.MaxCaptionChars != 28 {
		t.Fatalf("status.note display card = %+v, want configurable bounded card", statusCard)
	}
	if statusCard.Motion == nil || statusCard.Motion.Preset != "nod_soft" || statusCard.Motion.Intensity == nil || *statusCard.Motion.Intensity != 0.2 {
		t.Fatalf("status.note card motion = %+v, want configured card motion", statusCard.Motion)
	}
	expressionNod := cfg.StackChan.Expression.Cues["nod"]
	if expressionNod.Motion == nil || expressionNod.Motion.YawDeg != 0 || expressionNod.Motion.PitchDeg != 16 || expressionNod.Motion.Speed != 220 {
		t.Fatalf("nod expression motion = %+v, want configurable nod motion", expressionNod.Motion)
	}
	if expressionNod.LED == nil || expressionNod.LED.R != 0 || expressionNod.LED.G != 168 || expressionNod.LED.B != 96 {
		t.Fatalf("nod expression LED = %+v, want configurable nod LED", expressionNod.LED)
	}
	if expressionNod.Scene == nil || expressionNod.Scene.Caption != "我点头。" || expressionNod.Scene.Emotion != "ready" {
		t.Fatalf("nod expression scene = %+v, want configurable nod scene", expressionNod.Scene)
	}
	if cfg.StackChan.Body.MinCommandGapMS != 320 || cfg.StackChan.Body.MaxCommandsPerTurn != 6 || cfg.StackChan.Body.ListenStartMotionEnabled {
		t.Fatalf("body config = %+v, want conservative physical StackChan defaults", cfg.StackChan.Body)
	}
	if len(cfg.StackChan.Expression.EventCues) != 0 {
		t.Fatalf("event expression cues = %+v, want disabled default body event feedback", cfg.StackChan.Expression.EventCues)
	}
	agreeSequence := cfg.StackChan.Expression.Sequences["agree.quick"]
	if len(agreeSequence.Cues) != 2 || agreeSequence.Cues[0] != "attentive" || agreeSequence.Cues[1] != "nod" {
		t.Fatalf("agree.quick expression sequence = %+v, want configured cue preset", agreeSequence)
	}
}

func TestValidateFailsWhenDevicesMissing(t *testing.T) {
	cfg := validConfig()
	cfg.Devices = nil

	err := cfg.Validate(mapLookupEnv(map[string]string{}))

	requireValidationProblem(t, err, "devices must include at least one device")
}

func TestValidateFailsWhenAuthTokenEnvMissing(t *testing.T) {
	cfg := validConfig()
	cfg.Devices[0].AuthTokenEnv = ""

	err := cfg.Validate(mapLookupEnv(map[string]string{}))

	requireValidationProblem(t, err, "devices[0].auth_token_env is required")
}

func TestValidateFailsWhenSecretEnvIsNotSet(t *testing.T) {
	cfg := validConfig()

	err := cfg.Validate(mapLookupEnv(map[string]string{}))

	requireValidationProblem(t, err, "missing required secret env STACKCHAN_MAIN_AUTH_TOKEN")
	if strings.Contains(err.Error(), "plain-secret-value") {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestLoadFileStillRequiresAdminTokenForGatewayRuntime(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "stackchan-gateway.example.yaml")

	_, err := LoadFile(path, mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
	}))

	requireValidationProblem(t, err, "missing required secret env STACKCHAN_ADMIN_TOKEN")
}

func TestValidateFailsWhenAdminTokenEnvMissing(t *testing.T) {
	cfg := validConfig()
	cfg.Server.AdminTokenEnv = ""

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
	}))

	requireValidationProblem(t, err, "server.admin_token_env is required when server.admin_addr is set")
}

func TestValidateFailsWhenOTAPathDoesNotStartWithSlash(t *testing.T) {
	cfg := validConfig()
	cfg.Server.OTAPath = "xiaozhi/ota/"

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "server.ota_path must start with / when set")
}

func TestValidateFailsWhenOTAPathConflictsWithWebSocketPath(t *testing.T) {
	cfg := validConfig()
	cfg.Server.OTAPath = cfg.Server.WebsocketPath

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "server.ota_path must not equal server.websocket_path")
}

func TestValidateFailsWhenWebSocketVersionIsInvalid(t *testing.T) {
	cfg := validConfig()
	cfg.Server.WebsocketVersion = 0

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "server.websocket_version must be positive when server.ota_path is set")
}

func TestValidateAllowsSupportedWebSocketVersions(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		cfg := validConfig()
		cfg.Server.WebsocketVersion = version

		err := cfg.Validate(mapLookupEnv(map[string]string{
			"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
			"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		}))

		if err != nil {
			t.Fatalf("Validate() error = %v, want websocket version %d supported", err, version)
		}
	}
}

func TestValidateFailsWhenWebSocketVersionIsUnsupported(t *testing.T) {
	cfg := validConfig()
	cfg.Server.WebsocketVersion = 4

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "server.websocket_version must be 1, 2 or 3")
}

func TestValidateFailsWhenFrameDurationIsNotXiaozhiP0(t *testing.T) {
	cfg := validConfig()
	cfg.Audio.FrameDurationMS = 40

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "audio.frame_duration_ms must be 60 for xiaozhi P0")
}

func TestValidateFailsWhenAutoFallbackProfileIsUnknown(t *testing.T) {
	cfg := validConfig()
	cfg.Providers.AutoFallback.Enabled = true
	cfg.Providers.AutoFallback.Profiles = []string{"missing-profile"}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "providers.auto_fallback.profiles[0] must exist in providers.profiles")
}

func TestValidateFailsWhenAutoFallbackProfileIsNotVoiceRuntime(t *testing.T) {
	cfg := validConfig()
	cfg.Providers.AutoFallback.Enabled = true
	cfg.Providers.AutoFallback.Profiles = []string{"llm-only"}
	cfg.Providers.Profiles["llm-only"] = ProviderProfileConfig{LLM: "mock"}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "providers.auto_fallback.profiles[0] must define asr, llm and tts for voice runtime")
}

func TestValidateFailsWhenMemoryDBPathHasNoPersona(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.PersonaPath = ""

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "agent.persona_path is required when agent.memory_db_path is set")
}

func TestValidateAgentDefaultModeRejectsInvalidValue(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.DefaultMode = "root"

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "agent.default_mode must be casual, roleplay, professional or tool")
}

func TestValidateV21BridgeRequiresSafeConfigurationWhenEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.V21 = AgentV21Config{
		Enabled:              true,
		BaseURLEnv:           "V21_ADAPTER_URL",
		TokenEnv:             "V21_ADAPTER_TOKEN",
		AllowedCollectionIDs: []string{"col_vehicle"},
		TimeoutMS:            1500,
		MaxSpokenChars:       180,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"V21_ADAPTER_URL":           "https://v21.example.internal",
		"V21_ADAPTER_TOKEN":         "v21-secret-must-not-leak",
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateV21BridgeRejectsMissingSecretWithoutLeakingValue(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.V21 = AgentV21Config{
		Enabled:              true,
		BaseURLEnv:           "V21_ADAPTER_URL",
		TokenEnv:             "V21_ADAPTER_TOKEN",
		AllowedCollectionIDs: []string{"col_vehicle"},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"V21_ADAPTER_URL":           "https://v21.example.internal",
	}))

	requireValidationProblem(t, err, "missing required secret env V21_ADAPTER_TOKEN")
	if strings.Contains(err.Error(), "v21-secret") {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestValidateV21BridgeRejectsMissingCollectionAllowlist(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.V21 = AgentV21Config{
		Enabled:    true,
		BaseURLEnv: "V21_ADAPTER_URL",
		TokenEnv:   "V21_ADAPTER_TOKEN",
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"V21_ADAPTER_URL":           "https://v21.example.internal",
		"V21_ADAPTER_TOKEN":         "v21-secret",
	}))

	requireValidationProblem(t, err, "agent.v21.allowed_collection_ids must include at least one collection when enabled")
}

func TestValidateOpenClawBridgeRequiresSafeConfigurationWhenEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:            true,
		BaseURLEnv:         "OPENCLAW_WS_URL",
		TokenEnv:           "OPENCLAW_AGENT_TOKEN",
		TimeoutMS:          1500,
		MaxSpokenChars:     180,
		AllowedToolIntents: []string{"memory.lookup", "stackchan.express"},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"OPENCLAW_WS_URL":           "https://openclaw.example.internal",
		"OPENCLAW_AGENT_TOKEN":      "openclaw-secret-must-not-leak",
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateHermesBridgeRequiresSafeConfigurationWhenEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:            true,
		BaseURLEnv:         "HERMES_AGENT_URL",
		TokenEnv:           "HERMES_AGENT_KEY",
		TimeoutMS:          1500,
		MaxSpokenChars:     180,
		AllowedToolIntents: []string{"memory.lookup", "stackchan.express"},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "https://hermes.example.internal",
		"HERMES_AGENT_KEY":          "hermes-secret-must-not-leak",
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateHermesBridgeRejectsMissingSecretWithoutLeakingValue(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:    true,
		BaseURLEnv: "HERMES_AGENT_URL",
		TokenEnv:   "HERMES_AGENT_KEY",
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "https://hermes.example.internal",
	}))

	requireValidationProblem(t, err, "missing required secret env HERMES_AGENT_KEY")
	if strings.Contains(err.Error(), "hermes-secret") {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestValidateHermesBridgeRejectsUnsafeURLAndLimits(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:        true,
		BaseURLEnv:     "HERMES_AGENT_URL",
		TokenEnv:       "HERMES_AGENT_KEY",
		TimeoutMS:      -1,
		MaxSpokenChars: 601,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "file:///tmp/hermes",
		"HERMES_AGENT_KEY":          "hermes-secret",
	}))

	requireValidationProblem(t, err, "agent.hermes base URL env must contain an http or https URL")
	requireValidationProblem(t, err, "agent.hermes.timeout_ms must be >= 0")
	requireValidationProblem(t, err, "agent.hermes.max_spoken_chars must be between 0 and 600")
}

func TestValidateOpenClawBridgeRejectsMissingSecretWithoutLeakingValue(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:    true,
		BaseURLEnv: "OPENCLAW_WS_URL",
		TokenEnv:   "OPENCLAW_AGENT_TOKEN",
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"OPENCLAW_WS_URL":           "https://openclaw.example.internal",
	}))

	requireValidationProblem(t, err, "missing required secret env OPENCLAW_AGENT_TOKEN")
	if strings.Contains(err.Error(), "openclaw-secret") {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestValidateOpenClawBridgeRejectsUnsafeURLAndLimits(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:        true,
		BaseURLEnv:     "OPENCLAW_WS_URL",
		TokenEnv:       "OPENCLAW_AGENT_TOKEN",
		TimeoutMS:      -1,
		MaxSpokenChars: 601,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"OPENCLAW_WS_URL":           "file:///tmp/openclaw",
		"OPENCLAW_AGENT_TOKEN":      "openclaw-secret",
	}))

	requireValidationProblem(t, err, "agent.openclaw base URL env must contain an http or https URL")
	requireValidationProblem(t, err, "agent.openclaw.timeout_ms must be >= 0")
	requireValidationProblem(t, err, "agent.openclaw.max_spoken_chars must be between 0 and 600")
}

func TestValidateAgentBridgeRejectsUnsafeAllowedToolIntents(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:            true,
		BaseURLEnv:         "HERMES_AGENT_URL",
		TokenEnv:           "HERMES_AGENT_KEY",
		AllowedToolIntents: []string{"memory.lookup", "system.shell", " MEMORY.LOOKUP "},
	}
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:            true,
		BaseURLEnv:         "OPENCLAW_WS_URL",
		TokenEnv:           "OPENCLAW_AGENT_TOKEN",
		AllowedToolIntents: []string{"stackchan.express", "v21.voice_query", "STACKCHAN.EXPRESS"},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "https://hermes.example.internal",
		"HERMES_AGENT_KEY":          "hermes-secret",
		"OPENCLAW_WS_URL":           "https://openclaw.example.internal",
		"OPENCLAW_AGENT_TOKEN":      "openclaw-secret",
	}))

	requireValidationProblem(t, err, "agent.hermes.allowed_tool_intents[1] must be one of the bridge-safe gateway tools")
	requireValidationProblem(t, err, "agent.hermes.allowed_tool_intents[2] duplicates tool memory.lookup after normalization")
	requireValidationProblem(t, err, "agent.openclaw.allowed_tool_intents[1] must be one of the bridge-safe gateway tools")
	requireValidationProblem(t, err, "agent.openclaw.allowed_tool_intents[2] duplicates tool stackchan.express after normalization")
}

func TestValidateAgentBridgeRejectsInvalidMaxToolIntents(t *testing.T) {
	cfg := validConfig()
	negativeCap := -1
	tooLargeCap := 3
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:        true,
		BaseURLEnv:     "HERMES_AGENT_URL",
		TokenEnv:       "HERMES_AGENT_KEY",
		MaxToolIntents: &negativeCap,
	}
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:        true,
		BaseURLEnv:     "OPENCLAW_WS_URL",
		TokenEnv:       "OPENCLAW_AGENT_TOKEN",
		MaxToolIntents: &tooLargeCap,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "https://hermes.example.internal",
		"HERMES_AGENT_KEY":          "hermes-secret",
		"OPENCLAW_WS_URL":           "https://openclaw.example.internal",
		"OPENCLAW_AGENT_TOKEN":      "openclaw-secret",
	}))

	requireValidationProblem(t, err, "agent.hermes.max_tool_intents must be between 0 and 2")
	requireValidationProblem(t, err, "agent.openclaw.max_tool_intents must be between 0 and 2")
}

func TestValidateAgentBridgeRejectsInvalidRuntimeRouteRateLimits(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:                   true,
		BaseURLEnv:                "HERMES_AGENT_URL",
		TokenEnv:                  "HERMES_AGENT_KEY",
		MaxRuntimeRoutesPerMinute: -1,
	}
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:                   true,
		BaseURLEnv:                "OPENCLAW_WS_URL",
		TokenEnv:                  "OPENCLAW_AGENT_TOKEN",
		MaxRuntimeRoutesPerMinute: 121,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "https://hermes.example.internal",
		"HERMES_AGENT_KEY":          "hermes-secret",
		"OPENCLAW_WS_URL":           "https://openclaw.example.internal",
		"OPENCLAW_AGENT_TOKEN":      "openclaw-secret",
	}))

	requireValidationProblem(t, err, "agent.hermes.max_runtime_routes_per_minute must be between 0 and 120")
	requireValidationProblem(t, err, "agent.openclaw.max_runtime_routes_per_minute must be between 0 and 120")
}

func TestValidateAgentBridgeRejectsInvalidRuntimeInputLimits(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:              true,
		BaseURLEnv:           "HERMES_AGENT_URL",
		TokenEnv:             "HERMES_AGENT_KEY",
		MaxRuntimeInputChars: -1,
	}
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:              true,
		BaseURLEnv:           "OPENCLAW_WS_URL",
		TokenEnv:             "OPENCLAW_AGENT_TOKEN",
		MaxRuntimeInputChars: 2001,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "https://hermes.example.internal",
		"HERMES_AGENT_KEY":          "hermes-secret",
		"OPENCLAW_WS_URL":           "https://openclaw.example.internal",
		"OPENCLAW_AGENT_TOKEN":      "openclaw-secret",
	}))

	requireValidationProblem(t, err, "agent.hermes.max_runtime_input_chars must be between 0 and 2000")
	requireValidationProblem(t, err, "agent.openclaw.max_runtime_input_chars must be between 0 and 2000")
}

func TestValidateAgentBridgeRejectsInvalidRuntimeErrorCooldowns(t *testing.T) {
	cfg := validConfig()
	cfg.Agent.Hermes = AgentHermesConfig{
		Enabled:                        true,
		BaseURLEnv:                     "HERMES_AGENT_URL",
		TokenEnv:                       "HERMES_AGENT_KEY",
		MaxRuntimeErrorsBeforeCooldown: -1,
		RuntimeErrorCooldownMS:         30000,
	}
	cfg.Agent.OpenClaw = AgentOpenClawConfig{
		Enabled:                        true,
		BaseURLEnv:                     "OPENCLAW_WS_URL",
		TokenEnv:                       "OPENCLAW_AGENT_TOKEN",
		MaxRuntimeErrorsBeforeCooldown: 11,
		RuntimeErrorCooldownMS:         600001,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HERMES_AGENT_URL":          "https://hermes.example.internal",
		"HERMES_AGENT_KEY":          "hermes-secret",
		"OPENCLAW_WS_URL":           "https://openclaw.example.internal",
		"OPENCLAW_AGENT_TOKEN":      "openclaw-secret",
	}))

	requireValidationProblem(t, err, "agent.hermes.max_runtime_errors_before_cooldown must be between 0 and 10")
	requireValidationProblem(t, err, "agent.openclaw.max_runtime_errors_before_cooldown must be between 0 and 10")
	requireValidationProblem(t, err, "agent.openclaw.runtime_error_cooldown_ms must be between 0 and 600000")
}

func TestValidateHomeAssistantToolRequiresSafeConfigurationWhenEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.HomeAssistant = HomeAssistantConfig{
		Enabled:         true,
		BaseURL:         "https://ha.example.internal",
		TokenEnv:        "HOME_ASSISTANT_TOKEN",
		AllowedEntities: []string{"light.desk"},
		TimeoutMS:       1200,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HOME_ASSISTANT_TOKEN":      "ha-secret-must-not-leak",
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateHomeAssistantToolRejectsMissingSecretWithoutLeakingValue(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.HomeAssistant = HomeAssistantConfig{
		Enabled:         true,
		BaseURL:         "https://ha.example.internal",
		TokenEnv:        "HOME_ASSISTANT_TOKEN",
		AllowedEntities: []string{"light.desk"},
		TimeoutMS:       1200,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "missing required secret env HOME_ASSISTANT_TOKEN")
	if strings.Contains(err.Error(), "ha-secret") {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestValidateHomeAssistantToolRejectsMissingEntityAllowlist(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.HomeAssistant = HomeAssistantConfig{
		Enabled:  true,
		BaseURL:  "https://ha.example.internal",
		TokenEnv: "HOME_ASSISTANT_TOKEN",
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HOME_ASSISTANT_TOKEN":      "ha-secret",
	}))

	requireValidationProblem(t, err, "tools.home_assistant.allowed_entities must include at least one entity when enabled")
}

func TestValidateHomeAssistantActionAllowsConfiguredStaticAction(t *testing.T) {
	cfg := validConfig()
	minBrightness := float64(1)
	maxBrightness := float64(100)
	cfg.Tools.HomeAssistant = HomeAssistantConfig{
		Enabled:         true,
		BaseURL:         "https://ha.example.internal",
		TokenEnv:        "HOME_ASSISTANT_TOKEN",
		AllowedEntities: []string{"light.desk"},
		AllowedActions: []HomeAssistantActionConfig{
			{
				ActionID:    "desk_light_on",
				Description: "Turn on the desk light",
				Domain:      "light",
				Service:     "turn_on",
				EntityIDs:   []string{"light.desk"},
				Data:        map[string]any{"brightness_pct": 70},
				Slots: []HomeAssistantActionSlotConfig{
					{
						Name:        "brightness_pct",
						Description: "Brightness percent from 1 to 100.",
						Type:        "integer",
						Min:         &minBrightness,
						Max:         &maxBrightness,
					},
				},
			},
		},
		TimeoutMS: 1200,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HOME_ASSISTANT_TOKEN":      "ha-secret",
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateHomeAssistantActionRejectsUnsafeSlot(t *testing.T) {
	cfg := validConfig()
	minBrightness := float64(100)
	maxBrightness := float64(1)
	cfg.Tools.HomeAssistant = validHomeAssistantConfigWithAction()
	cfg.Tools.HomeAssistant.AllowedActions[0].Slots = []HomeAssistantActionSlotConfig{
		{
			Name: "entity_id",
			Type: "integer",
			Min:  &minBrightness,
			Max:  &maxBrightness,
		},
		{
			Name: "brightness_pct",
			Type: "object",
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HOME_ASSISTANT_TOKEN":      "ha-secret",
	}))

	requireValidationProblem(t, err, "tools.home_assistant.allowed_actions[0].slots[0].name is reserved")
	requireValidationProblem(t, err, "tools.home_assistant.allowed_actions[0].slots[0].min must be <= max")
	requireValidationProblem(t, err, "tools.home_assistant.allowed_actions[0].slots[1].type must be one of string, number, integer or boolean")
}

func TestValidateHomeAssistantActionRejectsDuplicateActionID(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.HomeAssistant = validHomeAssistantConfigWithAction()
	cfg.Tools.HomeAssistant.AllowedActions = append(cfg.Tools.HomeAssistant.AllowedActions, HomeAssistantActionConfig{
		ActionID:  "desk_light_on",
		Domain:    "light",
		Service:   "turn_off",
		EntityIDs: []string{"light.desk"},
	})

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HOME_ASSISTANT_TOKEN":      "ha-secret",
	}))

	requireValidationProblem(t, err, "tools.home_assistant.allowed_actions[1].action_id must be unique")
}

func TestValidateHomeAssistantActionRejectsEntityOutsideAllowlist(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.HomeAssistant = validHomeAssistantConfigWithAction()
	cfg.Tools.HomeAssistant.AllowedActions[0].EntityIDs = []string{"switch.secret"}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HOME_ASSISTANT_TOKEN":      "ha-secret",
	}))

	requireValidationProblem(t, err, "tools.home_assistant.allowed_actions[0].entity_ids[0] must also appear in tools.home_assistant.allowed_entities")
}

func TestValidateHomeAssistantActionRejectsUnsafeServiceAndDataTarget(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.HomeAssistant = validHomeAssistantConfigWithAction()
	cfg.Tools.HomeAssistant.AllowedActions[0].Domain = "light/../../secret"
	cfg.Tools.HomeAssistant.AllowedActions[0].Data = map[string]any{"target": map[string]any{"entity_id": "switch.secret"}}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"HOME_ASSISTANT_TOKEN":      "ha-secret",
	}))

	requireValidationProblem(t, err, "tools.home_assistant.allowed_actions[0].domain may contain only letters, digits or underscore")
	requireValidationProblem(t, err, "tools.home_assistant.allowed_actions[0].data must not include entity_id or target")
}

func TestValidateSearchToolRequiresSafeConfigurationWhenEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Search = SearchConfig{
		Enabled:        true,
		BaseURLEnv:     "SEARCH_ADAPTER_URL",
		TokenEnv:       "SEARCH_ADAPTER_TOKEN",
		AllowedDomains: []string{"docs.m5stack.com"},
		TimeoutMS:      1500,
		MaxResults:     3,
		MaxQueryChars:  160,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"SEARCH_ADAPTER_URL":        "https://search.example.internal",
		"SEARCH_ADAPTER_TOKEN":      "search-secret-must-not-leak",
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateSearchToolRejectsMissingSecretWithoutLeakingValue(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Search = SearchConfig{
		Enabled:       true,
		BaseURLEnv:    "SEARCH_ADAPTER_URL",
		TokenEnv:      "SEARCH_ADAPTER_TOKEN",
		TimeoutMS:     1500,
		MaxResults:    3,
		MaxQueryChars: 160,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"SEARCH_ADAPTER_URL":        "https://search.example.internal",
	}))

	requireValidationProblem(t, err, "missing required secret env SEARCH_ADAPTER_TOKEN")
	if strings.Contains(err.Error(), "search-secret") {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestValidateSearchToolRejectsUnsafeURLAndLimits(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Search = SearchConfig{
		Enabled:        true,
		BaseURLEnv:     "SEARCH_ADAPTER_URL",
		TokenEnv:       "SEARCH_ADAPTER_TOKEN",
		AllowedDomains: []string{"https://docs.m5stack.com/path"},
		TimeoutMS:      -1,
		MaxResults:     11,
		MaxQueryChars:  301,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"SEARCH_ADAPTER_URL":        "file:///tmp/search",
		"SEARCH_ADAPTER_TOKEN":      "search-secret",
	}))

	requireValidationProblem(t, err, "tools.search base URL env must contain an http or https URL")
	requireValidationProblem(t, err, "tools.search.timeout_ms must be >= 0")
	requireValidationProblem(t, err, "tools.search.max_results must be between 0 and 10")
	requireValidationProblem(t, err, "tools.search.max_query_chars must be between 0 and 300")
	requireValidationProblem(t, err, "tools.search.allowed_domains[0] must be a bare domain name")
}

func TestValidateToolFollowUpPolicyRejectsInvalidBounds(t *testing.T) {
	cfg := validConfig()
	zeroMaxResults := 0
	hugeMaxBytes := 70000
	cfg.Tools.ToolFollowUp.MaxResults = &zeroMaxResults
	cfg.Tools.ToolFollowUp.MaxResultBytes = &hugeMaxBytes
	cfg.Tools.ToolFollowUp.AllowedTools = []string{"memory.lookup", "bad tool name", "memory.lookup"}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "tools.tool_followup.max_results must be between 1 and 8")
	requireValidationProblem(t, err, "tools.tool_followup.max_result_bytes must be between 1 and 16384")
	requireValidationProblem(t, err, "tools.tool_followup.allowed_tools[1] must be a safe tool name")
	requireValidationProblem(t, err, "tools.tool_followup.allowed_tools[2] duplicates tool memory.lookup after normalization")
}

func TestValidateToolFollowUpPolicyRejectsUnsafeRecursiveToolCalls(t *testing.T) {
	cfg := validConfig()
	allowToolCalls := true
	zeroMaxToolCalls := 0
	cfg.Tools.ToolFollowUp.AllowToolCalls = &allowToolCalls
	cfg.Tools.ToolFollowUp.MaxToolCalls = &zeroMaxToolCalls
	cfg.Tools.ToolFollowUp.AllowedTools = nil

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "tools.tool_followup.max_tool_calls must be between 1 and 2")
	requireValidationProblem(t, err, "tools.tool_followup.allowed_tools is required when allow_tool_calls is true")
}

func TestValidateFeishuToolRequiresSafeConfigurationWhenEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Feishu = FeishuConfig{
		Enabled:      true,
		BaseURL:      "https://open.feishu.cn",
		AppIDEnv:     "FEISHU_APP_ID",
		AppSecretEnv: "FEISHU_APP_SECRET",
		TimeoutMS:    1500,
		MaxTextChars: 240,
		AllowedTargets: []FeishuTargetConfig{
			{
				TargetID:      "lab_group",
				Description:   "Lab group",
				ReceiveIDType: "chat_id",
				ReceiveIDEnv:  "FEISHU_LAB_CHAT_ID",
			},
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"FEISHU_APP_ID":             "cli_stackchan",
		"FEISHU_APP_SECRET":         "feishu-secret-must-not-leak",
		"FEISHU_LAB_CHAT_ID":        "oc_lab",
	}))

	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateFeishuToolRejectsMissingSecretWithoutLeakingValue(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Feishu = FeishuConfig{
		Enabled:      true,
		BaseURL:      "https://open.feishu.cn",
		AppIDEnv:     "FEISHU_APP_ID",
		AppSecretEnv: "FEISHU_APP_SECRET",
		AllowedTargets: []FeishuTargetConfig{
			{TargetID: "lab_group", ReceiveIDType: "chat_id", ReceiveIDEnv: "FEISHU_LAB_CHAT_ID"},
		},
		TimeoutMS:    1500,
		MaxTextChars: 240,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"FEISHU_APP_ID":             "cli_stackchan",
		"FEISHU_LAB_CHAT_ID":        "oc_lab",
	}))

	requireValidationProblem(t, err, "missing required secret env FEISHU_APP_SECRET")
	if strings.Contains(err.Error(), "feishu-secret") {
		t.Fatalf("validation error leaked secret value: %v", err)
	}
}

func TestValidateFeishuToolRejectsUnsafeTargetsAndLimits(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Feishu = FeishuConfig{
		Enabled:      true,
		BaseURL:      "file:///tmp/feishu",
		AppIDEnv:     "FEISHU_APP_ID",
		AppSecretEnv: "FEISHU_APP_SECRET",
		AllowedTargets: []FeishuTargetConfig{
			{TargetID: "lab/group", ReceiveIDType: "phone", ReceiveIDEnv: "FEISHU_LAB_CHAT_ID"},
			{TargetID: "lab/group", ReceiveIDType: "chat_id", ReceiveIDEnv: ""},
		},
		TimeoutMS:    -1,
		MaxTextChars: 601,
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
		"FEISHU_APP_ID":             "cli_stackchan",
		"FEISHU_APP_SECRET":         "feishu-secret",
		"FEISHU_LAB_CHAT_ID":        "oc_lab",
	}))

	requireValidationProblem(t, err, "tools.feishu.base_url must be an http or https URL")
	requireValidationProblem(t, err, "tools.feishu.timeout_ms must be >= 0")
	requireValidationProblem(t, err, "tools.feishu.max_text_chars must be between 0 and 600")
	requireValidationProblem(t, err, "tools.feishu.allowed_targets[0].target_id may contain only letters, digits, underscore or dash")
	requireValidationProblem(t, err, "tools.feishu.allowed_targets[0].receive_id_type must be open_id, user_id, union_id, email or chat_id")
	requireValidationProblem(t, err, "tools.feishu.allowed_targets[1].receive_id_env is required")
}

func TestValidateFailsWhenYawMinExceedsHardLimit(t *testing.T) {
	cfg := validConfig()
	cfg.StackChan.Body.YawMinDeg = -129

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "stackchan.body.yaw_min_deg must be >= -128")
}

func TestValidateFailsWhenDisplayLifecycleScenePolicyIsInvalid(t *testing.T) {
	cfg := validConfig()
	badIntensity := 2.0
	cfg.StackChan.Display.LifecycleScenes = map[string]DisplaySceneConfig{
		"raw_pixels": {
			Caption: "bad",
		},
		"thinking": {
			Emotion: "laser_mood",
			Accent:  "neon",
			Motion: &DisplayMotionConfig{
				Preset:    "spin_forever",
				Intensity: &badIntensity,
			},
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "stackchan.display.lifecycle_scenes.raw_pixels must be one of listening, thinking, speaking or idle")
	requireValidationProblem(t, err, "stackchan.display.lifecycle_scenes.thinking.emotion is invalid")
	requireValidationProblem(t, err, "stackchan.display.lifecycle_scenes.thinking.accent is invalid")
	requireValidationProblem(t, err, "stackchan.display.lifecycle_scenes.thinking.motion.preset is invalid")
	requireValidationProblem(t, err, "stackchan.display.lifecycle_scenes.thinking.motion.intensity must be between 0 and 1")
}

func TestValidateFailsWhenBodyLifecycleLEDPolicyIsInvalid(t *testing.T) {
	cfg := validConfig()
	cfg.StackChan.Body.LifecycleLEDs = map[string]LifecycleLEDConfig{
		"raw": {
			R: 1,
			G: 2,
			B: 3,
		},
		" Speaking ": {
			R: -1,
			G: 169,
			B: 999,
		},
		"speaking": {
			R: 0,
			G: 0,
			B: 168,
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "stackchan.body.lifecycle_leds.raw must be one of listening, thinking, speaking or idle")
	requireValidationProblem(t, err, "stackchan.body.lifecycle_leds.speaking.r must be between 0 and 168")
	requireValidationProblem(t, err, "stackchan.body.lifecycle_leds.speaking.g must be between 0 and 168")
	requireValidationProblem(t, err, "stackchan.body.lifecycle_leds.speaking.b must be between 0 and 168")
	requireValidationProblem(t, err, "stackchan.body.lifecycle_leds.speaking duplicates lifecycle speaking after normalization")
}

func TestValidateFailsWhenDisplayEventScenePolicyIsInvalid(t *testing.T) {
	cfg := validConfig()
	cfg.StackChan.Display.EventScenes = map[string]DisplaySceneConfig{
		"agent_mode.root": {
			Scene: "tool",
		},
		"tool.running": {
			Scene:   "raw_pixels",
			Emotion: "ready",
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "stackchan.display.event_scenes.agent_mode.root must be one of agent_mode.casual, agent_mode.professional, agent_mode.roleplay, agent_mode.tool, tool.running, tool.succeeded, tool.failed, homeassistant.state, homeassistant.action, search.web, memory.updated, camera.capturing, reminder.due, agent_route.openclaw, agent_route.hermes, agent_route.v21, agent_route.claude or agent_route.skipped")
	requireValidationProblem(t, err, "stackchan.display.event_scenes.tool.running.scene is invalid")
}

func TestValidateFailsWhenDisplayCardPolicyIsInvalid(t *testing.T) {
	cfg := validConfig()
	badIntensity := 1.5
	cfg.StackChan.Display.Cards = map[string]DisplayCardConfig{
		"bad card": {
			Scene:           "raw_pixels",
			Emotion:         "laser_mood",
			Accent:          "neon",
			AllowCaption:    true,
			MaxCaptionChars: 900,
			Motion: &DisplayMotionConfig{
				Preset:    "spin_forever",
				Intensity: &badIntensity,
			},
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "stackchan.display.cards.bad card must be a safe card id")
	requireValidationProblem(t, err, "stackchan.display.cards.bad card.scene is invalid")
	requireValidationProblem(t, err, "stackchan.display.cards.bad card.emotion is invalid")
	requireValidationProblem(t, err, "stackchan.display.cards.bad card.accent is invalid")
	requireValidationProblem(t, err, "stackchan.display.cards.bad card.motion.preset is invalid")
	requireValidationProblem(t, err, "stackchan.display.cards.bad card.motion.intensity must be between 0 and 1")
	requireValidationProblem(t, err, "stackchan.display.cards.bad card.max_caption_chars must be between 1 and stackchan.display.max_caption_chars")
}

func TestValidateFailsWhenExpressionCuePolicyIsInvalid(t *testing.T) {
	cfg := validConfig()
	cfg.StackChan.Expression.LifecycleCues = map[string]string{
		"thinking": "spin_forever",
		"raw":      "thinking",
	}
	cfg.StackChan.Expression.EventCues = map[string]string{
		"agent_route.root": "nod",
		"tool.failed":      "spin_forever",
		"tool.running":     "nod",
		" Tool.Running ":   "thinking",
	}
	cfg.StackChan.Expression.Cues = map[string]ExpressionCueConfig{
		"spin_forever": {
			Motion: &ExpressionMotionConfig{YawDeg: 0, PitchDeg: 0, Speed: 150},
		},
		"nod": {
			Motion: &ExpressionMotionConfig{YawDeg: 90, PitchDeg: 99, Speed: 5000},
			LED:    &ExpressionLEDConfig{R: 300, G: -1, B: 16},
			Scene: &DisplaySceneConfig{
				Scene:   "raw_pixels",
				Emotion: "laser_mood",
			},
		},
		" attentive ": {
			Motion: &ExpressionMotionConfig{YawDeg: 0, PitchDeg: 0, Speed: 150},
		},
		"ATTENTIVE": {
			Motion: &ExpressionMotionConfig{YawDeg: 0, PitchDeg: 0, Speed: 150},
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "stackchan.expression.cues.spin_forever must be one of attentive, nod, celebrate, thinking or settle")
	requireValidationProblem(t, err, "duplicates cue attentive after normalization")
	requireValidationProblem(t, err, "stackchan.expression.cues.nod.motion.yaw_deg must be between stackchan.body.yaw_min_deg and yaw_max_deg")
	requireValidationProblem(t, err, "stackchan.expression.cues.nod.motion.pitch_deg must be between stackchan.body.pitch_min_deg and pitch_max_deg")
	requireValidationProblem(t, err, "stackchan.expression.cues.nod.motion.speed must be between 100 and 1000")
	requireValidationProblem(t, err, "stackchan.expression.cues.nod.led.r must be between 0 and 168")
	requireValidationProblem(t, err, "stackchan.expression.cues.nod.led.g must be between 0 and 168")
	requireValidationProblem(t, err, "stackchan.expression.cues.nod.scene.scene is invalid")
	requireValidationProblem(t, err, "stackchan.expression.cues.nod.scene.emotion is invalid")
	requireValidationProblem(t, err, "stackchan.expression.lifecycle_cues.thinking must be one of attentive, nod, celebrate, thinking or settle")
	requireValidationProblem(t, err, "stackchan.expression.lifecycle_cues.raw must be one of listening, thinking, speaking or idle")
	requireValidationProblem(t, err, "stackchan.expression.event_cues.agent_route.root must be one of agent_mode.casual, agent_mode.professional, agent_mode.roleplay, agent_mode.tool, tool.running, tool.succeeded, tool.failed, homeassistant.state, homeassistant.action, search.web, memory.updated, camera.capturing, reminder.due, agent_route.openclaw, agent_route.hermes, agent_route.v21, agent_route.claude or agent_route.skipped")
	requireValidationProblem(t, err, "stackchan.expression.event_cues.tool.failed must be one of attentive, nod, celebrate, thinking or settle")
	requireValidationProblem(t, err, "stackchan.expression.event_cues.tool.running duplicates event tool.running after normalization")
}

func TestValidateFailsWhenExpressionSequencePolicyIsInvalid(t *testing.T) {
	cfg := validConfig()
	cfg.StackChan.Expression.Sequences = map[string]ExpressionSequenceConfig{
		"bad sequence": {
			Cues: []string{"nod"},
		},
		"empty": {
			Cues: nil,
		},
		"too.long": {
			Cues: []string{"attentive", "thinking", "nod", "settle"},
		},
		"unknown": {
			Cues: []string{"nod", "spin_forever"},
		},
		" AGREE.QUICK ": {
			Cues: []string{"attentive", "nod"},
		},
		"agree.quick": {
			Cues: []string{"attentive", "nod"},
		},
	}

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "stackchan.expression.sequences.bad sequence must be a safe sequence id")
	requireValidationProblem(t, err, "stackchan.expression.sequences.empty.cues must contain between 1 and 3 cues")
	requireValidationProblem(t, err, "stackchan.expression.sequences.too.long.cues must contain between 1 and 3 cues")
	requireValidationProblem(t, err, "stackchan.expression.sequences.unknown.cues[1] must be one of attentive, nod, celebrate, thinking or settle")
	requireValidationProblem(t, err, "stackchan.expression.sequences.agree.quick duplicates sequence agree.quick after normalization")
}

func TestValidateReminderToolBounds(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Reminder.Enabled = true
	cfg.Tools.Reminder.MaxTitleChars = 0
	cfg.Tools.Reminder.MaxMessageChars = 800

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "tools.reminder.max_title_chars must be between 1 and 120 when enabled")
	requireValidationProblem(t, err, "tools.reminder.max_message_chars must be between 1 and 600 when enabled")
}

func TestValidateCameraToolBounds(t *testing.T) {
	cfg := validConfig()
	cfg.Tools.Camera.Enabled = true
	cfg.Tools.Camera.MaxReasonChars = 0

	err := cfg.Validate(mapLookupEnv(map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "secret",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret",
	}))

	requireValidationProblem(t, err, "tools.camera.max_reason_chars must be between 1 and 160 when enabled")
}

func TestLoadFileReportsParseErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	_, err := LoadFile(path, mapLookupEnv(map[string]string{}))

	if err == nil {
		t.Fatal("LoadFile() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("error = %q, want parse config", err.Error())
	}
}

func requireValidationProblem(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("Validate() error = nil, want problem %q", want)
	}
	if !IsValidationError(err) {
		t.Fatalf("Validate() error type = %T, want ValidationError", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Validate() error = %q, want problem %q", err.Error(), want)
	}
}

func validHomeAssistantConfigWithAction() HomeAssistantConfig {
	return HomeAssistantConfig{
		Enabled:         true,
		BaseURL:         "https://ha.example.internal",
		TokenEnv:        "HOME_ASSISTANT_TOKEN",
		AllowedEntities: []string{"light.desk"},
		AllowedActions: []HomeAssistantActionConfig{
			{
				ActionID:  "desk_light_on",
				Domain:    "light",
				Service:   "turn_on",
				EntityIDs: []string{"light.desk"},
			},
		},
		TimeoutMS: 1200,
	}
}

func validConfig() Config {
	return Config{
		Server: ServerConfig{
			PublicBaseURL:         "https://stackchan.example.internal",
			ListenAddr:            "127.0.0.1:8080",
			WebsocketPath:         "/xiaozhi/v1/ws",
			OTAPath:               "/xiaozhi/ota/",
			WebsocketPublicURLEnv: "STACKCHAN_WEBSOCKET_URL",
			WebsocketVersion:      1,
			AdminAddr:             "127.0.0.1:8081",
			AdminTokenEnv:         "STACKCHAN_ADMIN_TOKEN",
			MetricsAddr:           "127.0.0.1:9090",
			ShutdownTimeoutMS:     5000,
		},
		Devices: []DeviceConfig{
			{
				DeviceID:     "stackchan-s3-main",
				ClientID:     "stackchan-s3-main-client",
				AuthTokenEnv: "STACKCHAN_MAIN_AUTH_TOKEN",
				DefaultMode:  "auto",
				AllowMCPTools: []string{
					"self.robot.get_head_angles",
				},
			},
		},
		Audio: AudioConfig{
			UplinkSampleRateHz:   16000,
			DownlinkSampleRateHz: 24000,
			Channels:             1,
			FrameDurationMS:      60,
			DownlinkQueueMS:      1200,
			MaxTurnMS:            30000,
		},
		Providers: ProvidersConfig{
			DefaultProfile: "cn-low-latency-cascade",
			Profiles: map[string]ProviderProfileConfig{
				"cn-low-latency-cascade": {
					ASR: "mock",
					LLM: "mock",
					TTS: "mock",
				},
			},
		},
		Agent: AgentConfig{
			DefaultMode:    "casual",
			PersonaPath:    "./configs/persona.stackchan.yaml",
			MemoryDBPath:   "./var/memory/stackchan-memory.sqlite3",
			MemoryMaxItems: 5,
			RecentTurns:    8,
		},
		StackChan: StackChanConfig{
			Body: BodyConfig{
				MinCommandGapMS:    160,
				MaxCommandsPerTurn: 16,
				YawMinDeg:          -45,
				YawMaxDeg:          45,
				PitchMinDeg:        0,
				PitchMaxDeg:        45,
				DefaultSpeed:       150,
			},
			Display: DisplayConfig{
				SceneTTLMS:      1800,
				MaxCaptionChars: 48,
			},
		},
		Observability: ObservabilityConfig{
			TraceJSONLPath: "./var/traces/turns.jsonl",
			RedactSecrets:  true,
		},
	}
}
