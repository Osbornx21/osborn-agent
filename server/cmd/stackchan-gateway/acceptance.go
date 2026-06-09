package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const physicalAcceptanceSchemaVersion = "stackchan_physical_acceptance_v2"

type physicalAcceptanceReport struct {
	SchemaVersion             string                                   `json:"schema_version"`
	DeviceID                  string                                   `json:"device_id"`
	HardwareDeviceID          string                                   `json:"hardware_device_id,omitempty"`
	ClientID                  string                                   `json:"client_id,omitempty"`
	FirmwareBuildID           string                                   `json:"firmware_build_id"`
	FirmwareVersion           string                                   `json:"firmware_version,omitempty"`
	GatewayCommit             string                                   `json:"gateway_commit"`
	ProviderProfile           string                                   `json:"provider_profile"`
	CompletedTurns            int                                      `json:"completed_turns"`
	P50FirstAudibleMS         int                                      `json:"p50_first_audible_ms"`
	P95FirstAudibleMS         int                                      `json:"p95_first_audible_ms"`
	BargeInStopLatencyMS      int                                      `json:"barge_in_stop_latency_ms"`
	BodyMCPToolSuccessRate    float64                                  `json:"body_mcp_tool_success_rate"`
	LLMRequestTurns           int                                      `json:"llm_request_turns"`
	LLMRecentContextTurns     int                                      `json:"llm_recent_context_turns"`
	MaxRecentTurnCount        int                                      `json:"max_recent_turn_count"`
	ContinuityContextOK       bool                                     `json:"continuity_context_ok"`
	ContinuityBasis           string                                   `json:"continuity_basis,omitempty"`
	TTSAudioQuality           physicalAcceptanceTTSAudioQualitySummary `json:"tts_audio_quality"`
	MetricsTurnWindow         string                                   `json:"metrics_turn_window,omitempty"`
	MetricsTraceSince         string                                   `json:"metrics_trace_since,omitempty"`
	FirstAudibleBasis         string                                   `json:"first_audible_basis,omitempty"`
	AudioPlaybackOK           bool                                     `json:"audio_playback_ok"`
	ScreenTextOK              bool                                     `json:"screen_text_ok"`
	HeadControlOK             bool                                     `json:"head_control_ok"`
	LEDLifecycleOK            bool                                     `json:"led_lifecycle_ok"`
	LEDRetestReport           string                                   `json:"led_retest_report"`
	CustomScreenSceneRequired bool                                     `json:"custom_screen_scene_mcp_required"`
	CustomScreenSceneOK       bool                                     `json:"custom_screen_scene_mcp_ok,omitempty"`
	CameraToolCallCount       int                                      `json:"camera_tool_call_count"`
	UnexpectedCameraTriggered bool                                     `json:"unexpected_camera_triggered"`
	WiFiReconnectOK           bool                                     `json:"wifi_reconnect_ok"`
	GatewayRestartReconnectOK bool                                     `json:"gateway_restart_reconnect_ok"`
	Notes                     string                                   `json:"notes,omitempty"`
}

