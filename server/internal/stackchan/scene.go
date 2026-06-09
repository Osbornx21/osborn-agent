package stackchan

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

const (
	SceneType    = "stackchan.scene"
	ToolShowCard = "stackchan.show_card"

	SceneIdle      = "idle"
	SceneListening = "listening"
	SceneThinking  = "thinking"
	SceneSpeaking  = "speaking"
	SceneTool      = "tool"
	SceneError     = "error"
	SceneSleep     = "sleep"

	EmotionNeutral = "neutral"
	EmotionCurious = "curious"
	EmotionHappy   = "happy"
	EmotionWarm    = "warm"
	EmotionReady   = "ready"
	EmotionError   = "error"

	AccentDefault = "default"
	AccentCyan    = "cyan"
	AccentGreen   = "green"
	AccentAmber   = "amber"
	AccentRed     = "red"

	MotionPresetNone      = "none"
	MotionPresetNodSoft   = "nod_soft"
	MotionPresetAttentive = "attentive"

	DisplayEventAgentModeCasual       = "agent_mode.casual"
	DisplayEventAgentModeProfessional = "agent_mode.professional"
	DisplayEventAgentModeRoleplay     = "agent_mode.roleplay"
	DisplayEventAgentModeTool         = "agent_mode.tool"
	DisplayEventToolRunning           = "tool.running"
	DisplayEventToolSucceeded         = "tool.succeeded"
	DisplayEventToolFailed            = "tool.failed"
	DisplayEventHomeAssistantState    = "homeassistant.state"
	DisplayEventHomeAssistantAction   = "homeassistant.action"
	DisplayEventSearchWeb             = "search.web"
	DisplayEventMemoryUpdated         = "memory.updated"
	DisplayEventCameraCapturing       = "camera.capturing"
	DisplayEventReminderDue           = "reminder.due"
	DisplayEventAgentRouteOpenClaw    = "agent_route.openclaw"
	DisplayEventAgentRouteHermes      = "agent_route.hermes"
	DisplayEventAgentRouteV21         = "agent_route.v21"
	DisplayEventAgentRouteClaude      = "agent_route.claude"
	DisplayEventAgentRouteSkipped     = "agent_route.skipped"
)

var ErrDisplayCardInvalid = errors.New("stackchan display card is invalid")

var validScenes = map[string]struct{}{
	SceneIdle:      {},
	SceneListening: {},
	SceneThinking:  {},
	SceneSpeaking:  {},
	SceneTool:      {},
	SceneError:     {},
	SceneSleep:     {},
}

var validEmotions = map[string]struct{}{
	"":             {},
	EmotionNeutral: {},
	EmotionCurious: {},
	EmotionHappy:   {},
	EmotionWarm:    {},
	EmotionReady:   {},
	EmotionError:   {},
}

var validAccents = map[string]struct{}{
	"":            {},
	AccentDefault: {},
	AccentCyan:    {},
	AccentGreen:   {},
	AccentAmber:   {},
	AccentRed:     {},
}

var validMotionPresets = map[string]struct{}{
	"":                    {},
	MotionPresetNone:      {},
	MotionPresetNodSoft:   {},
	MotionPresetAttentive: {},
}

var validDisplayEvents = map[string]struct{}{
	DisplayEventAgentModeCasual:       {},
	DisplayEventAgentModeProfessional: {},
	DisplayEventAgentModeRoleplay:     {},
	DisplayEventAgentModeTool:         {},
	DisplayEventToolRunning:           {},
	DisplayEventToolSucceeded:         {},
	DisplayEventToolFailed:            {},
	DisplayEventHomeAssistantState:    {},
	DisplayEventHomeAssistantAction:   {},
	DisplayEventSearchWeb:             {},
	DisplayEventMemoryUpdated:         {},
	DisplayEventCameraCapturing:       {},
	DisplayEventReminderDue:           {},
	DisplayEventAgentRouteOpenClaw:    {},
	DisplayEventAgentRouteHermes:      {},
	DisplayEventAgentRouteV21:         {},
	DisplayEventAgentRouteClaude:      {},
	DisplayEventAgentRouteSkipped:     {},
}

type SceneMotion struct {
	Preset    string  `json:"preset,omitempty"`
	Intensity float64 `json:"intensity,omitempty"`
}

type Scene struct {
	Type       string       `json:"type"`
	SessionID  string       `json:"session_id"`
	Generation int64        `json:"generation"`
	Scene      string       `json:"scene"`
	Emotion    string       `json:"emotion,omitempty"`
	Caption    string       `json:"caption,omitempty"`
	Accent     string       `json:"accent,omitempty"`
	Motion     *SceneMotion `json:"motion,omitempty"`
	TTLMS      int          `json:"ttl_ms"`
}

