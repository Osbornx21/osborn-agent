package search

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicetools "stackchan-gateway/internal/tools"
)

func TestRegisterWebSearchToolReturnsBoundedSafeResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body WebSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.SessionID != "session-1" || body.DeviceID != "stackchan-s3-main" || body.Query != "StackChan docs" || body.MaxResults != 2 {
			t.Fatalf("request body = %+v", body)
		}
		if len(body.AllowedDomains) != 1 || body.AllowedDomains[0] != "docs.m5stack.com" {
			t.Fatalf("allowed domains = %v", body.AllowedDomains)
		}
		w.Header().Set("Content-Type", "application/json")
		longTitle := strings.Repeat("题", defaultMaxTitleRunes+10)
		longSnippet := strings.Repeat("摘", defaultMaxSnippetRunes+10)
		_ = json.NewEncoder(w).Encode(WebSearchResponse{
			Provider: strings.Repeat("p", 80),
			Results: []WebSearchResult{
				{Title: longTitle, URL: "https://docs.m5stack.com/zh_CN/StackChan/", Snippet: longSnippet, SourceDomain: "docs.m5stack.com", PublishedAt: "2026-06-06T00:00:00+08:00"},
				{Title: "evil", URL: "https://evil.example/x", Snippet: "must not leak", SourceDomain: "evil.example"},
				{Title: "bad", URL: "file:///tmp/secret", Snippet: "must not leak"},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL, Token: "search-secret"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{WebSearchToolName},
		AllowedPermissions: []string{servicetools.PermissionExternal},
	})
	if err := RegisterWebSearchTool(registry, WebSearchToolOptions{
		Client:         client,
		MaxResults:     2,
		MaxQueryRunes:  24,
		AllowedDomains: []string{"docs.m5stack.com"},
	}); err != nil {
		t.Fatalf("RegisterWebSearchTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		SessionID: "session-1",
		DeviceID:  "stackchan-s3-main",
		Name:      WebSearchToolName,
		Arguments: map[string]any{
			"query":       "StackChan docs",
			"max_results": float64(99),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.SafeSummary != "results=1" {
		t.Fatalf("safe summary = %q, want results=1", result.SafeSummary)
	}
	var payload WebSearchPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.ResultCount != 1 || len(payload.Results) != 1 {
		t.Fatalf("payload = %+v, want one allowed result", payload)
	}
	if len([]rune(payload.Provider)) != 40 || len([]rune(payload.Results[0].Title)) != defaultMaxTitleRunes || len([]rune(payload.Results[0].Snippet)) != defaultMaxSnippetRunes {
		t.Fatalf("payload was not bounded: %+v", payload)
	}
	for _, forbidden := range []string{"evil", "must not leak", "file:///tmp/secret"} {
		if bytes.Contains(result.Payload, []byte(forbidden)) {
			t.Fatalf("payload leaked %q: %s", forbidden, string(result.Payload))
		}
	}
}

func TestRegisterWebSearchToolRejectsMissingAndTooLongQuery(t *testing.T) {
	client, err := NewClient(ClientOptions{BaseURL: "https://search.example.internal", Token: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{WebSearchToolName},
		AllowedPermissions: []string{servicetools.PermissionExternal},
	})
	if err := RegisterWebSearchTool(registry, WebSearchToolOptions{Client: client, MaxQueryRunes: 4}); err != nil {
		t.Fatalf("RegisterWebSearchTool() error = %v", err)
	}

	if _, err := registry.ExecuteTool(context.Background(), servicetools.Call{Name: WebSearchToolName}); servicetools.ErrorCode(err) != servicetools.ErrorCodeToolFailed {
		t.Fatalf("missing query error code = %q, want tool failed", servicetools.ErrorCode(err))
	}
	if _, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      WebSearchToolName,
		Arguments: map[string]any{"query": "12345"},
	}); servicetools.ErrorCode(err) != servicetools.ErrorCodeToolFailed {
		t.Fatalf("too-long query error code = %q, want tool failed", servicetools.ErrorCode(err))
	}
}
