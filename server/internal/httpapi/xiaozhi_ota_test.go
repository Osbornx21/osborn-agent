package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestXiaozhiOTAHandlerReturnsWebSocketConfig(t *testing.T) {
	handler := NewXiaozhiOTAHandler(XiaozhiOTAOptions{
		WebSocketURL:          "wss://stackchan.example.internal/xiaozhi/v1/ws",
		WebSocketVersion:      1,
		TimezoneOffsetMinutes: 480,
		Now:                   func() time.Time { return time.UnixMilli(1780000000123).UTC() },
		Devices: []XiaozhiOTADevice{
			{
				DeviceID:       "stackchan-s3-main",
				ClientID:       "stackchan-s3-main-client",
				WebSocketToken: "device-secret-token",
			},
		},
	})

	request := httptest.NewRequest(http.MethodPost, "/xiaozhi/ota/", nil)
	request.Header.Set("Device-Id", "stackchan-s3-main")
	request.Header.Set("Client-Id", "stackchan-s3-main-client")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	websocket, ok := response["websocket"].(map[string]any)
	if !ok {
		t.Fatalf("websocket response = %#v, want object", response["websocket"])
	}
	if websocket["url"] != "wss://stackchan.example.internal/xiaozhi/v1/ws" {
		t.Fatalf("websocket.url = %#v", websocket["url"])
	}
	if websocket["token"] != "device-secret-token" {
		t.Fatalf("websocket.token = %#v", websocket["token"])
	}
	if websocket["version"] != float64(1) {
		t.Fatalf("websocket.version = %#v", websocket["version"])
	}
	serverTime, ok := response["server_time"].(map[string]any)
	if !ok {
		t.Fatalf("server_time response = %#v, want object", response["server_time"])
	}
	if serverTime["timestamp"] != float64(1780000000123) {
		t.Fatalf("server_time.timestamp = %#v", serverTime["timestamp"])
	}
	if serverTime["timezone_offset"] != float64(480) {
		t.Fatalf("server_time.timezone_offset = %#v", serverTime["timezone_offset"])
	}
	if _, ok := response["mqtt"]; ok {
		t.Fatalf("response included mqtt config: %s", recorder.Body.String())
	}
	if _, ok := response["activation"]; ok {
		t.Fatalf("response included activation config: %s", recorder.Body.String())
	}
}

func TestXiaozhiOTAHandlerRejectsMissingWebSocketConfigSafely(t *testing.T) {
	handler := NewXiaozhiOTAHandler(XiaozhiOTAOptions{
		WebSocketURL:     "wss://stackchan.example.internal/xiaozhi/v1/ws",
		WebSocketVersion: 1,
		Devices: []XiaozhiOTADevice{
			{
				DeviceID: "stackchan-s3-main",
				ClientID: "stackchan-s3-main-client",
			},
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/xiaozhi/ota/", nil)
	request.Header.Set("Device-Id", "stackchan-s3-main")
	request.Header.Set("Client-Id", "stackchan-s3-main-client")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, forbidden := range []string{"STACKCHAN_MAIN_AUTH_TOKEN", "device-secret-token", "wss://stackchan.example.internal"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("error response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestXiaozhiOTAHandlerRejectsUnknownDeviceSafely(t *testing.T) {
	handler := NewXiaozhiOTAHandler(XiaozhiOTAOptions{
		WebSocketURL:     "wss://stackchan.example.internal/xiaozhi/v1/ws",
		WebSocketVersion: 1,
		Devices: []XiaozhiOTADevice{
			{
				DeviceID:       "stackchan-s3-main",
				ClientID:       "stackchan-s3-main-client",
				WebSocketToken: "device-secret-token",
			},
		},
	})

	request := httptest.NewRequest(http.MethodPost, "/xiaozhi/ota/", nil)
	request.Header.Set("Device-Id", "unpaired-device")
	request.Header.Set("Client-Id", "unpaired-client")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, forbidden := range []string{"device-secret-token", "wss://stackchan.example.internal", "STACKCHAN_MAIN_AUTH_TOKEN"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("error response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}