type SceneRequest struct {
	SessionID  string
	Generation int64
	Scene      string
	Emotion    string
	Caption    string
	Accent     string
	Motion     *SceneMotion
}

type SceneComposer struct {
	options DisplayOptions
}

func NewSceneComposer(options DisplayOptions) *SceneComposer {
	return &SceneComposer{options: options.withDefaults()}
}

func (c *SceneComposer) Compose(request SceneRequest) Scene {
	options := c.options.withDefaults()
	scene := request.Scene
	if !IsValidScene(scene) {
		scene = SceneIdle
	}
	return Scene{
		Type:       SceneType,
		SessionID:  request.SessionID,
		Generation: request.Generation,
		Scene:      scene,
		Emotion:    normalizeEmotion(request.Emotion),
		Caption:    ShortenCaption(request.Caption, options.MaxCaptionChars),
		Accent:     normalizeAccent(request.Accent),
		Motion:     normalizeSceneMotion(request.Motion),
		TTLMS:      options.SceneTTLMS,
	}
}

func (c *SceneComposer) ComposeLifecycle(request SceneRequest) Scene {
	options := c.options.withDefaults()
	scene := request.Scene
	if !IsLifecycleScene(scene) {
		scene = SceneIdle
	}
	policy := defaultLifecycleScenePolicy(scene)
	if configured, ok := options.LifecycleScenes[scene]; ok {
		policy = mergeScenePolicy(policy, configured)
	}
	return c.Compose(SceneRequest{
		SessionID:  request.SessionID,
		Generation: request.Generation,
		Scene:      scene,
		Emotion:    policy.Emotion,
		Caption:    policy.Caption,
		Accent:     policy.Accent,
		Motion:     policy.Motion,
	})
}

func (c *SceneComposer) ComposeEvent(event string, request SceneRequest) (Scene, bool) {
	event = normalizeDisplayEvent(event)
	options := c.options.withDefaults()
	policy, ok := options.EventScenes[event]
	if !ok {
		return Scene{}, false
	}
	if strings.TrimSpace(policy.Scene) == "" {
		policy.Scene = defaultEventScene(event)
	}
	return c.Compose(SceneRequest{
		SessionID:  request.SessionID,
		Generation: request.Generation,
		Scene:      policy.Scene,
		Emotion:    policy.Emotion,
		Caption:    policy.Caption,
		Accent:     policy.Accent,
		Motion:     policy.Motion,
	}), true
}

func (c *SceneComposer) ComposeCard(cardID string, request SceneRequest) (Scene, bool) {
	cardID = normalizeDisplayCardID(cardID)
	if cardID == "" {
		return Scene{}, false
	}
	options := c.options.withDefaults()
	policy, ok := options.Cards[cardID]
	if !ok {
		return Scene{}, false
	}
	if strings.TrimSpace(policy.Scene) == "" {
		policy.Scene = SceneTool
	}
	caption := policy.Caption
	if policy.AllowCaption && strings.TrimSpace(request.Caption) != "" {
		caption = strings.TrimSpace(request.Caption)
		if policy.MaxCaptionChars > 0 {
			caption = ShortenCaption(caption, policy.MaxCaptionChars)
		}
	}
	return c.Compose(SceneRequest{
		SessionID:  request.SessionID,
		Generation: request.Generation,
		Scene:      policy.Scene,
		Emotion:    policy.Emotion,
		Caption:    caption,
		Accent:     policy.Accent,
		Motion:     policy.Motion,
	}), true
}

