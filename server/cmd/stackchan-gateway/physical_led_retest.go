package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stackchan-gateway/internal/observability"
)

const physicalLEDRetestSchemaVersion = "stackchan_physical_led_retest_v1"

type physicalLEDRetestReport struct {
	SchemaVersion                   string   `json:"schema_version"`
	DeviceID                        string   `json:"device_id"`
	GatewayCommit                   string   `json:"gateway_commit,omitempty"`
	TraceID                         string   `json:"trace_id"`
	SessionID                       string   `json:"session_id"`
	Generation                      int64    `json:"generation"`
	ListenStartSequence             uint64   `json:"listen_start_sequence"`
	ServerTraceOK                   bool     `json:"server_trace_ok"`
	VisualGreenConfirmed            bool     `json:"visual_green_confirmed"`
	NoLEDOverwriteBeforeSpeechFinal bool     `json:"no_led_overwrite_before_speech_final"`
	ListenStartTimestamp            string   `json:"listen_start_timestamp,omitempty"`
	ListenStartElapsedMS            int64    `json:"listen_start_elapsed_ms"`
	FirstUplinkAudioElapsedMS       int64    `json:"first_uplink_audio_elapsed_ms"`
	LEDDispatchElapsedMS            int64    `json:"led_dispatch_elapsed_ms"`
	SpeechFinalElapsedMS            int64    `json:"speech_final_elapsed_ms"`
	ObservedAt                      string   `json:"observed_at"`
	ExpectedVisualState             string   `json:"expected_visual_state"`
	TraceEvents                     []string `json:"trace_events"`
	Notes                           string   `json:"notes,omitempty"`
}

