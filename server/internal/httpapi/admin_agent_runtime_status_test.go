package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/agents"
)

func TestAdminAgentRuntimeStatusReturnsSafePerDeviceReasons(t *testing.T) {
	modes := agents.NewModeStore(agents.ModeCasual, []string{"stackchan-s3-main"})
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", agents.ModeProfessional); err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}
	status := agents.NewRuntimeStatusStore(modes, agents.NewBridgeCatalogStore([]agents.BridgeStatus{
		{
			Bridge:                         agents.BridgeHermes,
			Enabled:                        false,
			RequiredMode:                   agents.ModeRoleplay,
			Invocation:                     agents.BridgeInvocationRuntimeRoute,
			ToolIntents:                    true,
			AllowedToolIntents:             []string{"memory.lookup"},
			MaxToolIntents:                 1,
			MaxRuntimeRoutesPerMinute:      12,
			MaxRuntimeInputChars:           360,
			MaxRuntimeErrorsBeforeCooldown: 2,
			RuntimeErrorCooldownMS:         30000,
			FallbackOnError:                true,
			FallbackOnEmpty:                true,
			BoundedSpokenOutput:            true,
		},
		{
			Bridge:              agents.BridgeV21,
			Enabled:             true,
			RequiredMode:        agents.ModeProfessional,
			Invocation:          agents.BridgeInvocationServiceTool,
			ServiceTool:         agents.V21VoiceQueryToolName,
			BoundedSpokenOutput: true,
		},
	}))
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:         "admin-token",
		AgentRuntimeStatus: status,
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-runtime-status", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"count":1`,
		`"bridge_count":2`,
		`"device_id":"stackchan-s3-main"`,
		`"active_mode":"professional"`,
		`"bridge":"hermes"`,
		`"available":false`,
		`"reason":"bridge_disabled"`,
		`"allowed_tool_intents":["memory.lookup"]`,
		`"max_tool_intents":1`,
		`"max_runtime_routes_per_minute":12`,
		`"max_runtime_input_chars":360`,
		`"max_runtime_errors_before_cooldown":2`,
		`"runtime_error_cooldown_ms":30000`,
		`"fallback_on_error":true`,
		`"fallback_on_empty_response":true`,
		`"bridge":"v21"`,
		`"available":true`,
		`"reason":"available"`,
		`"service_tool":"v21.voice_query"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "V21_ADAPTER", "HERMES_AGENT", "OPENCLAW", "http://", "https://", "secret", "token", "prompt", "persona"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminAgentRuntimeStatusRequiresConfiguredStatus(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-runtime-status", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"AGENT_RUNTIME_STATUS_NOT_CONFIGURED"`)) {
		t.Fatalf("response = %s, want safe runtime status error", recorder.Body.String())
	}
}
