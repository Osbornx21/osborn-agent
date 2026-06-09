package stackchan

import (
	"sort"
	"strings"

	"stackchan-gateway/internal/mcp"
)

var displaySceneCatalogLifecycleOrder = []string{
	SceneIdle,
	SceneListening,
	SceneThinking,
	SceneSpeaking,
}

type DisplaySceneCatalogOptions struct {
	Display DisplayOptions
	Devices []DisplaySceneDevice
}

type DisplaySceneDevice struct {
	DeviceID      string
	AllowMCPTools []string
}

type DisplaySceneCatalog struct {
	SceneTTLMS          int                         `json:"scene_ttl_ms"`
	MaxCaptionChars     int                         `json:"max_caption_chars"`
	LifecycleSceneCount int                         `json:"lifecycle_scene_count"`
	LifecycleScenes     []DisplaySceneCatalogEntry  `json:"lifecycle_scenes"`
	EventSceneCount     int                         `json:"event_scene_count"`
	EventScenes         []DisplaySceneCatalogEntry  `json:"event_scenes"`
	DeviceCount         int                         `json:"device_count"`
	Devices             []DisplaySceneCatalogDevice `json:"devices"`
}

type DisplaySceneCatalogEntry struct {
	Lifecycle        string  `json:"lifecycle,omitempty"`
	Event            string  `json:"event,omitempty"`
	Configured       bool    `json:"configured"`
	Scene            string  `json:"scene"`
	Emotion          string  `json:"emotion,omitempty"`
	Accent           string  `json:"accent,omitempty"`
	HasStaticCaption bool    `json:"has_static_caption"`
	MotionPreset     string  `json:"motion_preset,omitempty"`
	MotionIntensity  float64 `json:"motion_intensity,omitempty"`
}

type DisplaySceneCatalogDevice struct {
	DeviceID                string `json:"device_id"`
	ScreenSceneMCPAvailable bool   `json:"screen_scene_mcp_available"`
	Available               bool   `json:"available"`
	LifecycleSceneCount     int    `json:"lifecycle_scene_count"`
	EventSceneCount         int    `json:"event_scene_count"`
}

func NewDisplaySceneCatalog(options DisplaySceneCatalogOptions) *DisplaySceneCatalog {
	display := normalizeDisplaySceneOptions(options.Display.withDefaults())
	composer := NewSceneComposer(display)
	lifecycleScenes := displaySceneCatalogLifecycleScenes(composer, display)
	eventScenes := displaySceneCatalogEventScenes(composer, display)
	devices := displaySceneCatalogDevices(options.Devices, len(lifecycleScenes), len(eventScenes))
	return &DisplaySceneCatalog{
		SceneTTLMS:          display.SceneTTLMS,
		MaxCaptionChars:     display.MaxCaptionChars,
		LifecycleSceneCount: len(lifecycleScenes),
		LifecycleScenes:     lifecycleScenes,
		EventSceneCount:     len(eventScenes),
		EventScenes:         eventScenes,
		DeviceCount:         len(devices),
		Devices:             devices,
	}
}

func displaySceneCatalogLifecycleScenes(composer *SceneComposer, display DisplayOptions) []DisplaySceneCatalogEntry {
	configured := map[string]struct{}{}
	for lifecycle := range display.LifecycleScenes {
		lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
		if IsLifecycleScene(lifecycle) {
			configured[lifecycle] = struct{}{}
		}
	}
	scenes := make([]DisplaySceneCatalogEntry, 0, len(displaySceneCatalogLifecycleOrder))
	for _, lifecycle := range displaySceneCatalogLifecycleOrder {
		scene := composer.ComposeLifecycle(SceneRequest{Scene: lifecycle})
		_, isConfigured := configured[lifecycle]
		entry := displaySceneCatalogEntryFromScene(scene, isConfigured)
		entry.Lifecycle = lifecycle
		scenes = append(scenes, entry)
	}
	return scenes
}

func displaySceneCatalogEventScenes(composer *SceneComposer, display DisplayOptions) []DisplaySceneCatalogEntry {
	if len(display.EventScenes) == 0 {
		return nil
	}
	events := make([]string, 0, len(display.EventScenes))
	seen := map[string]struct{}{}
	for event := range display.EventScenes {
		event = normalizeDisplayEvent(event)
		if event == "" || !IsDisplayEvent(event) {
			continue
		}
		if _, ok := seen[event]; ok {
			continue
		}
		seen[event] = struct{}{}
		events = append(events, event)
	}
	sort.Strings(events)
	scenes := make([]DisplaySceneCatalogEntry, 0, len(events))
	for _, event := range events {
		scene, ok := composer.ComposeEvent(event, SceneRequest{})
		if !ok {
			continue
		}
		entry := displaySceneCatalogEntryFromScene(scene, true)
		entry.Event = event
		scenes = append(scenes, entry)
	}
	return scenes
}

func displaySceneCatalogEntryFromScene(scene Scene, configured bool) DisplaySceneCatalogEntry {
	entry := DisplaySceneCatalogEntry{
		Configured:       configured,
		Scene:            scene.Scene,
		Emotion:          normalizeEmotion(strings.TrimSpace(scene.Emotion)),
		Accent:           normalizeAccent(strings.TrimSpace(scene.Accent)),
		HasStaticCaption: strings.TrimSpace(scene.Caption) != "",
	}
	if scene.Motion != nil {
		motion := normalizeSceneMotion(scene.Motion)
		entry.MotionPreset = motion.Preset
		entry.MotionIntensity = motion.Intensity
	}
	return entry
}

func displaySceneCatalogDevices(configured []DisplaySceneDevice, lifecycleCount int, eventCount int) []DisplaySceneCatalogDevice {
	if len(configured) == 0 {
		return nil
	}
	devices := make([]DisplaySceneCatalogDevice, 0, len(configured))
	for _, device := range configured {
		deviceID := strings.TrimSpace(device.DeviceID)
		if deviceID == "" {
			continue
		}
		screenSceneAvailable := displaySceneDeviceAllowsScreenScene(device.AllowMCPTools)
		devices = append(devices, DisplaySceneCatalogDevice{
			DeviceID:                deviceID,
			ScreenSceneMCPAvailable: screenSceneAvailable,
			Available:               screenSceneAvailable && (lifecycleCount > 0 || eventCount > 0),
			LifecycleSceneCount:     lifecycleCount,
			EventSceneCount:         eventCount,
		})
	}
	return devices
}

func displaySceneDeviceAllowsScreenScene(tools []string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool) == mcp.ToolSetScreenScene {
			return true
		}
	}
	return false
}

func normalizeDisplaySceneOptions(display DisplayOptions) DisplayOptions {
	display.LifecycleScenes = normalizeScenePolicyMap(display.LifecycleScenes, func(key string) string {
		return strings.ToLower(strings.TrimSpace(key))
	})
	display.EventScenes = normalizeScenePolicyMap(display.EventScenes, normalizeDisplayEvent)
	return display
}

func normalizeScenePolicyMap(policies map[string]ScenePolicy, normalize func(string) string) map[string]ScenePolicy {
	if len(policies) == 0 {
		return nil
	}
	normalized := make(map[string]ScenePolicy, len(policies))
	for key, policy := range policies {
		key = normalize(key)
		if key != "" {
			normalized[key] = policy
		}
	}
	return normalized
}
