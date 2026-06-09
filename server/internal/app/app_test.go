package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/agent"
	"stackchan-gateway/internal/agents"
	"stackchan-gateway/internal/camera"
	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/feishu"
	"stackchan-gateway/internal/homeassistant"
	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/observability"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providerrouter"
	"stackchan-gateway/internal/reminder"
	"stackchan-gateway/internal/search"
	"stackchan-gateway/internal/session"
	"stackchan-gateway/internal/simulator"
	"stackchan-gateway/internal/stackchan"
	servicetools "stackchan-gateway/internal/tools"
)

func TestNewConfiguresPrivateMetricsServer(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if app.metricsServer == nil {
		t.Fatal("metricsServer is nil, want private metrics server")
	}
	if app.metricsServer.Addr != "127.0.0.1:9090" {
		t.Fatalf("metrics addr = %q, want 127.0.0.1:9090", app.metricsServer.Addr)
	}
	if app.metricsServer.Handler == nil {
		t.Fatal("metrics handler is nil")
	}
}

func TestNewConfiguresPrivateAdminServer(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if app.adminServer == nil {
		t.Fatal("adminServer is nil, want private admin server")
	}
	if app.adminServer.Addr != "127.0.0.1:8081" {
		t.Fatalf("admin addr = %q, want 127.0.0.1:8081", app.adminServer.Addr)
	}
	if app.adminServer.Handler == nil {
		t.Fatal("admin handler is nil")
	}
}

