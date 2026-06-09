package stackchan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExpressionForCueUsesConfiguredPolicy(t *testing.T) {
	plan, err := ExpressionForCueWithPolicies(CueNod, map[string]ExpressionPolicy{
		CueNod: {
			Motion: &MotionCommand{Yaw: 4, Pitch: 5, Speed: 240},
			LED:    &LEDCommand{R: 12, G: 34, B: 56},
			Scene: ScenePolicy{
				Scene:   SceneTool,
				Emotion: EmotionHappy,
				Caption: "收到。",
				Accent:  AccentGreen,
				Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.5},
			},
		},
	})
	if err != nil {
		t.Fatalf("ExpressionForCueWithPolicies() error = %v", err)
	}
	if plan.Motion == nil || plan.Motion.Yaw != 4 || plan.Motion.Pitch != 5 || plan.Motion.Speed != 240 || plan.Motion.Reason != "stackchan_express" {
		t.Fatalf("motion = %+v, want configured motion with safe reason", plan.Motion)
	}
	if plan.LED == nil || plan.LED.R != 12 || plan.LED.G != 34 || plan.LED.B != 56 || plan.LED.Reason != "stackchan_express" {
		t.Fatalf("led = %+v, want configured LED with safe reason", plan.LED)
	}
	if plan.Scene.Scene != SceneTool || plan.Scene.Caption != "收到。" || plan.Scene.Emotion != EmotionHappy || plan.Scene.Accent != AccentGreen {
		t.Fatalf("scene = %+v, want configured scene", plan.Scene)
	}
}

func TestExpressionForCueRejectsUnknownPolicyKey(t *testing.T) {
	if _, err := ExpressionForCueWithPolicies("spin_forever", map[string]ExpressionPolicy{
		"spin_forever": {Motion: &MotionCommand{Yaw: 45}},
	}); err != ErrExpressionCueInvalid {
		t.Fatalf("ExpressionForCueWithPolicies() error = %v, want ErrExpressionCueInvalid", err)
	}
}

func TestExpressionSequenceToolInputSchemaBoundsCueList(t *testing.T) {
	schema := ExpressionSequenceToolInputSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want map", schema["properties"])
	}
	cues, ok := properties["cues"].(map[string]any)
	if !ok {
		t.Fatalf("cues property = %#v, want map", properties["cues"])
	}
	if cues["type"] != "array" || cues["minItems"] != 1 || cues["maxItems"] != MaxExpressionSequenceCues {
		t.Fatalf("cues schema = %#v, want bounded cue array", cues)
	}
	items, ok := cues["items"].(map[string]any)
	if !ok || items["enum"] == nil {
		t.Fatalf("cues items = %#v, want enum item schema", cues["items"])
	}
}

func TestExpressionSequencePresetToolInputSchemaListsConfiguredPresets(t *testing.T) {
	schema := ExpressionSequencePresetToolInputSchema([]string{"agree.quick", "celebrate"})
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want map", schema["properties"])
	}
	sequence, ok := properties["sequence"].(map[string]any)
	if !ok {
		t.Fatalf("sequence property = %#v, want map", properties["sequence"])
	}
	values, ok := sequence["enum"].([]any)
	if !ok {
		t.Fatalf("sequence enum = %#v, want []any", sequence["enum"])
	}
	if len(values) != 2 || values[0] != "agree.quick" || values[1] != "celebrate" {
		t.Fatalf("sequence enum = %#v, want sorted configured preset ids", values)
	}
}

