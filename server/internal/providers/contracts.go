package providers

import (
	"context"
	"strings"
	"time"

	"stackchan-gateway/internal/audio"
)

const (
	ProviderMock = "mock"

	ASREventPartial = "partial"
	ASREventFinal   = "final"
)

type ASRProvider interface {
	Start(ctx context.Context, req ASRStartRequest) (ASRStream, error)
}

type ASRStartRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	StartedAt  time.Time
	FinalText  string
}

type ASRStream interface {
	AcceptOpus(frame audio.Frame) error
	Finish() error
	Events() <-chan ASREvent
	Close() error
}

type ASREvent struct {
	Type       string
	Text       string
	IsFinal    bool
	StartedAt  time.Time
	FinishedAt time.Time
}

type LLMProvider interface {
	Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error)
}

type LLMRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Text       string
	Messages   []LLMMessage
	Tools      []LLMTool
	CreatedAt  time.Time
}

type LLMMessage struct {
	Role    string
	Content string
}

type LLMTool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type LLMChunk struct {
	Text      string
	Emotion   string
	ToolCalls []ToolCall
	IsFinal   bool
	CreatedAt time.Time
}

type TTSProvider interface {
	Stream(ctx context.Context, req TTSRequest) (<-chan TTSFrame, error)
}

type TTSRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Text       string
	Voice      string
	CreatedAt  time.Time
}

type TTSFrame struct {
	Generation   int64
	Opus         []byte
	TextSpan     string
	Duration     time.Duration
	CreatedAt    time.Time
	AudioQuality audio.PCM16Stats
}

type ProviderConfigValidator interface {
	ValidateProviderConfig() error
}

func ValidateProviderConfig(provider any) error {
	validator, ok := provider.(ProviderConfigValidator)
	if !ok {
		return nil
	}
	return validator.ValidateProviderConfig()
}

func NormalizeLLMMessages(req LLMRequest) []LLMMessage {
	messages := make([]LLMMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		role := normalizeLLMMessageRole(message.Role)
		content := strings.TrimSpace(message.Content)
		if role == "" || content == "" {
			continue
		}
		messages = append(messages, LLMMessage{Role: role, Content: content})
	}
	if len(messages) > 0 {
		return messages
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil
	}
	return []LLMMessage{{Role: "user", Content: text}}
}

func HasLLMInput(req LLMRequest) bool {
	for _, message := range NormalizeLLMMessages(req) {
		if message.Role == "user" || message.Role == "assistant" {
			return true
		}
	}
	return false
}

func normalizeLLMMessageRole(role string) string {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "system", "user", "assistant":
		return normalized
	default:
		return ""
	}
}