func TestNewWiresPublicXiaozhiOTAConfig(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("STACKCHAN_WEBSOCKET_URL", "wss://ecs.example.internal/xiaozhi/v1/ws")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/xiaozhi/ota/", nil)
	request.Header.Set("Device-Id", "stackchan-s3-main")
	request.Header.Set("Client-Id", "stackchan-s3-main-client")
	recorder := httptest.NewRecorder()
	app.server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"websocket"`,
		`"url":"wss://ecs.example.internal/xiaozhi/v1/ws"`,
		`"token":"test-token"`,
		`"version":1`,
		`"server_time"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("OTA response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_MAIN_AUTH_TOKEN", "STACKCHAN_ADMIN_TOKEN", "admin-token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("OTA response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresPublicDeviceModeSelectWithDeviceAuth(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	selectRequest := httptest.NewRequest(http.MethodPost, "/xiaozhi/device-mode/select", strings.NewReader(`{"mode":"tool"}`))
	selectRequest.Header.Set("Device-Id", "stackchan-s3-main")
	selectRequest.Header.Set("Client-Id", "stackchan-s3-main-client")
	selectRequest.Header.Set("Authorization", "Bearer test-token")
	selectRecorder := httptest.NewRecorder()
	app.server.Handler.ServeHTTP(selectRecorder, selectRequest)

	if selectRecorder.Code != http.StatusOK {
		t.Fatalf("select status = %d, body = %s", selectRecorder.Code, selectRecorder.Body.String())
	}
	for _, want := range []string{
		`"device_id":"stackchan-s3-main"`,
		`"requested_mode":"tool"`,
		`"active_mode":"tool"`,
		`"available":false`,
		`"reason":"bridge_disabled"`,
	} {
		if !bytes.Contains(selectRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("select response missing %s: %s", want, selectRecorder.Body.String())
		}
	}

	status, err := app.agentModes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != agents.ModeTool || !status.Override {
		t.Fatalf("agent mode status = %+v, want tool override", status)
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "STACKCHAN_MAIN_AUTH_TOKEN", "test-token"} {
		if bytes.Contains(selectRecorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("select response leaked %q: %s", forbidden, selectRecorder.Body.String())
		}
	}
}

func TestAdminProviderProfileControlUsesRuntimeRouter(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	putRequest := httptest.NewRequest(http.MethodPut, "/internal/v1/devices/stackchan-s3-main/provider-profile", strings.NewReader(`{"profile":"cn-low-latency-cascade"}`))
	putRequest.Header.Set("Authorization", "Bearer admin-token")
	putRecorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(putRecorder, putRequest)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", putRecorder.Code, putRecorder.Body.String())
	}
	if !bytes.Contains(putRecorder.Body.Bytes(), []byte(`"active_profile":"cn-low-latency-cascade"`)) {
		t.Fatalf("PUT response = %s", putRecorder.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/devices/stackchan-s3-main/provider-profile", nil)
	getRequest.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}
	if !bytes.Contains(getRecorder.Body.Bytes(), []byte(`"override":true`)) {
		t.Fatalf("GET response = %s", getRecorder.Body.String())
	}
}

func TestAdminProviderProfileCatalogUsesRuntimeRouter(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/provider-profiles", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"default_profile":"siliconflow-dashscope-voice"`,
		`"profile":"siliconflow-dashscope-voice"`,
		`"profile":"cn-low-latency-cascade"`,
		`"device_id":"stackchan-s3-main"`,
		`"active_profile":"siliconflow-dashscope-voice"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"DASHSCOPE_API_KEY", "SILICONFLOW_API_KEY", "STACKCHAN_MAIN_AUTH_TOKEN", "admin-token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresAdminServiceToolCatalog(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/service-tools", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"count":1`,
		`"name":"memory.lookup"`,
		`"permission":"read"`,
		`"allowed":true`,
		`"permission_granted":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "DASHSCOPE_API_KEY", "SILICONFLOW_API_KEY", "metadata_json"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresAdminAgentBridgeCatalog(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-bridges", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"count":3`,
		`"bridge":"hermes"`,
		`"enabled":false`,
		`"required_mode":"roleplay"`,
		`"invocation":"runtime_route"`,
		`"fallback_on_error":true`,
		`"fallback_on_empty_response":true`,
		`"bridge":"openclaw"`,
		`"required_mode":"tool"`,
		`"bridge":"v21"`,
		`"required_mode":"professional"`,
		`"invocation":"service_tool"`,
		`"service_tool":"v21.voice_query"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "V21_ADAPTER", "HERMES_AGENT", "OPENCLAW_WS", "http://", "https://", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresAdminAgentRuntimeStatus(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-runtime-status", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"count":1`,
		`"bridge_count":3`,
		`"device_id":"stackchan-s3-main"`,
		`"active_mode":"casual"`,
		`"bridge":"hermes"`,
		`"reason":"bridge_disabled"`,
		`"bridge":"openclaw"`,
		`"required_mode":"tool"`,
		`"bridge":"v21"`,
		`"service_tool":"v21.voice_query"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("runtime response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "V21_ADAPTER", "HERMES_AGENT", "OPENCLAW_WS", "http://", "https://", "secret", "token", "prompt", "persona"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("runtime response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresAdminStackChanDisplayCardCatalog(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-cards", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
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
		`"device_count":1`,
		`"device_id":"stackchan-s3-main"`,
		`"screen_scene_mcp_available":false`,
		`"available":false`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "我有一条状态", "self.screen.set_scene", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresAdminStackChanExpressionCueCatalog(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-cues", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"cue_count":5`,
		`"cue":"nod"`,
		`"configured":true`,
		`"motion_pitch_deg":16`,
		`"motion_speed":220`,
		`"led_g":168`,
		`"has_scene":true`,
		`"scene":"speaking"`,
		`"emotion":"ready"`,
		`"accent":"green"`,
		`"has_static_caption":true`,
		`"device_count":1`,
		`"device_id":"stackchan-s3-main"`,
		`"body_mcp_available":true`,
		`"screen_scene_mcp_available":false`,
		`"available":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "我点头", "self.robot.set_head_angles", "self.robot.set_led_color", "self.screen.set_scene", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresAdminStackChanExpressionSequenceCatalog(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-sequences", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"sequence_count":1`,
		`"sequence_id":"agree.quick"`,
		`"configured":true`,
		`"cue_count":2`,
		`"device_count":1`,
		`"device_id":"stackchan-s3-main"`,
		`"body_mcp_available":true`,
		`"screen_scene_mcp_available":false`,
		`"available":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", `"cues"`, "attentive", "nod", "self.robot.set_head_angles", "self.robot.set_led_color", "self.screen.set_scene", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestNewWiresAdminStackChanDisplaySceneCatalog(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-scenes", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"scene_ttl_ms":1800`,
		`"max_caption_chars":48`,
		`"lifecycle_scene_count":4`,
		`"lifecycle":"thinking"`,
		`"scene":"thinking"`,
		`"emotion":"curious"`,
		`"accent":"amber"`,
		`"event":"tool.running"`,
		`"event":"agent_route.v21"`,
		`"device_count":1`,
		`"device_id":"stackchan-s3-main"`,
		`"screen_scene_mcp_available":false`,
		`"available":false`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "我在想", "我在调用工具", "我去查知识库", "self.screen.set_scene", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminMemoryRoutesUseConfiguredRepository(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()

	putRequest := httptest.NewRequest(http.MethodPut, "/internal/v1/memories/app-test-memory", strings.NewReader(`{
		"user_id":"owner",
		"device_id":"stackchan-s3-main",
		"type":"user_profile",
		"content":"用户偏好的称呼是阿豪。",
		"importance":5,
		"confidence":0.95
	}`))
	putRequest.Header.Set("Authorization", "Bearer admin-token")
	putRecorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(putRecorder, putRequest)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", putRecorder.Code, putRecorder.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/memories?device_id=stackchan-s3-main&user_id=owner&limit=10", nil)
	getRequest.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}
	if !bytes.Contains(getRecorder.Body.Bytes(), []byte(`"id":"app-test-memory"`)) {
		t.Fatalf("GET response = %s, want app-test-memory", getRecorder.Body.String())
	}

	compactRequest := httptest.NewRequest(http.MethodPost, "/internal/v1/memories/compact", strings.NewReader(`{"user_id":"owner","device_id":"stackchan-s3-main","max_facts":1}`))
	compactRequest.Header.Set("Authorization", "Bearer admin-token")
	compactRecorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(compactRecorder, compactRequest)
	if compactRecorder.Code != http.StatusOK {
		t.Fatalf("compact status = %d, body = %s", compactRecorder.Code, compactRecorder.Body.String())
	}
	if !bytes.Contains(compactRecorder.Body.Bytes(), []byte(`"upserted":true`)) {
		t.Fatalf("compact response = %s, want upserted summary", compactRecorder.Body.String())
	}
}

func TestReadyzChecksRuntimeDefaultProviderProfile(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("DASHSCOPE_API_KEY", "test-dashscope-key")
	t.Setenv("SILICONFLOW_API_KEY", "test-siliconflow-key")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()

	app.server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeReadyResponse(t, recorder.Body.Bytes())
	if !response.Ready {
		t.Fatalf("ready = false, body = %s", recorder.Body.String())
	}
	if response.Checks["config"] != "ok" {
		t.Fatalf("config check = %q, want ok", response.Checks["config"])
	}
	if response.Checks["providers"] != "ok" {
		t.Fatalf("providers check = %q, want ok", response.Checks["providers"])
	}
}

func TestAgentPromptBuilderUsesConfiguredSQLiteMemoryStore(t *testing.T) {
	dir := t.TempDir()
	personaPath := filepath.Join(dir, "persona.yaml")
	if err := os.WriteFile(personaPath, []byte(`
name: "Stack-chan"
identity: "桌面机器人"
core_rules:
  - "回答要短。"
`), 0o600); err != nil {
		t.Fatalf("write persona: %v", err)
	}
	memoryDBPath := filepath.Join(dir, "memory.sqlite3")
	agentConfig := gatewayconfig.AgentConfig{
		PersonaPath:    personaPath,
		MemoryDBPath:   memoryDBPath,
		MemoryMaxItems: 3,
		RecentTurns:    2,
	}
	builder, memoryWriter, conversationRecorder, repository, recentTurnReader, err := newAgentPromptBuilder(&gatewayconfig.Config{
		Agent: agentConfig,
	})
	if err != nil {
		t.Fatalf("newAgentPromptBuilder() error = %v", err)
	}
	if repository == nil {
		t.Fatal("repository is nil, want configured sqlite memory repository")
	}
	if memoryWriter == nil {
		t.Fatal("memoryWriter is nil, want configured transcript memory writer")
	}
	if conversationRecorder == nil {
		t.Fatal("conversationRecorder is nil, want configured recent-turn recorder")
	}
	if recentTurnReader == nil {
		t.Fatal("recentTurnReader is nil, want configured recent-turn reader")
	}

	if _, err := repository.Upsert(context.Background(), agent.Memory{
		ID:         "user-name",
		UserID:     "owner",
		DeviceID:   "stackchan-s3-main",
		Type:       agent.MemoryUserProfile,
		Content:    "用户偏好的称呼是阿豪。",
		Importance: 5,
		Confidence: 1,
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	contextOut, err := builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_memory",
		DeviceID:   "stackchan-s3-main",
		Transcript: "你记得怎么称呼我吗？",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext() error = %v", err)
	}
	if contextOut.MemoryCount != 1 || !strings.Contains(contextOut.Text, "用户偏好的称呼是阿豪。") {
		t.Fatalf("context missing sqlite memory: %+v\n%s", contextOut, contextOut.Text)
	}

	if err := conversationRecorder.RecordConversationTurn(context.Background(), session.ConversationTurnRecordRequest{
		SessionID:     "sess_memory",
		DeviceID:      "stackchan-s3-main",
		Generation:    1,
		UserText:      "刚才我们在验收什么？",
		AssistantText: "在验收 StackChan 语音链路。",
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("RecordConversationTurn() error = %v", err)
	}
	contextOut, err = builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_memory",
		DeviceID:   "stackchan-s3-main",
		Transcript: "继续。",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext(recent) error = %v", err)
	}
	if contextOut.RecentTurnCount != 1 || !strings.Contains(contextOut.Text, "在验收 StackChan 语音链路。") {
		t.Fatalf("context missing recent turn: %+v\n%s", contextOut, contextOut.Text)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("Close(repository) error = %v", err)
	}

	reopenedBuilder, _, _, reopenedRepository, reopenedRecentTurnReader, err := newAgentPromptBuilder(&gatewayconfig.Config{Agent: agentConfig})
	if err != nil {
		t.Fatalf("newAgentPromptBuilder(reopen) error = %v", err)
	}
	defer reopenedRepository.Close()
	if reopenedRecentTurnReader == nil {
		t.Fatal("reopenedRecentTurnReader is nil")
	}
	contextOut, err = reopenedBuilder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_memory_2",
		DeviceID:   "stackchan-s3-main",
		Transcript: "那刚才那条继续。",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext(reopen recent) error = %v", err)
	}
	if contextOut.RecentTurnCount != 1 || !strings.Contains(contextOut.Text, "在验收 StackChan 语音链路。") {
		t.Fatalf("reopened context missing persisted recent turn: %+v\n%s", contextOut, contextOut.Text)
	}
}

func TestAppRegistersMemoryLookupServiceTool(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	dir := t.TempDir()
	personaPath := filepath.Join(dir, "persona.yaml")
	if err := os.WriteFile(personaPath, []byte(`
name: "Stack-chan"
identity: "桌面机器人"
core_rules:
  - "回答要短。"
`), 0o600); err != nil {
		t.Fatalf("write persona: %v", err)
	}
	configPath := writeGatewayConfig(t, `
server:
  public_base_url: "https://stackchan.example.internal"
  listen_addr: "127.0.0.1:8080"
  websocket_path: "/xiaozhi/v1/ws"
  admin_addr: "127.0.0.1:8081"
  admin_token_env: "STACKCHAN_ADMIN_TOKEN"
  metrics_addr: "127.0.0.1:9090"
  shutdown_timeout_ms: 5000

devices:
  - device_id: "stackchan-s3-main"
    client_id: "stackchan-s3-main-client"
    auth_token_env: "STACKCHAN_MAIN_AUTH_TOKEN"
    default_mode: "auto"

audio:
  uplink_sample_rate_hz: 16000
  downlink_sample_rate_hz: 24000
  channels: 1
  frame_duration_ms: 60
  downlink_queue_ms: 1200
  max_turn_ms: 30000

providers:
  default_profile: "mock-runtime"
  profiles:
    mock-runtime:
      asr: "mock"
      llm: "mock"
      tts: "mock"

agent:
  persona_path: `+strconv.Quote(personaPath)+`
  memory_db_path: `+strconv.Quote(filepath.Join(dir, "memory.sqlite3"))+`
  memory_max_items: 1
  recent_turns: 2

stackchan:
  body:
    min_command_gap_ms: 160
    max_commands_per_turn: 16
    yaw_min_deg: -45
    yaw_max_deg: 45
    pitch_min_deg: 0
    pitch_max_deg: 45
    default_speed: 150
  display:
    scene_ttl_ms: 1800
    max_caption_chars: 48

observability:
  trace_jsonl_path: `+strconv.Quote(filepath.Join(dir, "turns.jsonl"))+`
  redact_secrets: true
`)

	app, err := New(Options{
		ConfigPath: configPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.agentMemory.Close()
	if app.serviceTools == nil || !app.serviceTools.HasTool(agent.MemoryLookupToolName) {
		t.Fatalf("serviceTools missing %s", agent.MemoryLookupToolName)
	}
	if _, err := app.agentMemory.Upsert(context.Background(), agent.Memory{
		ID:           "runtime-memory",
		UserID:       "owner",
		DeviceID:     "stackchan-s3-main",
		Type:         agent.MemoryUserProfile,
		Content:      "用户偏好的称呼是阿豪。",
		Importance:   5,
		Confidence:   1,
		MetadataJSON: `{"secret":"must-not-leak"}`,
	}); err != nil {
		t.Fatalf("Upsert(runtime-memory) error = %v", err)
	}
	if _, err := app.agentMemory.Upsert(context.Background(), agent.Memory{
		ID:         "other-device-memory",
		UserID:     "owner",
		DeviceID:   "other-device",
		Type:       agent.MemoryUserProfile,
		Content:    "不应由当前设备读到。",
		Importance: 5,
		Confidence: 1,
	}); err != nil {
		t.Fatalf("Upsert(other-device-memory) error = %v", err)
	}

	result, err := app.serviceTools.ExecuteTool(context.Background(), servicetools.Call{
		SessionID: "sess_app_memory_tool",
		DeviceID:  "stackchan-s3-main",
		Name:      agent.MemoryLookupToolName,
		Arguments: map[string]any{
			"device_id": "other-device",
		},
	})

	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	var payload struct {
		Memories []struct {
			Content string `json:"content"`
		} `json:"memories"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode memory lookup payload: %v", err)
	}
	if payload.Count != 1 || len(payload.Memories) != 1 || payload.Memories[0].Content != "用户偏好的称呼是阿豪。" {
		t.Fatalf("payload = %+v, want one current-device memory", payload)
	}
	for _, forbidden := range []string{"must-not-leak", "不应由当前设备读到"} {
		if bytes.Contains(result.Payload, []byte(forbidden)) {
			t.Fatalf("memory lookup leaked %q: %s", forbidden, string(result.Payload))
		}
	}

	conversationRecorder, ok := app.agentMemory.(interface {
		RecordConversationTurn(context.Context, session.ConversationTurnRecordRequest) error
	})
	if !ok {
		t.Fatal("agentMemory does not expose recent-turn recorder")
	}
	if err := conversationRecorder.RecordConversationTurn(context.Background(), session.ConversationTurnRecordRequest{
		SessionID:     "sess_app_recent",
		DeviceID:      "stackchan-s3-main",
		Generation:    11,
		UserText:      "刚才我们在验证什么？",
		AssistantText: "在验证连续对话记录入口。",
		CreatedAt:     time.Date(2026, 6, 8, 23, 55, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordConversationTurn() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/recent-turns?device_id=stackchan-s3-main&limit=2", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("recent turns status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"count":1`,
		`"user_text":"刚才我们在验证什么？"`,
		`"assistant_text":"在验证连续对话记录入口。"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("recent turns response missing %s: %s", want, recorder.Body.String())
		}
	}
}

func TestAgentServiceToolsRegistersHomeAssistantStateToolWhenEnabled(t *testing.T) {
	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/states/light.desk" {
			t.Fatalf("path = %s, want /api/states/light.desk", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ha-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entity_id":"light.desk","state":"on","attributes":{"friendly_name":"Desk Light"},"last_changed":"2026-06-06T10:00:00+00:00"}`))
	}))
	defer haServer.Close()
	t.Setenv("HOME_ASSISTANT_TOKEN", "ha-secret")

	registry, err := newAgentServiceTools(&gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{MemoryMaxItems: 5},
		Tools: gatewayconfig.ToolsConfig{
			HomeAssistant: gatewayconfig.HomeAssistantConfig{
				Enabled:         true,
				BaseURL:         haServer.URL,
				TokenEnv:        "HOME_ASSISTANT_TOKEN",
				AllowedEntities: []string{"light.desk"},
				TimeoutMS:       1200,
			},
		},
	}, agent.NewStaticMemoryStore(nil), nil)
	if err != nil {
		t.Fatalf("newAgentServiceTools() error = %v", err)
	}

	definitions := registry.Definitions()
	if !hasServiceToolDefinition(definitions, agent.MemoryLookupToolName) {
		t.Fatalf("definitions = %+v, missing memory lookup", definitions)
	}
	haDefinition, ok := serviceToolDefinition(definitions, homeassistant.GetStateToolName)
	if !ok {
		t.Fatalf("definitions = %+v, missing Home Assistant get_state", definitions)
	}
	if haDefinition.Permission != servicetools.PermissionExternal {
		t.Fatalf("HA permission = %q, want external", haDefinition.Permission)
	}
	properties, ok := haDefinition.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("HA schema properties = %#v", haDefinition.InputSchema["properties"])
	}
	entityID, ok := properties["entity_id"].(map[string]any)
	if !ok {
		t.Fatalf("HA entity schema = %#v", properties["entity_id"])
	}
	enum, ok := entityID["enum"].([]any)
	if !ok || len(enum) != 1 || enum[0] != "light.desk" {
		t.Fatalf("HA entity enum = %#v, want light.desk", entityID["enum"])
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      homeassistant.GetStateToolName,
		Arguments: map[string]any{"entity_id": "light.desk"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"state":"on"`)) || !bytes.Contains(result.Payload, []byte(`"friendly_name":"Desk Light"`)) {
		t.Fatalf("payload = %s, want safe Home Assistant state", string(result.Payload))
	}
}

func TestAgentServiceToolsRegistersHomeAssistantActionToolWhenConfigured(t *testing.T) {
	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services/light/turn_on" {
			t.Fatalf("path = %s, want /api/services/light/turn_on", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ha-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		entityIDs, ok := body["entity_id"].([]any)
		if !ok || len(entityIDs) != 1 || entityIDs[0] != "light.desk" {
			t.Fatalf("entity_id = %#v, want configured light.desk", body["entity_id"])
		}
		if body["brightness_pct"] != float64(35) {
			t.Fatalf("brightness_pct = %#v, want dynamic slot 35", body["brightness_pct"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"changed_states":[]}`))
	}))
	defer haServer.Close()
	t.Setenv("HOME_ASSISTANT_TOKEN", "ha-secret")
	minBrightness := float64(1)
	maxBrightness := float64(100)

	registry, err := newAgentServiceTools(&gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{MemoryMaxItems: 5},
		Tools: gatewayconfig.ToolsConfig{
			HomeAssistant: gatewayconfig.HomeAssistantConfig{
				Enabled:         true,
				BaseURL:         haServer.URL,
				TokenEnv:        "HOME_ASSISTANT_TOKEN",
				AllowedEntities: []string{"light.desk"},
				AllowedActions: []gatewayconfig.HomeAssistantActionConfig{
					{
						ActionID:    "desk_light_on",
						Description: "Turn on the desk light",
						Domain:      "light",
						Service:     "turn_on",
						EntityIDs:   []string{"light.desk"},
						Data:        map[string]any{"brightness_pct": 60},
						Slots: []gatewayconfig.HomeAssistantActionSlotConfig{
							{
								Name:        "brightness_pct",
								Description: "Brightness percent from 1 to 100.",
								Type:        "integer",
								Min:         &minBrightness,
								Max:         &maxBrightness,
							},
						},
					},
				},
				TimeoutMS: 1200,
			},
		},
	}, agent.NewStaticMemoryStore(nil), nil)
	if err != nil {
		t.Fatalf("newAgentServiceTools() error = %v", err)
	}

	definitions := registry.Definitions()
	haDefinition, ok := serviceToolDefinition(definitions, homeassistant.CallActionToolName)
	if !ok {
		t.Fatalf("definitions = %+v, missing Home Assistant call_action", definitions)
	}
	if haDefinition.Permission != servicetools.PermissionWrite {
		t.Fatalf("HA action permission = %q, want write", haDefinition.Permission)
	}
	properties, ok := haDefinition.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("HA action schema properties = %#v", haDefinition.InputSchema["properties"])
	}
	actionID, ok := properties["action_id"].(map[string]any)
	if !ok {
		t.Fatalf("HA action_id schema = %#v", properties["action_id"])
	}
	enum, ok := actionID["enum"].([]any)
	if !ok || len(enum) != 1 || enum[0] != "desk_light_on" {
		t.Fatalf("HA action enum = %#v, want desk_light_on", actionID["enum"])
	}
	brightness, ok := properties["brightness_pct"].(map[string]any)
	if !ok || brightness["type"] != "integer" || brightness["minimum"] != float64(1) || brightness["maximum"] != float64(100) {
		t.Fatalf("HA brightness slot schema = %#v", properties["brightness_pct"])
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      homeassistant.CallActionToolName,
		Arguments: map[string]any{"action_id": "desk_light_on", "brightness_pct": float64(35), "entity_id": "switch.secret"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"action_id":"desk_light_on"`)) || !bytes.Contains(result.Payload, []byte(`"ok":true`)) {
		t.Fatalf("payload = %s, want safe Home Assistant action result", string(result.Payload))
	}
	if bytes.Contains(result.Payload, []byte("switch.secret")) {
		t.Fatalf("payload leaked caller override: %s", string(result.Payload))
	}
}

func TestAgentServiceToolsRegistersSearchWebToolWhenEnabled(t *testing.T) {
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != search.WebSearchPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, search.WebSearchPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer search-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var body search.WebSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.Query != "StackChan docs" || body.MaxResults != 2 {
			t.Fatalf("request body = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider":"adapter","results":[{"title":"StackChan","url":"https://docs.m5stack.com/zh_CN/StackChan/","snippet":"official docs","source_domain":"docs.m5stack.com"}]}`))
	}))
	defer searchServer.Close()
	t.Setenv("SEARCH_ADAPTER_URL", searchServer.URL)
	t.Setenv("SEARCH_ADAPTER_TOKEN", "search-token")

	registry, err := newAgentServiceTools(&gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{MemoryMaxItems: 5},
		Tools: gatewayconfig.ToolsConfig{
			Search: gatewayconfig.SearchConfig{
				Enabled:        true,
				BaseURLEnv:     "SEARCH_ADAPTER_URL",
				TokenEnv:       "SEARCH_ADAPTER_TOKEN",
				AllowedDomains: []string{"docs.m5stack.com"},
				TimeoutMS:      1500,
				MaxResults:     2,
				MaxQueryChars:  80,
			},
		},
	}, agent.NewStaticMemoryStore(nil), nil)
	if err != nil {
		t.Fatalf("newAgentServiceTools() error = %v", err)
	}
	if !registry.HasTool(search.WebSearchToolName) {
		t.Fatalf("registry missing %s", search.WebSearchToolName)
	}
	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      search.WebSearchToolName,
		Arguments: map[string]any{"query": "StackChan docs", "max_results": float64(2)},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"title":"StackChan"`)) || !bytes.Contains(result.Payload, []byte(`"result_count":1`)) {
		t.Fatalf("payload = %s, want safe search result", string(result.Payload))
	}
}

func TestAgentServiceToolsRegistersFeishuToolsWhenEnabled(t *testing.T) {
	feishuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feishu HTTP request to %s", r.URL.Path)
	}))
	defer feishuServer.Close()
	t.Setenv("FEISHU_APP_ID", "cli_stackchan")
	t.Setenv("FEISHU_APP_SECRET", "feishu-secret")
	t.Setenv("FEISHU_LAB_CHAT_ID", "oc_lab")

	registry, err := newAgentServiceTools(&gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{MemoryMaxItems: 5},
		Tools: gatewayconfig.ToolsConfig{
			Feishu: gatewayconfig.FeishuConfig{
				Enabled:      true,
				BaseURL:      feishuServer.URL,
				AppIDEnv:     "FEISHU_APP_ID",
				AppSecretEnv: "FEISHU_APP_SECRET",
				AllowedTargets: []gatewayconfig.FeishuTargetConfig{
					{
						TargetID:      "lab_group",
						Description:   "Lab group",
						ReceiveIDType: "chat_id",
						ReceiveIDEnv:  "FEISHU_LAB_CHAT_ID",
					},
				},
				TimeoutMS:    1500,
				MaxTextChars: 240,
			},
		},
	}, agent.NewStaticMemoryStore(nil), nil)
	if err != nil {
		t.Fatalf("newAgentServiceTools() error = %v", err)
	}
	if !registry.HasTool(feishu.ListTargetsToolName) || !registry.HasTool(feishu.SendTextToolName) {
		t.Fatalf("registry missing Feishu tools")
	}

	listResult, err := registry.ExecuteTool(context.Background(), servicetools.Call{Name: feishu.ListTargetsToolName})
	if err != nil {
		t.Fatalf("list ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(listResult.Payload, []byte(`"target_id":"lab_group"`)) || bytes.Contains(listResult.Payload, []byte("oc_lab")) {
		t.Fatalf("list payload = %s, want safe target list", string(listResult.Payload))
	}
	sendResult, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      feishu.SendTextToolName,
		Arguments: map[string]any{"target_id": "lab_group", "text": "提醒大家开会"},
	})
	if err != nil {
		t.Fatalf("send ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(sendResult.Payload, []byte(`"requires_confirmation":true`)) || !bytes.Contains(sendResult.Payload, []byte(`"ok":false`)) {
		t.Fatalf("send payload = %s, want confirmation request", string(sendResult.Payload))
	}
	if bytes.Contains(sendResult.Payload, []byte("oc_lab")) || bytes.Contains(sendResult.Payload, []byte("提醒大家开会")) {
		t.Fatalf("send payload leaked receive id or text: %s", string(sendResult.Payload))
	}
}

func TestAgentServiceToolsRegistersReminderAnnounceToolWhenEnabled(t *testing.T) {
	registry, err := newAgentServiceTools(&gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{MemoryMaxItems: 5},
		Tools: gatewayconfig.ToolsConfig{
			Reminder: gatewayconfig.ReminderConfig{
				Enabled:         true,
				MaxTitleChars:   24,
				MaxMessageChars: 80,
			},
		},
	}, agent.NewStaticMemoryStore(nil), nil)
	if err != nil {
		t.Fatalf("newAgentServiceTools() error = %v", err)
	}
	if !registry.HasTool(reminder.AnnounceToolName) {
		t.Fatalf("registry missing %s", reminder.AnnounceToolName)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: reminder.AnnounceToolName,
		Arguments: map[string]any{
			"title":   "喝水",
			"message": "该喝水了，保存一下当前工作。",
			"urgency": "high",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"ok":true`)) || !bytes.Contains(result.Payload, []byte(`"urgency":"high"`)) {
		t.Fatalf("payload = %s, want safe reminder announcement", string(result.Payload))
	}
}

