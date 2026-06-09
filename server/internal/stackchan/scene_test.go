package stackchan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSceneComposerCreatesSupportedScenes(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{SceneTTLMS: 1200, MaxCaptionChars: 12})

	for _, sceneName := range []string{
		SceneIdle,
		SceneListening,
		SceneThinking,
		SceneSpeaking,
		SceneTool,
		SceneError,
		SceneSleep,
	} {
		t.Run(sceneName, func(t *testing.T) {
			scene := composer.Compose(SceneRequest{
				SessionID:  "sess_scene",
				Generation: 7,
				Scene:      sceneName,
				Emotion:    "curious",
				Caption:    "我在查一下。",
				Accent:     "cyan",
				Motion:     &SceneMotion{Preset: "nod_soft", Intensity: 0.35},
			})
			if scene.Type != SceneType {
				t.Fatalf("type = %q, want %q", scene.Type, SceneType)
			}
			if scene.Scene != sceneName {
				t.Fatalf("scene = %q, want %q", scene.Scene, sceneName)
			}
			if scene.TTLMS != 1200 {
				t.Fatalf("ttl = %d, want 1200", scene.TTLMS)
			}
			if _, err := json.Marshal(scene); err != nil {
				t.Fatalf("marshal scene: %v", err)
			}
		})
	}
}

func TestSceneComposerShortensLongCaptionAtRuneBoundary(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{MaxCaptionChars: 5})

	scene := composer.Compose(SceneRequest{
		SessionID:  "sess_scene",
		Generation: 1,
		Scene:      SceneSpeaking,
		Caption:    "你好世界准备好了",
	})

	if scene.Caption != "你好世界准" {
		t.Fatalf("caption = %q, want rune-boundary truncation", scene.Caption)
	}
}

func TestSceneComposerUsesConfiguredLifecycleScenePolicy(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{
		SceneTTLMS:      900,
		MaxCaptionChars: 6,
		LifecycleScenes: map[string]ScenePolicy{
			SceneThinking: {
				Emotion: EmotionReady,
				Caption: "稍等，我认真想一下。",
				Accent:  AccentRed,
				Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.42},
			},
		},
	})

	scene := composer.ComposeLifecycle(SceneRequest{
		SessionID:  "sess_lifecycle",
		Generation: 12,
		Scene:      SceneThinking,
	})

	if scene.Scene != SceneThinking || scene.Emotion != EmotionReady || scene.Accent != AccentRed {
		t.Fatalf("scene policy = %+v, want configured thinking policy", scene)
	}
	if scene.Caption != "稍等，我认真" {
		t.Fatalf("caption = %q, want configured caption truncated by display options", scene.Caption)
	}
	if scene.Motion == nil || scene.Motion.Preset != MotionPresetNodSoft || scene.Motion.Intensity != 0.42 {
		t.Fatalf("motion = %+v, want configured lifecycle motion", scene.Motion)
	}
	if scene.TTLMS != 900 || scene.SessionID != "sess_lifecycle" || scene.Generation != 12 {
		t.Fatalf("identity/ttl = %+v, want configured ttl and turn identity", scene)
	}
}

func TestSceneComposerUsesDefaultLifecyclePolicyWhenConfigOmitsScene(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{})

	scene := composer.ComposeLifecycle(SceneRequest{
		SessionID:  "sess_lifecycle_default",
		Generation: 13,
		Scene:      SceneSpeaking,
	})

	if scene.Scene != SceneSpeaking || scene.Emotion != EmotionWarm || scene.Caption != "我在说。" || scene.Accent != AccentGreen {
		t.Fatalf("default lifecycle scene = %+v, want speaking defaults", scene)
	}
	if scene.Motion == nil || scene.Motion.Preset != MotionPresetNodSoft {
		t.Fatalf("default speaking motion = %+v, want nod_soft", scene.Motion)
	}
}

