package moonshot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"stackchan-gateway/internal/providers"
)

func TestLLMMetadataMatchesOfficialDocs(t *testing.T) {
	provider := NewLLM(LLMOptions{
		APIKey: "moonshot-key",
		Model:  "moonshot-v1-8k",
	})

	if provider.ProviderID() != ProviderIDLLM {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDLLM)
	}
	if provider.ModelID() != "moonshot-v1-8k" {
		t.Fatalf("ModelID() = %q", provider.ModelID())
	}
	if provider.SourceDocURL() != SourceDocURLLLM {
		t.Fatalf("SourceDocURL() = %q, want %q", provider.SourceDocURL(), SourceDocURLLLM)
	}
	if provider.SourceDocCheckedAt() != SourceDocCheckedAt {
		t.Fatalf("SourceDocCheckedAt() = %q, want %q", provider.SourceDocCheckedAt(), SourceDocCheckedAt)
	}
}

func TestRegisterLLMAddsFactoryToProviderRegistry(t *testing.T) {
	registry := providers.NewRegistry(providers.MockConfig{})
	RegisterLLM(registry, LLMOptions{
		APIKey: "moonshot-key",
		Model:  "moonshot-v1-8k",
	})

	provider, err := registry.LLMProvider(ProviderIDLLM)
	if err != nil {
		t.Fatalf("LLMProvider(%s) error = %v", ProviderIDLLM, err)
	}
	if _, ok := provider.(*LLM); !ok {
		t.Fatalf("registered provider type = %T, want *LLM", provider)
	}
}

func TestLLMBuildsOpenAICompatibleStreamingRequestWithVoiceSafeDefaults(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { requestSeen <- struct{}{} }()

		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer moonshot-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "moonshot-v1-8k" {
			t.Fatalf("model = %v", body["model"])
		}
		if body["stream"] != true {
			t.Fatalf("stream = %v, want true", body["stream"])
		}
		if body["max_completion_tokens"] != float64(160) {
			t.Fatalf("max_completion_tokens = %v", body["max_completion_tokens"])
		}
		if _, ok := body["max_tokens"]; ok {
			t.Fatalf("request used deprecated max_tokens: %#v", body)
		}
		if _, ok := body["thinking"]; ok {
			t.Fatalf("default moonshot-v1 request must not send Kimi thinking: %#v", body)
		}
		if messages, ok := body["messages"].([]any); !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v, want one user message", body["messages"])
		}
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %#v, want one function tool", body["tools"])
		}
		tool, ok := tools[0].(map[string]any)
		if !ok || tool["type"] != "function" {
			t.Fatalf("tool = %#v, want function tool", tools[0])
		}
		function, ok := tool["function"].(map[string]any)
		if !ok || function["name"] != "memory_lookup" {
			t.Fatalf("function = %#v, want memory_lookup", tool["function"])
		}
		parameters, ok := function["parameters"].(map[string]any)
		if !ok || parameters["type"] != "object" {
			t.Fatalf("parameters = %#v, want object schema", function["parameters"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewLLM(LLMOptions{
		BaseURL: server.URL + "/v1",
		APIKey:  "moonshot-key",
		Model:   "moonshot-v1-8k",
		Client:  server.Client(),
	})

	chunks, err := provider.Stream(context.Background(), providers.LLMRequest{
		Text: "hello",
		Tools: []providers.LLMTool{{
			Name:        "memory_lookup",
			Description: "Look up scoped memory.",
			InputSchema: map[string]any{
				"type": "object",
			},
		}},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	for range chunks {
	}

	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("server did not receive request")
	}
}

func TestLLMParsesStreamingToolCallDeltasAfterArgumentsComplete(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-memory","type":"function","function":{"name":"memory_lookup","arguments":"{\"query\":\"低"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"延迟\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}, "\n")

	chunks, err := ParseLLMStream(bufio.NewReader(strings.NewReader(stream)))

	if err != nil {
		t.Fatalf("ParseLLMStream() error = %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks len = %d, want one final tool-call chunk: %+v", len(chunks), chunks)
	}
	if !chunks[0].IsFinal || len(chunks[0].ToolCalls) != 1 {
		t.Fatalf("chunk = %+v, want final chunk with one tool call", chunks[0])
	}
	call := chunks[0].ToolCalls[0]
	if call.ID != "call-memory" || call.Name != "memory_lookup" || call.Arguments["query"] != "低延迟" {
		t.Fatalf("tool call = %+v, want assembled memory lookup arguments", call)
	}
}

func TestLLMParsesSSEFixturesAndSkipsReasoningOrUsageOnlyChunks(t *testing.T) {
	stream := strings.Join([]string{
		readFixture(t, "sse_first_chunk.sse"),
		readFixture(t, "sse_reasoning_content_delta_chunk.sse"),
		readFixture(t, "sse_delta_chunk.sse"),
		readFixture(t, "sse_usage_before_done_chunk.sse"),
		readFixture(t, "sse_finish_chunk.sse"),
	}, "\n")

	chunks, err := ParseLLMStream(bufio.NewReader(strings.NewReader(stream)))
	if err != nil {
		t.Fatalf("ParseLLMStream() error = %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("chunks len = %d, want 3", len(chunks))
	}
	if chunks[0].Text != "Hi" || chunks[0].IsFinal {
		t.Fatalf("first chunk = %+v", chunks[0])
	}
	if chunks[1].Text != "." || chunks[1].IsFinal {
		t.Fatalf("delta chunk = %+v", chunks[1])
	}
	if chunks[2].Text != "" || !chunks[2].IsFinal {
		t.Fatalf("finish chunk = %+v", chunks[2])
	}
}

func TestLLMMapsProviderErrorsWithoutLeakingSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "super-secret") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_authentication_error","message":"bad key super-secret-moonshot-key"}}`))
			return
		}
		t.Fatalf("unexpected Authorization = %q", r.Header.Get("Authorization"))
	}))
	defer server.Close()

	provider := NewLLM(LLMOptions{
		BaseURL: server.URL + "/v1",
		APIKey:  "super-secret-moonshot-key",
		Model:   "moonshot-v1-8k",
		Client:  server.Client(),
	})

	_, err := provider.Stream(context.Background(), providers.LLMRequest{Text: "hello"})
	if err == nil {
		t.Fatal("Stream() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "super-secret-moonshot-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if providerErr.StatusCode != http.StatusUnauthorized || providerErr.Code != "invalid_authentication_error" {
		t.Fatalf("providerErr = %+v", providerErr)
	}
}

func TestLLMContextCancelClosesStream(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer is not a flusher")
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewLLM(LLMOptions{
		BaseURL: server.URL + "/v1",
		APIKey:  "moonshot-key",
		Model:   "moonshot-v1-8k",
		Client:  server.Client(),
	})

	chunks, err := provider.Stream(ctx, providers.LLMRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	select {
	case <-chunks:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first chunk")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not start streaming")
	}

	cancel()

	select {
	case _, ok := <-chunks:
		if ok {
			t.Fatal("chunk channel still open after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream close after cancel")
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("testdata/llm/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}