func TestAgentServiceToolsRegistersCameraRequestToolWhenEnabled(t *testing.T) {
	registry, err := newAgentServiceTools(&gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{MemoryMaxItems: 5},
		Tools: gatewayconfig.ToolsConfig{
			Camera: gatewayconfig.CameraConfig{
				Enabled:        true,
				MaxReasonChars: 64,
			},
		},
	}, agent.NewStaticMemoryStore(nil), nil)
	if err != nil {
		t.Fatalf("newAgentServiceTools() error = %v", err)
	}
	if !registry.HasTool(camera.RequestCaptureToolName) {
		t.Fatalf("registry missing %s", camera.RequestCaptureToolName)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      camera.RequestCaptureToolName,
		Arguments: map[string]any{"reason": "看一下桌面状态"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"requires_confirmation":true`)) || bytes.Contains(result.Payload, []byte("桌面状态")) {
		t.Fatalf("payload = %s, want confirmation without reason text", string(result.Payload))
	}
}

func TestStackChanExpressionPoliciesFromConfig(t *testing.T) {
	cfg := map[string]gatewayconfig.ExpressionCueConfig{
		" NOD ": {
			Motion: &gatewayconfig.ExpressionMotionConfig{YawDeg: 3, PitchDeg: 14, Speed: 260},
			LED:    &gatewayconfig.ExpressionLEDConfig{R: 10, G: 20, B: 30},
			Scene: &gatewayconfig.DisplaySceneConfig{
				Scene:   "speaking",
				Emotion: "happy",
				Caption: "收到。",
				Accent:  "green",
			},
		},
	}

	policies := expressionPoliciesFromConfig(cfg)

	policy := policies["nod"]
	if policy.Motion == nil || policy.Motion.Yaw != 3 || policy.Motion.Pitch != 14 || policy.Motion.Speed != 260 {
		t.Fatalf("policy motion = %+v, want configured motion", policy.Motion)
	}
	if policy.LED == nil || policy.LED.R != 10 || policy.LED.G != 20 || policy.LED.B != 30 {
		t.Fatalf("policy LED = %+v, want configured LED", policy.LED)
	}
	if policy.Scene.Scene != "speaking" || policy.Scene.Caption != "收到。" || policy.Scene.Emotion != "happy" {
		t.Fatalf("policy scene = %+v, want configured scene", policy.Scene)
	}
}

func TestStackChanLifecycleExpressionCuesFromConfig(t *testing.T) {
	cues := lifecycleExpressionCuesFromConfig(map[string]string{
		" Thinking ": " THINKING ",
	})

	if cues[stackchan.SceneThinking] != stackchan.CueThinking {
		t.Fatalf("lifecycle expression cues = %+v, want normalized thinking cue", cues)
	}
}

func TestStackChanLifecycleLEDsFromConfig(t *testing.T) {
	disabled := false
	leds := lifecycleLEDsFromConfig(map[string]gatewayconfig.LifecycleLEDConfig{
		" Speaking ": {R: 0, G: 0, B: 168},
		"idle":       {Enabled: &disabled, R: 0, G: 0, B: 0},
	})

	speaking := leds[stackchan.SceneSpeaking]
	if speaking.R != 0 || speaking.G != 0 || speaking.B != 168 || speaking.Reason != "speaking_start" {
		t.Fatalf("speaking lifecycle LED = %+v, want normalized blue speaking cue", speaking)
	}
	if _, exists := leds[stackchan.SceneIdle]; exists {
		t.Fatalf("idle lifecycle LED should be disabled, got %+v", leds[stackchan.SceneIdle])
	}
}

func TestStackChanEventExpressionCuesFromConfig(t *testing.T) {
	cues := eventExpressionCuesFromConfig(map[string]string{
		" Agent_Route.OpenClaw ": " NOD ",
	})

	if cues[stackchan.DisplayEventAgentRouteOpenClaw] != stackchan.CueNod {
		t.Fatalf("event expression cues = %+v, want normalized OpenClaw route cue", cues)
	}
}

func TestStackChanExpressionSequencesFromConfig(t *testing.T) {
	sequences := expressionSequencesFromConfig(map[string]gatewayconfig.ExpressionSequenceConfig{
		" Agree.Quick ": {Cues: []string{" ATTENTIVE ", " NOD "}},
	})

	cues := sequences["agree.quick"]
	if len(cues) != 2 || cues[0] != stackchan.CueAttentive || cues[1] != stackchan.CueNod {
		t.Fatalf("expression sequences = %+v, want normalized configured cue list", sequences)
	}
}

func TestStackChanDisplayCardsFromConfig(t *testing.T) {
	maxCaptionChars := 24
	cards := displayCardPoliciesFromConfig(map[string]gatewayconfig.DisplayCardConfig{
		" Status.Note ": {
			Scene:           stackchan.SceneTool,
			Emotion:         stackchan.EmotionWarm,
			Caption:         "默认卡片。",
			Accent:          stackchan.AccentGreen,
			AllowCaption:    true,
			MaxCaptionChars: maxCaptionChars,
			Motion: &gatewayconfig.DisplayMotionConfig{
				Preset: stackchan.MotionPresetNodSoft,
			},
		},
	})

	card := cards["status.note"]
	if card.Scene != stackchan.SceneTool || card.Emotion != stackchan.EmotionWarm || card.Caption != "默认卡片。" || card.Accent != stackchan.AccentGreen {
		t.Fatalf("display card = %+v, want normalized scene policy", card)
	}
	if !card.AllowCaption || card.MaxCaptionChars != maxCaptionChars {
		t.Fatalf("display card bounds = %+v, want model caption allowed with configured bound", card)
	}
	if card.Motion == nil || card.Motion.Preset != stackchan.MotionPresetNodSoft {
		t.Fatalf("display card motion = %+v, want configured motion", card.Motion)
	}
}

func TestToolResultFollowUpPolicyFromConfig(t *testing.T) {
	disabled := false
	maxResults := 1
	maxResultBytes := 512
	allowToolCalls := true
	maxToolCalls := 1
	policy := toolResultFollowUpPolicyFromConfig(gatewayconfig.ToolFollowUpConfig{
		Enabled:        &disabled,
		MaxResults:     &maxResults,
		MaxResultBytes: &maxResultBytes,
		AllowedTools:   []string{"memory.lookup", "search.web"},
		AllowToolCalls: &allowToolCalls,
		MaxToolCalls:   &maxToolCalls,
	})

	if policy.Enabled || policy.MaxResults != 1 || policy.MaxResultBytes != 512 || !policy.AllowToolCalls || policy.MaxToolCalls != 1 {
		t.Fatalf("tool follow-up policy = %+v, want disabled bounded policy", policy)
	}
	if len(policy.AllowedTools) != 2 || policy.AllowedTools[0] != "memory.lookup" || policy.AllowedTools[1] != "search.web" {
		t.Fatalf("tool follow-up allowed tools = %+v, want configured allowlist", policy.AllowedTools)
	}
}

func TestAgentServiceToolsRegistersGatedV21ToolWhenEnabled(t *testing.T) {
	v21Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agents.V21VoiceQueryPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, agents.V21VoiceQueryPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer v21-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query_run_id":"qry_1",
			"answer_type":"GROUNDED_ANSWER",
			"spoken_answer":"慢充预约支持设置开始时间和结束时间。",
			"full_answer":"full answer must not leak",
			"citations":[],
			"evidence":[],
			"confidence":0.91,
			"safe_to_style_wrap":false,
			"policy":{"require_citations":true,"allow_style_wrap":false,"fact_locked":true},
			"tool_results":[]
		}`))
	}))
	defer v21Server.Close()
	t.Setenv("V21_ADAPTER_URL", v21Server.URL)
	t.Setenv("V21_ADAPTER_TOKEN", "v21-token")
	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode:    "casual",
			MemoryMaxItems: 5,
			V21: gatewayconfig.AgentV21Config{
				Enabled:              true,
				BaseURLEnv:           "V21_ADAPTER_URL",
				TokenEnv:             "V21_ADAPTER_TOKEN",
				AllowedCollectionIDs: []string{"col_vehicle"},
				TimeoutMS:            1500,
				MaxSpokenChars:       180,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	registry, err := newAgentServiceTools(cfg, agent.NewStaticMemoryStore(nil), modes)
	if err != nil {
		t.Fatalf("newAgentServiceTools() error = %v", err)
	}
	if !registry.HasTool(agents.V21VoiceQueryToolName) {
		t.Fatalf("registry missing %s", agents.V21VoiceQueryToolName)
	}

	blocked, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		DeviceID: "stackchan-s3-main",
		Name:     agents.V21VoiceQueryToolName,
		Arguments: map[string]any{
			"question":      "慢充预约怎么设置？",
			"collection_id": "col_vehicle",
		},
	})
	if err != nil {
		t.Fatalf("blocked ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(blocked.Payload, []byte(`"blocked":true`)) {
		t.Fatalf("blocked payload = %s, want professional-mode block", string(blocked.Payload))
	}

	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeProfessional); err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}
	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		DeviceID: "stackchan-s3-main",
		Name:     agents.V21VoiceQueryToolName,
		Arguments: map[string]any{
			"question":      "慢充预约怎么设置？",
			"collection_id": "col_vehicle",
		},
	})
	if err != nil {
		t.Fatalf("professional ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"spoken_answer":"慢充预约支持设置开始时间和结束时间。"`)) {
		t.Fatalf("payload = %s, want safe V21 spoken answer", string(result.Payload))
	}
	if bytes.Contains(result.Payload, []byte("full answer must not leak")) {
		t.Fatalf("payload leaked V21 full answer: %s", string(result.Payload))
	}
}

