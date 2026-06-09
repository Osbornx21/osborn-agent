package config

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"stackchan-gateway/internal/agents"
	"stackchan-gateway/internal/stackchan"
)

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "config validation failed: " + strings.Join(e.Problems, "; ")
}

func (c *Config) Validate(lookup LookupEnv) error {
	return c.ValidateWithOptions(lookup, ValidationOptions{})
}

func (c *Config) ValidateWithOptions(lookup LookupEnv, options ValidationOptions) error {
	if lookup == nil {
		lookup = OSLookupEnv
	}

	var problems []string

	if strings.TrimSpace(c.Server.ListenAddr) == "" {
		problems = append(problems, "server.listen_addr is required")
	}
	if strings.TrimSpace(c.Server.WebsocketPath) == "" {
		problems = append(problems, "server.websocket_path is required")
	}
	if otaPath := strings.TrimSpace(c.Server.OTAPath); otaPath != "" {
		if !strings.HasPrefix(otaPath, "/") {
			problems = append(problems, "server.ota_path must start with / when set")
		}
		if otaPath == strings.TrimSpace(c.Server.WebsocketPath) {
			problems = append(problems, "server.ota_path must not equal server.websocket_path")
		}
		if c.Server.WebsocketVersion <= 0 {
			problems = append(problems, "server.websocket_version must be positive when server.ota_path is set")
		} else if c.Server.WebsocketVersion < 1 || c.Server.WebsocketVersion > 3 {
			problems = append(problems, "server.websocket_version must be 1, 2 or 3")
		}
	}
	if strings.TrimSpace(c.Server.AdminAddr) != "" && !options.SkipAdminAuth {
		adminTokenEnv := strings.TrimSpace(c.Server.AdminTokenEnv)
		if adminTokenEnv == "" {
			problems = append(problems, "server.admin_token_env is required when server.admin_addr is set")
		} else if token, ok := lookup(adminTokenEnv); !ok || token == "" {
			problems = append(problems, "missing required secret env "+adminTokenEnv)
		}
	}
	if c.Server.ShutdownTimeoutMS <= 0 {
		problems = append(problems, "server.shutdown_timeout_ms must be positive")
	}

	if len(c.Devices) == 0 {
		problems = append(problems, "devices must include at least one device")
	}
	for index, device := range c.Devices {
		prefix := fmt.Sprintf("devices[%d]", index)
		if strings.TrimSpace(device.DeviceID) == "" {
			problems = append(problems, prefix+".device_id is required")
		}
		if strings.TrimSpace(device.ClientID) == "" {
			problems = append(problems, prefix+".client_id is required")
		}
		authTokenEnv := strings.TrimSpace(device.AuthTokenEnv)
		if authTokenEnv == "" {
			problems = append(problems, prefix+".auth_token_env is required")
		} else if token, ok := lookup(authTokenEnv); !ok || token == "" {
			problems = append(problems, "missing required secret env "+authTokenEnv)
		}
		if mode := strings.TrimSpace(device.DefaultMode); mode != "" && mode != "auto" && mode != "manual" {
			problems = append(problems, prefix+".default_mode must be auto or manual for P0")
		}
	}

	if c.Audio.UplinkSampleRateHz != 16000 {
		problems = append(problems, "audio.uplink_sample_rate_hz must be 16000 for xiaozhi P0")
	}
	if c.Audio.DownlinkSampleRateHz != 24000 {
		problems = append(problems, "audio.downlink_sample_rate_hz must be 24000 for xiaozhi P0")
	}
	if c.Audio.Channels != 1 {
		problems = append(problems, "audio.channels must be 1 for xiaozhi P0")
	}
	if c.Audio.FrameDurationMS != 60 {
		problems = append(problems, "audio.frame_duration_ms must be 60 for xiaozhi P0")
	}
	if c.Audio.DownlinkQueueMS <= 0 {
		problems = append(problems, "audio.downlink_queue_ms must be positive")
	}
	if c.Audio.MaxTurnMS <= 0 {
		problems = append(problems, "audio.max_turn_ms must be positive")
	}

	if strings.TrimSpace(c.Providers.DefaultProfile) == "" {
		problems = append(problems, "providers.default_profile is required")
	} else if _, ok := c.Providers.Profiles[c.Providers.DefaultProfile]; !ok {
		problems = append(problems, "providers.default_profile must exist in providers.profiles")
	}
	if c.Providers.AutoFallback.Enabled {
		if len(c.Providers.AutoFallback.Profiles) == 0 {
			problems = append(problems, "providers.auto_fallback.profiles must include at least one profile when enabled")
		}
		for index, profile := range c.Providers.AutoFallback.Profiles {
			profile = strings.TrimSpace(profile)
			if profile == "" {
				problems = append(problems, fmt.Sprintf("providers.auto_fallback.profiles[%d] is required", index))
				continue
			}
			profileConfig, ok := c.Providers.Profiles[profile]
			if !ok {
				problems = append(problems, fmt.Sprintf("providers.auto_fallback.profiles[%d] must exist in providers.profiles", index))
				continue
			}
			if !isVoiceRuntimeProfile(profileConfig) {
				problems = append(problems, fmt.Sprintf("providers.auto_fallback.profiles[%d] must define asr, llm and tts for voice runtime", index))
			}
		}
	}
	if c.Providers.AutoFallback.YellowFirstAudioMS < 0 {
		problems = append(problems, "providers.auto_fallback.yellow_first_audio_ms must be >= 0")
	}
	if c.Providers.AutoFallback.ConsecutiveYellow < 0 {
		problems = append(problems, "providers.auto_fallback.consecutive_yellow must be >= 0")
	}
	if c.Providers.AutoFallback.ConsecutiveErrors < 0 {
		problems = append(problems, "providers.auto_fallback.consecutive_errors must be >= 0")
	}

	if c.Agent.MemoryMaxItems < 0 {
		problems = append(problems, "agent.memory_max_items must be >= 0")
	}
	if c.Agent.RecentTurns < 0 {
		problems = append(problems, "agent.recent_turns must be >= 0")
	}
	if mode := strings.TrimSpace(c.Agent.DefaultMode); mode != "" && !isValidAgentMode(mode) {
		problems = append(problems, "agent.default_mode must be casual, roleplay, professional or tool")
	}
	if strings.TrimSpace(c.Agent.MemoryDBPath) != "" && strings.TrimSpace(c.Agent.PersonaPath) == "" {
		problems = append(problems, "agent.persona_path is required when agent.memory_db_path is set")
	}
	problems = append(problems, validateAgentAllowedToolIntents("agent.hermes.allowed_tool_intents", c.Agent.Hermes.AllowedToolIntents)...)
	problems = append(problems, validateAgentAllowedToolIntents("agent.openclaw.allowed_tool_intents", c.Agent.OpenClaw.AllowedToolIntents)...)
	problems = append(problems, validateAgentMaxToolIntents("agent.hermes.max_tool_intents", c.Agent.Hermes.MaxToolIntents)...)
	problems = append(problems, validateAgentMaxToolIntents("agent.openclaw.max_tool_intents", c.Agent.OpenClaw.MaxToolIntents)...)
	problems = append(problems, validateAgentMaxRuntimeRoutesPerMinute("agent.hermes.max_runtime_routes_per_minute", c.Agent.Hermes.MaxRuntimeRoutesPerMinute)...)
	problems = append(problems, validateAgentMaxRuntimeRoutesPerMinute("agent.openclaw.max_runtime_routes_per_minute", c.Agent.OpenClaw.MaxRuntimeRoutesPerMinute)...)
	problems = append(problems, validateAgentMaxRuntimeInputChars("agent.hermes.max_runtime_input_chars", c.Agent.Hermes.MaxRuntimeInputChars)...)
	problems = append(problems, validateAgentMaxRuntimeInputChars("agent.openclaw.max_runtime_input_chars", c.Agent.OpenClaw.MaxRuntimeInputChars)...)
	problems = append(problems, validateAgentRuntimeErrorCooldown("agent.hermes", c.Agent.Hermes.MaxRuntimeErrorsBeforeCooldown, c.Agent.Hermes.RuntimeErrorCooldownMS)...)
	problems = append(problems, validateAgentRuntimeErrorCooldown("agent.openclaw", c.Agent.OpenClaw.MaxRuntimeErrorsBeforeCooldown, c.Agent.OpenClaw.RuntimeErrorCooldownMS)...)
	if c.Agent.V21.Enabled {
		v21 := c.Agent.V21
		baseURLEnv := strings.TrimSpace(v21.BaseURLEnv)
		if baseURLEnv == "" {
			problems = append(problems, "agent.v21.base_url_env is required when enabled")
		} else if baseURL, ok := lookup(baseURLEnv); !ok || strings.TrimSpace(baseURL) == "" {
			problems = append(problems, "missing required secret env "+baseURLEnv)
		} else if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "agent.v21 base URL env must contain an http or https URL")
		}
		tokenEnv := strings.TrimSpace(v21.TokenEnv)
		if tokenEnv == "" {
			problems = append(problems, "agent.v21.token_env is required when enabled")
		} else if token, ok := lookup(tokenEnv); !ok || strings.TrimSpace(token) == "" {
			problems = append(problems, "missing required secret env "+tokenEnv)
		}
		if len(nonEmptyStrings(v21.AllowedCollectionIDs)) == 0 {
			problems = append(problems, "agent.v21.allowed_collection_ids must include at least one collection when enabled")
		}
		if v21.TimeoutMS < 0 {
			problems = append(problems, "agent.v21.timeout_ms must be >= 0")
		}
		if v21.MaxSpokenChars < 0 || v21.MaxSpokenChars > 600 {
			problems = append(problems, "agent.v21.max_spoken_chars must be between 0 and 600")
		}
	}
	if c.Agent.Hermes.Enabled {
		hermes := c.Agent.Hermes
		baseURLEnv := strings.TrimSpace(hermes.BaseURLEnv)
		if baseURLEnv == "" {
			problems = append(problems, "agent.hermes.base_url_env is required when enabled")
		} else if baseURL, ok := lookup(baseURLEnv); !ok || strings.TrimSpace(baseURL) == "" {
			problems = append(problems, "missing required secret env "+baseURLEnv)
		} else if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "agent.hermes base URL env must contain an http or https URL")
		}
		tokenEnv := strings.TrimSpace(hermes.TokenEnv)
		if tokenEnv == "" {
			problems = append(problems, "agent.hermes.token_env is required when enabled")
		} else if token, ok := lookup(tokenEnv); !ok || strings.TrimSpace(token) == "" {
			problems = append(problems, "missing required secret env "+tokenEnv)
		}
		if hermes.TimeoutMS < 0 {
			problems = append(problems, "agent.hermes.timeout_ms must be >= 0")
		}
		if hermes.MaxSpokenChars < 0 || hermes.MaxSpokenChars > 600 {
			problems = append(problems, "agent.hermes.max_spoken_chars must be between 0 and 600")
		}
	}
	if c.Agent.OpenClaw.Enabled {
		openClaw := c.Agent.OpenClaw
		baseURLEnv := strings.TrimSpace(openClaw.BaseURLEnv)
		if baseURLEnv == "" {
			problems = append(problems, "agent.openclaw.base_url_env is required when enabled")
		} else if baseURL, ok := lookup(baseURLEnv); !ok || strings.TrimSpace(baseURL) == "" {
			problems = append(problems, "missing required secret env "+baseURLEnv)
		} else if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "agent.openclaw base URL env must contain an http or https URL")
		}
		tokenEnv := strings.TrimSpace(openClaw.TokenEnv)
		if tokenEnv == "" {
			problems = append(problems, "agent.openclaw.token_env is required when enabled")
		} else if token, ok := lookup(tokenEnv); !ok || strings.TrimSpace(token) == "" {
			problems = append(problems, "missing required secret env "+tokenEnv)
		}
		if openClaw.TimeoutMS < 0 {
			problems = append(problems, "agent.openclaw.timeout_ms must be >= 0")
		}
		if openClaw.MaxSpokenChars < 0 || openClaw.MaxSpokenChars > 600 {
			problems = append(problems, "agent.openclaw.max_spoken_chars must be between 0 and 600")
		}
	}
	if c.Tools.HomeAssistant.Enabled {
		ha := c.Tools.HomeAssistant
		baseURL := strings.TrimSpace(ha.BaseURL)
		if baseURL == "" {
			problems = append(problems, "tools.home_assistant.base_url is required when enabled")
		} else if parsed, err := url.Parse(baseURL); err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "tools.home_assistant.base_url must be an http or https URL")
		}
		tokenEnv := strings.TrimSpace(ha.TokenEnv)
		if tokenEnv == "" {
			problems = append(problems, "tools.home_assistant.token_env is required when enabled")
		} else if token, ok := lookup(tokenEnv); !ok || token == "" {
			problems = append(problems, "missing required secret env "+tokenEnv)
		}
		allowedEntities := nonEmptyStrings(ha.AllowedEntities)
		allowedEntitySet := nonEmptyStringSet(ha.AllowedEntities)
		if len(allowedEntities) == 0 {
			problems = append(problems, "tools.home_assistant.allowed_entities must include at least one entity when enabled")
		}
		if ha.TimeoutMS < 0 {
			problems = append(problems, "tools.home_assistant.timeout_ms must be >= 0")
		}
		seenActionIDs := make(map[string]struct{}, len(ha.AllowedActions))
		for index, action := range ha.AllowedActions {
			prefix := fmt.Sprintf("tools.home_assistant.allowed_actions[%d]", index)
			actionID := strings.TrimSpace(action.ActionID)
			if actionID == "" {
				problems = append(problems, prefix+".action_id is required")
			} else if !isSafeActionID(actionID) {
				problems = append(problems, prefix+".action_id may contain only letters, digits, underscore or dash")
			} else if _, ok := seenActionIDs[actionID]; ok {
				problems = append(problems, prefix+".action_id must be unique")
			} else {
				seenActionIDs[actionID] = struct{}{}
			}
			if domain := strings.TrimSpace(action.Domain); domain == "" {
				problems = append(problems, prefix+".domain is required")
			} else if !isSafeHomeAssistantServiceSegment(domain) {
				problems = append(problems, prefix+".domain may contain only letters, digits or underscore")
			}
			if service := strings.TrimSpace(action.Service); service == "" {
				problems = append(problems, prefix+".service is required")
			} else if !isSafeHomeAssistantServiceSegment(service) {
				problems = append(problems, prefix+".service may contain only letters, digits or underscore")
			}
			entityIDs := nonEmptyStrings(action.EntityIDs)
			if len(entityIDs) == 0 {
				problems = append(problems, prefix+".entity_ids must include at least one entity")
			}
			for entityIndex, entityID := range entityIDs {
				if _, ok := allowedEntitySet[entityID]; !ok {
					problems = append(problems, fmt.Sprintf("%s.entity_ids[%d] must also appear in tools.home_assistant.allowed_entities", prefix, entityIndex))
				}
			}
			for key := range action.Data {
				trimmedKey := strings.TrimSpace(key)
				if trimmedKey == "" {
					problems = append(problems, prefix+".data keys must be non-empty")
					continue
				}
				switch strings.ToLower(trimmedKey) {
				case "entity_id", "target":
					problems = append(problems, prefix+".data must not include entity_id or target")
				}
			}
			seenSlotNames := make(map[string]struct{}, len(action.Slots))
			for slotIndex, slot := range action.Slots {
				slotPrefix := fmt.Sprintf("%s.slots[%d]", prefix, slotIndex)
				slotName := strings.TrimSpace(slot.Name)
				if slotName == "" {
					problems = append(problems, slotPrefix+".name is required")
				} else if !isSafeHomeAssistantDataKey(slotName) {
					problems = append(problems, slotPrefix+".name may contain only letters, digits or underscore")
				} else if isReservedHomeAssistantActionArgument(slotName) {
					problems = append(problems, slotPrefix+".name is reserved")
				} else if _, ok := seenSlotNames[slotName]; ok {
					problems = append(problems, slotPrefix+".name must be unique within the action")
				} else {
					seenSlotNames[slotName] = struct{}{}
				}
				slotType := strings.ToLower(strings.TrimSpace(slot.Type))
				switch slotType {
				case "string":
					if len(slot.Enum) == 0 && (slot.MaxChars < 1 || slot.MaxChars > 120) {
						problems = append(problems, slotPrefix+".max_chars must be between 1 and 120 for string slots without enum")
					}
					if slot.MaxChars < 0 || slot.MaxChars > 120 {
						problems = append(problems, slotPrefix+".max_chars must be between 0 and 120")
					}
					seenEnum := make(map[string]struct{}, len(slot.Enum))
					for enumIndex, value := range slot.Enum {
						value = strings.TrimSpace(value)
						if value == "" {
							problems = append(problems, fmt.Sprintf("%s.enum[%d] must be non-empty", slotPrefix, enumIndex))
						} else if len([]rune(value)) > 120 {
							problems = append(problems, fmt.Sprintf("%s.enum[%d] must be at most 120 chars", slotPrefix, enumIndex))
						} else if _, ok := seenEnum[value]; ok {
							problems = append(problems, fmt.Sprintf("%s.enum[%d] must be unique", slotPrefix, enumIndex))
						} else {
							seenEnum[value] = struct{}{}
						}
					}
				case "number", "integer":
					if slot.Min == nil || slot.Max == nil {
						problems = append(problems, slotPrefix+".min and .max are required for numeric slots")
					} else if *slot.Min > *slot.Max {
						problems = append(problems, slotPrefix+".min must be <= max")
					}
					if slotType == "integer" {
						if slot.Min != nil && !isWholeNumber(*slot.Min) {
							problems = append(problems, slotPrefix+".min must be an integer")
						}
						if slot.Max != nil && !isWholeNumber(*slot.Max) {
							problems = append(problems, slotPrefix+".max must be an integer")
						}
					}
				case "boolean":
				default:
					problems = append(problems, slotPrefix+".type must be one of string, number, integer or boolean")
				}
			}
		}
	}
	if c.Tools.Search.Enabled {
		search := c.Tools.Search
		baseURLEnv := strings.TrimSpace(search.BaseURLEnv)
		if baseURLEnv == "" {
			problems = append(problems, "tools.search.base_url_env is required when enabled")
		} else if baseURL, ok := lookup(baseURLEnv); !ok || strings.TrimSpace(baseURL) == "" {
			problems = append(problems, "missing required secret env "+baseURLEnv)
		} else if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "tools.search base URL env must contain an http or https URL")
		}
		tokenEnv := strings.TrimSpace(search.TokenEnv)
		if tokenEnv == "" {
			problems = append(problems, "tools.search.token_env is required when enabled")
		} else if token, ok := lookup(tokenEnv); !ok || strings.TrimSpace(token) == "" {
			problems = append(problems, "missing required secret env "+tokenEnv)
		}
		if search.TimeoutMS < 0 {
			problems = append(problems, "tools.search.timeout_ms must be >= 0")
		}
		if search.MaxResults < 0 || search.MaxResults > 10 {
			problems = append(problems, "tools.search.max_results must be between 0 and 10")
		}
		if search.MaxQueryChars < 0 || search.MaxQueryChars > 300 {
			problems = append(problems, "tools.search.max_query_chars must be between 0 and 300")
		}
		for index, domain := range search.AllowedDomains {
			if !isSafeDomain(domain) {
				problems = append(problems, fmt.Sprintf("tools.search.allowed_domains[%d] must be a bare domain name", index))
			}
		}
	}
	if c.Tools.ToolFollowUp.MaxResults != nil && (*c.Tools.ToolFollowUp.MaxResults < 1 || *c.Tools.ToolFollowUp.MaxResults > 8) {
		problems = append(problems, "tools.tool_followup.max_results must be between 1 and 8")
	}
	if c.Tools.ToolFollowUp.MaxResultBytes != nil && (*c.Tools.ToolFollowUp.MaxResultBytes < 1 || *c.Tools.ToolFollowUp.MaxResultBytes > 16384) {
		problems = append(problems, "tools.tool_followup.max_result_bytes must be between 1 and 16384")
	}
	if c.Tools.ToolFollowUp.MaxToolCalls != nil && (*c.Tools.ToolFollowUp.MaxToolCalls < 1 || *c.Tools.ToolFollowUp.MaxToolCalls > 2) {
		problems = append(problems, "tools.tool_followup.max_tool_calls must be between 1 and 2")
	}
	if c.Tools.ToolFollowUp.AllowToolCalls != nil && *c.Tools.ToolFollowUp.AllowToolCalls && len(c.Tools.ToolFollowUp.AllowedTools) == 0 {
		problems = append(problems, "tools.tool_followup.allowed_tools is required when allow_tool_calls is true")
	}
	seenFollowUpTools := map[string]struct{}{}
	for index, toolName := range c.Tools.ToolFollowUp.AllowedTools {
		normalized := strings.ToLower(strings.TrimSpace(toolName))
		if !isSafeToolName(normalized) {
			problems = append(problems, fmt.Sprintf("tools.tool_followup.allowed_tools[%d] must be a safe tool name", index))
			continue
		}
		if _, exists := seenFollowUpTools[normalized]; exists {
			problems = append(problems, fmt.Sprintf("tools.tool_followup.allowed_tools[%d] duplicates tool %s after normalization", index, normalized))
			continue
		}
		seenFollowUpTools[normalized] = struct{}{}
	}
	if c.Tools.Feishu.Enabled {
		feishu := c.Tools.Feishu
		baseURL := strings.TrimSpace(feishu.BaseURL)
		if baseURL == "" {
			problems = append(problems, "tools.feishu.base_url is required when enabled")
		} else if parsed, err := url.Parse(baseURL); err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			problems = append(problems, "tools.feishu.base_url must be an http or https URL")
		}
		appIDEnv := strings.TrimSpace(feishu.AppIDEnv)
		if appIDEnv == "" {
			problems = append(problems, "tools.feishu.app_id_env is required when enabled")
		} else if appID, ok := lookup(appIDEnv); !ok || strings.TrimSpace(appID) == "" {
			problems = append(problems, "missing required secret env "+appIDEnv)
		}
		appSecretEnv := strings.TrimSpace(feishu.AppSecretEnv)
		if appSecretEnv == "" {
			problems = append(problems, "tools.feishu.app_secret_env is required when enabled")
		} else if appSecret, ok := lookup(appSecretEnv); !ok || strings.TrimSpace(appSecret) == "" {
			problems = append(problems, "missing required secret env "+appSecretEnv)
		}
		if len(feishu.AllowedTargets) == 0 {
			problems = append(problems, "tools.feishu.allowed_targets must include at least one target when enabled")
		}
		if feishu.TimeoutMS < 0 {
			problems = append(problems, "tools.feishu.timeout_ms must be >= 0")
		}
		if feishu.MaxTextChars < 0 || feishu.MaxTextChars > 600 {
			problems = append(problems, "tools.feishu.max_text_chars must be between 0 and 600")
		}
		seenTargets := make(map[string]struct{}, len(feishu.AllowedTargets))
		for index, target := range feishu.AllowedTargets {
			prefix := fmt.Sprintf("tools.feishu.allowed_targets[%d]", index)
			targetID := strings.TrimSpace(target.TargetID)
			if targetID == "" {
				problems = append(problems, prefix+".target_id is required")
			} else if !isSafeActionID(targetID) {
				problems = append(problems, prefix+".target_id may contain only letters, digits, underscore or dash")
			} else if _, ok := seenTargets[targetID]; ok {
				problems = append(problems, prefix+".target_id must be unique")
			} else {
				seenTargets[targetID] = struct{}{}
			}
			if receiveIDType := strings.TrimSpace(target.ReceiveIDType); receiveIDType == "" {
				problems = append(problems, prefix+".receive_id_type is required")
			} else if !isValidFeishuReceiveIDType(receiveIDType) {
				problems = append(problems, prefix+".receive_id_type must be open_id, user_id, union_id, email or chat_id")
			}
			receiveIDEnv := strings.TrimSpace(target.ReceiveIDEnv)
			if receiveIDEnv == "" {
				problems = append(problems, prefix+".receive_id_env is required")
			} else if receiveID, ok := lookup(receiveIDEnv); !ok || strings.TrimSpace(receiveID) == "" {
				problems = append(problems, "missing required secret env "+receiveIDEnv)
			}
		}
	}
	if c.Tools.Camera.Enabled {
		camera := c.Tools.Camera
		if camera.MaxReasonChars < 1 || camera.MaxReasonChars > 160 {
			problems = append(problems, "tools.camera.max_reason_chars must be between 1 and 160 when enabled")
		}
	}
	if c.Tools.Reminder.Enabled {
		reminder := c.Tools.Reminder
		if reminder.MaxTitleChars < 1 || reminder.MaxTitleChars > 120 {
			problems = append(problems, "tools.reminder.max_title_chars must be between 1 and 120 when enabled")
		}
		if reminder.MaxMessageChars < 1 || reminder.MaxMessageChars > 600 {
			problems = append(problems, "tools.reminder.max_message_chars must be between 1 and 600 when enabled")
		}
	}

	body := c.StackChan.Body
	if body.YawMinDeg < -128 {
		problems = append(problems, "stackchan.body.yaw_min_deg must be >= -128")
	}
	if body.YawMaxDeg > 128 {
		problems = append(problems, "stackchan.body.yaw_max_deg must be <= 128")
	}
	if body.YawMinDeg > body.YawMaxDeg {
		problems = append(problems, "stackchan.body.yaw_min_deg must be <= yaw_max_deg")
	}
	if body.PitchMinDeg < 0 {
		problems = append(problems, "stackchan.body.pitch_min_deg must be >= 0")
	}
	if body.PitchMaxDeg > 90 {
		problems = append(problems, "stackchan.body.pitch_max_deg must be <= 90")
	}
	if body.PitchMinDeg > body.PitchMaxDeg {
		problems = append(problems, "stackchan.body.pitch_min_deg must be <= pitch_max_deg")
	}
	if body.DefaultSpeed < 100 || body.DefaultSpeed > 1000 {
		problems = append(problems, "stackchan.body.default_speed must be between 100 and 1000")
	}
	if body.MinCommandGapMS <= 0 {
		problems = append(problems, "stackchan.body.min_command_gap_ms must be positive")
	}
	if body.MaxCommandsPerTurn <= 0 {
		problems = append(problems, "stackchan.body.max_commands_per_turn must be positive")
	}
	bodyLifecycleLEDNames := make([]string, 0, len(body.LifecycleLEDs))
	for lifecycle := range body.LifecycleLEDs {
		bodyLifecycleLEDNames = append(bodyLifecycleLEDNames, lifecycle)
	}
	sort.Slice(bodyLifecycleLEDNames, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(bodyLifecycleLEDNames[i]))
		right := strings.ToLower(strings.TrimSpace(bodyLifecycleLEDNames[j]))
		if left == right {
			return strings.TrimSpace(bodyLifecycleLEDNames[i]) < strings.TrimSpace(bodyLifecycleLEDNames[j])
		}
		return left < right
	})
	seenBodyLifecycleLEDKeys := map[string]struct{}{}
	for _, lifecycle := range bodyLifecycleLEDNames {
		policy := body.LifecycleLEDs[lifecycle]
		lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
		prefix := "stackchan.body.lifecycle_leds." + lifecycle
		if !stackchan.IsLifecycleScene(lifecycle) {
			problems = append(problems, prefix+" must be one of listening, thinking, speaking or idle")
			continue
		}
		if _, exists := seenBodyLifecycleLEDKeys[lifecycle]; exists {
			problems = append(problems, prefix+" duplicates lifecycle "+lifecycle+" after normalization")
			continue
		}
		seenBodyLifecycleLEDKeys[lifecycle] = struct{}{}
		if policy.R < stackchan.DefaultLEDChannelMin || policy.R > stackchan.DefaultLEDChannelMax {
			problems = append(problems, prefix+".r must be between 0 and 168")
		}
		if policy.G < stackchan.DefaultLEDChannelMin || policy.G > stackchan.DefaultLEDChannelMax {
			problems = append(problems, prefix+".g must be between 0 and 168")
		}
		if policy.B < stackchan.DefaultLEDChannelMin || policy.B > stackchan.DefaultLEDChannelMax {
			problems = append(problems, prefix+".b must be between 0 and 168")
		}
	}

	if c.StackChan.Display.SceneTTLMS <= 0 {
		problems = append(problems, "stackchan.display.scene_ttl_ms must be positive")
	}
	if c.StackChan.Display.MaxCaptionChars <= 0 {
		problems = append(problems, "stackchan.display.max_caption_chars must be positive")
	}
	for sceneName, policy := range c.StackChan.Display.LifecycleScenes {
		sceneName = strings.TrimSpace(sceneName)
		prefix := "stackchan.display.lifecycle_scenes." + sceneName
		if !stackchan.IsLifecycleScene(sceneName) {
			problems = append(problems, prefix+" must be one of listening, thinking, speaking or idle")
			continue
		}
		if emotion := strings.TrimSpace(policy.Emotion); emotion != "" && !stackchan.IsValidEmotion(emotion) {
			problems = append(problems, prefix+".emotion is invalid")
		}
		if accent := strings.TrimSpace(policy.Accent); accent != "" && !stackchan.IsValidAccent(accent) {
			problems = append(problems, prefix+".accent is invalid")
		}
		if policy.Motion != nil {
			if preset := strings.TrimSpace(policy.Motion.Preset); preset != "" && !stackchan.IsValidMotionPreset(preset) {
				problems = append(problems, prefix+".motion.preset is invalid")
			}
			if policy.Motion.Intensity != nil && (*policy.Motion.Intensity < 0 || *policy.Motion.Intensity > 1) {
				problems = append(problems, prefix+".motion.intensity must be between 0 and 1")
			}
		}
	}
	for eventName, policy := range c.StackChan.Display.EventScenes {
		eventName = strings.TrimSpace(eventName)
		prefix := "stackchan.display.event_scenes." + eventName
		if !stackchan.IsDisplayEvent(eventName) {
			problems = append(problems, prefix+" must be one of agent_mode.casual, agent_mode.professional, agent_mode.roleplay, agent_mode.tool, tool.running, tool.succeeded, tool.failed, homeassistant.state, homeassistant.action, search.web, memory.updated, camera.capturing, reminder.due, agent_route.openclaw, agent_route.hermes, agent_route.v21, agent_route.claude or agent_route.skipped")
			continue
		}
		if scene := strings.TrimSpace(policy.Scene); scene != "" && !stackchan.IsValidScene(scene) {
			problems = append(problems, prefix+".scene is invalid")
		}
		if emotion := strings.TrimSpace(policy.Emotion); emotion != "" && !stackchan.IsValidEmotion(emotion) {
			problems = append(problems, prefix+".emotion is invalid")
		}
		if accent := strings.TrimSpace(policy.Accent); accent != "" && !stackchan.IsValidAccent(accent) {
			problems = append(problems, prefix+".accent is invalid")
		}
		if policy.Motion != nil {
			if preset := strings.TrimSpace(policy.Motion.Preset); preset != "" && !stackchan.IsValidMotionPreset(preset) {
				problems = append(problems, prefix+".motion.preset is invalid")
			}
			if policy.Motion.Intensity != nil && (*policy.Motion.Intensity < 0 || *policy.Motion.Intensity > 1) {
				problems = append(problems, prefix+".motion.intensity must be between 0 and 1")
			}
		}
	}
	for cardID, policy := range c.StackChan.Display.Cards {
		cardID = strings.TrimSpace(cardID)
		prefix := "stackchan.display.cards." + cardID
		if !isSafeToolName(cardID) {
			problems = append(problems, prefix+" must be a safe card id")
		}
		if scene := strings.TrimSpace(policy.Scene); scene != "" && !stackchan.IsValidScene(scene) {
			problems = append(problems, prefix+".scene is invalid")
		}
		if emotion := strings.TrimSpace(policy.Emotion); emotion != "" && !stackchan.IsValidEmotion(emotion) {
			problems = append(problems, prefix+".emotion is invalid")
		}
		if accent := strings.TrimSpace(policy.Accent); accent != "" && !stackchan.IsValidAccent(accent) {
			problems = append(problems, prefix+".accent is invalid")
		}
		if policy.Motion != nil {
			if preset := strings.TrimSpace(policy.Motion.Preset); preset != "" && !stackchan.IsValidMotionPreset(preset) {
				problems = append(problems, prefix+".motion.preset is invalid")
			}
			if policy.Motion.Intensity != nil && (*policy.Motion.Intensity < 0 || *policy.Motion.Intensity > 1) {
				problems = append(problems, prefix+".motion.intensity must be between 0 and 1")
			}
		}
		if policy.AllowCaption && (policy.MaxCaptionChars < 1 || policy.MaxCaptionChars > c.StackChan.Display.MaxCaptionChars) {
			problems = append(problems, prefix+".max_caption_chars must be between 1 and stackchan.display.max_caption_chars")
		}
	}
	expressionLifecycleNames := make([]string, 0, len(c.StackChan.Expression.LifecycleCues))
	for lifecycle := range c.StackChan.Expression.LifecycleCues {
		expressionLifecycleNames = append(expressionLifecycleNames, lifecycle)
	}
	sort.Slice(expressionLifecycleNames, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(expressionLifecycleNames[i]))
		right := strings.ToLower(strings.TrimSpace(expressionLifecycleNames[j]))
		if left == right {
			return strings.TrimSpace(expressionLifecycleNames[i]) < strings.TrimSpace(expressionLifecycleNames[j])
		}
		return left < right
	})
	seenLifecycleExpressionKeys := map[string]struct{}{}
	for _, lifecycle := range expressionLifecycleNames {
		cue := c.StackChan.Expression.LifecycleCues[lifecycle]
		lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
		prefix := "stackchan.expression.lifecycle_cues." + lifecycle
		if !stackchan.IsLifecycleScene(lifecycle) {
			problems = append(problems, prefix+" must be one of listening, thinking, speaking or idle")
			continue
		}
		if _, exists := seenLifecycleExpressionKeys[lifecycle]; exists {
			problems = append(problems, prefix+" duplicates lifecycle "+lifecycle+" after normalization")
			continue
		}
		seenLifecycleExpressionKeys[lifecycle] = struct{}{}
		if !stackchan.IsExpressionCue(cue) {
			problems = append(problems, prefix+" must be one of attentive, nod, celebrate, thinking or settle")
		}
	}

	expressionEventNames := make([]string, 0, len(c.StackChan.Expression.EventCues))
	for eventName := range c.StackChan.Expression.EventCues {
		expressionEventNames = append(expressionEventNames, eventName)
	}
	sort.Slice(expressionEventNames, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(expressionEventNames[i]))
		right := strings.ToLower(strings.TrimSpace(expressionEventNames[j]))
		if left == right {
			return strings.TrimSpace(expressionEventNames[i]) < strings.TrimSpace(expressionEventNames[j])
		}
		return left < right
	})
	seenEventExpressionKeys := map[string]struct{}{}
	for _, eventName := range expressionEventNames {
		cue := c.StackChan.Expression.EventCues[eventName]
		eventName = strings.ToLower(strings.TrimSpace(eventName))
		prefix := "stackchan.expression.event_cues." + eventName
		if !stackchan.IsDisplayEvent(eventName) {
			problems = append(problems, prefix+" must be one of agent_mode.casual, agent_mode.professional, agent_mode.roleplay, agent_mode.tool, tool.running, tool.succeeded, tool.failed, homeassistant.state, homeassistant.action, search.web, memory.updated, camera.capturing, reminder.due, agent_route.openclaw, agent_route.hermes, agent_route.v21, agent_route.claude or agent_route.skipped")
			continue
		}
		if _, exists := seenEventExpressionKeys[eventName]; exists {
			problems = append(problems, prefix+" duplicates event "+eventName+" after normalization")
			continue
		}
		seenEventExpressionKeys[eventName] = struct{}{}
		if !stackchan.IsExpressionCue(cue) {
			problems = append(problems, prefix+" must be one of attentive, nod, celebrate, thinking or settle")
		}
	}

	expressionSequenceNames := make([]string, 0, len(c.StackChan.Expression.Sequences))
	for sequenceName := range c.StackChan.Expression.Sequences {
		expressionSequenceNames = append(expressionSequenceNames, sequenceName)
	}
	sort.Slice(expressionSequenceNames, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(expressionSequenceNames[i]))
		right := strings.ToLower(strings.TrimSpace(expressionSequenceNames[j]))
		if left == right {
			return strings.TrimSpace(expressionSequenceNames[i]) < strings.TrimSpace(expressionSequenceNames[j])
		}
		return left < right
	})
	seenExpressionSequences := map[string]struct{}{}
	for _, sequenceName := range expressionSequenceNames {
		policy := c.StackChan.Expression.Sequences[sequenceName]
		sequenceName = strings.TrimSpace(sequenceName)
		normalizedSequence := strings.ToLower(sequenceName)
		prefix := "stackchan.expression.sequences." + normalizedSequence
		if !isSafeToolName(sequenceName) {
			problems = append(problems, prefix+" must be a safe sequence id")
			continue
		}
		if _, exists := seenExpressionSequences[normalizedSequence]; exists {
			problems = append(problems, prefix+" duplicates sequence "+normalizedSequence+" after normalization")
			continue
		}
		seenExpressionSequences[normalizedSequence] = struct{}{}
		if len(policy.Cues) == 0 || len(policy.Cues) > stackchan.MaxExpressionSequenceCues {
			problems = append(problems, prefix+".cues must contain between 1 and 3 cues")
			continue
		}
		for index, cue := range policy.Cues {
			if !stackchan.IsExpressionCue(cue) {
				problems = append(problems, fmt.Sprintf("%s.cues[%d] must be one of attentive, nod, celebrate, thinking or settle", prefix, index))
			}
		}
	}

	expressionCueNames := make([]string, 0, len(c.StackChan.Expression.Cues))
	for cueName := range c.StackChan.Expression.Cues {
		expressionCueNames = append(expressionCueNames, cueName)
	}
	sort.Slice(expressionCueNames, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(expressionCueNames[i]))
		right := strings.ToLower(strings.TrimSpace(expressionCueNames[j]))
		if left == right {
			return strings.TrimSpace(expressionCueNames[i]) < strings.TrimSpace(expressionCueNames[j])
		}
		return left < right
	})
	seenExpressionCues := map[string]struct{}{}
	for _, cueName := range expressionCueNames {
		policy := c.StackChan.Expression.Cues[cueName]
		cueName = strings.TrimSpace(cueName)
		prefix := "stackchan.expression.cues." + cueName
		if !stackchan.IsExpressionCue(cueName) {
			problems = append(problems, prefix+" must be one of attentive, nod, celebrate, thinking or settle")
			continue
		}
		normalizedCue := strings.ToLower(cueName)
		if _, exists := seenExpressionCues[normalizedCue]; exists {
			problems = append(problems, prefix+" duplicates cue "+normalizedCue+" after normalization")
			continue
		}
		seenExpressionCues[normalizedCue] = struct{}{}
		if policy.Motion != nil {
			if policy.Motion.YawDeg < body.YawMinDeg || policy.Motion.YawDeg > body.YawMaxDeg {
				problems = append(problems, prefix+".motion.yaw_deg must be between stackchan.body.yaw_min_deg and yaw_max_deg")
			}
			if policy.Motion.PitchDeg < body.PitchMinDeg || policy.Motion.PitchDeg > body.PitchMaxDeg {
				problems = append(problems, prefix+".motion.pitch_deg must be between stackchan.body.pitch_min_deg and pitch_max_deg")
			}
			if policy.Motion.Speed < stackchan.DefaultSpeedMin || policy.Motion.Speed > stackchan.DefaultSpeedMax {
				problems = append(problems, prefix+".motion.speed must be between 100 and 1000")
			}
		}
		if policy.LED != nil {
			if policy.LED.R < stackchan.DefaultLEDChannelMin || policy.LED.R > stackchan.DefaultLEDChannelMax {
				problems = append(problems, prefix+".led.r must be between 0 and 168")
			}
			if policy.LED.G < stackchan.DefaultLEDChannelMin || policy.LED.G > stackchan.DefaultLEDChannelMax {
				problems = append(problems, prefix+".led.g must be between 0 and 168")
			}
			if policy.LED.B < stackchan.DefaultLEDChannelMin || policy.LED.B > stackchan.DefaultLEDChannelMax {
				problems = append(problems, prefix+".led.b must be between 0 and 168")
			}
		}
		if policy.Scene != nil {
			scenePrefix := prefix + ".scene"
			if scene := strings.TrimSpace(policy.Scene.Scene); scene != "" && !stackchan.IsValidScene(scene) {
				problems = append(problems, scenePrefix+".scene is invalid")
			}
			if emotion := strings.TrimSpace(policy.Scene.Emotion); emotion != "" && !stackchan.IsValidEmotion(emotion) {
				problems = append(problems, scenePrefix+".emotion is invalid")
			}
			if accent := strings.TrimSpace(policy.Scene.Accent); accent != "" && !stackchan.IsValidAccent(accent) {
				problems = append(problems, scenePrefix+".accent is invalid")
			}
			if policy.Scene.Motion != nil {
				if preset := strings.TrimSpace(policy.Scene.Motion.Preset); preset != "" && !stackchan.IsValidMotionPreset(preset) {
					problems = append(problems, scenePrefix+".motion.preset is invalid")
				}
				if policy.Scene.Motion.Intensity != nil && (*policy.Scene.Motion.Intensity < 0 || *policy.Scene.Motion.Intensity > 1) {
					problems = append(problems, scenePrefix+".motion.intensity must be between 0 and 1")
				}
			}
		}
	}
	if strings.TrimSpace(c.Observability.TraceJSONLPath) == "" {
		problems = append(problems, "observability.trace_jsonl_path is required")
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func isVoiceRuntimeProfile(profile ProviderProfileConfig) bool {
	return strings.TrimSpace(profile.ASR) != "" &&
		strings.TrimSpace(profile.LLM) != "" &&
		strings.TrimSpace(profile.TTS) != ""
}

func validateAgentAllowedToolIntents(prefix string, tools []string) []string {
	if len(tools) == 0 {
		return nil
	}
	var problems []string
	seen := make(map[string]struct{}, len(tools))
	for index, tool := range tools {
		normalized := strings.ToLower(strings.TrimSpace(tool))
		itemPrefix := fmt.Sprintf("%s[%d]", prefix, index)
		if normalized == "" || !agents.IsBridgeToolIntentAllowed(normalized) {
			problems = append(problems, itemPrefix+" must be one of the bridge-safe gateway tools")
			continue
		}
		if _, exists := seen[normalized]; exists {
			problems = append(problems, fmt.Sprintf("%s duplicates tool %s after normalization", itemPrefix, normalized))
			continue
		}
		seen[normalized] = struct{}{}
	}
	return problems
}

func validateAgentMaxToolIntents(prefix string, maxToolIntents *int) []string {
	if maxToolIntents == nil {
		return nil
	}
	if *maxToolIntents < 0 || *maxToolIntents > agents.MaxBridgeToolIntentsPerTurn {
		return []string{fmt.Sprintf("%s must be between 0 and %d", prefix, agents.MaxBridgeToolIntentsPerTurn)}
	}
	return nil
}

func validateAgentMaxRuntimeRoutesPerMinute(prefix string, value int) []string {
	if value < 0 || value > 120 {
		return []string{prefix + " must be between 0 and 120"}
	}
	return nil
}

func validateAgentMaxRuntimeInputChars(prefix string, value int) []string {
	if value < 0 || value > 2000 {
		return []string{prefix + " must be between 0 and 2000"}
	}
	return nil
}

func validateAgentRuntimeErrorCooldown(prefix string, maxErrors int, cooldownMS int) []string {
	var problems []string
	if maxErrors < 0 || maxErrors > 10 {
		problems = append(problems, prefix+".max_runtime_errors_before_cooldown must be between 0 and 10")
	}
	if cooldownMS < 0 || cooldownMS > 600000 {
		problems = append(problems, prefix+".runtime_error_cooldown_ms must be between 0 and 600000")
	}
	if maxErrors == 0 && cooldownMS > 0 {
		problems = append(problems, prefix+".max_runtime_errors_before_cooldown must be positive when runtime_error_cooldown_ms is set")
	}
	if maxErrors > 0 && cooldownMS == 0 {
		problems = append(problems, prefix+".runtime_error_cooldown_ms must be positive when max_runtime_errors_before_cooldown is set")
	}
	return problems
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func nonEmptyStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func isSafeActionID(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func isSafeToolName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	lastDot := false
	for _, ch := range value {
		if ch == '.' {
			if lastDot {
				return false
			}
			lastDot = true
			continue
		}
		lastDot = false
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func isSafeHomeAssistantServiceSegment(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func isSafeHomeAssistantDataKey(value string) bool {
	return isSafeHomeAssistantServiceSegment(value)
}

func isReservedHomeAssistantActionArgument(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "action_id", "domain", "service", "entity_id", "entity_ids", "target", "data":
		return true
	default:
		return false
	}
}

func isWholeNumber(value float64) bool {
	return value == float64(int64(value))
}

func isSafeDomain(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/:") {
		return false
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, ch := range label {
			if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func isValidFeishuReceiveIDType(value string) bool {
	switch strings.TrimSpace(value) {
	case "open_id", "user_id", "union_id", "email", "chat_id":
		return true
	default:
		return false
	}
}

func isValidAgentMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "casual", "roleplay", "professional", "tool":
		return true
	default:
		return false
	}
}

func IsValidationError(err error) bool {
	var validationError *ValidationError
	return errors.As(err, &validationError)
}
