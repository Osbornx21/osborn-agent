package providers

import "testing"

func TestToolCallDeltaAccumulatorFlushesCompleteArguments(t *testing.T) {
	accumulator := NewToolCallDeltaAccumulator()
	accumulator.Add([]OpenAIToolCallDelta{{
		Index: 0,
		ID:    "call-memory",
		Type:  "function",
		Function: OpenAIToolCallFunctionDelta{
			Name:      "memory.lookup",
			Arguments: `{"query":"低`,
		},
	}})
	if !accumulator.HasPending() {
		t.Fatal("HasPending() = false, want true after first delta")
	}
	accumulator.Add([]OpenAIToolCallDelta{{
		Index: 0,
		Function: OpenAIToolCallFunctionDelta{
			Arguments: `延迟"}`,
		},
	}})

	calls, err := accumulator.Flush()

	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls len = %d, want 1", len(calls))
	}
	if calls[0].ID != "call-memory" || calls[0].Name != "memory.lookup" || calls[0].Arguments["query"] != "低延迟" {
		t.Fatalf("call = %+v, want complete memory.lookup arguments", calls[0])
	}
	if accumulator.HasPending() {
		t.Fatal("HasPending() = true after Flush, want false")
	}
}

func TestToolCallDeltaAccumulatorRejectsInvalidJSONArguments(t *testing.T) {
	accumulator := NewToolCallDeltaAccumulator()
	accumulator.Add([]OpenAIToolCallDelta{{
		Index: 0,
		Function: OpenAIToolCallFunctionDelta{
			Name:      "memory.lookup",
			Arguments: `{"query":`,
		},
	}})

	if _, err := accumulator.Flush(); err == nil {
		t.Fatal("Flush() error = nil, want invalid JSON error")
	}
}