func TestNewWiresAdminAgentModeController(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	configPath := writeGatewayConfig(t, `
server:
  listen_addr: "127.0.0.1:0"
  websocket_path: "/xiaozhi/v1/ws"
  admin_addr: "127.0.0.1:0"
  admin_token_env: "STACKCHAN_ADMIN_TOKEN"
  shutdown_timeout_ms: 5000
devices:
  - device_id: "stackchan-s3-main"
    client_id: "stackchan-s3-main-client"
    auth_token_env: "STACKCHAN_MAIN_AUTH_TOKEN"
    default_mode: "auto"
audio:
  uplink_sample_rate_hz: 16000
  downlink_sample_rate_hz: 24000
  channels: 1
  frame_duration_ms: 60
  downlink_queue_ms: 1200
  max_turn_ms: 30000
providers:
  default_profile: "cn-low-latency-cascade"
  profiles:
    cn-low-latency-cascade:
      asr: "mock"
      llm: "mock"
      tts: "mock"
agent:
  default_mode: "casual"
  persona_path: "./configs/persona.stackchan.yaml"
  memory_db_path: ""
  memory_max_items: 5
  recent_turns: 8
stackchan:
  body:
    min_command_gap_ms: 160
    max_commands_per_turn: 16
    yaw_min_deg: -45
    yaw_max_deg: 45
    pitch_min_deg: 0
    pitch_max_deg: 45
    default_speed: 150
  display:
    scene_ttl_ms: 1800
    max_caption_chars: 48
observability:
  trace_jsonl_path: "./var/traces/test-agent-mode.jsonl"
  redact_secrets: true
`)
	app, err := New(Options{ConfigPath: configPath, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.shutdownServers()
	if app.adminServer == nil || app.adminServer.Handler == nil {
		t.Fatal("admin server is not configured")
	}

	request := httptest.NewRequest(http.MethodPut, "/internal/v1/devices/stackchan-s3-main/agent-mode", bytes.NewReader([]byte(`{"mode":"professional"}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"active_mode":"professional"`)) {
		t.Fatalf("response = %s, want professional mode", recorder.Body.String())
	}

	catalogRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-modes", nil)
	catalogRequest.Header.Set("Authorization", "Bearer admin-token")
	catalogRecorder := httptest.NewRecorder()
	app.adminServer.Handler.ServeHTTP(catalogRecorder, catalogRequest)
	if catalogRecorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, body = %s", catalogRecorder.Code, catalogRecorder.Body.String())
	}
	for _, want := range []string{`"default_mode":"casual"`, `"active_mode":"professional"`, `"device_id":"stackchan-s3-main"`} {
		if !bytes.Contains(catalogRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("catalog response missing %s: %s", want, catalogRecorder.Body.String())
		}
	}
	for _, forbidden := range []string{"STACKCHAN_ADMIN_TOKEN", "admin-token", "persona", "prompt"} {
		if bytes.Contains(catalogRecorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("catalog response leaked %q: %s", forbidden, catalogRecorder.Body.String())
		}
	}
}

func TestAgentModeCommandHandlerUpdatesRuntimeModeStore(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	handler := newAgentModeCommandHandler(modes)
	if handler == nil {
		t.Fatal("handler is nil")
	}

	result, err := handler.HandleAgentModeCommand(context.Background(), session.AgentModeCommandRequest{
		SessionID:  "sess_mode",
		DeviceID:   "stackchan-s3-main",
		Generation: 3,
		Transcript: "请进入专业模式。",
	})
	if err != nil {
		t.Fatalf("HandleAgentModeCommand() error = %v", err)
	}
	if !result.Handled || result.Mode != string(agents.ModeProfessional) || result.Action != agents.ModeCommandActionEnterProfessional {
		t.Fatalf("result = %+v, want professional entry", result)
	}
	status, err := modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != agents.ModeProfessional || !status.Override {
		t.Fatalf("status = %+v, want professional override", status)
	}

	result, err = handler.HandleAgentModeCommand(context.Background(), session.AgentModeCommandRequest{
		SessionID:  "sess_mode",
		DeviceID:   "stackchan-s3-main",
		Generation: 4,
		Transcript: "进入 Hermes 模式。",
	})
	if err != nil {
		t.Fatalf("HandleAgentModeCommand(roleplay) error = %v", err)
	}
	if !result.Handled || result.Mode != string(agents.ModeRoleplay) || result.Action != agents.ModeCommandActionEnterRoleplay {
		t.Fatalf("roleplay result = %+v, want roleplay entry", result)
	}
	status, err = modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode(roleplay) error = %v", err)
	}
	if status.ActiveMode != agents.ModeRoleplay || !status.Override {
		t.Fatalf("status = %+v, want roleplay override", status)
	}

	result, err = handler.HandleAgentModeCommand(context.Background(), session.AgentModeCommandRequest{
		SessionID:  "sess_mode",
		DeviceID:   "stackchan-s3-main",
		Generation: 5,
		Transcript: "进入工具模式。",
	})
	if err != nil {
		t.Fatalf("HandleAgentModeCommand(tool) error = %v", err)
	}
	if !result.Handled || result.Mode != string(agents.ModeTool) || result.Action != agents.ModeCommandActionEnterTool {
		t.Fatalf("tool result = %+v, want tool entry", result)
	}
	status, err = modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode(tool) error = %v", err)
	}
	if status.ActiveMode != agents.ModeTool || !status.Override {
		t.Fatalf("status = %+v, want tool override", status)
	}

	result, err = handler.HandleAgentModeCommand(context.Background(), session.AgentModeCommandRequest{
		SessionID:  "sess_mode",
		DeviceID:   "stackchan-s3-main",
		Generation: 6,
		Transcript: "退出专业模式。",
	})
	if err != nil {
		t.Fatalf("HandleAgentModeCommand(exit) error = %v", err)
	}
	if !result.Handled || result.Mode != string(agents.ModeCasual) || result.Action != agents.ModeCommandActionExitProfessional {
		t.Fatalf("exit result = %+v, want casual exit", result)
	}
	status, err = modes.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode(exit) error = %v", err)
	}
	if status.ActiveMode != agents.ModeCasual || status.Override {
		t.Fatalf("status after exit = %+v, want configured default without override", status)
	}

	result, err = handler.HandleAgentModeCommand(context.Background(), session.AgentModeCommandRequest{
		SessionID:  "sess_mode",
		DeviceID:   "stackchan-s3-main",
		Generation: 7,
		Transcript: "专业模式是什么？",
	})
	if err != nil {
		t.Fatalf("HandleAgentModeCommand(non-command) error = %v", err)
	}
	if result.Handled {
		t.Fatalf("non-command result = %+v, want not handled", result)
	}
}

func TestProviderProfileCommandHandlerUpdatesRuntimeProviderProfile(t *testing.T) {
	profiles := &recordingProviderProfileController{
		defaultProfile: "siliconflow-dashscope-voice",
		activeProfile:  "siliconflow-dashscope-voice",
	}
	handler := newProviderProfileCommandHandler(profiles)
	if handler == nil {
		t.Fatal("handler is nil")
	}

	result, err := handler.HandleProviderProfileCommand(context.Background(), session.ProviderProfileCommandRequest{
		SessionID:  "sess_provider_command",
		DeviceID:   "stackchan-s3-main",
		Generation: 3,
		Transcript: "大头，切到字节模型。",
	})
	if err != nil {
		t.Fatalf("HandleProviderProfileCommand() error = %v", err)
	}
	if !result.Handled || result.Profile != "doubao-dashscope-voice" || result.Action != "set" {
		t.Fatalf("result = %+v, want doubao set", result)
	}
	if result.SpokenText != "已切到字节豆包语音链路。" {
		t.Fatalf("spoken = %q, want ByteDance confirmation", result.SpokenText)
	}
	if len(profiles.setProfiles) != 1 || profiles.setProfiles[0] != "doubao-dashscope-voice" {
		t.Fatalf("set profiles = %v, want doubao-dashscope-voice", profiles.setProfiles)
	}

	result, err = handler.HandleProviderProfileCommand(context.Background(), session.ProviderProfileCommandRequest{
		DeviceID:   "stackchan-s3-main",
		Transcript: "当前语音链路是什么？",
	})
	if err != nil {
		t.Fatalf("HandleProviderProfileCommand(status) error = %v", err)
	}
	if !result.Handled || result.Action != "status" || result.Profile != "doubao-dashscope-voice" {
		t.Fatalf("status result = %+v, want current doubao profile", result)
	}

	result, err = handler.HandleProviderProfileCommand(context.Background(), session.ProviderProfileCommandRequest{
		DeviceID:   "stackchan-s3-main",
		Transcript: "切回默认语音链路。",
	})
	if err != nil {
		t.Fatalf("HandleProviderProfileCommand(clear) error = %v", err)
	}
	if !result.Handled || result.Action != "clear" || result.Profile != "siliconflow-dashscope-voice" {
		t.Fatalf("clear result = %+v, want default profile", result)
	}
	if !profiles.cleared {
		t.Fatal("expected profile override to be cleared")
	}

	result, err = handler.HandleProviderProfileCommand(context.Background(), session.ProviderProfileCommandRequest{
		DeviceID:   "stackchan-s3-main",
		Transcript: "阿里云服务器现在正常吗？",
	})
	if err != nil {
		t.Fatalf("HandleProviderProfileCommand(non-command) error = %v", err)
	}
	if result.Handled {
		t.Fatalf("non-command result = %+v, want not handled", result)
	}
}

func TestAgentModeReaderUsesRuntimeModeStore(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	reader := newAgentModeReader(modes)
	if reader == nil {
		t.Fatal("reader is nil")
	}
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeProfessional); err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}

	result, err := reader.CurrentAgentMode(context.Background(), session.AgentModeReadRequest{
		SessionID:  "sess_mode_read",
		DeviceID:   "stackchan-s3-main",
		Generation: 3,
	})

	if err != nil {
		t.Fatalf("CurrentAgentMode() error = %v", err)
	}
	if result.Mode != string(agents.ModeProfessional) {
		t.Fatalf("mode = %q, want professional", result.Mode)
	}
}