func runPhysicalLEDRetest(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("physical-led-retest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tracePath := flags.String("trace-file", "", "trace JSONL file to inspect")
	reportPath := flags.String("report", "", "optional physical LED retest report JSON path")
	deviceID := flags.String("device", "stackchan-s3-main", "expected device id")
	traceID := flags.String("trace-id", "", "optional exact trace id for an operator-observed turn")
	listenStartSequence := flags.Uint64("listen-start-sequence", 0, "optional exact listen_start sequence inside --trace-id")
	gatewayCommit := flags.String("gateway-commit", "", "deployed gateway commit for the retest report")
	visualGreenConfirmed := flags.Bool("visual-green-confirmed", false, "operator confirms the physical ASR/listening LED was visibly green")
	notes := flags.String("notes", "", "safe operator notes; do not include transcripts or secrets")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*tracePath) == "" {
		fmt.Fprintln(stderr, "physical-led-retest failed: --trace-file is required")
		return 2
	}
	if !*visualGreenConfirmed {
		fmt.Fprintln(stderr, "physical-led-retest failed: --visual-green-confirmed is required after observing the physical device")
		return 1
	}
	safeNotes, err := sanitizePhysicalLEDRetestNotes(*notes)
	if err != nil {
		fmt.Fprintf(stderr, "physical-led-retest failed: %v\n", err)
		return 2
	}
	report, err := buildPhysicalLEDRetestReport(*tracePath, strings.TrimSpace(*deviceID), strings.TrimSpace(*traceID), *listenStartSequence, strings.TrimSpace(*gatewayCommit), safeNotes, *visualGreenConfirmed)
	if err != nil {
		fmt.Fprintf(stderr, "physical-led-retest failed: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*reportPath) != "" {
		if err := writePhysicalLEDRetestReport(*reportPath, report); err != nil {
			fmt.Fprintf(stderr, "physical-led-retest failed: write report: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "physical led retest OK: device=%s trace_id=%s generation=%d listen_start_sequence=%d led_dispatch_elapsed_ms=%d speech_final_elapsed_ms=%d visual_green_confirmed=%t\n", report.DeviceID, report.TraceID, report.Generation, report.ListenStartSequence, report.LEDDispatchElapsedMS, report.SpeechFinalElapsedMS, report.VisualGreenConfirmed)
	return 0
}

func buildPhysicalLEDRetestReport(tracePath string, deviceID string, traceID string, listenStartSequence uint64, gatewayCommit string, notes string, visualGreenConfirmed bool) (physicalLEDRetestReport, error) {
	events, err := loadTraceEvents(tracePath)
	if err != nil {
		return physicalLEDRetestReport{}, err
	}
	candidate, err := findPhysicalLEDRetestCandidate(events, deviceID, traceID, listenStartSequence)
	if err != nil {
		return physicalLEDRetestReport{}, err
	}
	report := physicalLEDRetestReport{
		SchemaVersion:                   physicalLEDRetestSchemaVersion,
		DeviceID:                        candidate.listenStart.DeviceID,
		GatewayCommit:                   gatewayCommit,
		TraceID:                         candidate.listenStart.TraceID,
		SessionID:                       candidate.listenStart.SessionID,
		Generation:                      candidate.listenStart.Generation,
		ListenStartSequence:             candidate.listenStart.Sequence,
		ServerTraceOK:                   true,
		VisualGreenConfirmed:            visualGreenConfirmed,
		NoLEDOverwriteBeforeSpeechFinal: !candidate.ledOverwriteBeforeSpeechFinal,
		ListenStartTimestamp:            formatTraceTimestamp(candidate.listenStart.Timestamp),
		ListenStartElapsedMS:            candidate.listenStart.ElapsedMS,
		FirstUplinkAudioElapsedMS:       candidate.firstUplinkAudio.ElapsedMS,
		LEDDispatchElapsedMS:            candidate.ledDispatch.ElapsedMS,
		SpeechFinalElapsedMS:            candidate.speechFinal.ElapsedMS,
		ObservedAt:                      time.Now().UTC().Format(time.RFC3339),
		ExpectedVisualState:             "green_listening_asr",
		TraceEvents:                     []string{"listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"},
		Notes:                           notes,
	}
	if candidate.ledOverwriteBeforeSpeechFinal {
		return physicalLEDRetestReport{}, errors.New("LED was overwritten before speech_final in the trace")
	}
	return report, nil
}

type physicalLEDRetestCandidate struct {
	listenStart                   observability.TraceEvent
	firstUplinkAudio              observability.TraceEvent
	ledDispatch                   observability.TraceEvent
	speechFinal                   observability.TraceEvent
	ledOverwriteBeforeSpeechFinal bool
}

func findPhysicalLEDRetestCandidate(events []observability.TraceEvent, deviceID string, traceID string, listenStartSequence uint64) (physicalLEDRetestCandidate, error) {
	traceID = strings.TrimSpace(traceID)
	if listenStartSequence > 0 && traceID == "" {
		return physicalLEDRetestCandidate{}, errors.New("--trace-id is required when --listen-start-sequence is set")
	}
	if traceID == "" {
		return findLatestPhysicalLEDRetestCandidate(events, deviceID)
	}
	var matched []observability.TraceEvent
	for _, event := range events {
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		eventTraceID := strings.TrimSpace(event.TraceID)
		if eventTraceID == "" {
			eventTraceID = observability.TraceID(event.SessionID, event.Generation)
		}
		if eventTraceID == traceID {
			matched = append(matched, event)
		}
	}
	if len(matched) == 0 {
		if deviceID == "" {
			return physicalLEDRetestCandidate{}, fmt.Errorf("trace %s was not found", traceID)
		}
		return physicalLEDRetestCandidate{}, fmt.Errorf("trace %s for device %s was not found", traceID, deviceID)
	}
	return physicalLEDRetestCandidateForTrace(matched, listenStartSequence)
}

func loadTraceEvents(path string) ([]observability.TraceEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read trace file %q: %w", path, err)
	}
	defer file.Close()

	var events []observability.TraceEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event observability.TraceEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("decode trace jsonl: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read trace jsonl: %w", err)
	}
	if len(events) == 0 {
		return nil, errors.New("trace file has no events")
	}
	return events, nil
}

func findLatestPhysicalLEDRetestCandidate(events []observability.TraceEvent, deviceID string) (physicalLEDRetestCandidate, error) {
	groups := map[string][]observability.TraceEvent{}
	order := []string{}
	for _, event := range events {
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		traceID := strings.TrimSpace(event.TraceID)
		if traceID == "" {
			traceID = observability.TraceID(event.SessionID, event.Generation)
		}
		if _, ok := groups[traceID]; !ok {
			order = append(order, traceID)
		}
		groups[traceID] = append(groups[traceID], event)
	}

	latestTraceID := ""
	for _, traceID := range order {
		if traceHasEvent(groups[traceID], "listen_start") {
			latestTraceID = traceID
		}
	}
	if latestTraceID == "" {
		if deviceID == "" {
			return physicalLEDRetestCandidate{}, errors.New("no trace has listen_start")
		}
		return physicalLEDRetestCandidate{}, fmt.Errorf("no trace for device %s has listen_start", deviceID)
	}
	candidate, err := physicalLEDRetestCandidateForTrace(groups[latestTraceID], 0)
	if err != nil {
		return physicalLEDRetestCandidate{}, err
	}
	return candidate, nil
}

func physicalLEDRetestCandidateForTrace(events []observability.TraceEvent, listenStartSequence uint64) (physicalLEDRetestCandidate, error) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].ElapsedMS == events[j].ElapsedMS {
			return events[i].Sequence < events[j].Sequence
		}
		return events[i].ElapsedMS < events[j].ElapsedMS
	})
	var candidate physicalLEDRetestCandidate
	listenIndex := -1
	firstUplinkAudioIndex := -1
	ledIndex := -1
	speechFinalIndex := -1
	for index, event := range events {
		switch {
		case listenIndex < 0 && event.Event == "listen_start" && (listenStartSequence == 0 || event.Sequence == listenStartSequence):
			candidate.listenStart = event
			listenIndex = index
		case listenIndex >= 0 && candidate.firstUplinkAudio.Event == "" && event.Event == "first_uplink_audio":
			candidate.firstUplinkAudio = event
			firstUplinkAudioIndex = index
		case listenIndex >= 0 && candidate.ledDispatch.Event == "" && isListenStartLEDDispatch(event):
			candidate.ledDispatch = event
			ledIndex = index
		case listenIndex >= 0 && candidate.speechFinal.Event == "" && event.Event == "speech_final":
			candidate.speechFinal = event
			speechFinalIndex = index
		}
	}
	traceID := traceLabel(events)
	var missing []string
	if listenIndex < 0 {
		if listenStartSequence > 0 {
			missing = append(missing, fmt.Sprintf("listen_start sequence %d", listenStartSequence))
		} else {
			missing = append(missing, "listen_start")
		}
	}
	if firstUplinkAudioIndex < 0 {
		missing = append(missing, "first_uplink_audio")
	}
	if ledIndex < 0 {
		missing = append(missing, "listen-start led dispatch")
	}
	if speechFinalIndex < 0 {
		missing = append(missing, "speech_final")
	}
	if len(missing) > 0 {
		return physicalLEDRetestCandidate{}, fmt.Errorf("trace %s lacks %s", traceID, strings.Join(missing, ", "))
	}
	var orderingProblems []string
	if firstUplinkAudioIndex > speechFinalIndex {
		orderingProblems = append(orderingProblems, "first_uplink_audio before speech_final")
	}
	if ledIndex > speechFinalIndex {
		orderingProblems = append(orderingProblems, "listen-start led dispatch before speech_final")
	}
	if len(orderingProblems) > 0 {
		return physicalLEDRetestCandidate{}, fmt.Errorf("trace %s requires %s", traceID, strings.Join(orderingProblems, ", "))
	}
	if ledIndex >= 0 && speechFinalIndex >= 0 {
		for index := ledIndex + 1; index < speechFinalIndex; index++ {
			if isLEDOverwriteEvent(events[index]) {
				candidate.ledOverwriteBeforeSpeechFinal = true
				break
			}
		}
	}
	return candidate, nil
}