func (c *SceneComposer) CardIDs() []string {
	if c == nil {
		return nil
	}
	options := c.options.withDefaults()
	ids := make([]string, 0, len(options.Cards))
	for id := range options.Cards {
		id = normalizeDisplayCardID(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func DisplayCardToolDescription() string {
	return "Show one configured StackChan display card with optional bounded caption text."
}

func DisplayCardToolInputSchema(cardIDs []string) map[string]any {
	cards := make([]string, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		cardID = normalizeDisplayCardID(cardID)
		if cardID != "" {
			cards = append(cards, cardID)
		}
	}
	sort.Strings(cards)
	enum := make([]any, 0, len(cards))
	for _, card := range cards {
		enum = append(enum, card)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"card": map[string]any{
				"type":        "string",
				"description": "Configured display card id. Only ids configured by the operator are accepted.",
				"enum":        enum,
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional short caption. It is used only for cards configured to allow model captions and is bounded by gateway policy.",
			},
		},
		"required":             []any{"card"},
		"additionalProperties": false,
	}
}

func defaultLifecycleScenePolicy(scene string) ScenePolicy {
	switch scene {
	case SceneListening:
		return ScenePolicy{
			Emotion: EmotionCurious,
			Caption: "我在听。",
			Accent:  AccentCyan,
			Motion:  &SceneMotion{Preset: MotionPresetAttentive, Intensity: 0.35},
		}
	case SceneThinking:
		return ScenePolicy{
			Emotion: EmotionCurious,
			Caption: "我在想。",
			Accent:  AccentAmber,
			Motion:  &SceneMotion{Preset: MotionPresetAttentive, Intensity: 0.25},
		}
	case SceneSpeaking:
		return ScenePolicy{
			Emotion: EmotionWarm,
			Caption: "我在说。",
			Accent:  AccentGreen,
			Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.3},
		}
	default:
		return ScenePolicy{
			Emotion: EmotionNeutral,
			Accent:  AccentDefault,
		}
	}
}

func mergeScenePolicy(base ScenePolicy, override ScenePolicy) ScenePolicy {
	if override.Scene != "" {
		base.Scene = override.Scene
	}
	if override.Emotion != "" {
		base.Emotion = override.Emotion
	}
	if override.Caption != "" {
		base.Caption = override.Caption
	}
	if override.Accent != "" {
		base.Accent = override.Accent
	}
	if override.Motion != nil {
		motion := *override.Motion
		base.Motion = &motion
	}
	return base
}

func defaultEventScene(event string) string {
	switch normalizeDisplayEvent(event) {
	case DisplayEventAgentModeCasual:
		return SceneIdle
	case DisplayEventAgentModeProfessional, DisplayEventAgentModeRoleplay, DisplayEventAgentModeTool, DisplayEventToolRunning, DisplayEventToolSucceeded, DisplayEventHomeAssistantState, DisplayEventHomeAssistantAction, DisplayEventSearchWeb, DisplayEventMemoryUpdated, DisplayEventCameraCapturing, DisplayEventReminderDue, DisplayEventAgentRouteOpenClaw, DisplayEventAgentRouteHermes, DisplayEventAgentRouteV21, DisplayEventAgentRouteClaude, DisplayEventAgentRouteSkipped:
		return SceneTool
	case DisplayEventToolFailed:
		return SceneError
	default:
		return SceneTool
	}
}

func (s Scene) MCPArguments() map[string]any {
	data, err := json.Marshal(s)
	if err != nil {
		return map[string]any{}
	}
	var arguments map[string]any
	if err := json.Unmarshal(data, &arguments); err != nil {
		return map[string]any{}
	}
	return arguments
}

func IsValidScene(scene string) bool {
	_, ok := validScenes[scene]
	return ok
}

func IsLifecycleScene(scene string) bool {
	switch scene {
	case SceneListening, SceneThinking, SceneSpeaking, SceneIdle:
		return true
	default:
		return false
	}
}

func IsValidEmotion(emotion string) bool {
	_, ok := validEmotions[emotion]
	return ok
}

func IsValidAccent(accent string) bool {
	_, ok := validAccents[accent]
	return ok
}

func IsValidMotionPreset(preset string) bool {
	_, ok := validMotionPresets[preset]
	return ok
}

func IsDisplayEvent(event string) bool {
	_, ok := validDisplayEvents[normalizeDisplayEvent(event)]
	return ok
}

func DisplayEventAgentMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "casual":
		return DisplayEventAgentModeCasual
	case "professional":
		return DisplayEventAgentModeProfessional
	case "roleplay":
		return DisplayEventAgentModeRoleplay
	case "tool":
		return DisplayEventAgentModeTool
	default:
		return ""
	}
}

func DisplayEventAgentRoute(destination string) string {
	switch strings.ToLower(strings.TrimSpace(destination)) {
	case "openclaw":
		return DisplayEventAgentRouteOpenClaw
	case "hermes":
		return DisplayEventAgentRouteHermes
	case "v21":
		return DisplayEventAgentRouteV21
	case "claude":
		return DisplayEventAgentRouteClaude
	default:
		return ""
	}
}

func normalizeDisplayEvent(event string) string {
	return strings.ToLower(strings.TrimSpace(event))
}

func normalizeDisplayCardID(cardID string) string {
	return strings.ToLower(strings.TrimSpace(cardID))
}

func normalizeEmotion(emotion string) string {
	if _, ok := validEmotions[emotion]; ok {
		return emotion
	}
	return EmotionNeutral
}

func normalizeAccent(accent string) string {
	if _, ok := validAccents[accent]; ok {
		return accent
	}
	return AccentDefault
}

func normalizeSceneMotion(motion *SceneMotion) *SceneMotion {
	if motion == nil {
		return nil
	}
	normalized := *motion
	if _, ok := validMotionPresets[normalized.Preset]; !ok {
		normalized.Preset = MotionPresetNone
	}
	if normalized.Intensity < 0 {
		normalized.Intensity = 0
	}
	if normalized.Intensity > 1 {
		normalized.Intensity = 1
	}
	return &normalized
}
