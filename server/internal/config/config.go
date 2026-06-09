package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Devices       []DeviceConfig      `yaml:"devices"`
	Audio         AudioConfig         `yaml:"audio"`
	Providers     ProvidersConfig     `yaml:"providers"`
	Agent         AgentConfig         `yaml:"agent"`
	Tools         ToolsConfig         `yaml:"tools"`
	StackChan     StackChanConfig     `yaml:"stackchan"`
	Observability ObservabilityConfig `yaml:"observability"`
}

type ServerConfig struct {
	PublicBaseURL         string `yaml:"public_base_url"`
	ListenAddr            string `yaml:"listen_addr"`
	WebsocketPath         string `yaml:"websocket_path"`
	OTAPath               string `yaml:"ota_path"`
	WebsocketPublicURLEnv string `yaml:"websocket_public_url_env"`
	WebsocketVersion      int    `yaml:"websocket_version"`
	AdminAddr             string `yaml:"admin_addr"`
	AdminTokenEnv         string `yaml:"admin_token_env"`
	MetricsAddr           string `yaml:"metrics_addr"`
	ShutdownTimeoutMS     int    `yaml:"shutdown_timeout_ms"`
}

type DeviceConfig struct {
	DeviceID      string   `yaml:"device_id"`
	ClientID      string   `yaml:"client_id"`
	AuthTokenEnv  string   `yaml:"auth_token_env"`
	DefaultMode   string   `yaml:"default_mode"`
	AllowMCPTools []string `yaml:"allow_mcp_tools"`
}

type AudioConfig struct {
	UplinkSampleRateHz   int `yaml:"uplink_sample_rate_hz"`
	DownlinkSampleRateHz int `yaml:"downlink_sample_rate_hz"`
	Channels             int `yaml:"channels"`
	FrameDurationMS      int `yaml:"frame_duration_ms"`
	DownlinkQueueMS      int `yaml:"downlink_queue_ms"`
	MaxTurnMS            int `yaml:"max_turn_ms"`
}

type ProvidersConfig struct {
	DefaultProfile string                           `yaml:"default_profile"`
	Mock           ProviderMockConfig               `yaml:"mock"`
	AutoFallback   ProviderAutoFallbackConfig       `yaml:"auto_fallback"`
	Profiles       map[string]ProviderProfileConfig `yaml:"profiles"`
}

type ProviderMockConfig struct {
	ASRFinalDelayMS      int    `yaml:"asr_final_delay_ms"`
	ASRAutoFinalOnAudio  bool   `yaml:"asr_auto_final_on_audio"`
	ASRFinalText         string `yaml:"asr_final_text"`
	LLMFirstTokenDelayMS int    `yaml:"llm_first_token_delay_ms"`
	TTSFirstFrameDelayMS int    `yaml:"tts_first_frame_delay_ms"`
	TTSFrameCount        int    `yaml:"tts_frame_count"`
}

type ProviderAutoFallbackConfig struct {
	Enabled            bool     `yaml:"enabled"`
	Profiles           []string `yaml:"profiles"`
	YellowFirstAudioMS int      `yaml:"yellow_first_audio_ms"`
	ConsecutiveYellow  int      `yaml:"consecutive_yellow"`
	ConsecutiveErrors  int      `yaml:"consecutive_errors"`
}

type ProviderProfileConfig struct {
	ASR string `yaml:"asr"`
	LLM string `yaml:"llm"`
	TTS string `yaml:"tts"`
}

type AgentConfig struct {
	DefaultMode    string              `yaml:"default_mode"`
	PersonaPath    string              `yaml:"persona_path"`
	MemoryDBPath   string              `yaml:"memory_db_path"`
	MemoryMaxItems int                 `yaml:"memory_max_items"`
	RecentTurns    int                 `yaml:"recent_turns"`
	V21            AgentV21Config      `yaml:"v21"`
	Hermes         AgentHermesConfig   `yaml:"hermes"`
	OpenClaw       AgentOpenClawConfig `yaml:"openclaw"`
}

type AgentV21Config struct {
	Enabled              bool     `yaml:"enabled"`
	BaseURLEnv           string   `yaml:"base_url_env"`
	TokenEnv             string   `yaml:"token_env"`
	AllowedCollectionIDs []string `yaml:"allowed_collection_ids"`
	TimeoutMS            int      `yaml:"timeout_ms"`
	MaxSpokenChars       int      `yaml:"max_spoken_chars"`
}

