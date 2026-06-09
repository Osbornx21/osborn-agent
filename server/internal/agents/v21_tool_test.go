package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	servicetools "stackchan-gateway/internal/tools"
)

func TestV21ServiceToolBlocksOutsideProfessionalMode(t *testing.T) {
	called := false
	router := NewRouter(RouterOptions{
		V21: agentBridgeFunc(func(context.Context, RouteRequest) (RouteResponse, error) {
			called = true
			return RouteResponse{}, nil
		}),
	})
	store := NewModeStore(ModeCasual, []string{"stackchan-s3-main"})
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{V21VoiceQueryToolName},
		AllowedPermissions: []string{servicetools.PermissionExternal},
	})
	if err := RegisterV21VoiceQueryTool(registry, V21VoiceQueryToolOptions{
		Router:               router,
		Modes:                store,
		AllowedCollectionIDs: []string{"col_vehicle"},
	}); err != nil {
		t.Fatalf("RegisterV21VoiceQueryTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		DeviceID: "stackchan-s3-main",
		Name:     V21VoiceQueryToolName,
		Arguments: map[string]any{
			"question":      "慢充预约怎么设置？",
			"collection_id": "col_vehicle",
		},
	})

	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if called {
		t.Fatal("blocked V21 service tool reached bridge")
	}
	if !bytes.Contains(result.Payload, []byte(`"blocked":true`)) || !bytes.Contains(result.Payload, []byte(ReasonV21RequiresProfessionalMode)) {
		t.Fatalf("payload = %s, want professional-mode block", string(result.Payload))
	}
}

func TestV21ServiceToolCallsBridgeInProfessionalMode(t *testing.T) {
	v21Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request V21VoiceQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode V21 request: %v", err)
		}
		if request.Question != "慢充预约怎么设置？" || len(request.CollectionIDs) != 1 || request.CollectionIDs[0] != "col_vehicle" {
			t.Fatalf("V21 request = %+v, want allowlisted collection and question", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query_run_id":"qry_1",
			"answer_type":"GROUNDED_ANSWER",
			"spoken_answer":"慢充预约支持设置开始时间和结束时间。",
			"full_answer":"full answer must not leak into tool payload",
			"citations":[{"anchor_id":"ca_1","asset_version_id":"av_1","source_unit_id":"su_1","source_label":"充放电基线 PDF p12","excerpt":"citation excerpt must not leak"}],
			"evidence":[{"chunk_id":"chk_1","anchor_id":"ca_1","version_id":"av_1","source_unit_id":"su_1","source_label":"充放电基线 PDF p12","excerpt":"evidence excerpt must not leak","score":0.91}],
			"confidence":0.91,
			"safe_to_style_wrap":false,
			"policy":{"require_citations":true,"allow_style_wrap":false,"fact_locked":true},
			"tool_results":[]
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
	store := NewModeStore(ModeCasual, []string{"stackchan-s3-main"})
	if _, err := store.SetDeviceMode(context.Background(), "stackchan-s3-main", ModeProfessional); err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{V21VoiceQueryToolName},
		AllowedPermissions: []string{servicetools.PermissionExternal},
	})
	if err := RegisterV21VoiceQueryTool(registry, V21VoiceQueryToolOptions{
		Router:               router,
		Modes:                store,
		AllowedCollectionIDs: []string{"col_vehicle"},
	}); err != nil {
		t.Fatalf("RegisterV21VoiceQueryTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		SessionID: "sess_1",
		DeviceID:  "stackchan-s3-main",
		Name:      V21VoiceQueryToolName,
		Arguments: map[string]any{
			"question":      "慢充预约怎么设置？",
			"collection_id": "col_vehicle",
		},
	})

	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	var payload V21VoiceQueryToolPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Blocked || payload.SpokenAnswer != "慢充预约支持设置开始时间和结束时间。" || payload.AnswerType != "GROUNDED_ANSWER" {
		t.Fatalf("payload = %+v, want safe spoken answer", payload)
	}
	if payload.CitationCount != 1 || payload.EvidenceCount != 1 || len(payload.SourceLabels) != 1 || payload.SourceLabels[0] != "充放电基线 PDF p12" {
		t.Fatalf("payload source fields = %+v, want bounded source metadata", payload)
	}
	for _, forbidden := range []string{"full answer must not leak", "citation excerpt must not leak", "evidence excerpt must not leak"} {
		if bytes.Contains(result.Payload, []byte(forbidden)) {
			t.Fatalf("payload leaked %q: %s", forbidden, string(result.Payload))
		}
	}
}

func TestV21ServiceToolRejectsDisallowedCollectionBeforeBridge(t *testing.T) {
	called := false
	router := NewRouter(RouterOptions{
		V21: agentBridgeFunc(func(context.Context, RouteRequest) (RouteResponse, error) {
			called = true
			return RouteResponse{}, nil
		}),
	})
	store := NewModeStore(ModeProfessional, []string{"stackchan-s3-main"})
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{V21VoiceQueryToolName},
		AllowedPermissions: []string{servicetools.PermissionExternal},
	})
	if err := RegisterV21VoiceQueryTool(registry, V21VoiceQueryToolOptions{
		Router:               router,
		Modes:                store,
		AllowedCollectionIDs: []string{"col_vehicle"},
	}); err != nil {
		t.Fatalf("RegisterV21VoiceQueryTool() error = %v", err)
	}

	_, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		DeviceID: "stackchan-s3-main",
		Name:     V21VoiceQueryToolName,
		Arguments: map[string]any{
			"question":      "查一下内部资料",
			"collection_id": "secret_collection",
		},
	})

	if code := servicetools.ErrorCode(err); code != servicetools.ErrorCodeToolFailed {
		t.Fatalf("error code = %q, want %q", code, servicetools.ErrorCodeToolFailed)
	}
	if called {
		t.Fatal("disallowed collection reached bridge")
	}
}
