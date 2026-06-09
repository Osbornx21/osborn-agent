package agents

import (
	"sort"
	"strings"

	"stackchan-gateway/internal/providers"
)

const MaxBridgeToolIntentsPerTurn = 2

var safeBridgeToolIntentNames = map[string]struct{}{
	"memory.lookup":                      {},
	"homeassistant.get_state":            {},
	"homeassistant.call_action":          {},
	"search.web":                         {},
	"reminder.announce":                  {},
	"stackchan.express":                  {},
	"stackchan.expression_sequence":      {},
	"stackchan.play_expression_sequence": {},
	"stackchan.show_card":                {},
}

func bridgeToolIntentAllowed(name string) bool {
	_, ok := safeBridgeToolIntentNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func IsBridgeToolIntentAllowed(name string) bool {
	return bridgeToolIntentAllowed(name)
}

func NormalizeBridgeAllowedToolIntents(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	normalized := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || !bridgeToolIntentAllowed(name) {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)
	return normalized
}

func ResolveBridgeMaxToolIntents(maxToolIntents *int) int {
	if maxToolIntents == nil {
		return MaxBridgeToolIntentsPerTurn
	}
	return NormalizeBridgeMaxToolIntents(*maxToolIntents)
}

func NormalizeBridgeMaxToolIntents(maxToolIntents int) int {
	if maxToolIntents < 0 {
		return 0
	}
	if maxToolIntents > MaxBridgeToolIntentsPerTurn {
		return MaxBridgeToolIntentsPerTurn
	}
	return maxToolIntents
}

func appendBridgeToolCall(calls []providers.ToolCall, prefix string, index int, name string, arguments map[string]any) []providers.ToolCall {
	return appendBridgeToolCallWithAllowedTools(calls, prefix, index, name, arguments, nil)
}

func appendBridgeToolCallWithAllowedTools(calls []providers.ToolCall, prefix string, index int, name string, arguments map[string]any, allowedToolIntents []string) []providers.ToolCall {
	return appendBridgeToolCallWithPolicy(calls, prefix, index, name, arguments, allowedToolIntents, MaxBridgeToolIntentsPerTurn)
}

func appendBridgeToolCallWithPolicy(calls []providers.ToolCall, prefix string, index int, name string, arguments map[string]any, allowedToolIntents []string, maxToolIntents int) []providers.ToolCall {
	maxToolIntents = NormalizeBridgeMaxToolIntents(maxToolIntents)
	if maxToolIntents <= 0 || len(calls) >= maxToolIntents {
		return calls
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || !bridgeToolIntentAllowed(name) || !bridgeToolIntentAllowedByConfig(name, allowedToolIntents) {
		return calls
	}
	return append(calls, providers.ToolCall{
		ID:        toolIntentID(prefix, index),
		Name:      name,
		Arguments: cloneArguments(arguments),
	})
}

func bridgeToolIntentAllowedByConfig(name string, allowedToolIntents []string) bool {
	if len(allowedToolIntents) == 0 {
		return true
	}
	for _, allowed := range allowedToolIntents {
		if strings.ToLower(strings.TrimSpace(allowed)) == name {
			return true
		}
	}
	return false
}
