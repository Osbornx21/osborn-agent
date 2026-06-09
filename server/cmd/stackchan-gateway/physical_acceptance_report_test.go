package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhysicalAcceptanceReportCommandWritesAndValidatesV2Report(t *testing.T) {
	ledReportPath := writeLEDRetestAcceptanceReport(t, `{
  "schema_version": "stackchan_physical_led_retest_v1",
  "device_id": "stackchan-s3-main",
  "gateway_commit": "606ea39a4b35",
  "trace_id": "sess_1:1",
  "session_id": "sess_1",
  "generation": 1,
  "listen_start_sequence": 95,
  "server_trace_ok": true,
  "visual_green_confirmed": true,
  "no_led_overwrite_before_speech_final": true,
  "listen_start_timestamp": "2026-06-07T15:28:19Z",
  "listen_start_elapsed_ms": 59042,
  "first_uplink_audio_elapsed_ms": 59200,
  "led_dispatch_elapsed_ms": 59339,
  "speech_final_elapsed_ms": 64265,
  "observed_at": "2026-06-07T15:28:30Z",
  "expected_visual_state": "green_listening_asr",
  "trace_events": ["listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"],
  "notes": "operator confirmed lifecycle"
}`)
	reportPath := filepath.Join(t.TempDir(), "physical-acceptance.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-report",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--hardware-device-id", "44:1b:f6:e2:74:50",
		"--client-id", "36d53c70-30e7-41e9-9720-6a5000e40a3c",
		"--firmware-build-id", "StackChan-UserDemo V1.4.1",
		"--firmware-version", "V1.4.1",
		"--gateway-commit", "abcdef123456",
		"--provider-profile", "siliconflow-dashscope-voice",
		"--completed-turns", "20",
		"--p50-first-audible-ms", "980",
		"--p95-first-audible-ms", "1280",
		"--barge-in-stop-latency-ms", "180",
		"--body-mcp-tool-success-rate", "1.0",
		"--llm-request-turns", "20",
		"--llm-recent-context-turns", "19",
		"--max-recent-turn-count", "8",
		"--continuity-context-ok",
		"--audio-playback-ok",
		"--screen-text-ok",
		"--head-control-ok",
		"--led-lifecycle-ok",
		"--led-retest-report", ledReportPath,
		"--no-unexpected-camera-trigger",
		"--wifi-reconnect-ok",
		"--gateway-restart-reconnect-ok",
		"--notes", "operator confirmed physical acceptance",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-report code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "physical acceptance report written") {
		t.Fatalf("stdout = %q, want report written summary", stdout.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report physicalAcceptanceReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if report.SchemaVersion != physicalAcceptanceSchemaVersion || !report.AudioPlaybackOK || !report.ScreenTextOK {
		t.Fatalf("report = %+v, want v2 audio/screen acceptance", report)
	}
	if report.CustomScreenSceneRequired {
		t.Fatalf("report custom screen scene required = true, want false for official firmware")
	}
	if report.LLMRequestTurns != 20 || report.LLMRecentContextTurns != 19 || report.MaxRecentTurnCount != 8 || !report.ContinuityContextOK {
		t.Fatalf("report continuity fields = %+v, want context evidence copied into report", report)
	}
	if report.UnexpectedCameraTriggered {
		t.Fatalf("report unexpected camera triggered = true, want false")
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"acceptance",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--turns", "20",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("acceptance code = %d for generated report, stderr = %s", code, stderr.String())
	}
}

func TestPhysicalAcceptanceReportCommandUsesMetricsFile(t *testing.T) {
	metricsPath := filepath.Join(t.TempDir(), "acceptance-metrics.json")
	if err := os.WriteFile(metricsPath, []byte(`{
  "device_id": "stackchan-s3-main",
  "completed_turns": 20,
  "audio_turns": 20,
  "p50_first_audible_ms": 430,
  "p95_first_audible_ms": 760,
  "barge_in_stop_latency_ms": 180,
  "body_mcp_tool_success_rate": 1,
  "llm_request_turns": 20,
  "llm_recent_context_turns": 19,
  "max_recent_turn_count": 8,
  "continuity_context_ok": true,
  "continuity_basis": "llm_request.fields.recent_turn_count > 0",
  "camera_tool_call_count": 0,
  "unexpected_camera_triggered": false,
  "tts_audio_quality": {
    "event_count": 2,
    "sample_count": 24000,
    "duration_ms": 1000,
    "peak_dbfs_max": -1.5,
    "rms_dbfs_p50": -22.8,
    "rms_dbfs_p95": -18.4,
    "clipped_percent_max": 0.1,
    "silence_percent_max": 18.5,
    "dc_offset_max_abs": 0.004
  },
  "turn_window": "latest_20_completed_turns",
  "trace_since": "2026-06-07T15:00:00Z",
  "first_audible_basis": "first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms",
  "generated_at": "2026-06-08T00:00:00Z"
}`+"\n"), 0o600); err != nil {
		t.Fatalf("write metrics: %v", err)
	}
	ledReportPath := writeLEDRetestAcceptanceReport(t, `{
  "schema_version": "stackchan_physical_led_retest_v1",
  "device_id": "stackchan-s3-main",
  "gateway_commit": "606ea39a4b35",
  "trace_id": "sess_1:1",
  "session_id": "sess_1",
  "generation": 1,
  "listen_start_sequence": 95,
  "server_trace_ok": true,
  "visual_green_confirmed": true,
  "no_led_overwrite_before_speech_final": true,
  "listen_start_timestamp": "2026-06-07T15:28:19Z",
  "listen_start_elapsed_ms": 59042,
  "first_uplink_audio_elapsed_ms": 59200,
  "led_dispatch_elapsed_ms": 59339,
  "speech_final_elapsed_ms": 64265,
  "observed_at": "2026-06-07T15:28:30Z",
  "expected_visual_state": "green_listening_asr",
  "trace_events": ["listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"]
}`)
	reportPath := filepath.Join(t.TempDir(), "physical-acceptance.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-report",
		"--report", reportPath,
		"--metrics-file", metricsPath,
		"--device", "stackchan-s3-main",
		"--firmware-build-id", "StackChan-UserDemo V1.4.1",
		"--gateway-commit", "abcdef123456",
		"--provider-profile", "siliconflow-dashscope-voice",
		"--audio-playback-ok",
		"--screen-text-ok",
		"--head-control-ok",
		"--led-lifecycle-ok",
		"--led-retest-report", ledReportPath,
		"--no-unexpected-camera-trigger",
		"--wifi-reconnect-ok",
		"--gateway-restart-reconnect-ok",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-report code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report physicalAcceptanceReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if report.CompletedTurns != 20 || report.P50FirstAudibleMS != 430 || report.P95FirstAudibleMS != 760 || report.BargeInStopLatencyMS != 180 {
		t.Fatalf("report metrics = %+v, want metrics-file values", report)
	}
	if report.BodyMCPToolSuccessRate != 1 {
		t.Fatalf("report body MCP rate = %v, want metrics-file value", report.BodyMCPToolSuccessRate)
	}
	if report.LLMRequestTurns != 20 || report.LLMRecentContextTurns != 19 || report.MaxRecentTurnCount != 8 || !report.ContinuityContextOK {
		t.Fatalf("report continuity fields = %+v, want metrics-file values", report)
	}
	if report.MetricsTurnWindow != "latest_20_completed_turns" || report.MetricsTraceSince != "2026-06-07T15:00:00Z" || report.FirstAudibleBasis != "first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms" {
		t.Fatalf("report metrics provenance = turn_window=%q trace_since=%q first_audible_basis=%q", report.MetricsTurnWindow, report.MetricsTraceSince, report.FirstAudibleBasis)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse raw report: %v", err)
	}
	var ttsQuality physicalAcceptanceTTSAudioQualitySummary
	if err := json.Unmarshal(raw["tts_audio_quality"], &ttsQuality); err != nil {
		t.Fatalf("parse report tts audio quality: %v", err)
	}
	if ttsQuality.EventCount != 2 || ttsQuality.SampleCount != 24000 || ttsQuality.DurationMS != 1000 {
		t.Fatalf("report tts audio quality counts = %+v, want metrics-file values", ttsQuality)
	}
	if ttsQuality.PeakDBFSMax != -1.5 || ttsQuality.RMSDBFSP50 != -22.8 || ttsQuality.RMSDBFSP95 != -18.4 {
		t.Fatalf("report tts audio quality dbfs = %+v, want metrics-file values", ttsQuality)
	}
	if ttsQuality.ClippedPercentMax != 0.1 || ttsQuality.SilencePercentMax != 18.5 || ttsQuality.DCOffsetMaxAbs != 0.004 {
		t.Fatalf("report tts audio quality ratios = %+v, want metrics-file values", ttsQuality)
	}
}

func TestPhysicalAcceptanceReportCommandRejectsMetricsWithoutRecentTurnContext(t *testing.T) {
	metricsPath := filepath.Join(t.TempDir(), "acceptance-metrics.json")
	if err := os.WriteFile(metricsPath, []byte(`{
  "device_id": "stackchan-s3-main",
  "completed_turns": 20,
  "audio_turns": 20,
  "llm_request_turns": 20,
  "llm_recent_context_turns": 0,
  "max_recent_turn_count": 0,
  "continuity_context_ok": false,
  "continuity_basis": "llm_request.fields.recent_turn_count > 0",
  "p50_first_audible_ms": 430,
  "p95_first_audible_ms": 760,
  "barge_in_stop_latency_ms": 180,
  "body_mcp_tool_success_rate": 1,
  "camera_tool_call_count": 0,
  "unexpected_camera_triggered": false,
  "turn_window": "latest_20_completed_turns",
  "first_audible_basis": "first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms",
  "generated_at": "2026-06-08T00:00:00Z"
}`+"\n"), 0o600); err != nil {
		t.Fatalf("write metrics: %v", err)
	}
	ledReportPath := writeLEDRetestAcceptanceReport(t, `{
  "schema_version": "stackchan_physical_led_retest_v1",
  "device_id": "stackchan-s3-main",
  "gateway_commit": "606ea39a4b35",
  "trace_id": "sess_1:1",
  "session_id": "sess_1",
  "generation": 1,
  "listen_start_sequence": 95,
  "server_trace_ok": true,
  "visual_green_confirmed": true,
  "no_led_overwrite_before_speech_final": true,
  "listen_start_timestamp": "2026-06-07T15:28:19Z",
  "listen_start_elapsed_ms": 59042,
  "first_uplink_audio_elapsed_ms": 59200,
  "led_dispatch_elapsed_ms": 59339,
  "speech_final_elapsed_ms": 64265,
  "observed_at": "2026-06-07T15:28:30Z",
  "expected_visual_state": "green_listening_asr",
  "trace_events": ["listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"]
}`)
	reportPath := filepath.Join(t.TempDir(), "physical-acceptance.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-report",
		"--report", reportPath,
		"--metrics-file", metricsPath,
		"--device", "stackchan-s3-main",
		"--firmware-build-id", "StackChan-UserDemo V1.4.1",
		"--gateway-commit", "abcdef123456",
		"--provider-profile", "siliconflow-dashscope-voice",
		"--audio-playback-ok",
		"--screen-text-ok",
		"--head-control-ok",
		"--led-lifecycle-ok",
		"--led-retest-report", ledReportPath,
		"--no-unexpected-camera-trigger",
		"--wifi-reconnect-ok",
		"--gateway-restart-reconnect-ok",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-acceptance-report code = 0, want continuity context failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "continuity_context_ok") || !strings.Contains(stderr.String(), "llm_recent_context_turns") {
		t.Fatalf("stderr = %q, want continuity context failure", stderr.String())
	}
}

func TestPhysicalAcceptanceReportCommandRejectsLEDRetestBeforeMetricsSince(t *testing.T) {
	metricsPath := filepath.Join(t.TempDir(), "acceptance-metrics.json")
	if err := os.WriteFile(metricsPath, []byte(`{
  "device_id": "stackchan-s3-main",
  "completed_turns": 20,
  "audio_turns": 20,
  "p50_first_audible_ms": 430,
  "p95_first_audible_ms": 760,
  "barge_in_stop_latency_ms": 180,
  "body_mcp_tool_success_rate": 1,
  "camera_tool_call_count": 0,
  "unexpected_camera_triggered": false,
  "turn_window": "latest_20_completed_turns",
  "trace_since": "2026-06-08T00:10:00Z",
  "first_audible_basis": "first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms",
  "generated_at": "2026-06-08T00:20:00Z"
}`+"\n"), 0o600); err != nil {
		t.Fatalf("write metrics: %v", err)
	}
	ledReportPath := writeLEDRetestAcceptanceReport(t, `{
  "schema_version": "stackchan_physical_led_retest_v1",
  "device_id": "stackchan-s3-main",
  "gateway_commit": "606ea39a4b35",
  "trace_id": "sess_old:1",
  "session_id": "sess_old",
  "generation": 1,
  "listen_start_sequence": 95,
  "server_trace_ok": true,
  "visual_green_confirmed": true,
  "no_led_overwrite_before_speech_final": true,
  "listen_start_timestamp": "2026-06-08T00:00:00Z",
  "listen_start_elapsed_ms": 59042,
  "first_uplink_audio_elapsed_ms": 59200,
  "led_dispatch_elapsed_ms": 59339,
  "speech_final_elapsed_ms": 64265,
  "observed_at": "2026-06-08T00:00:30Z",
  "expected_visual_state": "green_listening_asr",
  "trace_events": ["listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"]
}`)
	reportPath := filepath.Join(t.TempDir(), "physical-acceptance.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-report",
		"--report", reportPath,
		"--metrics-file", metricsPath,
		"--device", "stackchan-s3-main",
		"--firmware-build-id", "StackChan-UserDemo V1.4.1",
		"--gateway-commit", "abcdef123456",
		"--provider-profile", "siliconflow-dashscope-voice",
		"--audio-playback-ok",
		"--screen-text-ok",
		"--head-control-ok",
		"--led-lifecycle-ok",
		"--led-retest-report", ledReportPath,
		"--no-unexpected-camera-trigger",
		"--wifi-reconnect-ok",
		"--gateway-restart-reconnect-ok",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-acceptance-report code = 0, want stale LED retest failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "led_retest_report") || !strings.Contains(stderr.String(), "trace_since") {
		t.Fatalf("stderr = %q, want LED retest trace_since failure", stderr.String())
	}
}

func TestAcceptanceCommandRejectsReportLEDRetestBeforeMetricsSince(t *testing.T) {
	ledReportPath := writeLEDRetestAcceptanceReport(t, `{
  "schema_version": "stackchan_physical_led_retest_v1",
  "device_id": "stackchan-s3-main",
  "gateway_commit": "606ea39a4b35",
  "trace_id": "sess_old:1",
  "session_id": "sess_old",
  "generation": 1,
  "listen_start_sequence": 95,
  "server_trace_ok": true,
  "visual_green_confirmed": true,
  "no_led_overwrite_before_speech_final": true,
  "listen_start_timestamp": "2026-06-08T00:00:00Z",
  "listen_start_elapsed_ms": 59042,
  "first_uplink_audio_elapsed_ms": 59200,
  "led_dispatch_elapsed_ms": 59339,
  "speech_final_elapsed_ms": 64265,
  "observed_at": "2026-06-08T00:00:30Z",
  "expected_visual_state": "green_listening_asr",
  "trace_events": ["listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"]
}`)
	reportPath := filepath.Join(t.TempDir(), "physical-acceptance.json")
	report := physicalAcceptanceReport{
		SchemaVersion:             physicalAcceptanceSchemaVersion,
		DeviceID:                  "stackchan-s3-main",
		FirmwareBuildID:           "StackChan-UserDemo V1.4.1",
		GatewayCommit:             "abcdef123456",
		ProviderProfile:           "siliconflow-dashscope-voice",
		CompletedTurns:            20,
		P50FirstAudibleMS:         430,
		P95FirstAudibleMS:         760,
		BargeInStopLatencyMS:      180,
		BodyMCPToolSuccessRate:    1,
		MetricsTurnWindow:         "latest_20_completed_turns",
		MetricsTraceSince:         "2026-06-08T00:10:00Z",
		FirstAudibleBasis:         "first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms",
		AudioPlaybackOK:           true,
		ScreenTextOK:              true,
		HeadControlOK:             true,
		LEDLifecycleOK:            true,
		LEDRetestReport:           ledReportPath,
		CustomScreenSceneRequired: false,
		WiFiReconnectOK:           true,
		GatewayRestartReconnectOK: true,
	}
	if err := writePhysicalAcceptanceReport(reportPath, report); err != nil {
		t.Fatalf("write report: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"acceptance",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--turns", "20",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("acceptance code = 0, want stale LED retest failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "led_retest_report") || !strings.Contains(stderr.String(), "metrics trace_since") {
		t.Fatalf("stderr = %q, want LED retest metrics trace_since failure", stderr.String())
	}
}

func TestPhysicalAcceptanceReportCommandFailsWhenMetricsContainCameraToolCall(t *testing.T) {
	metricsPath := filepath.Join(t.TempDir(), "acceptance-metrics.json")
	if err := os.WriteFile(metricsPath, []byte(`{
  "device_id": "stackchan-s3-main",
  "completed_turns": 20,
  "audio_turns": 20,
  "p50_first_audible_ms": 430,
  "p95_first_audible_ms": 760,
  "barge_in_stop_latency_ms": 180,
  "body_mcp_tool_success_rate": 1,
  "camera_tool_call_count": 1,
  "unexpected_camera_triggered": true,
  "first_audible_basis": "first_downlink_audio_sent.elapsed_ms - speech_final.elapsed_ms",
  "generated_at": "2026-06-08T00:00:00Z"
}`+"\n"), 0o600); err != nil {
		t.Fatalf("write metrics: %v", err)
	}
	ledReportPath := writeLEDRetestAcceptanceReport(t, `{
  "schema_version": "stackchan_physical_led_retest_v1",
  "device_id": "stackchan-s3-main",
  "gateway_commit": "606ea39a4b35",
  "trace_id": "sess_1:1",
  "session_id": "sess_1",
  "generation": 1,
  "listen_start_sequence": 95,
  "server_trace_ok": true,
  "visual_green_confirmed": true,
  "no_led_overwrite_before_speech_final": true,
  "listen_start_timestamp": "2026-06-07T15:28:19Z",
  "listen_start_elapsed_ms": 59042,
  "first_uplink_audio_elapsed_ms": 59200,
  "led_dispatch_elapsed_ms": 59339,
  "speech_final_elapsed_ms": 64265,
  "observed_at": "2026-06-07T15:28:30Z",
  "expected_visual_state": "green_listening_asr",
  "trace_events": ["listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"]
}`)
	reportPath := filepath.Join(t.TempDir(), "physical-acceptance.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-report",
		"--report", reportPath,
		"--metrics-file", metricsPath,
		"--device", "stackchan-s3-main",
		"--firmware-build-id", "StackChan-UserDemo V1.4.1",
		"--gateway-commit", "abcdef123456",
		"--provider-profile", "siliconflow-dashscope-voice",
		"--audio-playback-ok",
		"--screen-text-ok",
		"--head-control-ok",
		"--led-lifecycle-ok",
		"--led-retest-report", ledReportPath,
		"--no-unexpected-camera-trigger",
		"--wifi-reconnect-ok",
		"--gateway-restart-reconnect-ok",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-acceptance-report code = 0, want camera trace failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "camera_tool_call_count") || !strings.Contains(stderr.String(), "unexpected_camera_triggered") {
		t.Fatalf("stderr = %q, want trace-derived camera failure", stderr.String())
	}
}
