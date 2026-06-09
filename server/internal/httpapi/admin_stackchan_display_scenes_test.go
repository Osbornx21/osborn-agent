package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/stackchan"
)

func TestAdminStackChanDisplaySceneCatalogReturnsSafeReadOnlyCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		DisplayScenes: &stackchan.DisplaySceneCatalog{
			SceneTTLMS:          1200,
			MaxCaptionChars:     20,
			LifecycleSceneCount: 1,
			LifecycleScenes: []stackchan.DisplaySceneCatalogEntry{
				{
					Lifecycle:        stackchan.SceneThinking,
					Configured:       true,
					Scene:            stackchan.SceneThinking,
					Emotion:          stackchan.EmotionReady,
					Accent:           stackchan.AccentAmber,
					HasStaticCaption: true,
					MotionPreset:     stackchan.MotionPresetAttentive,
					MotionIntensity:  0.4,
				},
			},
			EventSceneCount: 1,
			EventScenes: []stackchan.DisplaySceneCatalogEntry{
				{
					Event:            stackchan.DisplayEventToolRunning,
					Configured:       true,
					Scene:            stackchan.SceneTool,
					Emotion:          stackchan.EmotionWarm,
					Accent:           stackchan.AccentGreen,
					HasStaticCaption: true,
					MotionPreset:     stackchan.MotionPresetNodSoft,
					MotionIntensity:  0.2,
				},
			},
			DeviceCount: 1,
			Devices: []stackchan.DisplaySceneCatalogDevice{
				{
					DeviceID:                "stackchan-s3-main",
					ScreenSceneMCPAvailable: true,
					Available:               true,
					LifecycleSceneCount:     1,
					EventSceneCount:         1,
				},
			},
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-scenes", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"scene_ttl_ms":1200`,
		`"max_caption_chars":20`,
		`"lifecycle_scene_count":1`,
		`"lifecycle":"thinking"`,
		`"configured":true`,
		`"scene":"thinking"`,
		`"emotion":"ready"`,
		`"accent":"amber"`,
		`"has_static_caption":true`,
		`"motion_preset":"attentive"`,
		`"motion_intensity":0.4`,
		`"event_scene_count":1`,
		`"event":"tool.running"`,
		`"device_count":1`,
		`"device_id":"stackchan-s3-main"`,
		`"screen_scene_mcp_available":true`,
		`"available":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "Bearer", "self.screen.set_scene", "我在想", "operator", "raw", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminStackChanDisplaySceneCatalogRequiresConfiguredCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-scenes", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"STACKCHAN_DISPLAY_SCENE_CATALOG_NOT_CONFIGURED"`)) {
		t.Fatalf("response = %s, want safe display-scene catalog error", recorder.Body.String())
	}
}