func TestSceneComposerUsesConfiguredEventScenePolicy(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{
		SceneTTLMS:      1100,
		MaxCaptionChars: 20,
		EventScenes: map[string]ScenePolicy{
			DisplayEventMemoryUpdated: {
				Scene:   SceneTool,
				Emotion: EmotionReady,
				Caption: "我记住了。",
				Accent:  AccentGreen,
				Motion:  &SceneMotion{Preset: MotionPresetAttentive, Intensity: 0.5},
			},
		},
	})

	scene, ok := composer.ComposeEvent(DisplayEventMemoryUpdated, SceneRequest{
		SessionID:  "sess_event_scene",
		Generation: 14,
	})

	if !ok {
		t.Fatal("ComposeEvent() ok = false, want configured event scene")
	}
	if scene.Scene != SceneTool || scene.Emotion != EmotionReady || scene.Caption != "我记住了。" || scene.Accent != AccentGreen {
		t.Fatalf("event scene = %+v, want configured memory update policy", scene)
	}
	if scene.Motion == nil || scene.Motion.Preset != MotionPresetAttentive || scene.Motion.Intensity != 0.5 {
		t.Fatalf("event motion = %+v, want configured motion", scene.Motion)
	}
}

func TestSceneComposerSkipsUnknownEventScenePolicy(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{})

	if scene, ok := composer.ComposeEvent("raw.event", SceneRequest{SessionID: "sess_unknown", Generation: 15}); ok || scene.Scene != "" {
		t.Fatalf("ComposeEvent(raw.event) = %+v/%t, want skipped", scene, ok)
	}
}

func TestToolOutcomeDisplayEventsAreRecognized(t *testing.T) {
	for _, event := range []string{"tool.succeeded", "tool.failed"} {
		if !IsDisplayEvent(event) {
			t.Fatalf("IsDisplayEvent(%q) = false, want true", event)
		}
	}
}

func TestDomainToolDisplayEventsAreRecognized(t *testing.T) {
	for _, event := range []string{"homeassistant.state", "homeassistant.action", "search.web"} {
		if !IsDisplayEvent(event) {
			t.Fatalf("IsDisplayEvent(%q) = false, want true", event)
		}
	}
}

func TestDisplayEventAgentRouteMapsKnownDestinations(t *testing.T) {
	for _, tc := range []struct {
		destination string
		want        string
	}{
		{destination: "openclaw", want: DisplayEventAgentRouteOpenClaw},
		{destination: "Hermes", want: DisplayEventAgentRouteHermes},
		{destination: " v21 ", want: DisplayEventAgentRouteV21},
		{destination: "claude", want: DisplayEventAgentRouteClaude},
		{destination: "raw", want: ""},
	} {
		if got := DisplayEventAgentRoute(tc.destination); got != tc.want {
			t.Fatalf("DisplayEventAgentRoute(%q) = %q, want %q", tc.destination, got, tc.want)
		}
	}
}

func TestAgentRouteSkippedDisplayEventIsRecognized(t *testing.T) {
	if !IsDisplayEvent("agent_route.skipped") {
		t.Fatal("IsDisplayEvent(agent_route.skipped) = false, want configurable bridge-skip feedback event")
	}
}

func TestSceneComposerFallsBackToIdleForUnknownScene(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{})

	scene := composer.Compose(SceneRequest{
		SessionID:  "sess_scene",
		Generation: 1,
		Scene:      "raw_pixels",
		Caption:    "ignored raw scene",
	})

	if scene.Scene != SceneIdle {
		t.Fatalf("scene = %q, want %q", scene.Scene, SceneIdle)
	}
}

