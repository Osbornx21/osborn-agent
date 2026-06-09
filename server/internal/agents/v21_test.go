package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterBlocksV21OutsideProfessionalMode(t *testing.T) {
	called := false
	router := NewRouter(RouterOptions{
		V21: agentBridgeFunc(func(context.Context, RouteRequest) (RouteResponse, error) {
			called = true
			return RouteResponse{}, nil
		}),
	})

	for _, mode := range []Mode{ModeCasual, ModeRoleplay} {
		t.Run(string(mode), func(t *testing.T) {
			response, err := router.Route(context.Background(), RouteRequest{
				Mode:        mode,
				Destination: DestinationV21,
				Text:        "慢充预约怎么设置？",
			})

			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if !response.Blocked || response.Reason != ReasonV21RequiresProfessionalMode {
				t.Fatalf("response = %+v, want professional-mode block", response)
			}
			if called {
				t.Fatal("blocked V21 route reached bridge")
			}
		})
	}
}

func TestRouterCallsV21InProfessionalModeWithToken(t *testing.T) {
	v21Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != V21VoiceQueryPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, V21VoiceQueryPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer v21-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var request V21VoiceQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode V21 request: %v", err)
		}
		if request.Question != "慢充预约怎么设置？" || len(request.CollectionIDs) != 1 || request.CollectionIDs[0] != "col_vehicle" {
			t.Fatalf("V21 request = %+v, want professional knowledge query", request)
		}
		if request.Mode != V21ModeGroundedQA || request.ResponseStyle != V21ResponseStyleShortSpoken || !request.RequireCitations {
			t.Fatalf("V21 policy fields = %+v, want grounded short spoken citations", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query_run_id":"qry_1",
			"answer_type":"GROUNDED_ANSWER",
			"spoken_answer":"慢充预约支持设置开始时间和结束时间。",
			"full_answer":"慢充预约支持设置开始时间和结束时间。[1]",
			"citations":[{"anchor_id":"ca_1","asset_version_id":"av_1","source_unit_id":"su_1","source_label":"充放电基线 PDF p12"}],
			"evidence":[{"chunk_id":"chk_1","anchor_id":"ca_1","version_id":"av_1","source_unit_id":"su_1","source_label":"充放电基线 PDF p12","excerpt":"慢充预约功能支持用户设置开始时间和结束时间。","score":0.91}],
			"confidence":0.91,
			"safe_to_style_wrap":false,
			"policy":{"require_citations":true,"allow_style_wrap":false,"fact_locked":true},
			"tool_results":[],
			"latency_ms":{"total":12},
			"trace_id":"trace_1"
		}`))
	}))
	defer v21Server.Close()
	router := NewRouter(RouterOptions{
		V21: NewV21Client(V21ClientOptions{
			BaseURL: v21Server.URL,
			Token:   "v21-token",
			Client:  v21Server.Client(),
		}),
	})

	response, err := router.Route(context.Background(), RouteRequest{
		Mode:          ModeProfessional,
		Destination:   DestinationV21,
		Text:          " 慢充预约怎么设置？ ",
		CollectionIDs: []string{"col_vehicle"},
		SessionID:     "sess_1",
		TurnID:        "turn_1",
		TraceID:       "trace_1",
	})

	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if response.Blocked || response.Text != "慢充预约支持设置开始时间和结束时间。" {
		t.Fatalf("response = %+v, want spoken V21 answer", response)
	}
	if response.OutputTarget != OutputTargetGatewayTTS {
		t.Fatalf("output target = %q, want gateway TTS", response.OutputTarget)
	}
	if response.V21 == nil || response.V21.QueryRunID != "qry_1" || len(response.V21.Evidence) != 1 {
		t.Fatalf("V21 payload = %+v, want evidence pack", response.V21)
	}
}

func TestRouterRunsOpenClawAsTextForGatewayTTS(t *testing.T) {
	router := NewRouter(RouterOptions{
		OpenClaw: agentBridgeFunc(func(_ context.Context, request RouteRequest) (RouteResponse, error) {
			if request.Destination != DestinationOpenClaw || request.Text != "进入工况分析" {
				t.Fatalf("request = %+v, want explicit OpenClaw route text", request)
			}
			return RouteResponse{
				Text:         "我会按工况分析继续。",
				OutputTarget: OutputTargetGatewayTTS,
			}, nil
		}),
	})

	response, err := router.Route(context.Background(), RouteRequest{
		Mode:        ModeTool,
		Destination: DestinationOpenClaw,
		Text:        "进入工况分析",
	})

	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if response.Text != "我会按工况分析继续。" || response.OutputTarget != OutputTargetGatewayTTS {
		t.Fatalf("response = %+v, want text handed back to gateway TTS", response)
	}
}

func TestOpenClawClientReturnsTextForGatewayTTSAndToolCalls(t *testing.T) {
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != OpenClawRespondPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, OpenClawRespondPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer openclaw-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var request OpenClawRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode OpenClaw request: %v", err)
		}
		if request.Text != "帮我进入工具分析" || request.DeviceID != "stackchan-s3-main" || request.SessionID != "sess_1" || request.TurnID != "7" {
			t.Fatalf("OpenClaw request = %+v, want routed voice turn", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text":"我会调用工具继续处理这个请求。",
			"tool_intents":[{"Tool":"homeassistant.call_action","Args":{"action_id":"desk_light_on"}}]
		}`))
	}))
	defer openClawServer.Close()

	client := NewOpenClawClient(OpenClawClientOptions{
		BaseURL:        openClawServer.URL,
		Token:          "openclaw-token",
		MaxSpokenChars: 8,
		Client:         openClawServer.Client(),
	})
	response, err := client.Route(context.Background(), RouteRequest{
		Mode:        ModeTool,
		Destination: DestinationOpenClaw,
		Text:        "帮我进入工具分析",
		DeviceID:    "stackchan-s3-main",
		SessionID:   "sess_1",
		TurnID:      "7",
	})

	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if response.OutputTarget != OutputTargetGatewayTTS || response.Text != "我会调用工具继续" {
		t.Fatalf("response = %+v, want truncated text for gateway TTS", response)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "homeassistant.call_action" || response.ToolCalls[0].Arguments["action_id"] != "desk_light_on" {
		t.Fatalf("tool calls = %+v, want gateway-owned tool call", response.ToolCalls)
	}
}

