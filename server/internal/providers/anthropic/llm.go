package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDLLM           = "anthropic-llm"
	DefaultLLMBaseURL       = "https://api.anthropic.com"
	DefaultLLMModel         = "claude-sonnet-4-6"
	DefaultAnthropicVersion = "2023-06-01"
	DefaultLLMMaxTokens     = 256
	SourceDocURLLLM         = "https://platform.claude.com/docs/claude/reference/messages_post"
	SourceDocURLStreaming   = "https://platform.claude.com/docs/en/build-with-claude/streaming"
	SourceDocCheckedAt      = "2026-06-06"
)

var (
	ErrMissingAPIKey = providers.NewProviderConfigurationError("anthropic api key is required")
	ErrMissingText   = errors.New("llm request text is required")
)

type LLMOptions struct {
	BaseURL          string
	APIKey           string
	Model            string
	MaxTokens        int
	AnthropicVersion string
	Client           *http.Client
}

type LLM struct {
	baseURL          string
	apiKey           string
	model            string
	maxTokens        int
	anthropicVersion string
	client           *http.Client
}

func NewLLM(options LLMOptions) *LLM {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultLLMBaseURL
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = DefaultLLMModel
	}
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = DefaultLLMMaxTokens
	}
	anthropicVersion := strings.TrimSpace(options.AnthropicVersion)
	if anthropicVersion == "" {
		anthropicVersion = DefaultAnthropicVersion
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &LLM{
		baseURL:          baseURL,
		apiKey:           strings.TrimSpace(options.APIKey),
		model:            model,
		maxTokens:        maxTokens,
		anthropicVersion: anthropicVersion,
		client:           client,
	}
}

func (p *LLM) ProviderID() string {
	return ProviderIDLLM
}

func (p *LLM) ModelID() string {
	return p.model
}

func (p *LLM) SourceDocURL() string {
	return SourceDocURLLLM
}

func (p *LLM) SourceDocCheckedAt() string {
	return SourceDocCheckedAt
}

func (p *LLM) ValidateProviderConfig() error {
	if p.apiKey == "" {
		return ErrMissingAPIKey
	}
	return nil
}

func (p *LLM) Stream(ctx context.Context, req providers.LLMRequest) (<-chan providers.LLMChunk, error) {
	if p.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if !providers.HasLLMInput(req) {
		return nil, ErrMissingText
	}

	httpReq, err := p.newStreamRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, decodeProviderError(resp, p.apiKey)
	}

	out := make(chan providers.LLMChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = resp.Body.Close()
			case <-done:
			}
		}()
		defer close(done)

		_ = ReadLLMStream(resp.Body, func(chunk providers.LLMChunk) bool {
			select {
			case <-ctx.Done():
				return false
			case out <- chunk:
				return true
			}
		})
	}()

	return out, nil
}

func (p *LLM) newStreamRequest(ctx context.Context, req providers.LLMRequest) (*http.Request, error) {
	system, messages := anthropicMessages(req)
	body := anthropicMessagesRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Stream:    true,
		System:    system,
		Messages:  messages,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.messagesURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", p.anthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (p *LLM) messagesURL() string {
	return p.baseURL + "/v1/messages"
}

type anthropicMessagesRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func anthropicMessages(req providers.LLMRequest) (string, []anthropicMessage) {
	normalized := providers.NormalizeLLMMessages(req)
	if len(normalized) == 0 {
		return "", nil
	}
	var systemParts []string
	messages := make([]anthropicMessage, 0, len(normalized))
	for _, message := range normalized {
		switch message.Role {
		case "system":
			systemParts = append(systemParts, message.Content)
		case "assistant":
			messages = append(messages, anthropicMessage{Role: "assistant", Content: message.Content})
		default:
			messages = append(messages, anthropicMessage{Role: "user", Content: message.Content})
		}
	}
	return strings.Join(systemParts, "\n\n"), messages
}

func ParseLLMStream(reader *bufio.Reader) ([]providers.LLMChunk, error) {
	var chunks []providers.LLMChunk
	if err := ReadLLMStream(reader, func(chunk providers.LLMChunk) bool {
		chunks = append(chunks, chunk)
		return true
	}); err != nil {
		return nil, err
	}
	return chunks, nil
}

func ReadLLMStream(reader io.Reader, emit func(providers.LLMChunk) bool) error {
	scanner := bufio.NewScanner(reader)
	eventType := ""
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := dispatchSSEEvent(eventType, dataLines, emit); err != nil {
				return err
			}
			eventType = ""
			dataLines = nil
			continue
		}
		if strings.HasPrefix(trimmed, "event:") {
			if len(dataLines) > 0 {
				if err := dispatchSSEEvent(eventType, dataLines, emit); err != nil {
					return err
				}
				dataLines = nil
			}
			eventType = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return dispatchSSEEvent(eventType, dataLines, emit)
}

func dispatchSSEEvent(eventType string, dataLines []string, emit func(providers.LLMChunk) bool) error {
	if len(dataLines) == 0 {
		return nil
	}
	payload := strings.Join(dataLines, "\n")
	chunk, ok, err := decodeStreamEvent(eventType, payload)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if !emit(chunk) {
		return nil
	}
	return nil
}

func decodeStreamEvent(eventType string, payload string) (providers.LLMChunk, bool, error) {
	var event anthropicStreamEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return providers.LLMChunk{}, false, fmt.Errorf("decode anthropic stream event: %w", err)
	}
	if eventType == "" {
		eventType = event.Type
	}
	switch eventType {
	case "content_block_delta":
		if event.Delta.Type != "text_delta" || event.Delta.Text == "" {
			return providers.LLMChunk{}, false, nil
		}
		return providers.LLMChunk{
			Text:      event.Delta.Text,
			CreatedAt: time.Now(),
		}, true, nil
	case "message_stop":
		return providers.LLMChunk{
			IsFinal:   true,
			CreatedAt: time.Now(),
		}, true, nil
	case "error":
		return providers.LLMChunk{}, false, &ProviderError{
			Code:    firstNonEmpty(event.Error.Type, "stream_error"),
			Message: firstNonEmpty(event.Error.Message, "provider stream failed"),
		}
	default:
		return providers.LLMChunk{}, false, nil
	}
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	} `json:"delta"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeProviderError(resp *http.Response, apiKey string) error {
	var payload anthropicErrorPayload
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	message := firstNonEmpty(payload.Error.Message, payload.Message, strings.TrimSpace(string(data)), "provider request failed")
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       firstNonEmpty(payload.Error.Type, payload.Type, http.StatusText(resp.StatusCode)),
		Message:    sanitizeProviderMessage(message, apiKey),
		RequestID:  payload.RequestID,
	}
}

type anthropicErrorPayload struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Error     struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type ProviderError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	requestID := ""
	if e.RequestID != "" {
		requestID = " request_id=" + e.RequestID
	}
	return fmt.Sprintf("anthropic provider error: status=%d code=%s%s message=%s", e.StatusCode, e.Code, requestID, e.Message)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sanitizeProviderMessage(message string, apiKey string) string {
	if apiKey != "" {
		message = strings.ReplaceAll(message, apiKey, "[REDACTED]")
	}
	if strings.Contains(message, "x-api-key") || strings.Contains(message, "Authorization") || strings.Contains(message, "Bearer ") {
		return "provider request failed"
	}
	return message
}