func TestSceneComposerNormalizesUnknownDisplayHints(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{})

	scene := composer.Compose(SceneRequest{
		SessionID:  "sess_scene",
		Generation: 1,
		Scene:      SceneTool,
		Emotion:    "raw-provider-mood",
		Accent:     "laser-purple",
		Motion:     &SceneMotion{Preset: "spin_forever", Intensity: 5},
	})

	if scene.Emotion != EmotionNeutral {
		t.Fatalf("emotion = %q, want %q", scene.Emotion, EmotionNeutral)
	}
	if scene.Accent != AccentDefault {
		t.Fatalf("accent = %q, want %q", scene.Accent, AccentDefault)
	}
	if scene.Motion == nil {
		t.Fatal("motion = nil, want normalized motion")
	}
	if scene.Motion.Preset != MotionPresetNone {
		t.Fatalf("motion preset = %q, want %q", scene.Motion.Preset, MotionPresetNone)
	}
	if scene.Motion.Intensity != 1 {
		t.Fatalf("motion intensity = %v, want 1", scene.Motion.Intensity)
	}
}

func TestSceneComposerClampsNegativeMotionIntensity(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{})

	scene := composer.Compose(SceneRequest{
		SessionID:  "sess_scene",
		Generation: 1,
		Scene:      SceneListening,
		Motion:     &SceneMotion{Preset: MotionPresetAttentive, Intensity: -0.5},
	})

	if scene.Motion.Intensity != 0 {
		t.Fatalf("motion intensity = %v, want 0", scene.Motion.Intensity)
	}
	if scene.Motion.Preset != MotionPresetAttentive {
		t.Fatalf("motion preset = %q, want %q", scene.Motion.Preset, MotionPresetAttentive)
	}
}

func TestSceneComposerComposesConfiguredDisplayCardWithBoundedCaption(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{
		SceneTTLMS:      1200,
		MaxCaptionChars: 16,
		Cards: map[string]DisplayCardPolicy{
			"status.note": {
				ScenePolicy: ScenePolicy{
					Scene:   SceneTool,
					Emotion: EmotionWarm,
					Caption: "静态说明。",
					Accent:  AccentGreen,
					Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.2},
				},
				AllowCaption:    true,
				MaxCaptionChars: 8,
			},
			"fixed": {
				ScenePolicy: ScenePolicy{
					Scene:   SceneThinking,
					Emotion: EmotionCurious,
					Caption: "固定卡片。",
				},
			},
		},
	})

	scene, ok := composer.ComposeCard(" STATUS.NOTE ", SceneRequest{
		SessionID:  "sess_card",
		Generation: 9,
		Caption:    "这是一段很长的模型摘要",
	})

	if !ok {
		t.Fatal("ComposeCard() ok = false, want configured card")
	}
	if scene.Scene != SceneTool || scene.Emotion != EmotionWarm || scene.Accent != AccentGreen || scene.TTLMS != 1200 {
		t.Fatalf("scene = %+v, want configured card scene", scene)
	}
	if scene.Caption != "这是一段很长的模" {
		t.Fatalf("caption = %q, want card-bounded model caption", scene.Caption)
	}

	fixed, ok := composer.ComposeCard("fixed", SceneRequest{
		SessionID:  "sess_card",
		Generation: 10,
		Caption:    "模型不应该覆盖",
	})
	if !ok {
		t.Fatal("ComposeCard(fixed) ok = false")
	}
	if fixed.Caption != "固定卡片。" {
		t.Fatalf("fixed caption = %q, want static configured caption", fixed.Caption)
	}
}

func TestDisplayCardToolInputSchemaListsConfiguredCards(t *testing.T) {
	schema := DisplayCardToolInputSchema([]string{"status.note", "brief"})
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want map", schema["properties"])
	}
	card, ok := properties["card"].(map[string]any)
	if !ok {
		t.Fatalf("card property = %#v, want map", properties["card"])
	}
	enum, ok := card["enum"].([]any)
	if !ok || len(enum) != 2 || enum[0] != "brief" || enum[1] != "status.note" {
		t.Fatalf("card enum = %#v, want sorted configured cards", card["enum"])
	}
	if properties["caption"] == nil {
		t.Fatalf("schema properties = %#v, want optional caption", properties)
	}
}

