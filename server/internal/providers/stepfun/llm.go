package stepfun

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

	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDLLM      = "stepfun-llm"
	DefaultLLMBaseURL  = "https://api.stepfun.com/v1"
	DefaultLLMModel    = "step-3.7-flash"
	SourceDocURLLLM    = "https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create"
	SourceDocCheckedAt = "2026-06-07"
	defaultMaxTokens   = 160
)

var (
	ErrMissingAPIKey = providers.NewProviderConfigurationError("stepfun api key is required")
	ErrMissingText   = errors.New("llm request text is required")
)

type LLMOptions struct {
	BaseURL         string
	APIKey          string
	Model           string
	MaxTokens       int
	ReasoningFormat string
	ReasoningEffort string
	Client          *http.Client
}

type LLM struct {
	baseURL         string
	apiKey          string
	model           string
	maxTokens       int
	reasoningFormat string
	reasoningEffort string
	client          *http.Client
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
		baseURL:         baseURL,
		apiKey:          strings.TrimSpace(options.APIKey),
		model:           model,
		maxTokens:       maxTokens,
		reasoningFormat: strings.TrimSpace(options.ReasoningFormat),
		reasoningEffort: strings.TrimSpace(options.ReasoningEffort),
		client:          client,
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
	body := stepfunChatRequest{
		Model:     p.model,
		Stream:    true,
		MaxTokens: p.maxTokens,
		Tools:     providers.OpenAIChatTools(req.Tools),
		Messages:  providers.OpenAIChatMessages(req),
	}
	if p.reasoningFormat != "" {
		body.ReasoningFormat = p.reasoningFormat
	}
	if p.reasoningEffort != "" {
		body.ReasoningEffort = p.reasoningEffort
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

type stepfunChatRequest struct {
	Model           string                        `json:"model"`
	Stream          bool                          `json:"stream"`
	MaxTokens       int                           `json:"max_tokens,omitempty"`
	ReasoningFormat string                        `json:"reasoning_format,omitempty"`
	ReasoningEffort string                        `json:"reasoning_effort,omitempty"`
	Tools           []providers.OpenAIChatTool    `json:"tools,omitempty"`
	Messages        []providers.OpenAIChatMessage `json:"messages"`
}

type stepfunChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	return providers.DecodeOpenAIStreamChunk(payload, toolCalls, "stepfun")
}

type stepfunStreamChunk struct {
	Choices []stepfunStreamChoice `json:"choices"`
}

type stepfunStreamChoice struct {
	Delta        stepfunStreamDelta `json:"delta"`
	FinishReason string             `json:"finish_reason"`
}

type stepfunStreamDelta struct {
	Content          string `json:"content"`
	Reasoning        string `json:"reasoning"`
	ReasoningContent string `json:"reasoning_content"`
}

func decodeProviderError(resp *http.Response, apiKey string) error {
	var payload stepfunErrorPayload
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	code := firstNonEmpty(payload.Code, payload.Error.Code, payload.Error.Type, http.StatusText(resp.StatusCode))
	message := firstNonEmpty(payload.Message, payload.Error.Message, "provider request failed")
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       code,
		Message:    sanitizeProviderMessage(message, apiKey),
	}
}

type stepfunErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
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
	return fmt.Sprintf("stepfun provider error: status=%d code=%s message=%s", e.StatusCode, e.Code, e.Message)
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