type AgentOpenClawConfig struct {
	Enabled                        bool     `yaml:"enabled"`
	BaseURLEnv                     string   `yaml:"base_url_env"`
	TokenEnv                       string   `yaml:"token_env"`
	TimeoutMS                      int      `yaml:"timeout_ms"`
	MaxSpokenChars                 int      `yaml:"max_spoken_chars"`
	AllowedToolIntents             []string `yaml:"allowed_tool_intents"`
	MaxToolIntents                 *int     `yaml:"max_tool_intents"`
	MaxRuntimeRoutesPerMinute      int      `yaml:"max_runtime_routes_per_minute"`
	MaxRuntimeInputChars           int      `yaml:"max_runtime_input_chars"`
	MaxRuntimeErrorsBeforeCooldown int      `yaml:"max_runtime_errors_before_cooldown"`
	RuntimeErrorCooldownMS         int      `yaml:"runtime_error_cooldown_ms"`
}

type AgentHermesConfig struct {
	Enabled                        bool     `yaml:"enabled"`
	BaseURLEnv                     string   `yaml:"base_url_env"`
	TokenEnv                       string   `yaml:"token_env"`
	TimeoutMS                      int      `yaml:"timeout_ms"`
	MaxSpokenChars                 int      `yaml:"max_spoken_chars"`
	AllowedToolIntents             []string `yaml:"allowed_tool_intents"`
	MaxToolIntents                 *int     `yaml:"max_tool_intents"`
	MaxRuntimeRoutesPerMinute      int      `yaml:"max_runtime_routes_per_minute"`
	MaxRuntimeInputChars           int      `yaml:"max_runtime_input_chars"`
	MaxRuntimeErrorsBeforeCooldown int      `yaml:"max_runtime_errors_before_cooldown"`
	RuntimeErrorCooldownMS         int      `yaml:"runtime_error_cooldown_ms"`
}

type ToolsConfig struct {
	HomeAssistant HomeAssistantConfig `yaml:"home_assistant"`
	Search        SearchConfig        `yaml:"search"`
	Feishu        FeishuConfig        `yaml:"feishu"`
	Camera        CameraConfig        `yaml:"camera"`
	Reminder      ReminderConfig      `yaml:"reminder"`
	ToolFollowUp  ToolFollowUpConfig  `yaml:"tool_followup"`
}

type ToolFollowUpConfig struct {
	Enabled        *bool    `yaml:"enabled"`
	MaxResults     *int     `yaml:"max_results"`
	MaxResultBytes *int     `yaml:"max_result_bytes"`
	AllowedTools   []string `yaml:"allowed_tools"`
	AllowToolCalls *bool    `yaml:"allow_tool_calls"`
	MaxToolCalls   *int     `yaml:"max_tool_calls"`
}

type HomeAssistantConfig struct {
	Enabled         bool                        `yaml:"enabled"`
	BaseURL         string                      `yaml:"base_url"`
	TokenEnv        string                      `yaml:"token_env"`
	AllowedEntities []string                    `yaml:"allowed_entities"`
	AllowedActions  []HomeAssistantActionConfig `yaml:"allowed_actions"`
	TimeoutMS       int                         `yaml:"timeout_ms"`
}

type HomeAssistantActionConfig struct {
	ActionID    string                          `yaml:"action_id"`
	Description string                          `yaml:"description"`
	Domain      string                          `yaml:"domain"`
	Service     string                          `yaml:"service"`
	EntityIDs   []string                        `yaml:"entity_ids"`
	Data        map[string]any                  `yaml:"data"`
	Slots       []HomeAssistantActionSlotConfig `yaml:"slots"`
}

type HomeAssistantActionSlotConfig struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Type        string   `yaml:"type"`
	Enum        []string `yaml:"enum"`
	Min         *float64 `yaml:"min"`
	Max         *float64 `yaml:"max"`
	MaxChars    int      `yaml:"max_chars"`
}

type SearchConfig struct {
	Enabled        bool     `yaml:"enabled"`
	BaseURLEnv     string   `yaml:"base_url_env"`
	TokenEnv       string   `yaml:"token_env"`
	AllowedDomains []string `yaml:"allowed_domains"`
	TimeoutMS      int      `yaml:"timeout_ms"`
	MaxResults     int      `yaml:"max_results"`
	MaxQueryChars  int      `yaml:"max_query_chars"`
}

type FeishuConfig struct {
	Enabled        bool                 `yaml:"enabled"`
	BaseURL        string               `yaml:"base_url"`
	AppIDEnv       string               `yaml:"app_id_env"`
	AppSecretEnv   string               `yaml:"app_secret_env"`
	AllowedTargets []FeishuTargetConfig `yaml:"allowed_targets"`
	TimeoutMS      int                  `yaml:"timeout_ms"`
	MaxTextChars   int                  `yaml:"max_text_chars"`
}

type FeishuTargetConfig struct {
	TargetID      string `yaml:"target_id"`
	Description   string `yaml:"description"`
	ReceiveIDType string `yaml:"receive_id_type"`
	ReceiveIDEnv  string `yaml:"receive_id_env"`
}