func TestDisplayCardCatalogRedactsCaptionsAndReportsDeviceAvailability(t *testing.T) {
	catalog := NewDisplayCardCatalog(DisplayCardCatalogOptions{
		Display: DisplayOptions{
			SceneTTLMS:      1200,
			MaxCaptionChars: 20,
			Cards: map[string]DisplayCardPolicy{
				" status.note ": {
					ScenePolicy: ScenePolicy{
						Scene:   SceneTool,
						Emotion: EmotionWarm,
						Caption: "operator caption must stay private",
						Accent:  AccentGreen,
						Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.2},
					},
					AllowCaption:    true,
					MaxCaptionChars: 12,
				},
			},
		},
		Devices: []DisplayCardDevice{
			{DeviceID: "stackchan-s3-main", AllowMCPTools: []string{"self.screen.set_scene"}},
			{DeviceID: "stackchan-s3-dev", AllowMCPTools: []string{"self.robot.set_head_angles"}},
		},
	})

	if catalog.CardCount != 1 || len(catalog.Cards) != 1 {
		t.Fatalf("catalog cards = %+v, want one configured card", catalog)
	}
	card := catalog.Cards[0]
	if card.CardID != "status.note" || card.Scene != SceneTool || card.Emotion != EmotionWarm || card.Accent != AccentGreen {
		t.Fatalf("card = %+v, want normalized safe card metadata", card)
	}
	if !card.AllowCaption || card.MaxCaptionChars != 12 || !card.HasStaticCaption {
		t.Fatalf("card caption policy = %+v, want flags without raw caption text", card)
	}
	if card.MotionPreset != MotionPresetNodSoft || card.MotionIntensity != 0.2 {
		t.Fatalf("card motion = %+v, want safe motion metadata", card)
	}
	if len(catalog.Devices) != 2 {
		t.Fatalf("devices = %+v, want two device statuses", catalog.Devices)
	}
	if !catalog.Devices[0].ScreenSceneMCPAvailable || !catalog.Devices[0].Available {
		t.Fatalf("main device = %+v, want display cards available", catalog.Devices[0])
	}
	if catalog.Devices[1].ScreenSceneMCPAvailable || catalog.Devices[1].Available {
		t.Fatalf("dev device = %+v, want display cards unavailable without screen MCP", catalog.Devices[1])
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	for _, forbidden := range []string{"operator caption", "self.screen.set_scene", "secret", "token"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("catalog leaked %q: %s", forbidden, string(data))
		}
	}
}

