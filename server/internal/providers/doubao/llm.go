package doubao

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
	ProviderIDLLM              = "doubao-llm"
	DefaultLLMBaseURL          = "https://ark.cn-beijing.volces.com/api/v3"
	DefaultLLMModel            = "doubao-seed-2-0-lite-260215"
	SourceDocURLLLM            = "https://www.volcengine.com/docs/82379/1302010"
	SourceDocCheckedAt         = "2026-06-07"
	defaultMaxCompletionTokens = 160
)

var (
	ErrMissingAPIKey = providers.NewProviderConfigurationError("doubao ark api key is required")
	ErrMissingText   = errors.New("llm request text is required")
)

type LLMOptions struct {
	BaseURL             string
	APIKey              string
	Model               string
	MaxCompletionTokens int
	ThinkingType        string
	Client              *http.Client
}

type LLM struct {
	baseURL             string
	apiKey              string
	model               string
	maxCompletionTokens int
	thinkingType        string
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
	thinkingType := strings.TrimSpace(options.ThinkingType)
	if thinkingType == "" {
		thinkingType = "disabled"
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
		thinkingType:        thinkingType,
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
	body := doubaoChatRequest{
		Model:               p.model,
		Stream:              true,
		MaxCompletionTokens: p.maxCompletionTokens,
		Tools:               providers.OpenAIChatTools(req.Tools),
		Messages:            providers.OpenAIChatMessages(req),
	}
	if p.thinkingType != "" {
		body.Thinking = &doubaoThinking{Type: p.thinkingType}
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

type doubaoChatRequest struct {
	Model               string                        `json:"model"`
	Stream              bool                          `json:"stream"`
	MaxCompletionTokens int                           `json:"max_completion_tokens,omitempty"`
	Thinking            *doubaoThinking               `json:"thinking,omitempty"`
	Tools               []providers.OpenAIChatTool    `json:"tools,omitempty"`
	Messages            []providers.OpenAIChatMessage `json:"messages"`
}

type doubaoThinking struct {
	Type string `json:"type"`
}

type doubaoChatMessage struct {
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
	return providers.DecodeOpenAIStreamChunk(payload, toolCalls, "doubao")
}

type doubaoStreamChunk struct {
	Choices []doubaoStreamChoice `json:"choices"`
}

type doubaoStreamChoice struct {
	Delta        doubaoStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type doubaoStreamDelta struct {
	Content string `json:"content"`
}

func decodeProviderError(resp *http.Response, apiKey string) error {
	var payload doubaoErrorPayload
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

type doubaoErrorPayload struct {
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
	return fmt.Sprintf("doubao provider error: status=%d code=%s message=%s", e.StatusCode, e.Code, e.Message)
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
