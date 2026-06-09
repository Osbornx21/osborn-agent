package agents

import "testing"

func TestHermesToolIntentsFilterUnsafeNamesAndCapCalls(t *testing.T) {
	calls := HermesToolIntentsToGatewayToolCalls([]HermesToolIntent{
		{Tool: " "},
		{Tool: "system.shell", Args: map[string]any{"cmd": "rm -rf /"}},
		{Tool: " memory.lookup ", Args: map[string]any{"query": "称呼"}},
		{Tool: "homeassistant.call_action", Args: map[string]any{"action_id": "desk_light_on"}},
		{Tool: "search.web", Args: map[string]any{"query": "天气"}},
	})

	if len(calls) != 2 {
		t.Fatalf("calls = %+v, want exactly two safe bridge tool calls", calls)
	}
	if calls[0].ID != "hermes_tool_2" || calls[0].Name != "memory.lookup" || calls[0].Arguments["query"] != "称呼" {
		t.Fatalf("first call = %+v, want trimmed memory lookup with original index", calls[0])
	}
	if calls[1].ID != "hermes_tool_3" || calls[1].Name != "homeassistant.call_action" || calls[1].Arguments["action_id"] != "desk_light_on" {
		t.Fatalf("second call = %+v, want HA action before cap", calls[1])
	}
}

func TestHermesToolIntentsRespectConfiguredAllowedTools(t *testing.T) {
	calls := HermesToolIntentsToGatewayToolCallsWithAllowedTools([]HermesToolIntent{
		{Tool: "memory.lookup", Args: map[string]any{"query": "称呼"}},
		{Tool: "homeassistant.call_action", Args: map[string]any{"action_id": "desk_light_on"}},
		{Tool: "stackchan.express", Args: map[string]any{"cue": "nod"}},
	}, []string{" memory.lookup ", "stackchan.express"})

	if len(calls) != 2 {
		t.Fatalf("calls = %+v, want configured allowlist to keep two tools", calls)
	}
	if calls[0].Name != "memory.lookup" || calls[0].Arguments["query"] != "称呼" {
		t.Fatalf("first call = %+v, want memory lookup", calls[0])
	}
	if calls[1].Name != "stackchan.express" || calls[1].Arguments["cue"] != "nod" {
		t.Fatalf("second call = %+v, want stackchan expression", calls[1])
	}
}

func TestHermesToolIntentsRespectConfiguredMaxToolIntents(t *testing.T) {
	intents := []HermesToolIntent{
		{Tool: "memory.lookup", Args: map[string]any{"query": "称呼"}},
		{Tool: "search.web", Args: map[string]any{"query": "天气"}},
		{Tool: "stackchan.express", Args: map[string]any{"cue": "nod"}},
	}

	calls := HermesToolIntentsToGatewayToolCallsWithPolicy(intents, []string{"memory.lookup", "search.web", "stackchan.express"}, 1)
	if len(calls) != 1 || calls[0].Name != "memory.lookup" {
		t.Fatalf("calls = %+v, want only first safe tool under configured cap", calls)
	}

	calls = HermesToolIntentsToGatewayToolCallsWithPolicy(intents, []string{"memory.lookup", "search.web", "stackchan.express"}, 0)
	if len(calls) != 0 {
		t.Fatalf("calls = %+v, want explicit zero cap to disable bridge tool intents", calls)
	}
}

func TestClaudeToolIntentsFilterUnsafeNamesAndCapCalls(t *testing.T) {
	calls := ClaudeToolIntentsToGatewayToolCalls([]ClaudeToolIntent{
		{Name: "stackchan.express", Input: map[string]any{"cue": "nod"}},
		{Name: "v21.voice_query", Input: map[string]any{"question": "跨桥调用"}},
		{Name: "stackchan.show_card", Input: map[string]any{"card_id": "status"}},
		{Name: "homeassistant.get_state", Input: map[string]any{"entity_id": "light.desk"}},
	})

	if len(calls) != 2 {
		t.Fatalf("calls = %+v, want cap of two safe gateway calls", calls)
	}
	if calls[0].Name != "stackchan.express" || calls[0].Arguments["cue"] != "nod" {
		t.Fatalf("first call = %+v, want stackchan expression", calls[0])
	}
	if calls[1].Name != "stackchan.show_card" || calls[1].Arguments["card_id"] != "status" {
		t.Fatalf("second call = %+v, want v21 filtered and card retained", calls[1])
	}
}