type recordingProviderProfileController struct {
	defaultProfile string
	activeProfile  string
	setProfiles    []string
	cleared        bool
}

func (c *recordingProviderProfileController) ListProfiles(context.Context) (providerrouter.ProfileCatalog, error) {
	return providerrouter.ProfileCatalog{}, nil
}

func (c *recordingProviderProfileController) GetDeviceProfile(context.Context, string) (providerrouter.Status, error) {
	return c.status(), nil
}

func (c *recordingProviderProfileController) SetDeviceProfile(_ context.Context, _ string, profile string) (providerrouter.Status, error) {
	c.activeProfile = profile
	c.setProfiles = append(c.setProfiles, profile)
	c.cleared = false
	return c.status(), nil
}

func (c *recordingProviderProfileController) ClearDeviceProfile(context.Context, string) (providerrouter.Status, error) {
	c.activeProfile = c.defaultProfile
	c.cleared = true
	return c.status(), nil
}

func (c *recordingProviderProfileController) status() providerrouter.Status {
	active := c.activeProfile
	if active == "" {
		active = c.defaultProfile
	}
	return providerrouter.Status{
		DeviceID:       "stackchan-s3-main",
		DefaultProfile: c.defaultProfile,
		ActiveProfile:  active,
		Override:       active != c.defaultProfile,
	}
}

func TestAgentRuntimeRouterRoutesToolModeToOpenClaw(t *testing.T) {
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agents.OpenClawRespondPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, agents.OpenClawRespondPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer openclaw-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var request agents.OpenClawRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode OpenClaw request: %v", err)
		}
		if request.Text != "帮我分析桌面状态" || request.DeviceID != "stackchan-s3-main" || request.SessionID != "sess_runtime" || request.TurnID != "11" {
			t.Fatalf("OpenClaw request = %+v, want current voice turn", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text":"我会调用桌面工具继续。",
			"tool_intents":[{"Tool":"memory.lookup","Args":{"query":"桌面偏好"}}]
		}`))
	}))
	defer openClawServer.Close()
	t.Setenv("OPENCLAW_WS_URL", openClawServer.URL)
	t.Setenv("OPENCLAW_AGENT_TOKEN", "openclaw-token")

	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode: "casual",
			OpenClaw: gatewayconfig.AgentOpenClawConfig{
				Enabled:        true,
				BaseURLEnv:     "OPENCLAW_WS_URL",
				TokenEnv:       "OPENCLAW_AGENT_TOKEN",
				TimeoutMS:      1500,
				MaxSpokenChars: 180,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	router := newAgentRuntimeRouter(cfg, modes)
	if router == nil {
		t.Fatal("agent runtime router is nil")
	}

	notHandled, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 10,
		Transcript: "日常聊天",
	})
	if err != nil {
		t.Fatalf("RouteAgentTurn(casual) error = %v", err)
	}
	if notHandled.Handled {
		t.Fatalf("casual result = %+v, want not handled", notHandled)
	}

	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeTool); err != nil {
		t.Fatalf("SetDeviceMode(tool) error = %v", err)
	}
	result, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 11,
		Transcript: "帮我分析桌面状态",
	})
	if err != nil {
		t.Fatalf("RouteAgentTurn(tool) error = %v", err)
	}
	if !result.Handled || result.Mode != string(agents.ModeTool) || result.Destination != string(agents.DestinationOpenClaw) || result.Text != "我会调用桌面工具继续。" {
		t.Fatalf("result = %+v, want OpenClaw handled text", result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != agent.MemoryLookupToolName || result.ToolCalls[0].Arguments["query"] != "桌面偏好" {
		t.Fatalf("tool calls = %+v, want gateway-owned memory lookup", result.ToolCalls)
	}
}

func TestAgentRuntimeRouterRestrictsOpenClawToolIntentsFromConfig(t *testing.T) {
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text":"我会按边界处理。",
			"tool_intents":[
				{"tool":"memory.lookup","args":{"query":"桌面偏好"}},
				{"tool":"search.web","args":{"query":"不该进入"}},
				{"tool":"stackchan.express","args":{"cue":"nod"}}
			]
		}`))
	}))
	defer openClawServer.Close()
	t.Setenv("OPENCLAW_WS_URL", openClawServer.URL)
	t.Setenv("OPENCLAW_AGENT_TOKEN", "openclaw-token")
	maxToolIntents := 1

	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode: "casual",
			OpenClaw: gatewayconfig.AgentOpenClawConfig{
				Enabled:            true,
				BaseURLEnv:         "OPENCLAW_WS_URL",
				TokenEnv:           "OPENCLAW_AGENT_TOKEN",
				TimeoutMS:          1500,
				MaxSpokenChars:     180,
				AllowedToolIntents: []string{"memory.lookup", "search.web", "stackchan.express"},
				MaxToolIntents:     &maxToolIntents,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeTool); err != nil {
		t.Fatalf("SetDeviceMode(tool) error = %v", err)
	}
	router := newAgentRuntimeRouter(cfg, modes)

	result, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 13,
		Transcript: "按配置调用工具",
	})

	if err != nil {
		t.Fatalf("RouteAgentTurn() error = %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != agent.MemoryLookupToolName || result.ToolCalls[0].Arguments["query"] != "桌面偏好" {
		t.Fatalf("tool calls = %+v, want only first allowed call under configured cap", result.ToolCalls)
	}
}

func TestAgentRuntimeRouterRateLimitsOpenClawRuntimeRoutes(t *testing.T) {
	callCount := 0
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"我会调用桌面工具继续。"}`))
	}))
	defer openClawServer.Close()
	t.Setenv("OPENCLAW_WS_URL", openClawServer.URL)
	t.Setenv("OPENCLAW_AGENT_TOKEN", "openclaw-token")

	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode: "casual",
			OpenClaw: gatewayconfig.AgentOpenClawConfig{
				Enabled:                   true,
				BaseURLEnv:                "OPENCLAW_WS_URL",
				TokenEnv:                  "OPENCLAW_AGENT_TOKEN",
				TimeoutMS:                 1500,
				MaxSpokenChars:            180,
				MaxRuntimeRoutesPerMinute: 1,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeTool); err != nil {
		t.Fatalf("SetDeviceMode(tool) error = %v", err)
	}
	router := newAgentRuntimeRouter(cfg, modes)

	first, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 21,
		Transcript: "第一次调用工具脑",
	})
	if err != nil {
		t.Fatalf("first RouteAgentTurn() error = %v", err)
	}
	if !first.Handled {
		t.Fatalf("first result = %+v, want OpenClaw handled", first)
	}
	second, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 22,
		Transcript: "第二次应该回落普通 LLM",
	})
	if err != nil {
		t.Fatalf("second RouteAgentTurn() error = %v", err)
	}
	if second.Handled {
		t.Fatalf("second result = %+v, want rate-limited bridge to fall back to normal LLM path", second)
	}
	if second.SkipReason != agents.RuntimeStatusReasonRuntimeRateLimited || second.Destination != string(agents.DestinationOpenClaw) || second.Mode != string(agents.ModeTool) {
		t.Fatalf("second result = %+v, want safe rate-limit skip metadata", second)
	}
	if callCount != 1 {
		t.Fatalf("OpenClaw calls = %d, want second route blocked before external call", callCount)
	}
	policyReader, ok := router.(agents.RuntimePolicyStatusReader)
	if !ok {
		t.Fatal("agent runtime router does not expose safe runtime policy status")
	}
	status := policyReader.RuntimePolicyStatus(context.Background(), "stackchan-s3-main", agents.BridgeOpenClaw)
	if status.Available || status.Reason != agents.RuntimeStatusReasonRuntimeRateLimited {
		t.Fatalf("runtime policy status = %+v, want rate-limited reason", status)
	}
}

func TestAgentRuntimeRouterSkipsOpenClawWhenInputTooLong(t *testing.T) {
	callCount := 0
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"不该被调用。"}`))
	}))
	defer openClawServer.Close()
	t.Setenv("OPENCLAW_WS_URL", openClawServer.URL)
	t.Setenv("OPENCLAW_AGENT_TOKEN", "openclaw-token")

	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode: "casual",
			OpenClaw: gatewayconfig.AgentOpenClawConfig{
				Enabled:              true,
				BaseURLEnv:           "OPENCLAW_WS_URL",
				TokenEnv:             "OPENCLAW_AGENT_TOKEN",
				TimeoutMS:            1500,
				MaxSpokenChars:       180,
				MaxRuntimeInputChars: 8,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeTool); err != nil {
		t.Fatalf("SetDeviceMode(tool) error = %v", err)
	}
	router := newAgentRuntimeRouter(cfg, modes)

	result, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 24,
		Transcript: strings.Repeat("长", 9),
	})

	if err != nil {
		t.Fatalf("RouteAgentTurn() error = %v", err)
	}
	if result.Handled {
		t.Fatalf("result = %+v, want long bridge input to fall back to normal LLM path", result)
	}
	if result.SkipReason != agents.RuntimeStatusReasonRuntimeInputTooLong || result.Destination != string(agents.DestinationOpenClaw) || result.Mode != string(agents.ModeTool) {
		t.Fatalf("result = %+v, want safe input-limit skip metadata", result)
	}
	if callCount != 0 {
		t.Fatalf("OpenClaw calls = %d, want input limit to block before external call", callCount)
	}
}

func TestAgentRuntimeRouterCoolsDownOpenClawAfterErrors(t *testing.T) {
	callCount := 0
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		http.Error(w, "bridge temporarily down", http.StatusBadGateway)
	}))
	defer openClawServer.Close()
	t.Setenv("OPENCLAW_WS_URL", openClawServer.URL)
	t.Setenv("OPENCLAW_AGENT_TOKEN", "openclaw-token")

	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode: "casual",
			OpenClaw: gatewayconfig.AgentOpenClawConfig{
				Enabled:                        true,
				BaseURLEnv:                     "OPENCLAW_WS_URL",
				TokenEnv:                       "OPENCLAW_AGENT_TOKEN",
				TimeoutMS:                      1500,
				MaxSpokenChars:                 180,
				MaxRuntimeErrorsBeforeCooldown: 2,
				RuntimeErrorCooldownMS:         60000,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeTool); err != nil {
		t.Fatalf("SetDeviceMode(tool) error = %v", err)
	}
	router := newAgentRuntimeRouter(cfg, modes)

	for i := 0; i < 2; i++ {
		_, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
			SessionID:  "sess_runtime",
			DeviceID:   "stackchan-s3-main",
			Generation: int64(30 + i),
			Transcript: "工具脑现在失败",
		})
		if err == nil {
			t.Fatalf("attempt %d RouteAgentTurn() error = nil, want bridge error before cooldown", i+1)
		}
	}

	result, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 33,
		Transcript: "第三次应该直接回落普通 LLM",
	})
	if err != nil {
		t.Fatalf("cooldown RouteAgentTurn() error = %v, want silent fallback", err)
	}
	if result.Handled {
		t.Fatalf("cooldown result = %+v, want not handled so VoiceLoop can use normal LLM", result)
	}
	if result.SkipReason != agents.RuntimeStatusReasonRuntimeErrorCooldown || result.Destination != string(agents.DestinationOpenClaw) || result.Mode != string(agents.ModeTool) {
		t.Fatalf("cooldown result = %+v, want safe cooldown skip metadata", result)
	}
	if callCount != 2 {
		t.Fatalf("OpenClaw calls = %d, want third route blocked before external call", callCount)
	}
	policyReader, ok := router.(agents.RuntimePolicyStatusReader)
	if !ok {
		t.Fatal("agent runtime router does not expose safe runtime policy status")
	}
	status := policyReader.RuntimePolicyStatus(context.Background(), "stackchan-s3-main", agents.BridgeOpenClaw)
	if status.Available || status.Reason != agents.RuntimeStatusReasonRuntimeErrorCooldown {
		t.Fatalf("runtime policy status = %+v, want error-cooldown reason", status)
	}
}