func TestExpressionCatalogRedactsCaptionsAndReportsDeviceAvailability(t *testing.T) {
	catalog := NewExpressionCatalog(ExpressionCatalogOptions{
		Policies: map[string]ExpressionPolicy{
			CueNod: {
				Motion: &MotionCommand{Yaw: 4, Pitch: 5, Speed: 240},
				LED:    &LEDCommand{R: 12, G: 34, B: 56},
				Scene: ScenePolicy{
					Scene:   SceneTool,
					Emotion: EmotionHappy,
					Caption: "operator expression caption must stay private",
					Accent:  AccentGreen,
					Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.5},
				},
			},
		},
		LifecycleCues: map[string]string{
			SceneThinking: CueThinking,
		},
		EventCues: map[string]string{
			DisplayEventAgentRouteOpenClaw: CueNod,
		},
		Devices: []ExpressionDevice{
			{
				DeviceID:      "stackchan-s3-main",
				AllowMCPTools: []string{"self.robot.set_head_angles", "self.robot.set_led_color", "self.screen.set_scene"},
			},
			{
				DeviceID:      "screen-only",
				AllowMCPTools: []string{"self.screen.set_scene"},
			},
			{
				DeviceID:      "status-only",
				AllowMCPTools: []string{"self.get_device_status"},
			},
		},
	})

	if catalog.CueCount != 5 || len(catalog.Cues) != 5 {
		t.Fatalf("catalog cues = %+v, want five fixed semantic cues", catalog.Cues)
	}
	nod := expressionCatalogCueByID(t, catalog.Cues, CueNod)
	if !nod.Configured || !nod.HasMotion || nod.MotionYawDeg != 4 || nod.MotionPitchDeg != 5 || nod.MotionSpeed != 240 {
		t.Fatalf("nod motion = %+v, want configured safe motion metadata", nod)
	}
	if !nod.HasLED || nod.LEDR != 12 || nod.LEDG != 34 || nod.LEDB != 56 {
		t.Fatalf("nod LED = %+v, want configured safe LED metadata", nod)
	}
	if !nod.HasScene || nod.Scene != SceneTool || nod.Emotion != EmotionHappy || nod.Accent != AccentGreen || !nod.HasStaticCaption {
		t.Fatalf("nod scene = %+v, want safe scene metadata without caption text", nod)
	}
	if nod.SceneMotionPreset != MotionPresetNodSoft || nod.SceneMotionIntensity != 0.5 {
		t.Fatalf("nod scene motion = %+v, want configured scene motion metadata", nod)
	}
	if catalog.LifecycleCueCount != 1 || len(catalog.LifecycleCues) != 1 || catalog.LifecycleCues[0].Lifecycle != SceneThinking || catalog.LifecycleCues[0].Cue != CueThinking {
		t.Fatalf("lifecycle cues = %+v, want configured thinking cue", catalog.LifecycleCues)
	}
	if catalog.EventCueCount != 1 || len(catalog.EventCues) != 1 || catalog.EventCues[0].Event != DisplayEventAgentRouteOpenClaw || catalog.EventCues[0].Cue != CueNod {
		t.Fatalf("event cues = %+v, want configured OpenClaw route cue", catalog.EventCues)
	}
	main := expressionCatalogDeviceByID(t, catalog.Devices, "stackchan-s3-main")
	if !main.Available || !main.BodyMCPAvailable || !main.ScreenSceneMCPAvailable || !main.HeadMCPAvailable || !main.LEDMCPAvailable {
		t.Fatalf("main device = %+v, want body and screen expression available", main)
	}
	screenOnly := expressionCatalogDeviceByID(t, catalog.Devices, "screen-only")
	if !screenOnly.Available || screenOnly.BodyMCPAvailable || !screenOnly.ScreenSceneMCPAvailable {
		t.Fatalf("screen-only device = %+v, want scene-only expression available", screenOnly)
	}
	statusOnly := expressionCatalogDeviceByID(t, catalog.Devices, "status-only")
	if statusOnly.Available || statusOnly.BodyMCPAvailable || statusOnly.ScreenSceneMCPAvailable {
		t.Fatalf("status-only device = %+v, want expression unavailable", statusOnly)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	for _, forbidden := range []string{"operator expression caption", "self.robot.set_head_angles", "self.robot.set_led_color", "self.screen.set_scene", "secret", "token"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("catalog leaked %q: %s", forbidden, string(data))
		}
	}
}

func expressionCatalogCueByID(t *testing.T, cues []ExpressionCatalogCue, cueID string) ExpressionCatalogCue {
	t.Helper()
	for _, cue := range cues {
		if cue.Cue == cueID {
			return cue
		}
	}
	t.Fatalf("missing cue %q in %+v", cueID, cues)
	return ExpressionCatalogCue{}
}

func expressionCatalogDeviceByID(t *testing.T, devices []ExpressionCatalogDevice, deviceID string) ExpressionCatalogDevice {
	t.Helper()
	for _, device := range devices {
		if device.DeviceID == deviceID {
			return device
		}
	}
	t.Fatalf("missing device %q in %+v", deviceID, devices)
	return ExpressionCatalogDevice{}
}