func TestHermesClientReturnsTextForGatewayTTSAndToolCalls(t *testing.T) {
	hermesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != HermesRespondPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, HermesRespondPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer hermes-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var request HermesRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode Hermes request: %v", err)
		}
		if request.Text != "按角色继续回应" || request.DeviceID != "stackchan-s3-main" || request.SessionID != "sess_1" || request.TurnID != "9" {
			t.Fatalf("Hermes request = %+v, want routed voice turn", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text":"我会保持这个角色并继续。",
			"tool_intents":[{"tool":"memory.lookup","args":{"query":"角色偏好"}}]
		}`))
	}))
	defer hermesServer.Close()

	client := NewHermesClient(HermesClientOptions{
		BaseURL:        hermesServer.URL,
		Token:          "hermes-token",
		MaxSpokenChars: 8,
		Client:         hermesServer.Client(),
	})
	response, err := client.Route(context.Background(), RouteRequest{
		Mode:        ModeRoleplay,
		Destination: DestinationHermes,
		Text:        "按角色继续回应",
		DeviceID:    "stackchan-s3-main",
		SessionID:   "sess_1",
		TurnID:      "9",
	})

	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if response.OutputTarget != OutputTargetGatewayTTS || response.Text != "我会保持这个角色" {
		t.Fatalf("response = %+v, want truncated text for gateway TTS", response)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "memory.lookup" || response.ToolCalls[0].Arguments["query"] != "角色偏好" {
		t.Fatalf("tool calls = %+v, want gateway-owned tool call", response.ToolCalls)
	}
}

func TestOpenClawClientErrorsDoNotLeakTokenOrBody(t *testing.T) {
	openClawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token openclaw-token"}`))
	}))
	defer openClawServer.Close()
	client := NewOpenClawClient(OpenClawClientOptions{
		BaseURL: openClawServer.URL,
		Token:   "openclaw-token",
		Client:  openClawServer.Client(),
	})

	_, err := client.Respond(context.Background(), OpenClawRequest{Text: "分析一下"})

	if err == nil {
		t.Fatal("Respond() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "openclaw-token") || strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error leaked token or provider body: %v", err)
	}
}

func TestHermesClientErrorsDoNotLeakTokenOrBody(t *testing.T) {
	hermesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token hermes-token"}`))
	}))
	defer hermesServer.Close()
	client := NewHermesClient(HermesClientOptions{
		BaseURL: hermesServer.URL,
		Token:   "hermes-token",
		Client:  hermesServer.Client(),
	})

	_, err := client.Respond(context.Background(), HermesRequest{Text: "继续回应"})

	if err == nil {
		t.Fatal("Respond() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "hermes-token") || strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error leaked token or provider body: %v", err)
	}
}

func TestClaudeAndHermesToolIntentsBecomeGatewayToolCalls(t *testing.T) {
	claudeCalls := ClaudeToolIntentsToGatewayToolCalls([]ClaudeToolIntent{
		{Name: "homeassistant.call_action", Input: map[string]any{"action_id": "desk_light_on"}},
	})
	if len(claudeCalls) != 1 || claudeCalls[0].Name != "homeassistant.call_action" || claudeCalls[0].Arguments["action_id"] != "desk_light_on" {
		t.Fatalf("claude calls = %+v, want gateway-owned tool call", claudeCalls)
	}

	hermesCalls := HermesToolIntentsToGatewayToolCalls([]HermesToolIntent{
		{Tool: "memory.lookup", Args: map[string]any{"query": "称呼"}},
	})
	if len(hermesCalls) != 1 || hermesCalls[0].Name != "memory.lookup" || hermesCalls[0].Arguments["query"] != "称呼" {
		t.Fatalf("hermes calls = %+v, want gateway-owned tool call", hermesCalls)
	}
}

func TestV21ClientErrorsDoNotLeakTokenOrBody(t *testing.T) {
	v21Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad token v21-token"}}`))
	}))
	defer v21Server.Close()
	client := NewV21Client(V21ClientOptions{
		BaseURL: v21Server.URL,
		Token:   "v21-token",
		Client:  v21Server.Client(),
	})

	_, err := client.Query(context.Background(), V21VoiceQueryRequest{
		CollectionIDs: []string{"col_vehicle"},
		Question:      "慢充预约怎么设置？",
	})

	if err == nil {
		t.Fatal("Query() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "v21-token") || strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error leaked token or provider body: %v", err)
	}
}

type agentBridgeFunc func(context.Context, RouteRequest) (RouteResponse, error)

func (f agentBridgeFunc) Route(ctx context.Context, request RouteRequest) (RouteResponse, error) {
	return f(ctx, request)
}
