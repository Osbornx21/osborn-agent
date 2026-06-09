package dashscope

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
		APIKey: "fake-dashscope-key",
		Model:  "qwen3.6-plus",
	})

	if provider.ProviderID() != ProviderIDLLM {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDLLM)
	}
	if provider.ModelID() != "qwen3.6-plus" {
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
		APIKey: "fake-dashscope-key",
		Model:  "qwen3.6-plus",
	})

	provider, err := registry.LLMProvider(ProviderIDLLM)
	if err != nil {
		t.Fatalf("LLMProvider(%s) error = %v", ProviderIDLLM, err)
	}
	if _, ok := provider.(*LLM); !ok {
		t.Fatalf("registered provider type = %T, want *LLM", provider)
	}
}

func TestLLMBuildsOpenAICompatibleStreamingRequest(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { requestSeen <- struct{}{} }()

		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/compatible-mode/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-dashscope-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "qwen3.6-plus" {
			t.Fatalf("model = %v", body["model"])
		}
		if body["stream"] != true {
			t.Fatalf("stream = %v, want true", body["stream"])
		}
		if body["enable_thinking"] != false {
			t.Fatalf("enable_thinking = %v, want false for voice path", body["enable_thinking"])
		}
		if body["max_completion_tokens"] != float64(160) {
			t.Fatalf("max_completion_tokens = %v, want 160 for voice path", body["max_completion_tokens"])
		}
		if _, ok := body["max_tokens"]; ok {
			t.Fatalf("request used deprecated max_tokens: %#v", body)
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
		if !ok || function["name"] != "memory.lookup" {
			t.Fatalf("function = %#v, want memory.lookup", tool["function"])
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
		BaseURL: server.URL + "/compatible-mode/v1",
		APIKey:  "fake-dashscope-key",
		Model:   "qwen3.6-plus",
		Client:  server.Client(),
	})

	chunks, err := provider.Stream(context.Background(), providers.LLMRequest{
		Text: "hello",
		Tools: []providers.LLMTool{{
			Name:        "memory.lookup",
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

func TestLLMBuildsStructuredMessagesWhenProvided(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { requestSeen <- struct{}{} }()

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("messages = %#v, want system plus user", body["messages"])
		}
		systemMessage, ok := messages[0].(map[string]any)
		if !ok || systemMessage["role"] != "system" || systemMessage["content"] != "voice context" {
			t.Fatalf("system message = %#v", messages[0])
		}
		userMessage, ok := messages[1].(map[string]any)
		if !ok || userMessage["role"] != "user" || userMessage["content"] != "current user" {
			t.Fatalf("user message = %#v", messages[1])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewLLM(LLMOptions{
		BaseURL: server.URL + "/compatible-mode/v1",
		APIKey:  "fake-dashscope-key",
		Model:   "qwen3.6-flash",
		Client:  server.Client(),
	})

	chunks, err := provider.Stream(context.Background(), providers.LLMRequest{
		Text: "legacy fallback text",
		Messages: []providers.LLMMessage{
			{Role: "system", Content: "voice context"},
			{Role: "user", Content: "current user"},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	for range chunks {
	}

	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("provider did not send request")
	}
}

func TestLLMParsesStreamingToolCallDeltasAfterArgumentsComplete(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-memory","type":"function","function":{"name":"memory.lookup","arguments":"{\"query\":\"低"}}]}}]}`,
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
	if call.ID != "call-memory" || call.Name != "memory.lookup" || call.Arguments["query"] != "低延迟" {
		t.Fatalf("tool call = %+v, want assembled memory lookup arguments", call)
	}
}

func TestLLMParsesSSEFixtures(t *testing.T) {
	stream := strings.Join([]string{
		readFixture(t, "sse_first_chunk.sse"),
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
	if chunks[0].Text != "Hello" || chunks[0].IsFinal {
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
			_, _ = w.Write([]byte(`{"code":"InvalidApiKey","message":"bad key: super-secret-dashscope-key"}`))
			return
		}
		t.Fatalf("unexpected Authorization = %q", r.Header.Get("Authorization"))
	}))
	defer server.Close()

	provider := NewLLM(LLMOptions{
		BaseURL: server.URL + "/compatible-mode/v1",
		APIKey:  "super-secret-dashscope-key",
		Model:   "qwen3.6-plus",
		Client:  server.Client(),
	})

	_, err := provider.Stream(context.Background(), providers.LLMRequest{Text: "hello"})
	if err == nil {
		t.Fatal("Stream() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "super-secret-dashscope-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if providerErr.StatusCode != http.StatusUnauthorized || providerErr.Code != "InvalidApiKey" {
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
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n"))
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewLLM(LLMOptions{
		BaseURL: server.URL + "/compatible-mode/v1",
		APIKey:  "fake-dashscope-key",
		Model:   "qwen3.6-plus",
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