func TestDisplaySceneCatalogRedactsCaptionsAndReportsDeviceAvailability(t *testing.T) {
	catalog := NewDisplaySceneCatalog(DisplaySceneCatalogOptions{
		Display: DisplayOptions{
			SceneTTLMS:      1200,
			MaxCaptionChars: 20,
			LifecycleScenes: map[string]ScenePolicy{
				SceneThinking: {
					Emotion: EmotionReady,
					Caption: "operator lifecycle caption must stay private",
					Accent:  AccentAmber,
					Motion:  &SceneMotion{Preset: MotionPresetAttentive, Intensity: 0.4},
				},
			},
			EventScenes: map[string]ScenePolicy{
				DisplayEventToolRunning: {
					Emotion: EmotionWarm,
					Caption: "operator event caption must stay private",
					Accent:  AccentGreen,
					Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.2},
				},
			},
		},
		Devices: []DisplaySceneDevice{
			{DeviceID: "stackchan-s3-main", AllowMCPTools: []string{"self.screen.set_scene"}},
			{DeviceID: "head-only", AllowMCPTools: []string{"self.robot.set_head_angles"}},
		},
	})

	if catalog.SceneTTLMS != 1200 || catalog.MaxCaptionChars != 20 {
		t.Fatalf("catalog display bounds = %+v, want configured safe display bounds", catalog)
	}
	if catalog.LifecycleSceneCount != 4 || len(catalog.LifecycleScenes) != 4 {
		t.Fatalf("lifecycle scenes = %+v, want four lifecycle entries", catalog.LifecycleScenes)
	}
	thinking := displaySceneCatalogLifecycleByName(t, catalog.LifecycleScenes, SceneThinking)
	if !thinking.Configured || thinking.Scene != SceneThinking || thinking.Emotion != EmotionReady || thinking.Accent != AccentAmber || !thinking.HasStaticCaption {
		t.Fatalf("thinking lifecycle = %+v, want configured safe metadata without caption text", thinking)
	}
	if thinking.MotionPreset != MotionPresetAttentive || thinking.MotionIntensity != 0.4 {
		t.Fatalf("thinking motion = %+v, want configured motion metadata", thinking)
	}
	if catalog.EventSceneCount != 1 || len(catalog.EventScenes) != 1 {
		t.Fatalf("event scenes = %+v, want one configured event", catalog.EventScenes)
	}
	toolRunning := displaySceneCatalogEventByName(t, catalog.EventScenes, DisplayEventToolRunning)
	if !toolRunning.Configured || toolRunning.Scene != SceneTool || toolRunning.Emotion != EmotionWarm || toolRunning.Accent != AccentGreen || !toolRunning.HasStaticCaption {
		t.Fatalf("tool.running event = %+v, want default scene plus configured safe metadata", toolRunning)
	}
	main := displaySceneCatalogDeviceByID(t, catalog.Devices, "stackchan-s3-main")
	if !main.ScreenSceneMCPAvailable || !main.Available {
		t.Fatalf("main device = %+v, want display scenes available", main)
	}
	headOnly := displaySceneCatalogDeviceByID(t, catalog.Devices, "head-only")
	if headOnly.ScreenSceneMCPAvailable || headOnly.Available {
		t.Fatalf("head-only device = %+v, want display scenes unavailable without screen MCP", headOnly)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	for _, forbidden := range []string{"operator lifecycle caption", "operator event caption", "self.screen.set_scene", "self.robot.set_head_angles", "secret", "token"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("catalog leaked %q: %s", forbidden, string(data))
		}
	}
}

func displaySceneCatalogLifecycleByName(t *testing.T, scenes []DisplaySceneCatalogEntry, lifecycle string) DisplaySceneCatalogEntry {
	t.Helper()
	for _, scene := range scenes {
		if scene.Lifecycle == lifecycle {
			return scene
		}
	}
	t.Fatalf("missing lifecycle %q in %+v", lifecycle, scenes)
	return DisplaySceneCatalogEntry{}
}

func displaySceneCatalogEventByName(t *testing.T, scenes []DisplaySceneCatalogEntry, event string) DisplaySceneCatalogEntry {
	t.Helper()
	for _, scene := range scenes {
		if scene.Event == event {
			return scene
		}
	}
	t.Fatalf("missing event %q in %+v", event, scenes)
	return DisplaySceneCatalogEntry{}
}

func displaySceneCatalogDeviceByID(t *testing.T, devices []DisplaySceneCatalogDevice, deviceID string) DisplaySceneCatalogDevice {
	t.Helper()
	for _, device := range devices {
		if device.DeviceID == deviceID {
			return device
		}
	}
	t.Fatalf("missing device %q in %+v", deviceID, devices)
	return DisplaySceneCatalogDevice{}
}

func TestSceneBuildsMCPArguments(t *testing.T) {
	composer := NewSceneComposer(DisplayOptions{SceneTTLMS: 1800, MaxCaptionChars: 48})

	scene := composer.Compose(SceneRequest{
		SessionID:  "sess_scene",
		Generation: 3,
		Scene:      SceneListening,
		Emotion:    EmotionCurious,
		Caption:    "我在听。",
		Accent:     AccentCyan,
		Motion:     &SceneMotion{Preset: MotionPresetAttentive, Intensity: 0.35},
	})
	args := scene.MCPArguments()

	if args["type"] != SceneType || args["scene"] != SceneListening || args["caption"] != "我在听。" {
		t.Fatalf("args = %#v, want scene type/listening/caption", args)
	}
	if args["ttl_ms"] != float64(1800) || args["generation"] != float64(3) {
		t.Fatalf("numeric args = %#v, want configured ttl and generation", args)
	}
}