func TestAgentRuntimeRouterIgnoresBlankOpenClawResponseWithoutToolCalls(t *testing.T) {
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"   ","tool_intents":[]}`))
	}))
	defer openClawServer.Close()
	t.Setenv("OPENCLAW_WS_URL", openClawServer.URL)
	t.Setenv("OPENCLAW_AGENT_TOKEN", "openclaw-token")

	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode: "casual",
			OpenClaw: gatewayconfig.AgentOpenClawConfig{
				Enabled:        true,
				BaseURLEnv:     "OPENCLAW_WS_URL",
				TokenEnv:       "OPENCLAW_AGENT_TOKEN",
				TimeoutMS:      1500,
				MaxSpokenChars: 180,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeTool); err != nil {
		t.Fatalf("SetDeviceMode(tool) error = %v", err)
	}
	router := newAgentRuntimeRouter(cfg, modes)

	result, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 14,
		Transcript: "空结果不要吞掉这一轮",
	})

	if err != nil {
		t.Fatalf("RouteAgentTurn() error = %v", err)
	}
	if result.Handled {
		t.Fatalf("result = %+v, want blank bridge response to fall back to normal LLM path", result)
	}
}

func TestAgentRuntimeRouterRoutesRoleplayModeToHermes(t *testing.T) {
	hermesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agents.HermesRespondPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, agents.HermesRespondPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer hermes-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var request agents.HermesRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode Hermes request: %v", err)
		}
		if request.Text != "继续用这个角色说话" || request.DeviceID != "stackchan-s3-main" || request.SessionID != "sess_runtime" || request.TurnID != "12" {
			t.Fatalf("Hermes request = %+v, want current voice turn", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text":"我会保持角色继续。",
			"tool_intents":[{"tool":"memory.lookup","args":{"query":"角色设定"}}]
		}`))
	}))
	defer hermesServer.Close()
	t.Setenv("HERMES_AGENT_URL", hermesServer.URL)
	t.Setenv("HERMES_AGENT_KEY", "hermes-token")

	cfg := &gatewayconfig.Config{
		Agent: gatewayconfig.AgentConfig{
			DefaultMode: "casual",
			Hermes: gatewayconfig.AgentHermesConfig{
				Enabled:        true,
				BaseURLEnv:     "HERMES_AGENT_URL",
				TokenEnv:       "HERMES_AGENT_KEY",
				TimeoutMS:      1500,
				MaxSpokenChars: 180,
			},
		},
		Devices: []gatewayconfig.DeviceConfig{{DeviceID: "stackchan-s3-main"}},
	}
	modes := newAgentModeStore(cfg)
	router := newAgentRuntimeRouter(cfg, modes)
	if router == nil {
		t.Fatal("agent runtime router is nil")
	}

	notHandled, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 11,
		Transcript: "日常聊天",
	})
	if err != nil {
		t.Fatalf("RouteAgentTurn(casual) error = %v", err)
	}
	if notHandled.Handled {
		t.Fatalf("casual result = %+v, want not handled", notHandled)
	}

	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeRoleplay); err != nil {
		t.Fatalf("SetDeviceMode(roleplay) error = %v", err)
	}
	result, err := router.RouteAgentTurn(context.Background(), session.AgentRuntimeRequest{
		SessionID:  "sess_runtime",
		DeviceID:   "stackchan-s3-main",
		Generation: 12,
		Transcript: "继续用这个角色说话",
	})
	if err != nil {
		t.Fatalf("RouteAgentTurn(roleplay) error = %v", err)
	}
	if !result.Handled || result.Mode != string(agents.ModeRoleplay) || result.Destination != string(agents.DestinationHermes) || result.Text != "我会保持角色继续。" {
		t.Fatalf("result = %+v, want Hermes handled text", result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != agent.MemoryLookupToolName || result.ToolCalls[0].Arguments["query"] != "角色设定" {
		t.Fatalf("tool calls = %+v, want gateway-owned memory lookup", result.ToolCalls)
	}
}

func hasServiceToolDefinition(definitions []servicetools.Definition, name string) bool {
	_, ok := serviceToolDefinition(definitions, name)
	return ok
}

func serviceToolDefinition(definitions []servicetools.Definition, name string) (servicetools.Definition, bool) {
	for _, definition := range definitions {
		if definition.Name == name {
			return definition, true
		}
	}
	return servicetools.Definition{}, false
}

func TestReadyzRejectsDefaultProviderConfigurationError(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("MOONSHOT_API_KEY", "")
	configPath := writeGatewayConfig(t, `
server:
  public_base_url: "https://stackchan.example.internal"
  listen_addr: "127.0.0.1:8080"
  websocket_path: "/xiaozhi/v1/ws"
  admin_addr: "127.0.0.1:8081"
  admin_token_env: "STACKCHAN_ADMIN_TOKEN"
  metrics_addr: "127.0.0.1:9090"
  shutdown_timeout_ms: 5000

devices:
  - device_id: "stackchan-s3-main"
    client_id: "stackchan-s3-main-client"
    auth_token_env: "STACKCHAN_MAIN_AUTH_TOKEN"
    default_mode: "auto"

audio:
  uplink_sample_rate_hz: 16000
  downlink_sample_rate_hz: 24000
  channels: 1
  frame_duration_ms: 60
  downlink_queue_ms: 1200
  max_turn_ms: 30000

providers:
  default_profile: "moonshot-runtime"
  profiles:
    moonshot-runtime:
      asr: "mock"
      llm: "moonshot-llm"
      tts: "mock"

stackchan:
  body:
    min_command_gap_ms: 160
    max_commands_per_turn: 16
    yaw_min_deg: -45
    yaw_max_deg: 45
    pitch_min_deg: 0
    pitch_max_deg: 45
    default_speed: 150
  display:
    scene_ttl_ms: 1800
    max_caption_chars: 48

observability:
  trace_jsonl_path: "./var/traces/turns.jsonl"
  redact_secrets: true
`)

	app, err := New(Options{
		ConfigPath: configPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()

	app.server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeReadyResponse(t, recorder.Body.Bytes())
	if response.Ready {
		t.Fatalf("ready = true, body = %s", recorder.Body.String())
	}
	if response.Checks["config"] != "ok" {
		t.Fatalf("config check = %q, want ok", response.Checks["config"])
	}
	if response.Checks["providers"] != "provider_config_error" {
		t.Fatalf("providers check = %q, want provider_config_error; body = %s", response.Checks["providers"], recorder.Body.String())
	}
	for _, forbidden := range []string{"moonshot api key is required", "test-token", "admin-token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("ready response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestReadyzRejectsMissingProviderDecoderConfiguration(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("DOUBAO_API_KEY", "fake-doubao-key")
	configPath := writeGatewayConfig(t, `
server:
  public_base_url: "https://stackchan.example.internal"
  listen_addr: "127.0.0.1:8080"
  websocket_path: "/xiaozhi/v1/ws"
  admin_addr: "127.0.0.1:8081"
  admin_token_env: "STACKCHAN_ADMIN_TOKEN"
  metrics_addr: "127.0.0.1:9090"
  shutdown_timeout_ms: 5000

devices:
  - device_id: "stackchan-s3-main"
    client_id: "stackchan-s3-main-client"
    auth_token_env: "STACKCHAN_MAIN_AUTH_TOKEN"
    default_mode: "auto"

audio:
  uplink_sample_rate_hz: 16000
  downlink_sample_rate_hz: 24000
  channels: 1
  frame_duration_ms: 60
  downlink_queue_ms: 1200
  max_turn_ms: 30000

providers:
  default_profile: "doubao-asr-runtime"
  profiles:
    doubao-asr-runtime:
      asr: "doubao-asr"
      llm: "mock"
      tts: "mock"

stackchan:
  body:
    min_command_gap_ms: 160
    max_commands_per_turn: 16
    yaw_min_deg: -45
    yaw_max_deg: 45
    pitch_min_deg: 0
    pitch_max_deg: 45
    default_speed: 150
  display:
    scene_ttl_ms: 1800
    max_caption_chars: 48

observability:
  trace_jsonl_path: "./var/traces/turns.jsonl"
  redact_secrets: true
`)

	app, err := New(Options{
		ConfigPath: configPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()

	app.server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeReadyResponse(t, recorder.Body.Bytes())
	if response.Ready {
		t.Fatalf("ready = true, body = %s", recorder.Body.String())
	}
	if response.Checks["providers"] != "provider_config_error" {
		t.Fatalf("providers check = %q, want provider_config_error; body = %s", response.Checks["providers"], recorder.Body.String())
	}
	for _, forbidden := range []string{"fake-doubao-key", "decoder", "test-token", "admin-token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("ready response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminProviderProbeUsesRuntimeProviderRegistry(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("MOONSHOT_API_KEY", "")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/moonshot-llm/probe", bytes.NewReader([]byte(`{"modality":"llm","timeout_ms":100}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	app.adminServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusNotFound {
		t.Fatalf("status = 404, admin probe is using mock-only registry; body = %s", recorder.Body.String())
	}
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 provider configuration failure; body = %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) {
		t.Fatalf("response leaked admin token: %s", recorder.Body.String())
	}
}

func TestWebSocketRoutesXiaozhiVoiceLoop(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: mockDefaultExampleConfig(t),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": false},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}

	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "start",
		"mode":  "auto",
	})
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "stop",
	})

	seenSTT := false
	seenTTSStart := false
	seenTTSSentence := false
	seenBinary := false
	seenTTSStop := false
	for i := 0; i < 40 && !seenTTSStop; i++ {
		event := readWebSocketEvent(t, conn)
		switch {
		case event.Kind == "binary":
			seenBinary = true
		case event.Type == xiaozhi.MessageTypeSTT:
			seenSTT = true
		case event.Type == xiaozhi.MessageTypeTTS && event.State == "start":
			seenTTSStart = true
		case event.Type == xiaozhi.MessageTypeTTS && event.State == "sentence_start":
			seenTTSSentence = true
		case event.Type == xiaozhi.MessageTypeTTS && event.State == "stop":
			seenTTSStop = true
		}
	}
	if !seenSTT || !seenTTSStart || !seenTTSSentence || !seenBinary || !seenTTSStop {
		t.Fatalf("voice loop events missing: stt=%t tts_start=%t tts_sentence=%t binary=%t tts_stop=%t", seenSTT, seenTTSStart, seenTTSSentence, seenBinary, seenTTSStop)
	}
}

func TestWebSocketMCPDeviceReceivesHeadMotionOnListenStart(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: mockDefaultExampleConfigWithListenStartMotion(t),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": true},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}

	initialize := readMCPRequest(t, conn, mcp.MethodInitialize, 500*time.Millisecond)
	writeMCPResult(t, conn, initialize, json.RawMessage(`{}`))
	toolsList := readMCPRequest(t, conn, mcp.MethodToolsList, 500*time.Millisecond)
	writeMCPResult(t, conn, toolsList, mcp.ToolsListResult{
		Tools: []mcp.Tool{
			{Name: mcp.ToolSetHeadAngles},
		},
	})

	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "start",
		"mode":  "auto",
	})

	call := readMCPRequest(t, conn, mcp.MethodToolsCall, 2*time.Second)
	var params mcp.ToolCallParams
	if err := json.Unmarshal(call.Params, &params); err != nil {
		t.Fatalf("decode tools/call params: %v", err)
	}
	if params.Name != mcp.ToolSetHeadAngles {
		t.Fatalf("tool name = %q, want %q", params.Name, mcp.ToolSetHeadAngles)
	}
	if params.Arguments["yaw"] != float64(0) || params.Arguments["pitch"] != float64(8) || params.Arguments["speed"] != float64(150) {
		t.Fatalf("head motion arguments = %#v, want safe listening pose", params.Arguments)
	}
	writeMCPResult(t, conn, call, json.RawMessage(`{"ok":true}`))
}

func TestWebSocketMCPDeviceReceivesDisplaySceneOnListenStart(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: mockDefaultExampleConfigWithScreenScene(t),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": true},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}

	initialize := readMCPRequest(t, conn, mcp.MethodInitialize, 500*time.Millisecond)
	writeMCPResult(t, conn, initialize, json.RawMessage(`{}`))
	toolsList := readMCPRequest(t, conn, mcp.MethodToolsList, 500*time.Millisecond)
	writeMCPResult(t, conn, toolsList, mcp.ToolsListResult{
		Tools: []mcp.Tool{
			{Name: mcp.ToolSetHeadAngles},
			{Name: mcp.ToolSetScreenScene},
		},
	})

	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "start",
		"mode":  "auto",
	})

	call := readMCPToolCallByName(t, conn, mcp.ToolSetScreenScene, 2*time.Second)
	if call.Arguments["type"] != "stackchan.scene" || call.Arguments["scene"] != "listening" {
		t.Fatalf("display scene args = %#v, want stackchan listening scene", call.Arguments)
	}
	if call.Arguments["emotion"] != "curious" || call.Arguments["accent"] != "cyan" {
		t.Fatalf("display hints = %#v, want curious/cyan", call.Arguments)
	}
	if call.Arguments["caption"] != "我在听。" || call.Arguments["ttl_ms"] != float64(1800) {
		t.Fatalf("display caption/ttl = %#v, want short listening caption and configured ttl", call.Arguments)
	}
}

func TestWebSocketMCPDeviceReceivesLEDFeedbackOnListenStart(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: mockDefaultExampleConfig(t),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": true},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}

	initialize := readMCPRequest(t, conn, mcp.MethodInitialize, 500*time.Millisecond)
	writeMCPResult(t, conn, initialize, json.RawMessage(`{}`))
	toolsList := readMCPRequest(t, conn, mcp.MethodToolsList, 500*time.Millisecond)
	writeMCPResult(t, conn, toolsList, mcp.ToolsListResult{
		Tools: []mcp.Tool{
			{Name: mcp.ToolSetHeadAngles},
			{Name: mcp.ToolSetScreenScene},
			{Name: mcp.ToolSetLEDColor},
		},
	})

	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "start",
		"mode":  "auto",
	})

	call := readMCPToolCallByName(t, conn, mcp.ToolSetLEDColor, 2*time.Second)
	if call.Arguments["red"] != float64(0) || call.Arguments["green"] != float64(168) || call.Arguments["blue"] != float64(0) {
		t.Fatalf("led arguments = %#v, want green listening/asr feedback", call.Arguments)
	}
}

