package siliconflow

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDLLM      = "siliconflow-llm"
	DefaultLLMBaseURL  = "https://api.siliconflow.cn/v1"
	DefaultLLMModel    = "Qwen/Qwen3.5-35B-A3B"
	SourceDocURLLLM    = "https://docs.siliconflow.com/en/api-reference/chat-completions/chat-completions_copy"
	SourceDocCheckedAt = "2026-06-06"
	defaultMaxTokens   = 160
)

var (
	ErrMissingAPIKey = providers.NewProviderConfigurationError("siliconflow api key is required")
	ErrMissingText   = errors.New("llm request text is required")
)

type LLMOptions struct {
	BaseURL        string
	APIKey         string
	Model          string
	MaxTokens      int
	EnableThinking bool
	Client         *http.Client
}

type LLM struct {
	baseURL        string
	apiKey         string
	model          string
	maxTokens      int
	enableThinking bool
	client         *http.Client
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
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &LLM{
		baseURL:        baseURL,
		apiKey:         strings.TrimSpace(options.APIKey),
		model:          model,
		maxTokens:      maxTokens,
		enableThinking: options.EnableThinking,
		client:         client,
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
	body := siliconflowChatRequest{
		Model:          p.model,
		Stream:         true,
		MaxTokens:      p.maxTokens,
		EnableThinking: p.enableThinking,
		Tools:          siliconflowTools(req.Tools),
		Messages:       providers.OpenAIChatMessages(req),
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

type siliconflowChatRequest struct {
	Model          string                        `json:"model"`
	Stream         bool                          `json:"stream"`
	MaxTokens      int                           `json:"max_tokens"`
	EnableThinking bool                          `json:"enable_thinking"`
	Tools          []siliconflowChatTool         `json:"tools,omitempty"`
	Messages       []providers.OpenAIChatMessage `json:"messages"`
}

type siliconflowChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type siliconflowChatTool struct {
	Type     string                      `json:"type"`
	Function siliconflowChatToolFunction `json:"function"`
}

type siliconflowChatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

func siliconflowTools(tools []providers.LLMTool) []siliconflowChatTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]siliconflowChatTool, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, siliconflowChatTool{
			Type: "function",
			Function: siliconflowChatToolFunction{
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
	var event siliconflowStreamChunk
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return providers.LLMChunk{}, false, fmt.Errorf("decode siliconflow stream chunk: %w", err)
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

type siliconflowStreamChunk struct {
	Choices []siliconflowStreamChoice `json:"choices"`
}

type siliconflowStreamChoice struct {
	Delta        siliconflowStreamDelta `json:"delta"`
	FinishReason string                 `json:"finish_reason"`
}

type siliconflowStreamDelta struct {
	Content          string                          `json:"content"`
	ReasoningContent string                          `json:"reasoning_content"`
	ToolCalls        []providers.OpenAIToolCallDelta `json:"tool_calls"`
}

func decodeProviderError(resp *http.Response, apiKey string) error {
	var payload siliconflowErrorPayload
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	code := firstNonEmpty(codeToString(payload.Code), payload.Error.Code, payload.Error.Type, http.StatusText(resp.StatusCode))
	message := firstNonEmpty(payload.Message, payload.Error.Message, "provider request failed")
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       code,
		Message:    sanitizeProviderMessage(message, apiKey),
	}
}

type siliconflowErrorPayload struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	Error   struct {
		Code    string `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
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
	return fmt.Sprintf("siliconflow provider error: status=%d code=%s message=%s", e.StatusCode, e.Code, e.Message)
}

func codeToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
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
