package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/agents"
)

func TestAdminAgentModeGetPutAndClear(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		AgentModes: modes,
	})

	put := httptest.NewRequest(http.MethodPut, "/internal/v1/devices/stackchan-s3-main/agent-mode", bytes.NewReader([]byte(`{"mode":"professional"}`)))
	put.Header.Set("Authorization", "Bearer admin-token")
	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, put)

	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", putRecorder.Code, putRecorder.Body.String())
	}
	for _, want := range []string{`"active_mode":"professional"`, `"override":true`} {
		if !bytes.Contains(putRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("put response missing %s: %s", want, putRecorder.Body.String())
		}
	}

	get := httptest.NewRequest(http.MethodGet, "/internal/v1/devices/stackchan-s3-main/agent-mode", nil)
	get.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, get)

	if getRecorder.Code != http.StatusOK || !bytes.Contains(getRecorder.Body.Bytes(), []byte(`"active_mode":"professional"`)) {
		t.Fatalf("get status/body = %d %s, want professional", getRecorder.Code, getRecorder.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/internal/v1/devices/stackchan-s3-main/agent-mode", nil)
	del.Header.Set("Authorization", "Bearer admin-token")
	delRecorder := httptest.NewRecorder()
	router.ServeHTTP(delRecorder, del)

	if delRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", delRecorder.Code, delRecorder.Body.String())
	}
	for _, want := range []string{`"active_mode":"casual"`, `"override":false`} {
		if !bytes.Contains(delRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("delete response missing %s: %s", want, delRecorder.Body.String())
		}
	}

	status, err := modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != agents.ModeCasual || status.Override {
		t.Fatalf("status after delete = %+v, want casual default", status)
	}
}

func TestAdminAgentModeRejectsInvalidModeAndDoesNotLeakToken(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		AgentModes: agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"}),
	})

	request := httptest.NewRequest(http.MethodPut, "/internal/v1/devices/stackchan-s3-main/agent-mode", bytes.NewReader([]byte(`{"mode":"root","secret":"admin-token"}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("root")) {
		t.Fatalf("response leaked request body or token: %s", recorder.Body.String())
	}
}

func TestAdminAgentModeMapsUnknownDeviceTo404(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		AgentModes: agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/devices/missing-device/agent-mode", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"DEVICE_NOT_FOUND"`)) {
		t.Fatalf("response = %s, want DEVICE_NOT_FOUND", recorder.Body.String())
	}
}

func TestAdminAgentModeCatalogReturnsSafeModesAndDevices(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeProfessional); err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		AgentModes: modes,
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-modes", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"default_mode":"casual"`,
		`"available_modes":["casual","roleplay","professional","tool"]`,
		`"device_id":"stackchan-s3-main"`,
		`"active_mode":"professional"`,
		`"override":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "persona", "prompt", "OPENCLAW", "HERMES", "V21_ADAPTER_TOKEN"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}
