package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MetricSessionsActive                 = "stackchan_sessions_active"
	MetricTurnsTotal                     = "stackchan_turns_total"
	MetricTurnErrorsTotal                = "stackchan_turn_errors_total"
	MetricProviderRequestsTotal          = "stackchan_provider_requests_total"
	MetricProviderFirstTokenSeconds      = "stackchan_provider_first_token_seconds"
	MetricProviderFirstAudioSeconds      = "stackchan_provider_first_audio_seconds"
	MetricSpeechEndToFirstAudibleSeconds = "stackchan_speech_end_to_first_audible_seconds"
	MetricBargeInStopSeconds             = "stackchan_barge_in_stop_seconds"
	MetricDownlinkQueueFrames            = "stackchan_downlink_queue_frames"
	MetricMCPToolCallsTotal              = "stackchan_mcp_tool_calls_total"
	MetricMCPToolLatencySeconds          = "stackchan_mcp_tool_latency_seconds"
)

type Metrics struct {
	mu        sync.Mutex
	counters  map[string]sample
	gauges    map[string]sample
	summaries map[string]summary
}

type sample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

type summary struct {
	Name   string
	Labels map[string]string
	Count  uint64
	Sum    float64
}

type metricDefinition struct {
	Name string
	Kind string
	Help string
}

var metricDefinitions = []metricDefinition{
	{Name: MetricSessionsActive, Kind: "gauge", Help: "Active xiaozhi device sessions."},
	{Name: MetricTurnsTotal, Kind: "counter", Help: "Total voice turns started."},
	{Name: MetricTurnErrorsTotal, Kind: "counter", Help: "Total voice turn errors by code."},
	{Name: MetricProviderRequestsTotal, Kind: "counter", Help: "Total ASR, LLM, and TTS provider requests."},
	{Name: MetricProviderFirstTokenSeconds, Kind: "summary", Help: "LLM request to first token latency in seconds."},
	{Name: MetricProviderFirstAudioSeconds, Kind: "summary", Help: "TTS request to first audio frame latency in seconds."},
	{Name: MetricSpeechEndToFirstAudibleSeconds, Kind: "summary", Help: "ASR final speech to first audible downlink latency in seconds."},
	{Name: MetricBargeInStopSeconds, Kind: "summary", Help: "Time from barge-in or abort cancellation to TTS stop send in seconds."},
	{Name: MetricDownlinkQueueFrames, Kind: "gauge", Help: "Current downlink queue frame depth."},
	{Name: MetricMCPToolCallsTotal, Kind: "counter", Help: "Total MCP tool calls by tool and allow decision."},
	{Name: MetricMCPToolLatencySeconds, Kind: "summary", Help: "MCP tool call latency in seconds."},
}

func NewMetrics() *Metrics {
	return &Metrics{
		counters:  make(map[string]sample),
		gauges:    make(map[string]sample),
		summaries: make(map[string]summary),
	}
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(m.Render()))
}

func (m *Metrics) Handler() http.Handler {
	return m
}

func (m *Metrics) IncSessionsActive() {
	m.AddGauge(MetricSessionsActive, nil, 1)
}

func (m *Metrics) DecSessionsActive() {
	m.AddGauge(MetricSessionsActive, nil, -1)
}

func (m *Metrics) IncTurns() {
	m.IncCounter(MetricTurnsTotal, nil, 1)
}

func (m *Metrics) IncTurnErrors(errorCode string) {
	m.IncCounter(MetricTurnErrorsTotal, map[string]string{"error_code": labelOrUnknown(errorCode)}, 1)
}

func (m *Metrics) IncProviderRequest(kind string, provider string) {
	m.IncCounter(MetricProviderRequestsTotal, map[string]string{
		"kind":     labelOrUnknown(kind),
		"provider": labelOrUnknown(provider),
	}, 1)
}

func (m *Metrics) ObserveProviderFirstToken(provider string, latency time.Duration) {
	m.ObserveSummary(MetricProviderFirstTokenSeconds, map[string]string{"provider": labelOrUnknown(provider)}, latency.Seconds())
}

func (m *Metrics) ObserveProviderFirstAudio(provider string, latency time.Duration) {
	m.ObserveSummary(MetricProviderFirstAudioSeconds, map[string]string{"provider": labelOrUnknown(provider)}, latency.Seconds())
}

func (m *Metrics) ObserveSpeechEndToFirstAudible(provider string, latency time.Duration) {
	m.ObserveSummary(MetricSpeechEndToFirstAudibleSeconds, map[string]string{"provider": labelOrUnknown(provider)}, latency.Seconds())
}

func (m *Metrics) ObserveBargeInStop(reason string, latency time.Duration) {
	m.ObserveSummary(MetricBargeInStopSeconds, map[string]string{"reason": labelOrUnknown(reason)}, latency.Seconds())
}

func (m *Metrics) SetDownlinkQueueFrames(frames int) {
	m.SetGauge(MetricDownlinkQueueFrames, nil, float64(frames))
}

func (m *Metrics) IncMCPToolCall(tool string, allowed bool) {
	m.IncCounter(MetricMCPToolCallsTotal, map[string]string{
		"tool":    labelOrUnknown(tool),
		"allowed": strconv.FormatBool(allowed),
	}, 1)
}

