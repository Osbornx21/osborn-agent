package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/agents"
)

func TestXiaozhiDeviceModeSelectAndStatusUseDeviceAuth(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	router := NewRouter(RouterOptions{
		DeviceModeHandler: NewXiaozhiDeviceModeHandler(XiaozhiDeviceModeOptions{
			AgentModes: modes,
			Devices: []XiaozhiDeviceModeDevice{
				{DeviceID: "stackchan-s3-main", ClientID: "client-1", AuthToken: "device-token"},
			},
		}),
	})

	selectRequest := httptest.NewRequest(http.MethodPost, "/xiaozhi/device-mode/select", bytes.NewReader([]byte(`{"mode":"professional"}`)))
	selectRequest.Header.Set("Device-Id", "stackchan-s3-main")
	selectRequest.Header.Set("Client-Id", "client-1")
	selectRequest.Header.Set("Authorization", "Bearer device-token")
	selectRecorder := httptest.NewRecorder()
	router.ServeHTTP(selectRecorder, selectRequest)

	if selectRecorder.Code != http.StatusOK {
		t.Fatalf("select status = %d, body = %s", selectRecorder.Code, selectRecorder.Body.String())
	}
	for _, want := range []string{
		`"device_id":"stackchan-s3-main"`,
		`"requested_mode":"professional"`,
		`"active_mode":"professional"`,
		`"available":true`,
		`"reason":"available"`,
	} {
		if !bytes.Contains(selectRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("select response missing %s: %s", want, selectRecorder.Body.String())
		}
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/xiaozhi/device-mode/status", nil)
	statusRequest.Header.Set("Device-Id", "stackchan-s3-main")
	statusRequest.Header.Set("Client-Id", "client-1")
	statusRequest.Header.Set("Authorization", "device-token")
	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(statusRecorder, statusRequest)

	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status status = %d, body = %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	for _, want := range []string{
		`"requested_mode":"professional"`,
		`"active_mode":"professional"`,
		`"override":true`,
	} {
		if !bytes.Contains(statusRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("status response missing %s: %s", want, statusRecorder.Body.String())
		}
	}
}

func TestXiaozhiDeviceModeReportsRuntimeAvailability(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	runtimeStatus := agents.NewRuntimeStatusStore(modes, agents.NewBridgeCatalogStore([]agents.BridgeStatus{
		{
			Bridge:       agents.BridgeHermes,
			Enabled:      false,
			RequiredMode: agents.ModeRoleplay,
			Invocation:   agents.BridgeInvocationRuntimeRoute,
			RuntimeRoute: true,
		},
		{
			Bridge:       agents.BridgeOpenClaw,
			Enabled:      true,
			RequiredMode: agents.ModeTool,
			Invocation:   agents.BridgeInvocationRuntimeRoute,
			RuntimeRoute: true,
		},
		{
			Bridge:       agents.BridgeV21,
			Enabled:      true,
			RequiredMode: agents.ModeProfessional,
			Invocation:   agents.BridgeInvocationServiceTool,
			ServiceTool:  agents.V21VoiceQueryToolName,
		},
	}))
	router := NewRouter(RouterOptions{
		DeviceModeHandler: NewXiaozhiDeviceModeHandler(XiaozhiDeviceModeOptions{
			AgentModes:    modes,
			RuntimeStatus: runtimeStatus,
			Devices: []XiaozhiDeviceModeDevice{
				{DeviceID: "stackchan-s3-main", ClientID: "client-1", AuthToken: "device-token"},
			},
		}),
	})

	request := httptest.NewRequest(http.MethodPost, "/xiaozhi/device-mode/select", bytes.NewReader([]byte(`{"mode":"roleplay"}`)))
	request.Header.Set("Device-Id", "stackchan-s3-main")
	request.Header.Set("Client-Id", "client-1")
	request.Header.Set("Authorization", "Bearer device-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"requested_mode":"roleplay"`,
		`"active_mode":"roleplay"`,
		`"available":false`,
		`"reason":"bridge_disabled"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	status, err := modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != agents.ModeRoleplay || !status.Override {
		t.Fatalf("mode status = %+v, want selected but unavailable roleplay", status)
	}
}

func TestXiaozhiDeviceModeSelectDefaultClearsOverride(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	router := NewRouter(RouterOptions{
		DeviceModeHandler: NewXiaozhiDeviceModeHandler(XiaozhiDeviceModeOptions{
			AgentModes: modes,
			Devices: []XiaozhiDeviceModeDevice{
				{DeviceID: "stackchan-s3-main", ClientID: "client-1", AuthToken: "device-token"},
			},
		}),
	})

	selectProfessional := httptest.NewRequest(http.MethodPost, "/xiaozhi/device-mode/select", bytes.NewReader([]byte(`{"mode":"professional"}`)))
	selectProfessional.Header.Set("Device-Id", "stackchan-s3-main")
	selectProfessional.Header.Set("Client-Id", "client-1")
	selectProfessional.Header.Set("Authorization", "Bearer device-token")
	professionalRecorder := httptest.NewRecorder()
	router.ServeHTTP(professionalRecorder, selectProfessional)
	if professionalRecorder.Code != http.StatusOK {
		t.Fatalf("professional status = %d, body = %s", professionalRecorder.Code, professionalRecorder.Body.String())
	}

	selectDefault := httptest.NewRequest(http.MethodPost, "/xiaozhi/device-mode/select", bytes.NewReader([]byte(`{"mode":"casual"}`)))
	selectDefault.Header.Set("Device-Id", "stackchan-s3-main")
	selectDefault.Header.Set("Client-Id", "client-1")
	selectDefault.Header.Set("Authorization", "Bearer device-token")
	defaultRecorder := httptest.NewRecorder()
	router.ServeHTTP(defaultRecorder, selectDefault)

	if defaultRecorder.Code != http.StatusOK {
		t.Fatalf("default status = %d, body = %s", defaultRecorder.Code, defaultRecorder.Body.String())
	}
	for _, want := range []string{
		`"requested_mode":"casual"`,
		`"active_mode":"casual"`,
		`"override":false`,
	} {
		if !bytes.Contains(defaultRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("default response missing %s: %s", want, defaultRecorder.Body.String())
		}
	}
	status, err := modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != agents.ModeCasual || status.Override {
		t.Fatalf("mode status = %+v, want default without override", status)
	}
}

func TestXiaozhiDeviceModeRejectsAdminTokenAndDoesNotChangeMode(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	router := NewRouter(RouterOptions{
		DeviceModeHandler: NewXiaozhiDeviceModeHandler(XiaozhiDeviceModeOptions{
			AgentModes: modes,
			Devices: []XiaozhiDeviceModeDevice{
				{DeviceID: "stackchan-s3-main", ClientID: "client-1", AuthToken: "device-token"},
			},
		}),
	})

	request := httptest.NewRequest(http.MethodPost, "/xiaozhi/device-mode/select", bytes.NewReader([]byte(`{"mode":"tool"}`)))
	request.Header.Set("Device-Id", "stackchan-s3-main")
	request.Header.Set("Client-Id", "client-1")
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, forbidden := range []string{"admin-token", "device-token", "tool"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	status, err := modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != agents.ModeCasual || status.Override {
		t.Fatalf("mode status = %+v, want unchanged casual default", status)
	}
}

func TestXiaozhiDeviceModeRejectsUnknownFieldsAndInvalidMode(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	router := NewRouter(RouterOptions{
		DeviceModeHandler: NewXiaozhiDeviceModeHandler(XiaozhiDeviceModeOptions{
			AgentModes: modes,
			Devices: []XiaozhiDeviceModeDevice{
				{DeviceID: "stackchan-s3-main", ClientID: "client-1", AuthToken: "device-token"},
			},
		}),
	})

	for _, body := range []string{
		`{"mode":"root"}`,
		`{"mode":"professional","device_id":"other-device"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/xiaozhi/device-mode/select", bytes.NewReader([]byte(body)))
		request.Header.Set("Device-Id", "stackchan-s3-main")
		request.Header.Set("Client-Id", "client-1")
		request.Header.Set("Authorization", "Bearer device-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, response = %s", body, recorder.Code, recorder.Body.String())
		}
		for _, forbidden := range []string{"root", "other-device", "device-token"} {
			if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
				t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
			}
		}
	}
}
