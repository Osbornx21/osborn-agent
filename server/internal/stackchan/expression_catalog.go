package stackchan

import (
	"sort"
	"strings"

	"stackchan-gateway/internal/mcp"
)

var expressionCatalogCueOrder = []string{
	CueAttentive,
	CueNod,
	CueCelebrate,
	CueThinking,
	CueSettle,
}

type ExpressionCatalogOptions struct {
	Policies      map[string]ExpressionPolicy
	LifecycleCues map[string]string
	EventCues     map[string]string
	Devices       []ExpressionDevice
}

type ExpressionDevice struct {
	DeviceID      string
	AllowMCPTools []string
}

type ExpressionCatalog struct {
	CueCount          int                       `json:"cue_count"`
	Cues              []ExpressionCatalogCue    `json:"cues"`
	LifecycleCueCount int                       `json:"lifecycle_cue_count"`
	LifecycleCues     []ExpressionLifecycleCue  `json:"lifecycle_cues"`
	EventCueCount     int                       `json:"event_cue_count"`
	EventCues         []ExpressionEventCue      `json:"event_cues"`
	DeviceCount       int                       `json:"device_count"`
	Devices           []ExpressionCatalogDevice `json:"devices"`
}

type ExpressionCatalogCue struct {
	Cue                  string  `json:"cue"`
	Configured           bool    `json:"configured"`
	HasMotion            bool    `json:"has_motion"`
	MotionYawDeg         int     `json:"motion_yaw_deg,omitempty"`
	MotionPitchDeg       int     `json:"motion_pitch_deg,omitempty"`
	MotionSpeed          int     `json:"motion_speed,omitempty"`
	HasLED               bool    `json:"has_led"`
	LEDR                 int     `json:"led_r,omitempty"`
	LEDG                 int     `json:"led_g,omitempty"`
	LEDB                 int     `json:"led_b,omitempty"`
	HasScene             bool    `json:"has_scene"`
	Scene                string  `json:"scene,omitempty"`
	Emotion              string  `json:"emotion,omitempty"`
	Accent               string  `json:"accent,omitempty"`
	HasStaticCaption     bool    `json:"has_static_caption"`
	SceneMotionPreset    string  `json:"scene_motion_preset,omitempty"`
	SceneMotionIntensity float64 `json:"scene_motion_intensity,omitempty"`
}

type ExpressionLifecycleCue struct {
	Lifecycle string `json:"lifecycle"`
	Cue       string `json:"cue"`
}

type ExpressionEventCue struct {
	Event string `json:"event"`
	Cue   string `json:"cue"`
}

type ExpressionCatalogDevice struct {
	DeviceID                string `json:"device_id"`
	HeadMCPAvailable        bool   `json:"head_mcp_available"`
	LEDMCPAvailable         bool   `json:"led_mcp_available"`
	BodyMCPAvailable        bool   `json:"body_mcp_available"`
	ScreenSceneMCPAvailable bool   `json:"screen_scene_mcp_available"`
	Available               bool   `json:"available"`
	CueCount                int    `json:"cue_count"`
}

func NewExpressionCatalog(options ExpressionCatalogOptions) *ExpressionCatalog {
	policies := normalizeExpressionPolicies(options.Policies)
	cues := expressionCatalogCues(policies)
	lifecycleCues := expressionCatalogLifecycleCues(options.LifecycleCues)
	eventCues := expressionCatalogEventCues(options.EventCues)
	devices := expressionCatalogDevices(options.Devices, len(cues))
	return &ExpressionCatalog{
		CueCount:          len(cues),
		Cues:              cues,
		LifecycleCueCount: len(lifecycleCues),
		LifecycleCues:     lifecycleCues,
		EventCueCount:     len(eventCues),
		EventCues:         eventCues,
		DeviceCount:       len(devices),
		Devices:           devices,
	}
}

func expressionCatalogCues(policies map[string]ExpressionPolicy) []ExpressionCatalogCue {
	cues := make([]ExpressionCatalogCue, 0, len(expressionCatalogCueOrder))
	for _, cue := range expressionCatalogCueOrder {
		plan, err := ExpressionForCueWithPolicies(cue, policies)
		if err != nil {
			continue
		}
		_, configured := policies[cue]
		cues = append(cues, expressionCatalogCueFromPlan(plan, configured))
	}
	return cues
}