func TestStackchanSimMockGatewaySuiteRunsAgainstRuntime(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("OPENCLAW_WS_URL", "https://openclaw.example.internal")
	t.Setenv("OPENCLAW_AGENT_TOKEN", "openclaw-token")

	app, err := New(Options{
		ConfigPath: mockAgentBridgeSkipFeedbackExampleConfig(t),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	summary, err := simulator.RunScenario(context.Background(), simulator.ScenarioOptions{
		Scenario:   simulator.ScenarioMockGatewaySuite,
		GatewayURL: "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath,
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Turns:      2,
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || summary.Success != 10 {
		t.Fatalf("summary = %+v, want suite pass with 10 successful turns", summary)
	}
	wantScenarios := []string{
		simulator.ScenarioHappyPath20Turns,
		simulator.ScenarioASRFinalWithoutListenStop,
		simulator.ScenarioAbortDuringTTS,
		simulator.ScenarioProviderSlowFirstAudio,
		simulator.ScenarioWSReconnect,
		simulator.ScenarioMCPHeadMotion,
		simulator.ScenarioMCPDisplayScene,
		simulator.ScenarioMCPLEDFeedback,
		simulator.ScenarioMCPAgentBridgeSkipFeedback,
	}
	if len(summary.Scenarios) != len(wantScenarios) {
		t.Fatalf("suite scenarios = %d, want %d; summary=%+v", len(summary.Scenarios), len(wantScenarios), summary)
	}
	for index, want := range wantScenarios {
		if got := summary.Scenarios[index].Scenario; got != want {
			t.Fatalf("suite scenario[%d] = %q, want %q", index, got, want)
		}
		if !summary.Scenarios[index].Passed {
			t.Fatalf("suite scenario[%d] did not pass: %+v", index, summary.Scenarios[index])
		}
	}
}

func TestStackchanSimProviderProfileCommandRunsAgainstRuntime(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	tracePath := filepath.Join(t.TempDir(), "turns.jsonl")

	app, err := New(Options{
		ConfigPath: mockProviderProfileCommandExampleConfig(t, tracePath),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	summary, err := simulator.RunScenario(context.Background(), simulator.ScenarioOptions{
		Scenario:           simulator.ScenarioProviderProfileSwitch,
		GatewayURL:         "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath,
		DeviceID:           "stackchan-s3-main",
		ClientID:           "stackchan-s3-main-client",
		AuthToken:          "test-token",
		Timeout:            3 * time.Second,
		TraceFile:          tracePath,
		RequireTraceEvents: []string{"speech_final", "provider_profile_command", "turn_complete"},
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || !summary.ProviderProfileSwitch {
		t.Fatalf("summary = %+v, want provider profile switch pass", summary)
	}
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if bytes.Contains(data, []byte(`"event":"llm_request"`)) {
		t.Fatalf("provider profile command reached LLM path: %s", string(data))
	}
	if !bytes.Contains(data, []byte(`"profile":"doubao-dashscope-voice"`)) {
		t.Fatalf("trace missing selected provider profile: %s", string(data))
	}
}

func TestWebSocketVoiceLoopWritesTraceJSONL(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	tracePath := filepath.Join(t.TempDir(), "turns.jsonl")

	app, err := New(Options{
		ConfigPath: mockDefaultExampleConfigWithTrace(t, tracePath),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	startWebSocketSpeakingTurn(t, conn)
	readUntilTTSStop(t, conn, 2*time.Second)

	eventually(t, func() bool {
		data, err := os.ReadFile(tracePath)
		if err != nil {
			return false
		}
		text := string(data)
		return strings.Contains(text, `"event":"hello_received"`) &&
			strings.Contains(text, `"event":"turn_complete"`) &&
			!strings.Contains(text, "test-token")
	})
}

func TestWebSocketNormalCloseClearsActiveSession(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	eventually(t, func() bool {
		_, ok := app.sessions.ActiveSessionForDevice("stackchan-s3-main")
		return ok
	})
	if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		t.Fatalf("write close: %v", err)
	}

	eventually(t, func() bool {
		_, ok := app.sessions.ActiveSessionForDevice("stackchan-s3-main")
		return !ok
	})
}

func TestWebSocketNormalCloseDecrementsActiveSessionMetric(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": false},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}
	if got := metricValue(t, app.metrics.Render(), observability.MetricSessionsActive); got != 1 {
		t.Fatalf("active sessions metric after hello = %v, want 1", got)
	}

	if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		t.Fatalf("write close: %v", err)
	}
	eventually(t, func() bool {
		return metricValue(t, app.metrics.Render(), observability.MetricSessionsActive) == 0
	})
}

func TestShutdownClosesActiveWebSocketVoiceLoops(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": false},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}

	if err := app.shutdownServers(); err != nil {
		t.Fatalf("shutdownServers() error = %v", err)
	}

	waitForWebSocketClose(t, conn, 2*time.Second)
	eventually(t, func() bool {
		return metricValue(t, app.metrics.Render(), observability.MetricSessionsActive) == 0
	})
}

func TestClosedVoiceRuntimeRejectsNewWebSocketConnection(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	app.voiceRuntime.CloseAll(context.Background())

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	waitForWebSocketClose(t, conn, 2*time.Second)
	if _, ok := app.sessions.ActiveSessionForDevice("stackchan-s3-main"); ok {
		t.Fatal("active session exists after voice runtime is closed")
	}
	if got := metricValue(t, app.metrics.Render(), observability.MetricSessionsActive); got != 0 {
		t.Fatalf("active sessions metric after rejected connection = %v, want 0", got)
	}
}

func decodeReadyResponse(t *testing.T, data []byte) struct {
	Ready  bool              `json:"ready"`
	Checks map[string]string `json:"checks"`
} {
	t.Helper()
	var response struct {
		Ready  bool              `json:"ready"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode ready response: %v", err)
	}
	return response
}

func writeGatewayConfig(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/stackchan-gateway.yaml"
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func mockDefaultExampleConfig(t *testing.T) string {
	t.Helper()
	return mockDefaultExampleConfigWithTrace(t, "./var/traces/turns.jsonl")
}

func mockDefaultExampleConfigWithTrace(t *testing.T, tracePath string) string {
	t.Helper()
	configData, err := os.ReadFile("../../configs/stackchan-gateway.example.yaml")
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}
	config := string(configData)
	if !strings.Contains(config, `default_profile: "siliconflow-dashscope-voice"`) {
		t.Fatalf("base config no longer defaults to siliconflow-dashscope-voice")
	}
	config = strings.ReplaceAll(config, `default_profile: "siliconflow-dashscope-voice"`, `default_profile: "cn-low-latency-cascade"`)
	config = strings.ReplaceAll(config, `trace_jsonl_path: "./var/traces/turns.jsonl"`, `trace_jsonl_path: "`+tracePath+`"`)
	return writeGatewayConfig(t, config)
}

func mockDefaultExampleConfigWithScreenScene(t *testing.T) string {
	t.Helper()
	configData, err := os.ReadFile("../../configs/stackchan-gateway.example.yaml")
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}
	config := string(configData)
	replacements := []struct {
		old string
		new string
	}{
		{
			old: `default_profile: "siliconflow-dashscope-voice"`,
			new: `default_profile: "cn-low-latency-cascade"`,
		},
		{
			old: `      - "self.screen.set_theme"`,
			new: `      - "self.screen.set_theme"
      - "self.screen.set_scene"`,
		},
	}
	for _, replacement := range replacements {
		if !strings.Contains(config, replacement.old) {
			t.Fatalf("base config missing expected fragment %q", replacement.old)
		}
		config = strings.Replace(config, replacement.old, replacement.new, 1)
	}
	return writeGatewayConfig(t, config)
}

func mockProviderProfileCommandExampleConfig(t *testing.T, tracePath string) string {
	t.Helper()
	configData, err := os.ReadFile("../../configs/stackchan-gateway.example.yaml")
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}
	config := string(configData)
	replacements := []struct {
		old string
		new string
	}{
		{
			old: `providers:
  default_profile: "siliconflow-dashscope-voice"`,
			new: `providers:
  mock:
    asr_final_text: "切到字节模型。"
  default_profile: "siliconflow-dashscope-voice"`,
		},
		{
			old: `default_profile: "siliconflow-dashscope-voice"`,
			new: `default_profile: "cn-low-latency-cascade"`,
		},
		{
			old: `    doubao-dashscope-voice:
      asr: "dashscope-asr"
      llm: "doubao-llm"
      tts: "dashscope-tts"`,
			new: `    doubao-dashscope-voice:
      asr: "mock"
      llm: "mock"
      tts: "mock"`,
		},
		{
			old: `trace_jsonl_path: "./var/traces/turns.jsonl"`,
			new: `trace_jsonl_path: "` + tracePath + `"`,
		},
	}
	for _, replacement := range replacements {
		if !strings.Contains(config, replacement.old) {
			t.Fatalf("base config missing expected fragment %q", replacement.old)
		}
		config = strings.Replace(config, replacement.old, replacement.new, 1)
	}
	return writeGatewayConfig(t, config)
}

func mockDefaultExampleConfigWithListenStartMotion(t *testing.T) string {
	t.Helper()
	configData, err := os.ReadFile("../../configs/stackchan-gateway.example.yaml")
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}
	config := string(configData)
	replacements := []struct {
		old string
		new string
	}{
		{
			old: `default_profile: "siliconflow-dashscope-voice"`,
			new: `default_profile: "cn-low-latency-cascade"`,
		},
		{
			old: `    listen_start_motion_enabled: false`,
			new: `    listen_start_motion_enabled: true`,
		},
		{
			old: `      - "self.robot.get_head_angles"
      - "self.robot.set_led_color"`,
			new: `      - "self.robot.get_head_angles"
      - "self.robot.set_head_angles"
      - "self.robot.set_led_color"`,
		},
	}
	for _, replacement := range replacements {
		if !strings.Contains(config, replacement.old) {
			t.Fatalf("base config missing expected fragment %q", replacement.old)
		}
		config = strings.Replace(config, replacement.old, replacement.new, 1)
	}
	return writeGatewayConfig(t, config)
}

func mockAgentBridgeSkipFeedbackExampleConfig(t *testing.T) string {
	t.Helper()
	configData, err := os.ReadFile("../../configs/stackchan-gateway.example.yaml")
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}
	config := string(configData)
	replacements := []struct {
		old string
		new string
	}{
		{
			old: `providers:
  default_profile: "siliconflow-dashscope-voice"`,
			new: `providers:
  mock:
    asr_auto_final_on_audio: true
  default_profile: "siliconflow-dashscope-voice"`,
		},
		{
			old: `default_profile: "siliconflow-dashscope-voice"`,
			new: `default_profile: "cn-low-latency-cascade"`,
		},
		{
			old: `agent:
  default_mode: "casual"`,
			new: `agent:
  default_mode: "tool"`,
		},
		{
			old: `  openclaw:
    enabled: false`,
			new: `  openclaw:
    enabled: true`,
		},
		{
			old: `    max_runtime_input_chars: 360
    max_runtime_errors_before_cooldown: 2
    runtime_error_cooldown_ms: 30000

tools:`,
			new: `    max_runtime_input_chars: 1
    max_runtime_errors_before_cooldown: 2
    runtime_error_cooldown_ms: 30000

tools:`,
		},
		{
			old: `    min_command_gap_ms: 320`,
			new: `    min_command_gap_ms: 1`,
		},
		{
			old: `    listen_start_motion_enabled: false`,
			new: `    listen_start_motion_enabled: true`,
		},
		{
			old: `      - "self.robot.get_head_angles"
      - "self.robot.set_led_color"`,
			new: `      - "self.robot.get_head_angles"
      - "self.robot.set_head_angles"
      - "self.robot.set_led_color"`,
		},
		{
			old: `      - "self.screen.set_theme"`,
			new: `      - "self.screen.set_theme"
      - "self.screen.set_scene"`,
		},
		{
			old: `    event_cues: {}`,
			new: `    event_cues:
      agent_route.skipped: "settle"`,
		},
	}
	for _, replacement := range replacements {
		if !strings.Contains(config, replacement.old) {
			t.Fatalf("base config missing expected fragment %q", replacement.old)
		}
		config = strings.Replace(config, replacement.old, replacement.new, 1)
	}
	return writeGatewayConfig(t, config)
}

func TestWebSocketAbortInterruptsSpeakingTurn(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: mockDefaultExampleConfig(t),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": false},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}

	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "start",
		"mode":  "auto",
	})
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "stop",
	})

	waitForWebSocketBinary(t, conn)
	writeJSONMessage(t, conn, map[string]any{"type": "abort"})

	binaryAfterAbort := readUntilTTSStop(t, conn, 450*time.Millisecond)
	if binaryAfterAbort > 2 {
		t.Fatalf("binary frames after abort = %d, want at most 2 old frames", binaryAfterAbort)
	}
}

func TestWebSocketReconnectReplacesPreviousDeviceConnection(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: mockDefaultExampleConfig(t),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(app.server.Handler)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + app.Config().Server.WebsocketPath

	first, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("first Dial() error = %v, response=%v", err, response)
	}
	defer first.Close()

	startWebSocketSpeakingTurn(t, first)
	waitForWebSocketBinary(t, first)
	if _, ok := app.sessions.GetSession("sess_1"); !ok {
		t.Fatal("first session missing before reconnect")
	}

	second, response, err := websocket.DefaultDialer.Dial(wsURL, websocketHeaders("Bearer test-token"))
	if err != nil {
		t.Fatalf("second Dial() error = %v, response=%v", err, response)
	}
	defer second.Close()

	waitForWebSocketClose(t, first, 2*time.Second)
	eventually(t, func() bool {
		_, ok := app.sessions.GetSession("sess_1")
		return !ok
	})
}

func TestAuthenticatorForConfigRejectsClientIDMismatch(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = AuthenticatorForConfig(app.Config()).AuthenticateDevice(context.Background(), xiaozhi.AuthRequest{
		Authorization:   "test-token",
		ProtocolVersion: xiaozhi.BinaryProtocolV1,
		DeviceID:        "stackchan-s3-main",
		ClientID:        "wrong-client",
	})

	if err == nil {
		t.Fatal("AuthenticateDevice() error = nil, want client mismatch rejection")
	}
	var authError *xiaozhi.AuthError
	if !errors.As(err, &authError) {
		t.Fatalf("AuthenticateDevice() error type = %T, want AuthError", err)
	}
	if authError.Status != http.StatusForbidden || authError.Code != "INVALID_CLIENT_ID" {
		t.Fatalf("auth error = %#v, want INVALID_CLIENT_ID forbidden", authError)
	}
	if errText := err.Error(); errText == "" || containsAny(errText, "test-token", "wrong-client") {
		t.Fatalf("auth error leaked sensitive or caller-provided value: %q", errText)
	}
}

func TestAuthenticatorForConfigAcceptsBearerAuthorization(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := AuthenticatorForConfig(app.Config()).AuthenticateDevice(context.Background(), xiaozhi.AuthRequest{
		Authorization:   "Bearer test-token",
		ProtocolVersion: xiaozhi.BinaryProtocolV1,
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
	})

	if err != nil {
		t.Fatalf("AuthenticateDevice() error = %v, want nil", err)
	}
	if result.DeviceID != "stackchan-s3-main" || result.ClientID != "stackchan-s3-main-client" {
		t.Fatalf("auth result = %#v, want configured device identity", result)
	}
}

func TestAuthenticatorForConfigRejectsProtocolVersionMismatch(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = AuthenticatorForConfig(app.Config()).AuthenticateDevice(context.Background(), xiaozhi.AuthRequest{
		Authorization:   "Bearer test-token",
		ProtocolVersion: xiaozhi.BinaryProtocolV3,
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
	})

	if err == nil {
		t.Fatal("AuthenticateDevice() error = nil, want protocol mismatch rejection")
	}
	var authError *xiaozhi.AuthError
	if !errors.As(err, &authError) {
		t.Fatalf("AuthenticateDevice() error type = %T, want AuthError", err)
	}
	if authError.Status != http.StatusBadRequest || authError.Code != "PROTOCOL_VERSION_MISMATCH" {
		t.Fatalf("auth error = %#v, want PROTOCOL_VERSION_MISMATCH bad request", authError)
	}
	if errText := err.Error(); containsAny(errText, "test-token", "stackchan-s3-main-client") {
		t.Fatalf("auth error leaked sensitive or caller-provided value: %q", errText)
	}
}

func TestAuthenticatorForConfigAcceptsConfiguredProtocolVersion(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.Config().Server.WebsocketVersion = xiaozhi.BinaryProtocolV3

	result, err := AuthenticatorForConfig(app.Config()).AuthenticateDevice(context.Background(), xiaozhi.AuthRequest{
		Authorization:   "Bearer test-token",
		ProtocolVersion: xiaozhi.BinaryProtocolV3,
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
	})

	if err != nil {
		t.Fatalf("AuthenticateDevice() error = %v, want nil", err)
	}
	if result.ProtocolVersion != xiaozhi.BinaryProtocolV3 {
		t.Fatalf("protocol version = %d, want v3", result.ProtocolVersion)
	}
}

func TestAuthenticatorForConfigRejectsInvalidBearerAuthorization(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = AuthenticatorForConfig(app.Config()).AuthenticateDevice(context.Background(), xiaozhi.AuthRequest{
		Authorization:   "Bearer wrong-token",
		ProtocolVersion: xiaozhi.BinaryProtocolV1,
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
	})

	if err == nil {
		t.Fatal("AuthenticateDevice() error = nil, want authorization rejection")
	}
	var authError *xiaozhi.AuthError
	if !errors.As(err, &authError) {
		t.Fatalf("AuthenticateDevice() error type = %T, want AuthError", err)
	}
	if authError.Status != http.StatusForbidden || authError.Code != "INVALID_AUTHORIZATION" {
		t.Fatalf("auth error = %#v, want INVALID_AUTHORIZATION forbidden", authError)
	}
	if errText := err.Error(); containsAny(errText, "wrong-token", "test-token", "Bearer") {
		t.Fatalf("auth error leaked sensitive or caller-provided value: %q", errText)
	}
}

func TestAuthenticatorForConfigChecksAuthorizationBeforeClientID(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	app, err := New(Options{
		ConfigPath: "../../configs/stackchan-gateway.example.yaml",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = AuthenticatorForConfig(app.Config()).AuthenticateDevice(context.Background(), xiaozhi.AuthRequest{
		Authorization:   "wrong-token",
		ProtocolVersion: xiaozhi.BinaryProtocolV1,
		DeviceID:        "stackchan-s3-main",
		ClientID:        "wrong-client",
	})

	if err == nil {
		t.Fatal("AuthenticateDevice() error = nil, want authorization rejection")
	}
	var authError *xiaozhi.AuthError
	if !errors.As(err, &authError) {
		t.Fatalf("AuthenticateDevice() error type = %T, want AuthError", err)
	}
	if authError.Status != http.StatusForbidden || authError.Code != "INVALID_AUTHORIZATION" {
		t.Fatalf("auth error = %#v, want INVALID_AUTHORIZATION forbidden", authError)
	}
	if errText := err.Error(); containsAny(errText, "wrong-token", "wrong-client") {
		t.Fatalf("auth error leaked sensitive or caller-provided value: %q", errText)
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if value != "" && strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func metricValue(t *testing.T, rendered string, name string) float64 {
	t.Helper()

	for _, line := range strings.Split(rendered, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[0] != name {
			continue
		}
		value, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			t.Fatalf("parse metric %q value %q: %v", name, parts[1], err)
		}
		return value
	}
	t.Fatalf("metric %q not found in:\n%s", name, rendered)
	return 0
}

func websocketHeaders(authorization string) http.Header {
	headers := http.Header{}
	headers.Set(xiaozhi.HeaderAuthorization, authorization)
	headers.Set(xiaozhi.HeaderProtocolVersion, "1")
	headers.Set(xiaozhi.HeaderDeviceID, "stackchan-s3-main")
	headers.Set(xiaozhi.HeaderClientID, "stackchan-s3-main-client")
	return headers
}

func writeJSONMessage(t *testing.T, conn *websocket.Conn, message map[string]any) {
	t.Helper()

	data, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write json message: %v", err)
	}
}

type websocketEvent struct {
	Kind  string
	Type  string
	State string
}

func readWebSocketEvent(t *testing.T, conn *websocket.Conn) websocketEvent {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if messageType == websocket.BinaryMessage {
		return websocketEvent{Kind: "binary"}
	}
	var envelope struct {
		Type  string `json:"type"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode websocket json %q: %v", string(data), err)
	}
	return websocketEvent{Kind: "json", Type: envelope.Type, State: envelope.State}
}

func readMCPRequest(t *testing.T, conn *websocket.Conn, wantMethod string, timeout time.Duration) mcp.Message {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(time.Until(deadline))); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read mcp request %s: %v", wantMethod, err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var envelope struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode websocket json %q: %v", string(data), err)
		}
		if envelope.Type != xiaozhi.MessageTypeMCP {
			t.Fatalf("downlink type = %q, want mcp for method %s; json=%s", envelope.Type, wantMethod, string(data))
		}
		message, err := mcp.ParseMessage(envelope.Payload)
		if err != nil {
			t.Fatalf("parse MCP payload %s: %v", string(envelope.Payload), err)
		}
		if message.Method != wantMethod {
			t.Fatalf("mcp method = %q, want %q; payload=%s", message.Method, wantMethod, string(envelope.Payload))
		}
		if !message.IsRequest() {
			t.Fatalf("mcp message for %s is not a request: %s", wantMethod, string(envelope.Payload))
		}
		return message
	}
	t.Fatalf("timed out waiting for mcp method %s", wantMethod)
	return mcp.Message{}
}

func writeMCPResult(t *testing.T, conn *websocket.Conn, request mcp.Message, result any) {
	t.Helper()

	if request.ID == nil {
		t.Fatalf("mcp request %s missing id", request.Method)
	}
	response, err := mcp.NewResultResponse(*request.ID, result)
	if err != nil {
		t.Fatalf("NewResultResponse() error = %v", err)
	}
	raw, err := response.Raw()
	if err != nil {
		t.Fatalf("MCP response Raw() error = %v", err)
	}
	writeJSONMessage(t, conn, map[string]any{
		"type":    xiaozhi.MessageTypeMCP,
		"payload": raw,
	})
}

func readMCPToolCallByName(t *testing.T, conn *websocket.Conn, toolName string, timeout time.Duration) mcp.ToolCallParams {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		request := readMCPRequest(t, conn, mcp.MethodToolsCall, time.Until(deadline))
		var params mcp.ToolCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatalf("decode tools/call params: %v", err)
		}
		writeMCPResult(t, conn, request, json.RawMessage(`{"ok":true}`))
		if params.Name == toolName {
			return params
		}
	}
	t.Fatalf("timed out waiting for MCP tool %s", toolName)
	return mcp.ToolCallParams{}
}

func waitForWebSocketBinary(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		event := readWebSocketEventWithDeadline(t, conn, time.Until(deadline))
		if event.Kind == "binary" {
			return
		}
	}
	t.Fatal("timed out waiting for binary websocket message")
}

func readUntilTTSStop(t *testing.T, conn *websocket.Conn, timeout time.Duration) int {
	t.Helper()

	binaryCount := 0
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		event := readWebSocketEventWithDeadline(t, conn, time.Until(deadline))
		if event.Kind == "binary" {
			binaryCount++
			continue
		}
		if event.Type == xiaozhi.MessageTypeTTS && event.State == "stop" {
			return binaryCount
		}
	}
	t.Fatalf("timed out waiting for tts stop after %s; binary_after_abort=%d", timeout, binaryCount)
	return binaryCount
}

