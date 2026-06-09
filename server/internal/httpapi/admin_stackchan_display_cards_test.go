package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/stackchan"
)

func TestAdminStackChanDisplayCardCatalogReturnsSafeReadOnlyCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		DisplayCards: &stackchan.DisplayCardCatalog{
			CardCount: 1,
			Cards: []stackchan.DisplayCardCatalogCard{
				{
					CardID:           "status.note",
					Scene:            stackchan.SceneTool,
					Emotion:          stackchan.EmotionWarm,
					Accent:           stackchan.AccentGreen,
					AllowCaption:     true,
					MaxCaptionChars:  28,
					HasStaticCaption: true,
					MotionPreset:     stackchan.MotionPresetNodSoft,
					MotionIntensity:  0.2,
				},
			},
			DeviceCount: 1,
			Devices: []stackchan.DisplayCardCatalogDevice{
				{
					DeviceID:                "stackchan-s3-main",
					ScreenSceneMCPAvailable: true,
					Available:               true,
					CardCount:               1,
				},
			},
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-cards", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"card_count":1`,
		`"card_id":"status.note"`,
		`"scene":"tool"`,
		`"emotion":"warm"`,
		`"accent":"green"`,
		`"allow_caption":true`,
		`"max_caption_chars":28`,
		`"has_static_caption":true`,
		`"motion_preset":"nod_soft"`,
		`"motion_intensity":0.2`,
		`"device_count":1`,
		`"device_id":"stackchan-s3-main"`,
		`"screen_scene_mcp_available":true`,
		`"available":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "Bearer", "self.screen.set_scene", "我有一条状态", "raw", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminStackChanDisplayCardCatalogRequiresConfiguredCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-cards", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"STACKCHAN_DISPLAY_CARD_CATALOG_NOT_CONFIGURED"`)) {
		t.Fatalf("response = %s, want safe display-card catalog error", recorder.Body.String())
	}
}
