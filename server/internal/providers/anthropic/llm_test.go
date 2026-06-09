package anthropic

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
		APIKey: "claude-key",
		Model:  "claude-sonnet-4-6",
	})

	if provider.ProviderID() != ProviderIDLLM {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDLLM)
	}
	if provider.ModelID() != "claude-sonnet-4-6" {
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
		APIKey: "claude-key",
		Model:  "claude-sonnet-4-6",
	})

	provider, err := registry.LLMProvider(ProviderIDLLM)
	if err != nil {
		t.Fatalf("LLMProvider(%s) error = %v", ProviderIDLLM, err)
	}
	if _, ok := provider.(*LLM); !ok {
		t.Fatalf("registered provider type = %T, want *LLM", provider)
	}
}

func TestLLMBuildsAnthropicMessagesStreamingRequest(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { requestSeen <- struct{}{} }()

		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "claude-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization must be omitted for Claude direct API, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("anthropic-version = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "claude-sonnet-4-6" {
			t.Fatalf("model = %v", body["model"])
		}
		if body["stream"] != true {
			t.Fatalf("stream = %v, want true", body["stream"])
		}
		if body["max_tokens"] != float64(160) {
			t.Fatalf("max_tokens = %v", body["max_tokens"])
		}
		if messages, ok := body["messages"].([]any); !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v, want one user message", body["messages"])
		}
		if _, ok := body["temperature"]; ok {
			t.Fatalf("temperature should be omitted for current Claude 4.x safety: %#v", body["temperature"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewLLM(LLMOptions{
		BaseURL:   server.URL,
		APIKey:    "claude-key",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 160,
		Client:    server.Client(),
	})

	chunks, err := provider.Stream(context.Background(), providers.LLMRequest{Text: "hello"})
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

func TestLLMParsesAnthropicSSEFixtures(t *testing.T) {
	stream := strings.Join([]string{
		readFixture(t, "sse_first_chunk.sse"),
		"event: ping\ndata: {\"type\":\"ping\"}\n",
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hidden reasoning"}}`,
		readFixture(t, "sse_delta_chunk.sse"),
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

func TestLLMParsesAnthropicSSEErrorEvent(t *testing.T) {
	_, err := ParseLLMStream(bufio.NewReader(strings.NewReader(readFixture(t, "sse_error_event.sse"))))
	if err == nil {
		t.Fatal("ParseLLMStream() error = nil, want stream provider error")
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if providerErr.Code != "overloaded_error" {
		t.Fatalf("providerErr = %+v", providerErr)
	}
}

func TestLLMMapsProviderErrorsWithoutLeakingSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("x-api-key"), "claude-secret") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad key: claude-secret"},"request_id":"req_secret"}`))
			return
		}
		t.Fatalf("unexpected x-api-key = %q", r.Header.Get("x-api-key"))
	}))
	defer server.Close()

	provider := NewLLM(LLMOptions{
		BaseURL: server.URL,
		APIKey:  "claude-secret",
		Model:   "claude-sonnet-4-6",
		Client:  server.Client(),
	})

	_, err := provider.Stream(context.Background(), providers.LLMRequest{Text: "hello"})
	if err == nil {
		t.Fatal("Stream() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "claude-secret") || strings.Contains(err.Error(), "x-api-key") {
		t.Fatalf("error leaked secret material: %v", err)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if providerErr.StatusCode != http.StatusUnauthorized || providerErr.Code != "authentication_error" || providerErr.RequestID != "req_secret" {
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
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"))
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewLLM(LLMOptions{
		BaseURL: server.URL,
		APIKey:  "claude-key",
		Model:   "claude-sonnet-4-6",
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
