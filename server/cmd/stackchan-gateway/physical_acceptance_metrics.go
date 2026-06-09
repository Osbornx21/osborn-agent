package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/observability"
)

type physicalAcceptanceMetricsSummary struct {
	DeviceID               string                                   `json:"device_id"`
	CompletedTurns         int                                      `json:"completed_turns"`
	AudioTurns             int                                      `json:"audio_turns"`
	LLMRequestTurns        int                                      `json:"llm_request_turns"`
	LLMRecentContextTurns  int                                      `json:"llm_recent_context_turns"`
	MaxRecentTurnCount     int                                      `json:"max_recent_turn_count"`
	ContinuityContextOK    bool                                     `json:"continuity_context_ok"`
	P50FirstAudibleMS      int                                      `json:"p50_first_audible_ms"`
	P95FirstAudibleMS      int                                      `json:"p95_first_audible_ms"`
	BargeInStopLatencyMS   int                                      `json:"barge_in_stop_latency_ms"`
	BodyMCPToolSuccessRate float64                                  `json:"body_mcp_tool_success_rate"`
	CameraToolCallCount    int                                      `json:"camera_tool_call_count"`
	UnexpectedCamera       bool                                     `json:"unexpected_camera_triggered"`
	TTSAudioQuality        physicalAcceptanceTTSAudioQualitySummary `json:"tts_audio_quality"`
	TurnWindow             string                                   `json:"turn_window"`
	TraceSince             string                                   `json:"trace_since,omitempty"`
	FirstAudibleBasis      string                                   `json:"first_audible_basis"`
	ContinuityBasis        string                                   `json:"continuity_basis"`
	GeneratedAt            string                                   `json:"generated_at"`
}

type physicalAcceptanceTTSAudioQualitySummary struct {
	EventCount        int     `json:"event_count"`
	SampleCount       int64   `json:"sample_count"`
	DurationMS        int     `json:"duration_ms"`
	PeakDBFSMax       float64 `json:"peak_dbfs_max"`
	RMSDBFSP50        float64 `json:"rms_dbfs_p50"`
	RMSDBFSP95        float64 `json:"rms_dbfs_p95"`
	ClippedPercentMax float64 `json:"clipped_percent_max"`
	SilencePercentMax float64 `json:"silence_percent_max"`
	DCOffsetMaxAbs    float64 `json:"dc_offset_max_abs"`
}

