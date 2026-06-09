package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/stackchan"
)

func TestAdminStackChanExpressionCueCatalogReturnsSafeReadOnlyCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		ExpressionCues: &stackchan.ExpressionCatalog{
			CueCount: 1,
			Cues: []stackchan.ExpressionCatalogCue{
				{
					Cue:                  stackchan.CueNod,
					Configured:           true,
					HasMotion:            true,
					MotionYawDeg:         4,
					MotionPitchDeg:       5,
					MotionSpeed:          240,
					HasLED:               true,
					LEDR:                 12,
					LEDG:                 34,
					LEDB:                 56,
					HasScene:             true,
					Scene:                stackchan.SceneTool,
					Emotion:              stackchan.EmotionHappy,
					Accent:               stackchan.AccentGreen,
					HasStaticCaption:     true,
					SceneMotionPreset:    stackchan.MotionPresetNodSoft,
					SceneMotionIntensity: 0.5,
				},
			},
			LifecycleCueCount: 1,
			LifecycleCues: []stackchan.ExpressionLifecycleCue{
				{Lifecycle: stackchan.SceneThinking, Cue: stackchan.CueThinking},
			},
			EventCueCount: 1,
			EventCues: []stackchan.ExpressionEventCue{
				{Event: stackchan.DisplayEventAgentRouteOpenClaw, Cue: stackchan.CueNod},
			},
			DeviceCount: 1,
			Devices: []stackchan.ExpressionCatalogDevice{
				{
					DeviceID:                "stackchan-s3-main",
					HeadMCPAvailable:        true,
					LEDMCPAvailable:         true,
					BodyMCPAvailable:        true,
					ScreenSceneMCPAvailable: true,
					Available:               true,
					CueCount:                1,
				},
			},
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-cues", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"cue_count":1`,
		`"cue":"nod"`,
		`"configured":true`,
		`"has_motion":true`,
		`"motion_yaw_deg":4`,
		`"motion_pitch_deg":5`,
		`"motion_speed":240`,
		`"has_led":true`,
		`"led_r":12`,
		`"led_g":34`,
		`"led_b":56`,
		`"has_scene":true`,
		`"scene":"tool"`,
		`"emotion":"happy"`,
		`"accent":"green"`,
		`"has_static_caption":true`,
		`"scene_motion_preset":"nod_soft"`,
		`"scene_motion_intensity":0.5`,
		`"lifecycle_cue_count":1`,
		`"lifecycle":"thinking"`,
		`"event_cue_count":1`,
		`"event":"agent_route.openclaw"`,
		`"device_count":1`,
		`"device_id":"stackchan-s3-main"`,
		`"body_mcp_available":true`,
		`"screen_scene_mcp_available":true`,
		`"available":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "Bearer", "self.robot.set_head_angles", "self.robot.set_led_color", "self.screen.set_scene", "我点头", "raw", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminStackChanExpressionCueCatalogRequiresConfiguredCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-cues", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"STACKCHAN_EXPRESSION_CUE_CATALOG_NOT_CONFIGURED"`)) {
		t.Fatalf("response = %s, want safe expression-cue catalog error", recorder.Body.String())
	}
}
