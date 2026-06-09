package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"stackchan-gateway/internal/agents"
)

func TestAdminAgentBridgeCatalogReturnsSafeBridgeStatuses(t *testing.T) {
	bridges := agents.NewBridgeCatalogStore([]agents.BridgeStatus{
		{
			Bridge:              agents.BridgeV21,
			Enabled:             true,
			RequiredMode:        agents.ModeProfessional,
			Invocation:          agents.BridgeInvocationServiceTool,
			ServiceTool:         agents.V21VoiceQueryToolName,
			BoundedSpokenOutput: true,
		},
		{
			Bridge:                         agents.BridgeHermes,
			Enabled:                        true,
			RequiredMode:                   agents.ModeRoleplay,
			Invocation:                     agents.BridgeInvocationRuntimeRoute,
			RuntimeRoute:                   true,
			ToolIntents:                    true,
			AllowedToolIntents:             []string{"memory.lookup", "stackchan.express"},
			MaxToolIntents:                 1,
			MaxRuntimeRoutesPerMinute:      12,
			MaxRuntimeInputChars:           360,
			MaxRuntimeErrorsBeforeCooldown: 2,
			RuntimeErrorCooldownMS:         30000,
			FallbackOnError:                true,
			FallbackOnEmpty:                true,
			BoundedSpokenOutput:            true,
		},
	})
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:   "admin-token",
		AgentBridges: bridges,
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-bridges", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"count":2`,
		`"bridge":"hermes"`,
		`"required_mode":"roleplay"`,
		`"invocation":"runtime_route"`,
		`"runtime_route":true`,
		`"tool_intents":true`,
		`"allowed_tool_intents":["memory.lookup","stackchan.express"]`,
		`"max_tool_intents":1`,
		`"max_runtime_routes_per_minute":12`,
		`"max_runtime_input_chars":360`,
		`"max_runtime_errors_before_cooldown":2`,
		`"runtime_error_cooldown_ms":30000`,
		`"fallback_on_error":true`,
		`"fallback_on_empty_response":true`,
		`"bridge":"v21"`,
		`"required_mode":"professional"`,
		`"invocation":"service_tool"`,
		`"service_tool":"v21.voice_query"`,
		`"bounded_spoken_output":true`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "V21_ADAPTER", "HERMES_AGENT", "OPENCLAW", "http://", "https://", "secret", "token"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminAgentBridgeCatalogRequiresConfiguredCatalog(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-bridges", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"AGENT_BRIDGE_CATALOG_NOT_CONFIGURED"`)) {
		t.Fatalf("response = %s, want safe bridge catalog error", recorder.Body.String())
	}
}