func runPhysicalAcceptanceMetrics(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("physical-acceptance-metrics", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tracePath := flags.String("trace-file", "", "trace JSONL file to summarize")
	deviceID := flags.String("device", "", "expected trace device id; use the runtime Xiaozhi Device-Id/MAC for physical hardware")
	minTurns := flags.Int("min-turns", 20, "minimum completed turns with downlink audio")
	latestTurns := flags.Int("latest-turns", 0, "optional latest completed-turn window to summarize; 0 means all completed turns")
	sinceRaw := flags.String("since", "", "optional RFC3339 timestamp; ignore trace events before this time")
	bargeInStopLatencyMS := flags.Int("barge-in-stop-latency-ms", 0, "optional measured barge-in stop latency in ms")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*tracePath) == "" {
		fmt.Fprintln(stderr, "physical-acceptance-metrics failed: --trace-file is required")
		return 2
	}
	if strings.TrimSpace(*deviceID) == "" {
		fmt.Fprintln(stderr, "physical-acceptance-metrics failed: --device is required; use the runtime Xiaozhi Device-Id/MAC from the trace")
		return 2
	}
	since, err := parseAcceptanceMetricsSince(*sinceRaw)
	if err != nil {
		fmt.Fprintf(stderr, "physical-acceptance-metrics failed: %v\n", err)
		return 2
	}
	summary, err := buildPhysicalAcceptanceMetricsSummary(*tracePath, strings.TrimSpace(*deviceID), *minTurns, *latestTurns, *bargeInStopLatencyMS, since)
	if err != nil {
		fmt.Fprintf(stderr, "physical-acceptance-metrics failed: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "physical-acceptance-metrics failed: marshal summary: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

func buildPhysicalAcceptanceMetricsSummary(tracePath string, deviceID string, minTurns int, latestTurns int, bargeInStopLatencyMS int, since time.Time) (physicalAcceptanceMetricsSummary, error) {
	if minTurns <= 0 {
		return physicalAcceptanceMetricsSummary{}, errors.New("--min-turns must be positive")
	}
	if latestTurns < 0 {
		return physicalAcceptanceMetricsSummary{}, errors.New("--latest-turns must be non-negative")
	}
	if latestTurns > 0 && latestTurns < minTurns {
		return physicalAcceptanceMetricsSummary{}, errors.New("--latest-turns must be >= --min-turns")
	}
	if bargeInStopLatencyMS < 0 || bargeInStopLatencyMS > 350 {
		return physicalAcceptanceMetricsSummary{}, errors.New("--barge-in-stop-latency-ms must be between 0 and 350")
	}
	events, err := loadTraceEvents(tracePath)
	if err != nil {
		return physicalAcceptanceMetricsSummary{}, err
	}
	events = filterAcceptanceMetricEventsSince(events, since)
	turns := groupAcceptanceMetricTurns(events, deviceID)
	selectedTurns := selectAcceptanceMetricTurns(turns, latestTurns)
	if bargeInStopLatencyMS == 0 {
		bargeInStopLatencyMS = deriveBargeInStopLatencyMS(events, deviceID, selectedTurns)
	}
	cameraToolCallCount := countAcceptanceMetricCameraToolCalls(events, deviceID, selectedTurns)
	ttsAudioQuality := summarizeAcceptanceMetricTTSAudioQuality(events, deviceID, selectedTurns)
	completedTurns := len(selectedTurns)
	latencies := []int{}
	bodySent := 0
	bodyTotal := 0
	llmRequestTurns := 0
	llmRecentContextTurns := 0
	maxRecentTurnCount := 0
	for _, turn := range selectedTurns {
		bodySent += turn.bodyDispatchSent
		bodyTotal += turn.bodyDispatchTotal
		if turn.speechFinalElapsedMS > 0 && turn.firstDownlinkElapsedMS > 0 && turn.firstDownlinkElapsedMS >= turn.speechFinalElapsedMS {
			latencies = append(latencies, int(turn.firstDownlinkElapsedMS-turn.speechFinalElapsedMS))
		}
		if turn.llmRequestSeen {
			llmRequestTurns++
		}
		if turn.recentTurnCountMax > 0 {
			llmRecentContextTurns++
			if turn.recentTurnCountMax > maxRecentTurnCount {
				maxRecentTurnCount = turn.recentTurnCountMax
			}
		}
	}
	sort.Ints(latencies)
	bodyRate := bodyMCPToolSuccessRate(bodySent, bodyTotal)
	if completedTurns < minTurns {
		return physicalAcceptanceMetricsSummary{}, fmt.Errorf("completed_turns must be >= %d, got %d", minTurns, completedTurns)
	}
	if len(latencies) < minTurns {
		return physicalAcceptanceMetricsSummary{}, fmt.Errorf("audio_turns must be >= %d, got %d", minTurns, len(latencies))
	}
	traceSince := ""
	if !since.IsZero() {
		traceSince = since.UTC().Format(time.RFC3339)
	}
	return physicalAcceptanceMetricsSummary{
		DeviceID:               deviceID,
		CompletedTurns:         completedTurns,
		AudioTurns:             len(latencies),
		LLMRequestTurns:        llmRequestTurns,
		LLMRecentContextTurns:  llmRecentContextTurns,
		MaxRecentTurnCount:     maxRecentTurnCount,
		ContinuityContextOK:    llmRequestTurns >= minTurns && llmRecentContextTurns > 0 && maxRecentTurnCount > 0,
		P50FirstAudibleMS:      percentileNearestRank(latencies, 0.50),
		P95FirstAudibleMS:      percentileNearestRank(latencies, 0.95),
		BargeInStopLatencyMS:   bargeInStopLatencyMS,
		BodyMCPToolSuccessRate: bodyRate,
		CameraToolCallCount:    cameraToolCallCount,
		UnexpectedCamera:       cameraToolCallCount > 0,
		TTSAudioQuality:        ttsAudioQuality,
		TurnWindow:             acceptanceMetricTurnWindow(latestTurns),
		TraceSince:             traceSince,
		FirstAudibleBasis:      "first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms",
		ContinuityBasis:        "llm_request.fields.recent_turn_count > 0",
		GeneratedAt:            time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func parseAcceptanceMetricsSince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	since, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since must be an RFC3339 timestamp")
	}
	return since, nil
}

func filterAcceptanceMetricEventsSince(events []observability.TraceEvent, since time.Time) []observability.TraceEvent {
	if since.IsZero() {
		return events
	}
	filtered := make([]observability.TraceEvent, 0, len(events))
	for _, event := range events {
		if event.Timestamp.IsZero() || event.Timestamp.Before(since) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func loadPhysicalAcceptanceMetricsSummary(path string) (physicalAcceptanceMetricsSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return physicalAcceptanceMetricsSummary{}, fmt.Errorf("read metrics %q: %w", path, err)
	}
	var summary physicalAcceptanceMetricsSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return physicalAcceptanceMetricsSummary{}, fmt.Errorf("parse metrics %q: %w", path, err)
	}
	return summary, nil
}

type acceptanceMetricTurn struct {
	completed              bool
	errorCode              string
	speechFinalElapsedMS   int64
	firstDownlinkElapsedMS int64
	bodyDispatchSent       int
	bodyDispatchTotal      int
	llmRequestSeen         bool
	recentTurnCountMax     int
	startEventIndex        int
	endEventIndex          int
}

func groupAcceptanceMetricTurns(events []observability.TraceEvent, deviceID string) []acceptanceMetricTurn {
	turns := []acceptanceMetricTurn{}
	current := -1
	for index, event := range events {
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		if event.Event == "listen_start" {
			turns = append(turns, acceptanceMetricTurn{
				startEventIndex: index,
				endEventIndex:   index,
			})
			current = len(turns) - 1
			continue
		}
		if current < 0 {
			continue
		}
		turn := turns[current]
		switch event.Event {
		case "speech_final":
			if turn.speechFinalElapsedMS == 0 || event.ElapsedMS < turn.speechFinalElapsedMS {
				turn.speechFinalElapsedMS = event.ElapsedMS
			}
		case "first_downlink_audio_sent":
			if turn.firstDownlinkElapsedMS == 0 || event.ElapsedMS < turn.firstDownlinkElapsedMS {
				turn.firstDownlinkElapsedMS = event.ElapsedMS
			}
		case "turn_complete":
			turn.completed = true
			turn.errorCode = strings.TrimSpace(event.ErrorCode)
		case "llm_request":
			turn.llmRequestSeen = true
			if recentTurnCount := fieldInt(event.Fields, "recent_turn_count"); recentTurnCount > turn.recentTurnCountMax {
				turn.recentTurnCountMax = recentTurnCount
			}
		case "stackchan_body_dispatch":
			channel := fieldString(event.Fields, "channel")
			reason := fieldString(event.Fields, "reason")
			if isAcceptanceMetricBodyDispatchRelevant(channel, reason) {
				turn.bodyDispatchTotal++
				if fieldString(event.Fields, "result") == "sent" {
					turn.bodyDispatchSent++
				}
			}
		}
		turn.endEventIndex = index
		turns[current] = turn
	}
	return turns
}

func selectAcceptanceMetricTurns(turns []acceptanceMetricTurn, latestTurns int) []acceptanceMetricTurn {
	selected := []acceptanceMetricTurn{}
	for _, turn := range turns {
		if turn.completed && turn.errorCode == "" {
			selected = append(selected, turn)
		}
	}
	if latestTurns > 0 && len(selected) > latestTurns {
		return selected[len(selected)-latestTurns:]
	}
	return selected
}

func isAcceptanceMetricBodyDispatchRelevant(channel string, reason string) bool {
	channel = strings.ToLower(strings.TrimSpace(channel))
	reason = strings.ToLower(strings.TrimSpace(reason))
	return !(channel == "led" && reason == "idle_start")
}

func deriveBargeInStopLatencyMS(events []observability.TraceEvent, deviceID string, selectedTurns []acceptanceMetricTurn) int {
	if len(selectedTurns) == 0 {
		return 0
	}
	selectedStarts := make(map[int]bool, len(selectedTurns))
	minIndex := selectedTurns[0].startEventIndex
	maxIndex := selectedTurns[0].endEventIndex
	for _, turn := range selectedTurns {
		selectedStarts[turn.startEventIndex] = true
		if turn.startEventIndex < minIndex {
			minIndex = turn.startEventIndex
		}
		if turn.endEventIndex > maxIndex {
			maxIndex = turn.endEventIndex
		}
	}

	latencies := []int{}
	for index, event := range events {
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		if event.Event != "tts_stop_sent" {
			continue
		}
		reason := strings.ToLower(strings.TrimSpace(fieldString(event.Fields, "reason")))
		if reason != "listen_start" && reason != "abort" {
			continue
		}
		if index < minIndex || index > maxIndex {
			if !selectedStarts[nextListenStartIndex(events, deviceID, index)] {
				continue
			}
		}
		latency := fieldInt(event.Fields, "stop_latency_ms")
		if latency > 0 && latency <= 350 {
			latencies = append(latencies, latency)
		}
	}
	if len(latencies) == 0 {
		return 0
	}
	sort.Ints(latencies)
	return percentileNearestRank(latencies, 0.95)
}

func countAcceptanceMetricCameraToolCalls(events []observability.TraceEvent, deviceID string, selectedTurns []acceptanceMetricTurn) int {
	if len(selectedTurns) == 0 {
		return 0
	}
	minIndex := selectedTurns[0].startEventIndex
	maxIndex := selectedTurns[0].endEventIndex
	for _, turn := range selectedTurns {
		if turn.startEventIndex < minIndex {
			minIndex = turn.startEventIndex
		}
		if turn.endEventIndex > maxIndex {
			maxIndex = turn.endEventIndex
		}
	}

	count := 0
	for index, event := range events {
		if index < minIndex || index > maxIndex {
			continue
		}
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		if event.Event != "llm_tool_call" {
			continue
		}
		if fieldString(event.Fields, "tool_name") == mcp.ToolTakePhoto {
			count++
		}
	}
	return count
}

func summarizeAcceptanceMetricTTSAudioQuality(events []observability.TraceEvent, deviceID string, selectedTurns []acceptanceMetricTurn) physicalAcceptanceTTSAudioQualitySummary {
	if len(selectedTurns) == 0 {
		return physicalAcceptanceTTSAudioQualitySummary{}
	}
	minIndex := selectedTurns[0].startEventIndex
	maxIndex := selectedTurns[0].endEventIndex
	for _, turn := range selectedTurns {
		if turn.startEventIndex < minIndex {
			minIndex = turn.startEventIndex
		}
		if turn.endEventIndex > maxIndex {
			maxIndex = turn.endEventIndex
		}
	}

	rmsValues := []float64{}
	summary := physicalAcceptanceTTSAudioQualitySummary{}
	peakSet := false
	for index, event := range events {
		if index < minIndex || index > maxIndex {
			continue
		}
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		if event.Event != "tts_audio_quality" {
			continue
		}
		summary.EventCount++
		summary.SampleCount += int64(fieldInt(event.Fields, "sample_count"))
		summary.DurationMS += fieldInt(event.Fields, "duration_ms")
		peak := fieldFloat(event.Fields, "peak_dbfs")
		if !peakSet || peak > summary.PeakDBFSMax {
			summary.PeakDBFSMax = peak
			peakSet = true
		}
		rmsValues = append(rmsValues, fieldFloat(event.Fields, "rms_dbfs"))
		if clipped := fieldFloat(event.Fields, "clipped_percent"); clipped > summary.ClippedPercentMax {
			summary.ClippedPercentMax = clipped
		}
		if silence := fieldFloat(event.Fields, "silence_percent"); silence > summary.SilencePercentMax {
			summary.SilencePercentMax = silence
		}
		if dcOffset := math.Abs(fieldFloat(event.Fields, "dc_offset")); dcOffset > summary.DCOffsetMaxAbs {
			summary.DCOffsetMaxAbs = dcOffset
		}
	}
	sort.Float64s(rmsValues)
	summary.RMSDBFSP50 = percentileNearestRankFloat(rmsValues, 0.50)
	summary.RMSDBFSP95 = percentileNearestRankFloat(rmsValues, 0.95)
	return summary
}

func nextListenStartIndex(events []observability.TraceEvent, deviceID string, afterIndex int) int {
	for index := afterIndex + 1; index < len(events); index++ {
		event := events[index]
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		if event.Event == "listen_start" {
			return index
		}
	}
	return -1
}

func fieldInt(fields map[string]any, key string) int {
	if fields == nil {
		return 0
	}
	switch value := fields[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0
		}
		return int(parsed)
	default:
		return 0
	}
}

func fieldFloat(fields map[string]any, key string) float64 {
	if fields == nil {
		return 0
	}
	switch value := fields[key].(type) {
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case float64:
		return value
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func bodyMCPToolSuccessRate(sent int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(sent) / float64(total)
}

func acceptanceMetricTurnWindow(latestTurns int) string {
	if latestTurns <= 0 {
		return "all_completed_turns"
	}
	return fmt.Sprintf("latest_%d_completed_turns", latestTurns)
}

func percentileNearestRankFloat(sortedValues []float64, percentile float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	index := int(percentile*float64(len(sortedValues)) + 0.999999999)
	if index < 1 {
		index = 1
	}
	if index > len(sortedValues) {
		index = len(sortedValues)
	}
	return sortedValues[index-1]
}

func percentileNearestRank(sortedValues []int, percentile float64) int {
	if len(sortedValues) == 0 {
		return 0
	}
	index := int(percentile*float64(len(sortedValues)) + 0.999999999)
	if index < 1 {
		index = 1
	}
	if index > len(sortedValues) {
		index = len(sortedValues)
	}
	return sortedValues[index-1]
}