func isListenStartLEDDispatch(event observability.TraceEvent) bool {
	return event.Event == "stackchan_body_dispatch" &&
		fieldString(event.Fields, "channel") == "led" &&
		fieldString(event.Fields, "reason") == "listen_start" &&
		fieldString(event.Fields, "result") == "sent"
}

func isLEDOverwriteEvent(event observability.TraceEvent) bool {
	if event.Event == "stackchan_body_dispatch" &&
		fieldString(event.Fields, "channel") == "led" &&
		fieldString(event.Fields, "result") == "sent" {
		return true
	}
	if event.Event == "stackchan_expression_cue_dispatch" &&
		fieldBool(event.Fields, "has_led") &&
		fieldString(event.Fields, "result") == "sent" {
		return true
	}
	return false
}

func traceHasEvent(events []observability.TraceEvent, eventName string) bool {
	for _, event := range events {
		if event.Event == eventName {
			return true
		}
	}
	return false
}

func traceLabel(events []observability.TraceEvent) string {
	if len(events) == 0 {
		return "trace:unknown"
	}
	if traceID := strings.TrimSpace(events[0].TraceID); traceID != "" {
		return traceID
	}
	return observability.TraceID(events[0].SessionID, events[0].Generation)
}

func fieldString(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	value, _ := fields[key].(string)
	return strings.ToLower(strings.TrimSpace(value))
}

func fieldBool(fields map[string]any, key string) bool {
	if fields == nil {
		return false
	}
	value, _ := fields[key].(bool)
	return value
}

func writePhysicalLEDRetestReport(path string, report physicalLEDRetestReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func sanitizePhysicalLEDRetestNotes(notes string) (string, error) {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > 240 {
		return "", errors.New("notes contain unsafe or oversized content; keep retest notes short and do not include transcripts, prompts, tokens or raw LED values")
	}
	lower := strings.ToLower(trimmed)
	for _, fragment := range []string{
		"authorization",
		"bearer ",
		"api_key",
		"apikey",
		"access_key",
		"secret",
		"password",
		"passwd",
		"token",
		"transcript",
		"prompt",
		"generated text",
		"raw audio",
		"payload_base64",
		"payload_hex",
		"red=",
		"green=",
		"blue=",
		`"red"`,
		`"green"`,
		`"blue"`,
		"rgb(",
	} {
		if strings.Contains(lower, fragment) {
			return "", errors.New("notes contain unsafe or oversized content; keep retest notes short and do not include transcripts, prompts, tokens or raw LED values")
		}
	}
	if containsLongHexRun(trimmed, 32) || strings.Contains(lower, "sk-") {
		return "", errors.New("notes contain unsafe or oversized content; keep retest notes short and do not include transcripts, prompts, tokens or raw LED values")
	}
	return trimmed, nil
}

func containsLongHexRun(value string, threshold int) bool {
	run := 0
	for _, r := range value {
		if ('0' <= r && r <= '9') || ('a' <= r && r <= 'f') || ('A' <= r && r <= 'F') {
			run++
			if run >= threshold {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

func formatTraceTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