type CameraConfig struct {
	Enabled        bool `yaml:"enabled"`
	MaxReasonChars int  `yaml:"max_reason_chars"`
}

type ReminderConfig struct {
	Enabled         bool `yaml:"enabled"`
	MaxTitleChars   int  `yaml:"max_title_chars"`
	MaxMessageChars int  `yaml:"max_message_chars"`
}

type StackChanConfig struct {
	Body       BodyConfig       `yaml:"body"`
	Display    DisplayConfig    `yaml:"display"`
	Expression ExpressionConfig `yaml:"expression"`
}

type BodyConfig struct {
	MinCommandGapMS          int                           `yaml:"min_command_gap_ms"`
	MaxCommandsPerTurn       int                           `yaml:"max_commands_per_turn"`
	ListenStartMotionEnabled bool                          `yaml:"listen_start_motion_enabled"`
	YawMinDeg                int                           `yaml:"yaw_min_deg"`
	YawMaxDeg                int                           `yaml:"yaw_max_deg"`
	PitchMinDeg              int                           `yaml:"pitch_min_deg"`
	PitchMaxDeg              int                           `yaml:"pitch_max_deg"`
	DefaultSpeed             int                           `yaml:"default_speed"`
	LifecycleLEDs            map[string]LifecycleLEDConfig `yaml:"lifecycle_leds"`
}

type LifecycleLEDConfig struct {
	Enabled *bool `yaml:"enabled"`
	R       int   `yaml:"r"`
	G       int   `yaml:"g"`
	B       int   `yaml:"b"`
}

type DisplayConfig struct {
	SceneTTLMS      int                           `yaml:"scene_ttl_ms"`
	MaxCaptionChars int                           `yaml:"max_caption_chars"`
	LifecycleScenes map[string]DisplaySceneConfig `yaml:"lifecycle_scenes"`
	EventScenes     map[string]DisplaySceneConfig `yaml:"event_scenes"`
	Cards           map[string]DisplayCardConfig  `yaml:"cards"`
}

type DisplaySceneConfig struct {
	Scene   string               `yaml:"scene"`
	Emotion string               `yaml:"emotion"`
	Caption string               `yaml:"caption"`
	Accent  string               `yaml:"accent"`
	Motion  *DisplayMotionConfig `yaml:"motion"`
}

type DisplayCardConfig struct {
	Scene           string               `yaml:"scene"`
	Emotion         string               `yaml:"emotion"`
	Caption         string               `yaml:"caption"`
	Accent          string               `yaml:"accent"`
	Motion          *DisplayMotionConfig `yaml:"motion"`
	AllowCaption    bool                 `yaml:"allow_caption"`
	MaxCaptionChars int                  `yaml:"max_caption_chars"`
}

type ExpressionConfig struct {
	ProviderToolsEnabled bool                                `yaml:"provider_tools_enabled"`
	LifecycleCues        map[string]string                   `yaml:"lifecycle_cues"`
	EventCues            map[string]string                   `yaml:"event_cues"`
	Sequences            map[string]ExpressionSequenceConfig `yaml:"sequences"`
	Cues                 map[string]ExpressionCueConfig      `yaml:"cues"`
}

type ExpressionSequenceConfig struct {
	Cues []string `yaml:"cues"`
}

type ExpressionCueConfig struct {
	Motion *ExpressionMotionConfig `yaml:"motion"`
	LED    *ExpressionLEDConfig    `yaml:"led"`
	Scene  *DisplaySceneConfig     `yaml:"scene"`
}

type ExpressionMotionConfig struct {
	YawDeg   int `yaml:"yaw_deg"`
	PitchDeg int `yaml:"pitch_deg"`
	Speed    int `yaml:"speed"`
}

type ExpressionLEDConfig struct {
	R int `yaml:"r"`
	G int `yaml:"g"`
	B int `yaml:"b"`
}

type DisplayMotionConfig struct {
	Preset    string   `yaml:"preset"`
	Intensity *float64 `yaml:"intensity"`
}

type ObservabilityConfig struct {
	TraceJSONLPath string `yaml:"trace_jsonl_path"`
	RedactSecrets  bool   `yaml:"redact_secrets"`
}

type ValidationOptions struct {
	SkipAdminAuth bool
}

func LoadFile(path string, lookup LookupEnv) (*Config, error) {
	return LoadFileWithValidationOptions(path, lookup, ValidationOptions{})
}

func LoadFileWithValidationOptions(path string, lookup LookupEnv, options ValidationOptions) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if err := cfg.ValidateWithOptions(lookup, options); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) DeviceByID(deviceID string) (DeviceConfig, bool) {
	for _, device := range c.Devices {
		if device.DeviceID == deviceID {
			return device, true
		}
	}
	return DeviceConfig{}, false
}
