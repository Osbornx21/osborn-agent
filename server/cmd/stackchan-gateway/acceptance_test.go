package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcceptanceCommandValidatesOfficialStackChanV14Report(t *testing.T) {
	reportPath := writeOfficialStackChanV14AcceptanceReport(t, officialAcceptanceOptions{})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"acceptance",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--turns", "20",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("acceptance code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "physical acceptance report OK") {
		t.Fatalf("stdout = %q, want acceptance OK summary", stdout.String())
	}
}

func TestAcceptanceCommandRejectsCustomScreenSceneRequirement(t *testing.T) {
	reportPath := writeOfficialStackChanV14AcceptanceReport(t, officialAcceptanceOptions{
		customScreenSceneRequired: true,
		customScreenSceneOK:       true,
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"acceptance",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--turns", "20",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("acceptance code = 0, want custom screen scene requirement failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "custom_screen_scene_mcp_required") {
		t.Fatalf("stderr = %q, want custom_screen_scene_mcp_required failure", stderr.String())
	}
}

func TestAcceptanceCommandRejectsUnexpectedCameraTrigger(t *testing.T) {
	reportPath := writeOfficialStackChanV14AcceptanceReport(t, officialAcceptanceOptions{
		unexpectedCameraTriggered: true,
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"acceptance",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--turns", "20",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("acceptance code = 0, want unexpected camera failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected_camera_triggered") {
		t.Fatalf("stderr = %q, want unexpected_camera_triggered failure", stderr.String())
	}
}

func TestAcceptanceCommandRejectsMissingContinuityContext(t *testing.T) {
	continuityOK := false
	reportPath := writeOfficialStackChanV14AcceptanceReport(t, officialAcceptanceOptions{
		llmRequestTurns:       20,
		llmRecentContextTurns: 0,
		maxRecentTurnCount:    0,
		continuityContextOK:   &continuityOK,
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"acceptance",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--turns", "20",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("acceptance code = 0, want continuity context failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "continuity_context_ok") || !strings.Contains(stderr.String(), "llm_recent_context_turns") {
		t.Fatalf("stderr = %q, want continuity context failure", stderr.String())
	}
}

func TestAcceptanceCommandRejectsInsufficientTurns(t *testing.T) {
	reportPath := writeOfficialStackChanV14AcceptanceReport(t, officialAcceptanceOptions{completedTurns: 19})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"acceptance",
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--turns", "20",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("acceptance code = 0, want insufficient turns failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "completed_turns") {
		t.Fatalf("stderr = %q, want completed_turns failure", stderr.String())
	}
}

type officialAcceptanceOptions struct {
	completedTurns            int
	llmRequestTurns           int
	llmRecentContextTurns     int
	maxRecentTurnCount        int
	continuityContextOK       *bool
	customScreenSceneRequired bool
	customScreenSceneOK       bool
	unexpectedCameraTriggered bool
	ledServerTraceOK          *bool
	ledVisualGreenConfirmed   *bool
	ledNoOverwriteBeforeFinal *bool
	ledExpectedVisualState    string
}

func writeOfficialStackChanV14AcceptanceReport(t *testing.T, opts officialAcceptanceOptions) string {
	t.Helper()
	completedTurns := opts.completedTurns
	if completedTurns == 0 {
		completedTurns = 20
	}
	llmRequestTurns := opts.llmRequestTurns
	if llmRequestTurns == 0 {
		llmRequestTurns = completedTurns
	}
	continuityContextOK := boolOption(opts.continuityContextOK, true)
	llmRecentContextTurns := opts.llmRecentContextTurns
	if llmRecentContextTurns == 0 && continuityContextOK && completedTurns > 1 {
		llmRecentContextTurns = completedTurns - 1
	}
	maxRecentTurnCount := opts.maxRecentTurnCount
	if maxRecentTurnCount == 0 && continuityContextOK && llmRecentContextTurns > 0 {
		maxRecentTurnCount = 8
	}
	ledServerTraceOK := boolOption(opts.ledServerTraceOK, true)
	ledVisualGreenConfirmed := boolOption(opts.ledVisualGreenConfirmed, true)
	ledNoOverwriteBeforeFinal := boolOption(opts.ledNoOverwriteBeforeFinal, true)
	ledExpectedVisualState := opts.ledExpectedVisualState
	if ledExpectedVisualState == "" {
		ledExpectedVisualState = "green_listening_asr"
	}
	ledReportPath := writeLEDRetestAcceptanceReport(t, fmt.Sprintf(`{
  "schema_version": "stackchan_physical_led_retest_v1",
  "device_id": "stackchan-s3-main",
  "gateway_commit": "606ea39a4b35",
  "trace_id": "sess_1:1",
  "session_id": "sess_1",
  "generation": 1,
  "listen_start_sequence": 95,
  "server_trace_ok": %t,
  "visual_green_confirmed": %t,
  "no_led_overwrite_before_speech_final": %t,
  "listen_start_timestamp": "2026-06-07T15:28:19Z",
  "listen_start_elapsed_ms": 59042,
  "first_uplink_audio_elapsed_ms": 59200,
  "led_dispatch_elapsed_ms": 59339,
  "speech_final_elapsed_ms": 64265,
  "observed_at": "2026-06-07T15:28:30Z",
  "expected_visual_state": %s,
  "trace_events": ["listen_start", "first_uplink_audio", "stackchan_body_dispatch", "speech_final"],
  "notes": "operator confirmed lifecycle"
}`, ledServerTraceOK, ledVisualGreenConfirmed, ledNoOverwriteBeforeFinal, strconv.Quote(ledExpectedVisualState)))
	return writeAcceptanceReport(t, fmt.Sprintf(`{
  "schema_version": "stackchan_physical_acceptance_v2",
  "device_id": "stackchan-s3-main",
  "hardware_device_id": "44:1b:f6:e2:74:50",
  "client_id": "36d53c70-30e7-41e9-9720-6a5000e40a3c",
  "firmware_build_id": "StackChan-UserDemo V1.4.1",
  "firmware_version": "V1.4.1",
  "gateway_commit": "abcdef123456",
  "provider_profile": "siliconflow-dashscope-voice",
  "completed_turns": %d,
  "p50_first_audible_ms": 980,
  "p95_first_audible_ms": 1280,
  "barge_in_stop_latency_ms": 180,
  "body_mcp_tool_success_rate": 1.0,
  "llm_request_turns": %d,
  "llm_recent_context_turns": %d,
  "max_recent_turn_count": %d,
  "continuity_context_ok": %t,
  "continuity_basis": "llm_request.fields.recent_turn_count > 0",
  "audio_playback_ok": true,
  "screen_text_ok": true,
  "head_control_ok": true,
  "led_lifecycle_ok": true,
  "led_retest_report": %s,
  "custom_screen_scene_mcp_required": %t,
  "custom_screen_scene_mcp_ok": %t,
  "unexpected_camera_triggered": %t,
  "wifi_reconnect_ok": true,
  "gateway_restart_reconnect_ok": true,
  "notes": "hardware acceptance run"
}`, completedTurns, llmRequestTurns, llmRecentContextTurns, maxRecentTurnCount, continuityContextOK, strconv.Quote(ledReportPath), opts.customScreenSceneRequired, opts.customScreenSceneOK, opts.unexpectedCameraTriggered))
}

func boolOption(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func writeAcceptanceReport(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "physical-acceptance.json")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write acceptance report: %v", err)
	}
	return path
}

func writeLEDRetestAcceptanceReport(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "physical-led-retest.json")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write led retest report: %v", err)
	}
	return path
}
