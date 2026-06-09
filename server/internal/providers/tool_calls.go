package providers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type OpenAIChatTool struct {
	Type     string                 `json:"type"`
	Function OpenAIChatToolFunction `json:"function"`
}

type OpenAIChatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type OpenAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIToolCallDelta struct {
	Index    int                         `json:"index"`
	ID       string                      `json:"id,omitempty"`
	Type     string                      `json:"type,omitempty"`
	Function OpenAIToolCallFunctionDelta `json:"function,omitempty"`
}

type OpenAIToolCallFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func OpenAIChatTools(tools []LLMTool) []OpenAIChatTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]OpenAIChatTool, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, OpenAIChatTool{
			Type: "function",
			Function: OpenAIChatToolFunction{
				Name:        name,
				Description: strings.TrimSpace(tool.Description),
				Parameters:  tool.InputSchema,
			},
		})
	}
	return out
}

func OpenAIChatMessages(req LLMRequest) []OpenAIChatMessage {
	messages := NormalizeLLMMessages(req)
	if len(messages) == 0 {
		return nil
	}
	out := make([]OpenAIChatMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, OpenAIChatMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	return out
}

type ToolCallDeltaAccumulator struct {
	entries map[int]*toolCallDeltaEntry
	order   []int
}

type toolCallDeltaEntry struct {
	id        string
	name      string
	arguments strings.Builder
}

func NewToolCallDeltaAccumulator() *ToolCallDeltaAccumulator {
	return &ToolCallDeltaAccumulator{
		entries: make(map[int]*toolCallDeltaEntry),
	}
}

func (a *ToolCallDeltaAccumulator) Add(deltas []OpenAIToolCallDelta) {
	if a == nil {
		return
	}
	for _, delta := range deltas {
		entry, ok := a.entries[delta.Index]
		if !ok {
			entry = &toolCallDeltaEntry{}
			a.entries[delta.Index] = entry
			a.order = append(a.order, delta.Index)
		}
		if strings.TrimSpace(delta.ID) != "" {
			entry.id = strings.TrimSpace(delta.ID)
		}
		if strings.TrimSpace(delta.Function.Name) != "" {
			entry.name = strings.TrimSpace(delta.Function.Name)
		}
		if delta.Function.Arguments != "" {
			entry.arguments.WriteString(delta.Function.Arguments)
		}
	}
}

func (a *ToolCallDeltaAccumulator) HasPending() bool {
	return a != nil && len(a.order) > 0
}

func (a *ToolCallDeltaAccumulator) Flush() ([]ToolCall, error) {
	if a == nil || len(a.order) == 0 {
		return nil, nil
	}
	calls := make([]ToolCall, 0, len(a.order))
	for _, index := range a.order {
		entry := a.entries[index]
		if entry == nil {
			continue
		}
		arguments := make(map[string]any)
		rawArguments := strings.TrimSpace(entry.arguments.String())
		if rawArguments != "" {
			if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
				return nil, fmt.Errorf("decode tool call arguments for %q: %w", entry.name, err)
			}
		}
		calls = append(calls, ToolCall{
			ID:        entry.id,
			Name:      entry.name,
			Arguments: arguments,
		})
	}
	a.entries = make(map[int]*toolCallDeltaEntry)
	a.order = nil
	return calls, nil
}

func DecodeOpenAIStreamChunk(payload string, toolCalls *ToolCallDeltaAccumulator, providerName string) (LLMChunk, bool, error) {
	var event openAIStreamChunk
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		if strings.TrimSpace(providerName) == "" {
			providerName = "openai-compatible"
		}
		return LLMChunk{}, false, fmt.Errorf("decode %s stream chunk: %w", providerName, err)
	}
	if len(event.Choices) == 0 {
		return LLMChunk{}, false, nil
	}

	choice := event.Choices[0]
	toolCalls.Add(choice.Delta.ToolCalls)
	text := choice.Delta.Content
	isFinal := strings.TrimSpace(choice.FinishReason) != ""
	if text == "" && !isFinal {
		return LLMChunk{}, false, nil
	}
	var completeToolCalls []ToolCall
	if isFinal && toolCalls.HasPending() {
		calls, err := toolCalls.Flush()
		if err != nil {
			return LLMChunk{}, false, err
		}
		completeToolCalls = calls
	}
	return LLMChunk{
		Text:      text,
		ToolCalls: completeToolCalls,
		IsFinal:   isFinal,
		CreatedAt: time.Now(),
	}, true, nil
}

type openAIStreamChunk struct {
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type openAIStreamDelta struct {
	Content          string                `json:"content"`
	Reasoning        string                `json:"reasoning"`
	ReasoningContent string                `json:"reasoning_content"`
	ToolCalls        []OpenAIToolCallDelta `json:"tool_calls"`
}