func runAcceptance(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("acceptance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reportPath := flags.String("report", "", "physical acceptance report JSON path")
	deviceID := flags.String("device", "stackchan-s3-main", "expected device id")
	turns := flags.Int("turns", 20, "minimum completed half-duplex turns")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*reportPath) == "" {
		fmt.Fprintln(stderr, "acceptance failed: --report is required; hardware execution is deferred until the physical StackChan is available")
		return 2
	}
	report, err := loadPhysicalAcceptanceReport(*reportPath)
	if err != nil {
		fmt.Fprintf(stderr, "acceptance failed: %v\n", err)
		return 1
	}
	if err := validatePhysicalAcceptanceReport(report, strings.TrimSpace(*deviceID), *turns); err != nil {
		fmt.Fprintf(stderr, "acceptance failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "physical acceptance report OK: device=%s turns=%d p50_first_audible_ms=%d p95_first_audible_ms=%d barge_in_stop_latency_ms=%d audio_playback_ok=%t screen_text_ok=%t led_lifecycle_ok=%t\n", report.DeviceID, report.CompletedTurns, report.P50FirstAudibleMS, report.P95FirstAudibleMS, report.BargeInStopLatencyMS, report.AudioPlaybackOK, report.ScreenTextOK, report.LEDLifecycleOK)
	return 0
}

func runPhysicalAcceptanceReport(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("physical-acceptance-report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reportPath := flags.String("report", "", "physical acceptance report JSON path to write")
	metricsPath := flags.String("metrics-file", "", "optional physical-acceptance-metrics JSON path to fill turn and latency fields")
	deviceID := flags.String("device", "stackchan-s3-main", "expected logical device id")
	hardwareDeviceID := flags.String("hardware-device-id", "", "physical Device-Id observed on the unit")
	clientID := flags.String("client-id", "", "physical Client-Id/UUID observed on the unit")
	firmwareBuildID := flags.String("firmware-build-id", "", "official firmware build id or package version")
	firmwareVersion := flags.String("firmware-version", "", "official firmware version")
	gatewayCommit := flags.String("gateway-commit", "", "deployed gateway commit")
	providerProfile := flags.String("provider-profile", "", "provider profile used for the run")
	completedTurns := flags.Int("completed-turns", 0, "completed half-duplex turns")
	p50FirstAudibleMS := flags.Int("p50-first-audible-ms", 0, "p50 first audible latency in ms")
	p95FirstAudibleMS := flags.Int("p95-first-audible-ms", 0, "p95 first audible latency in ms")
	bargeInStopLatencyMS := flags.Int("barge-in-stop-latency-ms", 0, "barge-in stop latency in ms")
	bodyMCPToolSuccessRate := flags.Float64("body-mcp-tool-success-rate", 0, "head/LED body MCP tool success rate")
	llmRequestTurns := flags.Int("llm-request-turns", 0, "turns with a traced LLM request")
	llmRecentContextTurns := flags.Int("llm-recent-context-turns", 0, "turns whose LLM request used recent-turn context")
	maxRecentTurnCount := flags.Int("max-recent-turn-count", 0, "maximum recent_turn_count observed on traced LLM requests")
	continuityContextOK := flags.Bool("continuity-context-ok", false, "operator or metrics confirms recent-turn context was present in the acceptance window")
	audioPlaybackOK := flags.Bool("audio-playback-ok", false, "operator confirms physical audio playback worked")
	screenTextOK := flags.Bool("screen-text-ok", false, "operator confirms official Xiaozhi ASR/LLM/TTS screen text was visible and normal")
	headControlOK := flags.Bool("head-control-ok", false, "operator confirms physical head control worked")
	ledLifecycleOK := flags.Bool("led-lifecycle-ok", false, "operator confirms physical lifecycle LED behavior worked")
	ledRetestReport := flags.String("led-retest-report", "", "trace-bound physical LED retest report JSON path")
	noUnexpectedCameraTrigger := flags.Bool("no-unexpected-camera-trigger", false, "operator confirms no surprise camera capture happened during acceptance")
	wifiReconnectOK := flags.Bool("wifi-reconnect-ok", false, "operator confirms Wi-Fi reconnect worked")
	gatewayRestartReconnectOK := flags.Bool("gateway-restart-reconnect-ok", false, "operator confirms reconnect after gateway restart worked")
	notes := flags.String("notes", "", "safe operator notes; do not include transcripts or secrets")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*reportPath) == "" {
		fmt.Fprintln(stderr, "physical-acceptance-report failed: --report is required")
		return 2
	}
	safeNotes, err := sanitizePhysicalAcceptanceNotes(*notes)
	if err != nil {
		fmt.Fprintf(stderr, "physical-acceptance-report failed: %v\n", err)
		return 2
	}
	metrics := physicalAcceptanceMetricsSummary{}
	cameraToolCallCount := 0
	metricsTraceSince := time.Time{}
	if strings.TrimSpace(*metricsPath) != "" {
		var err error
		metrics, err = loadPhysicalAcceptanceMetricsSummary(*metricsPath)
		if err != nil {
			fmt.Fprintf(stderr, "physical-acceptance-report failed: %v\n", err)
			return 1
		}
		if strings.TrimSpace(metrics.DeviceID) != "" && strings.TrimSpace(*deviceID) != "" && metrics.DeviceID != strings.TrimSpace(*deviceID) {
			fmt.Fprintf(stderr, "physical-acceptance-report failed: metrics device_id must match %s\n", strings.TrimSpace(*deviceID))
			return 1
		}
		if *completedTurns == 0 {
			*completedTurns = metrics.CompletedTurns
		}
		if *p50FirstAudibleMS == 0 {
			*p50FirstAudibleMS = metrics.P50FirstAudibleMS
		}
		if *p95FirstAudibleMS == 0 {
			*p95FirstAudibleMS = metrics.P95FirstAudibleMS
		}
		if *bargeInStopLatencyMS == 0 {
			*bargeInStopLatencyMS = metrics.BargeInStopLatencyMS
		}
		if *bodyMCPToolSuccessRate == 0 {
			*bodyMCPToolSuccessRate = metrics.BodyMCPToolSuccessRate
		}
		if *llmRequestTurns == 0 {
			*llmRequestTurns = metrics.LLMRequestTurns
		}
		if *llmRecentContextTurns == 0 {
			*llmRecentContextTurns = metrics.LLMRecentContextTurns
		}
		if *maxRecentTurnCount == 0 {
			*maxRecentTurnCount = metrics.MaxRecentTurnCount
		}
		if !*continuityContextOK {
			*continuityContextOK = metrics.ContinuityContextOK
		}
		cameraToolCallCount = metrics.CameraToolCallCount
		if strings.TrimSpace(metrics.TraceSince) != "" {
			metricsTraceSince, err = parsePhysicalAcceptanceReportTraceSince(metrics.TraceSince)
			if err != nil {
				fmt.Fprintf(stderr, "physical-acceptance-report failed: %v\n", err)
				return 1
			}
		}
	}
	continuityBasis := strings.TrimSpace(metrics.ContinuityBasis)
	if continuityBasis == "" && (*llmRequestTurns > 0 || *llmRecentContextTurns > 0 || *maxRecentTurnCount > 0 || *continuityContextOK) {
		continuityBasis = "llm_request.fields.recent_turn_count > 0"
	}
	report := physicalAcceptanceReport{
		SchemaVersion:             physicalAcceptanceSchemaVersion,
		DeviceID:                  strings.TrimSpace(*deviceID),
		HardwareDeviceID:          strings.TrimSpace(*hardwareDeviceID),
		ClientID:                  strings.TrimSpace(*clientID),
		FirmwareBuildID:           strings.TrimSpace(*firmwareBuildID),
		FirmwareVersion:           strings.TrimSpace(*firmwareVersion),
		GatewayCommit:             strings.TrimSpace(*gatewayCommit),
		ProviderProfile:           strings.TrimSpace(*providerProfile),
		CompletedTurns:            *completedTurns,
		P50FirstAudibleMS:         *p50FirstAudibleMS,
		P95FirstAudibleMS:         *p95FirstAudibleMS,
		BargeInStopLatencyMS:      *bargeInStopLatencyMS,
		BodyMCPToolSuccessRate:    *bodyMCPToolSuccessRate,
		LLMRequestTurns:           *llmRequestTurns,
		LLMRecentContextTurns:     *llmRecentContextTurns,
		MaxRecentTurnCount:        *maxRecentTurnCount,
		ContinuityContextOK:       *continuityContextOK,
		ContinuityBasis:           continuityBasis,
		TTSAudioQuality:           metrics.TTSAudioQuality,
		MetricsTurnWindow:         strings.TrimSpace(metrics.TurnWindow),
		MetricsTraceSince:         strings.TrimSpace(metrics.TraceSince),
		FirstAudibleBasis:         strings.TrimSpace(metrics.FirstAudibleBasis),
		AudioPlaybackOK:           *audioPlaybackOK,
		ScreenTextOK:              *screenTextOK,
		HeadControlOK:             *headControlOK,
		LEDLifecycleOK:            *ledLifecycleOK,
		LEDRetestReport:           strings.TrimSpace(*ledRetestReport),
		CustomScreenSceneRequired: false,
		CameraToolCallCount:       cameraToolCallCount,
		UnexpectedCameraTriggered: !*noUnexpectedCameraTrigger || metrics.UnexpectedCamera,
		WiFiReconnectOK:           *wifiReconnectOK,
		GatewayRestartReconnectOK: *gatewayRestartReconnectOK,
		Notes:                     safeNotes,
	}
	if err := validatePhysicalAcceptanceReportWithLEDWindow(report, strings.TrimSpace(*deviceID), *completedTurns, metricsTraceSince); err != nil {
		fmt.Fprintf(stderr, "physical-acceptance-report failed: %v\n", err)
		return 1
	}
	if err := writePhysicalAcceptanceReport(*reportPath, report); err != nil {
		fmt.Fprintf(stderr, "physical-acceptance-report failed: write report: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "physical acceptance report written: path=%s device=%s turns=%d p50_first_audible_ms=%d p95_first_audible_ms=%d\n", *reportPath, report.DeviceID, report.CompletedTurns, report.P50FirstAudibleMS, report.P95FirstAudibleMS)
	return 0
}

func loadPhysicalAcceptanceReport(path string) (physicalAcceptanceReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return physicalAcceptanceReport{}, fmt.Errorf("read report %q: %w", path, err)
	}
	var report physicalAcceptanceReport
	if err := json.Unmarshal(data, &report); err != nil {
		return physicalAcceptanceReport{}, fmt.Errorf("parse report %q: %w", path, err)
	}
	return report, nil
}

func validatePhysicalAcceptanceReport(report physicalAcceptanceReport, expectedDeviceID string, minTurns int) error {
	ledNotBefore, err := parsePhysicalAcceptanceReportTraceSince(report.MetricsTraceSince)
	if err != nil {
		return err
	}
	return validatePhysicalAcceptanceReportWithLEDWindow(report, expectedDeviceID, minTurns, ledNotBefore)
}

func validatePhysicalAcceptanceReportWithLEDWindow(report physicalAcceptanceReport, expectedDeviceID string, minTurns int, ledNotBefore time.Time) error {
	var problems []string
	if report.SchemaVersion != physicalAcceptanceSchemaVersion {
		problems = append(problems, "schema_version must be "+physicalAcceptanceSchemaVersion)
	}
	if expectedDeviceID != "" && report.DeviceID != expectedDeviceID {
		problems = append(problems, "device_id must match "+expectedDeviceID)
	}
	if strings.TrimSpace(report.FirmwareBuildID) == "" {
		problems = append(problems, "firmware_build_id is required")
	}
	if strings.TrimSpace(report.GatewayCommit) == "" {
		problems = append(problems, "gateway_commit is required")
	}
	if strings.TrimSpace(report.ProviderProfile) == "" {
		problems = append(problems, "provider_profile is required")
	}
	if minTurns <= 0 {
		problems = append(problems, "--turns must be positive")
	} else if report.CompletedTurns < minTurns {
		problems = append(problems, fmt.Sprintf("completed_turns must be >= %d", minTurns))
	}
	if report.P50FirstAudibleMS <= 0 {
		problems = append(problems, "p50_first_audible_ms must be positive")
	}
	if report.P95FirstAudibleMS <= 0 {
		problems = append(problems, "p95_first_audible_ms must be positive")
	}
	if report.P50FirstAudibleMS > 0 && report.P95FirstAudibleMS > 0 && report.P95FirstAudibleMS < report.P50FirstAudibleMS {
		problems = append(problems, "p95_first_audible_ms must be >= p50_first_audible_ms")
	}
	if report.BargeInStopLatencyMS <= 0 || report.BargeInStopLatencyMS > 350 {
		problems = append(problems, "barge_in_stop_latency_ms must be between 1 and 350")
	}
	if report.BodyMCPToolSuccessRate < 0.99 || report.BodyMCPToolSuccessRate > 1 {
		problems = append(problems, "body_mcp_tool_success_rate must be between 0.99 and 1")
	}
	if report.LLMRequestTurns < minTurns {
		problems = append(problems, fmt.Sprintf("llm_request_turns must be >= %d", minTurns))
	}
	if report.LLMRecentContextTurns <= 0 {
		problems = append(problems, "llm_recent_context_turns must be positive")
	}
	if report.LLMRecentContextTurns > report.LLMRequestTurns {
		problems = append(problems, "llm_recent_context_turns must be <= llm_request_turns")
	}
	if report.MaxRecentTurnCount <= 0 {
		problems = append(problems, "max_recent_turn_count must be positive")
	}
	if !report.ContinuityContextOK {
		problems = append(problems, "continuity_context_ok must be true")
	}
	if strings.TrimSpace(report.ContinuityBasis) == "" {
		problems = append(problems, "continuity_basis is required")
	}
	if !report.AudioPlaybackOK {
		problems = append(problems, "audio_playback_ok must be true")
	}
	if !report.ScreenTextOK {
		problems = append(problems, "screen_text_ok must be true")
	}
	if !report.HeadControlOK {
		problems = append(problems, "head_control_ok must be true")
	}
	if !report.LEDLifecycleOK {
		problems = append(problems, "led_lifecycle_ok must be true")
	}
	if strings.TrimSpace(report.LEDRetestReport) == "" {
		problems = append(problems, "led_retest_report is required")
	} else if err := validatePhysicalAcceptanceLEDRetestReport(report.LEDRetestReport, report.DeviceID, ledNotBefore); err != nil {
		problems = append(problems, "led_retest_report invalid: "+err.Error())
	}
	if report.CustomScreenSceneRequired {
		problems = append(problems, "custom_screen_scene_mcp_required must be false for official StackChan acceptance")
	}
	if report.UnexpectedCameraTriggered {
		problems = append(problems, "unexpected_camera_triggered must be false")
	}
	if report.CameraToolCallCount > 0 {
		problems = append(problems, "camera_tool_call_count must be 0")
	}
	if !report.WiFiReconnectOK {
		problems = append(problems, "wifi_reconnect_ok must be true")
	}
	if !report.GatewayRestartReconnectOK {
		problems = append(problems, "gateway_restart_reconnect_ok must be true")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func parsePhysicalAcceptanceReportTraceSince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("metrics trace_since must be an RFC3339 timestamp")
	}
	return timestamp, nil
}

func writePhysicalAcceptanceReport(path string, report physicalAcceptanceReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func sanitizePhysicalAcceptanceNotes(notes string) (string, error) {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > 240 {
		return "", errors.New("notes contain unsafe or oversized content; keep acceptance notes short and do not include transcripts, prompts, tokens, raw audio, generated text or raw device values")
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
			return "", errors.New("notes contain unsafe or oversized content; keep acceptance notes short and do not include transcripts, prompts, tokens, raw audio, generated text or raw device values")
		}
	}
	if containsLongHexRun(trimmed, 32) || strings.Contains(lower, "sk-") {
		return "", errors.New("notes contain unsafe or oversized content; keep acceptance notes short and do not include transcripts, prompts, tokens, raw audio, generated text or raw device values")
	}
	return trimmed, nil
}

func validatePhysicalAcceptanceLEDRetestReport(path string, expectedDeviceID string, notBefore time.Time) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	var report physicalLEDRetestReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parse %q: %w", path, err)
	}
	var problems []string
	if report.SchemaVersion != physicalLEDRetestSchemaVersion {
		problems = append(problems, "schema_version must be "+physicalLEDRetestSchemaVersion)
	}
	if expectedDeviceID != "" && report.DeviceID != expectedDeviceID {
		problems = append(problems, "device_id must match "+expectedDeviceID)
	}
	if strings.TrimSpace(report.TraceID) == "" {
		problems = append(problems, "trace_id is required")
	}
	if report.ListenStartSequence == 0 {
		problems = append(problems, "listen_start_sequence must be positive")
	}
	if !report.ServerTraceOK {
		problems = append(problems, "server_trace_ok must be true")
	}
	if !report.VisualGreenConfirmed {
		problems = append(problems, "visual_green_confirmed must be true")
	}
	if !report.NoLEDOverwriteBeforeSpeechFinal {
		problems = append(problems, "no_led_overwrite_before_speech_final must be true")
	}
	if report.ExpectedVisualState != "green_listening_asr" {
		problems = append(problems, "expected_visual_state must be green_listening_asr")
	}
	if !notBefore.IsZero() {
		listenStartTimestamp, err := parsePhysicalAcceptanceLEDListenStartTimestamp(report.ListenStartTimestamp)
		if err != nil {
			problems = append(problems, err.Error())
		} else if listenStartTimestamp.Before(notBefore) {
			problems = append(problems, "listen_start_timestamp must be >= metrics trace_since")
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func parsePhysicalAcceptanceLEDListenStartTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("listen_start_timestamp is required when metrics trace_since is set")
	}
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("listen_start_timestamp must be RFC3339 when metrics trace_since is set")
	}
	return timestamp, nil
}