func startWebSocketSpeakingTurn(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	writeJSONMessage(t, conn, map[string]any{
		"type":      "hello",
		"version":   1,
		"features":  map[string]any{"mcp": false},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	first := readWebSocketEvent(t, conn)
	if first.Kind != "json" || first.Type != xiaozhi.MessageTypeHello {
		t.Fatalf("first event = %+v, want server hello", first)
	}
	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "start",
		"mode":  "auto",
	})
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	writeJSONMessage(t, conn, map[string]any{
		"type":  "listen",
		"state": "stop",
	})
}

func waitForWebSocketClose(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(time.Until(deadline))); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return
		}
		t.Fatalf("read websocket close: %v", err)
	}
	t.Fatalf("timed out waiting for websocket close after %s", timeout)
}

func readWebSocketEventWithDeadline(t *testing.T, conn *websocket.Conn, timeout time.Duration) websocketEvent {
	t.Helper()

	if timeout <= 0 {
		timeout = time.Millisecond
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if messageType == websocket.BinaryMessage {
		return websocketEvent{Kind: "binary"}
	}
	var envelope struct {
		Type  string `json:"type"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode websocket json %q: %v", string(data), err)
	}
	return websocketEvent{Kind: "json", Type: envelope.Type, State: envelope.State}
}

func eventually(t *testing.T, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before deadline")
}