func (m *Metrics) ObserveMCPToolLatency(tool string, latency time.Duration) {
	m.ObserveSummary(MetricMCPToolLatencySeconds, map[string]string{"tool": labelOrUnknown(tool)}, latency.Seconds())
}

func (m *Metrics) IncCounter(name string, labels map[string]string, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := sampleKey(name, labels)
	current := m.counters[key]
	current.Name = name
	current.Labels = cloneLabels(labels)
	current.Value += delta
	m.counters[key] = current
}

func (m *Metrics) SetGauge(name string, labels map[string]string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := sampleKey(name, labels)
	m.gauges[key] = sample{Name: name, Labels: cloneLabels(labels), Value: value}
}

func (m *Metrics) AddGauge(name string, labels map[string]string, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := sampleKey(name, labels)
	current := m.gauges[key]
	current.Name = name
	current.Labels = cloneLabels(labels)
	current.Value += delta
	m.gauges[key] = current
}

func (m *Metrics) ObserveSummary(name string, labels map[string]string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := sampleKey(name, labels)
	current := m.summaries[key]
	current.Name = name
	current.Labels = cloneLabels(labels)
	current.Count++
	current.Sum += value
	m.summaries[key] = current
}

func (m *Metrics) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var builder strings.Builder
	for _, definition := range metricDefinitions {
		builder.WriteString("# HELP ")
		builder.WriteString(definition.Name)
		builder.WriteByte(' ')
		builder.WriteString(definition.Help)
		builder.WriteByte('\n')
		builder.WriteString("# TYPE ")
		builder.WriteString(definition.Name)
		builder.WriteByte(' ')
		builder.WriteString(definition.Kind)
		builder.WriteByte('\n')

		switch definition.Kind {
		case "counter":
			writeSamples(&builder, definition.Name, m.counterSamplesLocked(definition.Name))
		case "gauge":
			writeSamples(&builder, definition.Name, m.gaugeSamplesLocked(definition.Name))
		case "summary":
			writeSummaries(&builder, definition.Name, m.summarySamplesLocked(definition.Name))
		}
	}
	return builder.String()
}

func (m *Metrics) counterSamplesLocked(name string) []sample {
	return samplesForName(m.counters, name)
}

func (m *Metrics) gaugeSamplesLocked(name string) []sample {
	return samplesForName(m.gauges, name)
}

func (m *Metrics) summarySamplesLocked(name string) []summary {
	summaries := make([]summary, 0)
	for _, item := range m.summaries {
		if item.Name == name {
			summaries = append(summaries, item)
		}
	}
	sort.Slice(summaries, func(i, j int) bool {
		return labelsString(summaries[i].Labels) < labelsString(summaries[j].Labels)
	})
	return summaries
}

func samplesForName(source map[string]sample, name string) []sample {
	samples := make([]sample, 0)
	for _, item := range source {
		if item.Name == name {
			samples = append(samples, item)
		}
	}
	sort.Slice(samples, func(i, j int) bool {
		return labelsString(samples[i].Labels) < labelsString(samples[j].Labels)
	})
	return samples
}

func writeSamples(builder *strings.Builder, name string, samples []sample) {
	if len(samples) == 0 {
		builder.WriteString(name)
		builder.WriteString(" 0\n")
		return
	}
	for _, item := range samples {
		builder.WriteString(name)
		builder.WriteString(formatLabels(item.Labels))
		builder.WriteByte(' ')
		builder.WriteString(strconv.FormatFloat(item.Value, 'f', -1, 64))
		builder.WriteByte('\n')
	}
}

func writeSummaries(builder *strings.Builder, name string, summaries []summary) {
	if len(summaries) == 0 {
		builder.WriteString(name)
		builder.WriteString("_count 0\n")
		builder.WriteString(name)
		builder.WriteString("_sum 0\n")
		return
	}
	for _, item := range summaries {
		labels := formatLabels(item.Labels)
		builder.WriteString(name)
		builder.WriteString("_count")
		builder.WriteString(labels)
		builder.WriteByte(' ')
		builder.WriteString(strconv.FormatUint(item.Count, 10))
		builder.WriteByte('\n')
		builder.WriteString(name)
		builder.WriteString("_sum")
		builder.WriteString(labels)
		builder.WriteByte(' ')
		builder.WriteString(strconv.FormatFloat(item.Sum, 'f', -1, 64))
		builder.WriteByte('\n')
	}
}

func sampleKey(name string, labels map[string]string) string {
	return name + labelsString(labels)
}

func labelsString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteByte('|')
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(labels[key])
	}
	return builder.String()
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(key)
		builder.WriteString("=\"")
		builder.WriteString(escapeLabel(labels[key]))
		builder.WriteByte('"')
	}
	builder.WriteByte('}')
	return builder.String()
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	clone := make(map[string]string, len(labels))
	for key, value := range labels {
		clone[key] = value
	}
	return clone
}

func escapeLabel(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(value)
}

func labelOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func (m *Metrics) String() string {
	return fmt.Sprintf("Metrics{%d counters, %d gauges, %d summaries}", len(m.counters), len(m.gauges), len(m.summaries))
}