func expressionCatalogCueFromPlan(plan ExpressionPlan, configured bool) ExpressionCatalogCue {
	cue := ExpressionCatalogCue{
		Cue:        plan.Cue,
		Configured: configured,
	}
	if plan.Motion != nil {
		cue.HasMotion = true
		cue.MotionYawDeg = plan.Motion.Yaw
		cue.MotionPitchDeg = plan.Motion.Pitch
		cue.MotionSpeed = plan.Motion.Speed
	}
	if plan.LED != nil {
		cue.HasLED = true
		cue.LEDR = plan.LED.R
		cue.LEDG = plan.LED.G
		cue.LEDB = plan.LED.B
	}
	scene := strings.TrimSpace(plan.Scene.Scene)
	if scene != "" {
		cue.HasScene = true
		if IsValidScene(scene) {
			cue.Scene = scene
		} else {
			cue.Scene = SceneIdle
		}
		cue.Emotion = normalizeEmotion(strings.TrimSpace(plan.Scene.Emotion))
		cue.Accent = normalizeAccent(strings.TrimSpace(plan.Scene.Accent))
		cue.HasStaticCaption = strings.TrimSpace(plan.Scene.Caption) != ""
		if motion := normalizeSceneMotion(plan.Scene.Motion); motion != nil {
			cue.SceneMotionPreset = motion.Preset
			cue.SceneMotionIntensity = motion.Intensity
		}
	}
	return cue
}

func expressionCatalogLifecycleCues(configured map[string]string) []ExpressionLifecycleCue {
	if len(configured) == 0 {
		return nil
	}
	lifecycles := make([]string, 0, len(configured))
	normalized := make(map[string]string, len(configured))
	for lifecycle, cue := range configured {
		lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
		cue = normalizeCue(cue)
		if lifecycle == "" || cue == "" || !IsLifecycleScene(lifecycle) || !IsExpressionCue(cue) {
			continue
		}
		if _, exists := normalized[lifecycle]; !exists {
			lifecycles = append(lifecycles, lifecycle)
		}
		normalized[lifecycle] = cue
	}
	sort.Strings(lifecycles)
	out := make([]ExpressionLifecycleCue, 0, len(lifecycles))
	for _, lifecycle := range lifecycles {
		out = append(out, ExpressionLifecycleCue{Lifecycle: lifecycle, Cue: normalized[lifecycle]})
	}
	return out
}

func expressionCatalogEventCues(configured map[string]string) []ExpressionEventCue {
	if len(configured) == 0 {
		return nil
	}
	events := make([]string, 0, len(configured))
	normalized := make(map[string]string, len(configured))
	for event, cue := range configured {
		event = normalizeDisplayEvent(event)
		cue = normalizeCue(cue)
		if event == "" || cue == "" || !IsDisplayEvent(event) || !IsExpressionCue(cue) {
			continue
		}
		if _, exists := normalized[event]; !exists {
			events = append(events, event)
		}
		normalized[event] = cue
	}
	sort.Strings(events)
	out := make([]ExpressionEventCue, 0, len(events))
	for _, event := range events {
		out = append(out, ExpressionEventCue{Event: event, Cue: normalized[event]})
	}
	return out
}

func expressionCatalogDevices(configured []ExpressionDevice, cueCount int) []ExpressionCatalogDevice {
	if len(configured) == 0 {
		return nil
	}
	devices := make([]ExpressionCatalogDevice, 0, len(configured))
	for _, device := range configured {
		deviceID := strings.TrimSpace(device.DeviceID)
		if deviceID == "" {
			continue
		}
		headAvailable := expressionDeviceAllowsTool(device.AllowMCPTools, mcp.ToolSetHeadAngles)
		ledAvailable := expressionDeviceAllowsTool(device.AllowMCPTools, mcp.ToolSetLEDColor)
		screenSceneAvailable := expressionDeviceAllowsTool(device.AllowMCPTools, mcp.ToolSetScreenScene)
		bodyAvailable := headAvailable || ledAvailable
		devices = append(devices, ExpressionCatalogDevice{
			DeviceID:                deviceID,
			HeadMCPAvailable:        headAvailable,
			LEDMCPAvailable:         ledAvailable,
			BodyMCPAvailable:        bodyAvailable,
			ScreenSceneMCPAvailable: screenSceneAvailable,
			Available:               cueCount > 0 && (bodyAvailable || screenSceneAvailable),
			CueCount:                cueCount,
		})
	}
	return devices
}

func expressionDeviceAllowsTool(tools []string, target string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool) == target {
			return true
		}
	}
	return false
}

func normalizeExpressionPolicies(policies map[string]ExpressionPolicy) map[string]ExpressionPolicy {
	if len(policies) == 0 {
		return nil
	}
	normalized := make(map[string]ExpressionPolicy, len(policies))
	for cue, policy := range policies {
		cue = normalizeCue(cue)
		if cue != "" {
			normalized[cue] = policy
		}
	}
	return normalized
}
