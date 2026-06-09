package agents

import (
	"strconv"
	"strings"

	"stackchan-gateway/internal/providers"
)

type ClaudeToolIntent struct {
	Name  string
	Input map[string]any
}

func ClaudeToolIntentsToGatewayToolCalls(intents []ClaudeToolIntent) []providers.ToolCall {
	calls := make([]providers.ToolCall, 0, len(intents))
	for index, intent := range intents {
		calls = appendBridgeToolCall(calls, "claude", index, intent.Name, intent.Input)
	}
	return calls
}

func toolIntentID(prefix string, index int) string {
	return strings.TrimSpace(prefix) + "_tool_" + strconv.Itoa(index)
}
