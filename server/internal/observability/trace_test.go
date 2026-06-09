package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTraceRecorderOrdersEventsByMonotonicElapsed(t *testing.T) {
	base := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	times := []time.Time{
		base,
		base.Add(12 * time.Millisecond),
		base.Add(6 * time.Millisecond),
	}
	var index int
	now := func() time.Time {
		if index >= len(times) {
			return times[len(times)-1]
		}
		value := times[index]
		index++
		return value
	}

	var output bytes.Buffer
	recorder := NewTraceRecorder(TraceRecorderOptions{Writer: &output, Now: now})
	for _, event := range []string{"listen_start", "speech_final", "turn_complete"} {
		if err := recorder.Record(context.Background(), TraceEvent{
			TraceID:    "trace-1",
			SessionID:  "sess-1",
			DeviceID:   "stackchan",
			Generation: 1,
			Event:      event,
		}); err != nil {
			t.Fatalf("Record(%s) error = %v", event, err)
		}
	}

	events := decodeTraceEvents(t, output.Bytes())
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].ElapsedMS < events[i-1].ElapsedMS {
			t.Fatalf("elapsed[%d] = %d before elapsed[%d] = %d", i, events[i].ElapsedMS, i-1, events[i-1].ElapsedMS)
		}
		if events[i].Sequence <= events[i-1].Sequence {
			t.Fatalf("sequence[%d] = %d before sequence[%d] = %d", i, events[i].Sequence, i-1, events[i-1].Sequence)
		}
	}
}

func TestTraceRecorderRedactsSecretLikeFields(t *testing.T) {
	var output bytes.Buffer
	recorder := NewTraceRecorder(TraceRecorderOptions{Writer: &output, RedactSecrets: true})

	err := recorder.Record(context.Background(), TraceEvent{
		TraceID:    "trace-redact",
		SessionID:  "sess-1",
		DeviceID:   "stackchan",
		Generation: 1,
		Event:      "hello_received",
		Fields: map[string]any{
			"Authorization": "Bearer visible-token",
			"normal":        "kept",
			"nested": map[string]any{
				"dashscope_api_key": "sk-abc",
				"model":             "qwen-plus",
			},
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	events := decodeTraceEvents(t, output.Bytes())
	fields := events[0].Fields
	if fields["Authorization"] != redactedValue {
		t.Fatalf("Authorization = %v, want redacted", fields["Authorization"])
	}
	if fields["normal"] != "kept" {
		t.Fatalf("normal = %v, want kept", fields["normal"])
	}
	nested, ok := fields["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested type = %T, want map[string]any", fields["nested"])
	}
	if nested["dashscope_api_key"] != redactedValue {
		t.Fatalf("nested api key = %v, want redacted", nested["dashscope_api_key"])
	}
	if nested["model"] != "qwen-plus" {
		t.Fatalf("nested model = %v, want qwen-plus", nested["model"])
	}
}

func TestMetricsRenderIncludesRequiredPrometheusNames(t *testing.T) {
	metrics := NewMetrics()
	metrics.IncTurns()
	metrics.IncProviderRequest("llm", "mock")
	metrics.ObserveProviderFirstToken("mock", 25*time.Millisecond)

	body := metrics.Render()
	for _, name := range []string{
		MetricSessionsActive,
		MetricTurnsTotal,
		MetricTurnErrorsTotal,
		MetricProviderRequestsTotal,
		MetricProviderFirstTokenSeconds,
		MetricProviderFirstAudioSeconds,
		MetricSpeechEndToFirstAudibleSeconds,
		MetricBargeInStopSeconds,
		MetricDownlinkQueueFrames,
		MetricMCPToolCallsTotal,
		MetricMCPToolLatencySeconds,
	} {
		if !bytes.Contains([]byte(body), []byte(name)) {
			t.Fatalf("metrics output missing %s:\n%s", name, body)
		}
	}
}

func decodeTraceEvents(t *testing.T, data []byte) []TraceEvent {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	events := make([]TraceEvent, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event TraceEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode trace line %q: %v", string(line), err)
		}
		events = append(events, event)
	}
	return events
}
