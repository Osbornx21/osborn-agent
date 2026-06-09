package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/stackchan"
)

func TestAdminStackChanExpressionSequenceCatalogReturnsSafeReadOnlyCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		ExpressionSequences: &stackchan.ExpressionSequenceCatalog{
			SequenceCount: 1,
			Sequences: []stackchan.ExpressionSequenceCatalogEntry{
				{
					SequenceID: "agree.quick",
					Configured: true,
					CueCount:   2,
				},
			},
			DeviceCount: 1,
			Devices: []stackchan.ExpressionSequenceCatalogDevice{
				{
					DeviceID:                "stackchan-s3-main",
					HeadMCPAvailable:        true,
					LEDMCPAvailable:         true,
					BodyMCPAvailable:        true,
					ScreenSceneMCPAvailable: true,
					Available:               true,
					SequenceCount:           1,
				},
			},
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-sequences", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"sequence_count":1`,
		`"sequence_id":"agree.quick"`,
		`"configured":true`,
		`"cue_count":2`,
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
	for _, forbidden := range []string{"admin-token", "Bearer", `"cues"`, "attentive", "nod", "self.robot.set_head_angles", "self.robot.set_led_color", "self.screen.set_scene", "raw", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminStackChanExpressionSequenceCatalogRequiresConfiguredCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-sequences", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"STACKCHAN_EXPRESSION_SEQUENCE_CATALOG_NOT_CONFIGURED"`)) {
		t.Fatalf("response = %s, want safe expression-sequence catalog error", recorder.Body.String())
	}
}
