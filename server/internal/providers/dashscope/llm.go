package dashscope

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
	ProviderIDLLM              = "dashscope-llm"
	DefaultLLMBaseURL          = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	DefaultLLMModel            = "qwen3.6-plus"
	SourceDocURLLLM            = "https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions"
	SourceDocCheckedAt         = "2026-06-07"
	defaultMaxCompletionTokens = 160
)

var (
	ErrMissingAPIKey = providers.NewProviderConfigurationError("dashscope api key is required")
	ErrMissingText   = errors.New("llm request text is required")
)

type LLMOptions struct {
	BaseURL             string
	APIKey              string
	Model               string
	MaxCompletionTokens int
	Client              *http.Client
}

type LLM struct {
	baseURL             string
	apiKey              string
	model               string
	maxCompletionTokens int
	client              *http.Client
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
	maxCompletionTokens := options.MaxCompletionTokens
	if maxCompletionTokens <= 0 {
		maxCompletionTokens = defaultMaxCompletionTokens
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &LLM{
		baseURL:             baseURL,
		apiKey:              strings.TrimSpace(options.APIKey),
		model:               model,
		maxCompletionTokens: maxCompletionTokens,
		client:              client,
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
	body := dashscopeChatRequest{
		Model:               p.model,
		Stream:              true,
		EnableThinking:      false,
		MaxCompletionTokens: p.maxCompletionTokens,
		Tools:               dashscopeTools(req.Tools),
		Messages:            providers.OpenAIChatMessages(req),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatCompletionsURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (p *LLM) chatCompletionsURL() string {
	return p.baseURL + "/chat/completions"
}

type dashscopeChatRequest struct {
	Model               string                        `json:"model"`
	Stream              bool                          `json:"stream"`
	EnableThinking      bool                          `json:"enable_thinking"`
	MaxCompletionTokens int                           `json:"max_completion_tokens,omitempty"`
	Tools               []dashscopeChatTool           `json:"tools,omitempty"`
	Messages            []providers.OpenAIChatMessage `json:"messages"`
}

type dashscopeChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type dashscopeChatTool struct {
	Type     string                    `json:"type"`
	Function dashscopeChatToolFunction `json:"function"`
}

type dashscopeChatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

func dashscopeTools(tools []providers.LLMTool) []dashscopeChatTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]dashscopeChatTool, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, dashscopeChatTool{
			Type: "function",
			Function: dashscopeChatToolFunction{
				Name:        name,
				Description: strings.TrimSpace(tool.Description),
				Parameters:  tool.InputSchema,
			},
		})
	}
	return out
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
	toolCalls := providers.NewToolCallDeltaAccumulator()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return nil
		}
		chunk, ok, err := decodeStreamChunk(payload, toolCalls)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if !emit(chunk) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func decodeStreamChunk(payload string, toolCalls *providers.ToolCallDeltaAccumulator) (providers.LLMChunk, bool, error) {
	var event dashscopeStreamChunk
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return providers.LLMChunk{}, false, fmt.Errorf("decode dashscope stream chunk: %w", err)
	}
	if len(event.Choices) == 0 {
		return providers.LLMChunk{}, false, nil
	}

	choice := event.Choices[0]
	toolCalls.Add(choice.Delta.ToolCalls)
	text := choice.Delta.Content
	isFinal := strings.TrimSpace(choice.FinishReason) != ""
	if text == "" && !isFinal {
		return providers.LLMChunk{}, false, nil
	}
	var completeToolCalls []providers.ToolCall
	if isFinal && toolCalls.HasPending() {
		calls, err := toolCalls.Flush()
		if err != nil {
			return providers.LLMChunk{}, false, err
		}
		completeToolCalls = calls
	}
	return providers.LLMChunk{
		Text:      text,
		ToolCalls: completeToolCalls,
		IsFinal:   isFinal,
		CreatedAt: time.Now(),
	}, true, nil
}

type dashscopeStreamChunk struct {
	Choices []dashscopeStreamChoice `json:"choices"`
}

type dashscopeStreamChoice struct {
	Delta        dashscopeStreamDelta `json:"delta"`
	FinishReason string               `json:"finish_reason"`
}

type dashscopeStreamDelta struct {
	Content   string                          `json:"content"`
	ToolCalls []providers.OpenAIToolCallDelta `json:"tool_calls"`
}

func decodeProviderError(resp *http.Response, apiKey string) error {
	var payload dashscopeErrorPayload
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       firstNonEmpty(payload.Code, http.StatusText(resp.StatusCode)),
		Message:    sanitizeProviderMessage(firstNonEmpty(payload.Message, "provider request failed"), apiKey),
	}
}

type dashscopeErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ProviderError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("dashscope provider error: status=%d code=%s message=%s", e.StatusCode, e.Code, e.Message)
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
	if strings.Contains(message, "Bearer ") {
		return "provider request failed"
	}
	return message
}
