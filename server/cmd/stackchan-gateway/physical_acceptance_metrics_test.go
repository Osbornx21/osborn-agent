package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhysicalAcceptanceMetricsCommandSummarizesTraceLatencies(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1400}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":1500,"fields":{"channel":"motion","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":1600,"fields":{"channel":"led","reason":"speaking","result":"sent"}}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"turn_complete","elapsed_ms":3000}
{"timestamp":"2026-06-08T00:01:00Z","trace_id":"trace-2","session_id":"sess-2","device_id":"stackchan-s3-main","generation":2,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"trace-2","session_id":"sess-2","device_id":"stackchan-s3-main","generation":2,"event":"speech_final","elapsed_ms":1100}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"stackchan-s3-main","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":1700}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"stackchan-s3-main","generation":2,"event":"stackchan_body_dispatch","elapsed_ms":1750,"fields":{"channel":"motion","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"stackchan-s3-main","generation":2,"event":"stackchan_body_dispatch","elapsed_ms":1800,"fields":{"channel":"led","reason":"speaking","result":"failed"}}
{"timestamp":"2026-06-08T00:01:03Z","trace_id":"trace-2","session_id":"sess-2","device_id":"stackchan-s3-main","generation":2,"event":"turn_complete","elapsed_ms":3000}
{"timestamp":"2026-06-08T00:02:00Z","trace_id":"trace-3","session_id":"sess-3","device_id":"stackchan-s3-main","generation":3,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:02:01Z","trace_id":"trace-3","session_id":"sess-3","device_id":"stackchan-s3-main","generation":3,"event":"speech_final","elapsed_ms":1200}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"stackchan-s3-main","generation":3,"event":"first_downlink_audio_sent","elapsed_ms":2200}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"stackchan-s3-main","generation":3,"event":"stackchan_body_dispatch","elapsed_ms":2250,"fields":{"channel":"motion","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-08T00:02:03Z","trace_id":"trace-3","session_id":"sess-3","device_id":"stackchan-s3-main","generation":3,"event":"turn_complete","elapsed_ms":3000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "stackchan-s3-main",
		"--min-turns", "3",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.CompletedTurns != 3 || summary.AudioTurns != 3 {
		t.Fatalf("summary counts = %+v, want completed/audio 3", summary)
	}
	if summary.P50FirstAudibleMS != 600 || summary.P95FirstAudibleMS != 1000 {
		t.Fatalf("summary latencies = %+v, want p50=600 p95=1000", summary)
	}
	if summary.BargeInStopLatencyMS != 0 {
		t.Fatalf("barge latency = %d, want 0 when not trace-derived", summary.BargeInStopLatencyMS)
	}
	if summary.BodyMCPToolSuccessRate != 0.8 {
		t.Fatalf("body MCP success rate = %v, want 0.8", summary.BodyMCPToolSuccessRate)
	}
}

type acceptanceMetricsTestSummary struct {
	DeviceID               string  `json:"device_id"`
	CompletedTurns         int     `json:"completed_turns"`
	AudioTurns             int     `json:"audio_turns"`
	LLMRequestTurns        int     `json:"llm_request_turns"`
	LLMRecentContextTurns  int     `json:"llm_recent_context_turns"`
	MaxRecentTurnCount     int     `json:"max_recent_turn_count"`
	ContinuityContextOK    bool    `json:"continuity_context_ok"`
	P50FirstAudibleMS      int     `json:"p50_first_audible_ms"`
	P95FirstAudibleMS      int     `json:"p95_first_audible_ms"`
	BargeInStopLatencyMS   int     `json:"barge_in_stop_latency_ms"`
	BodyMCPToolSuccessRate float64 `json:"body_mcp_tool_success_rate"`
	CameraToolCallCount    int     `json:"camera_tool_call_count"`
	UnexpectedCamera       bool    `json:"unexpected_camera_triggered"`
	TTSAudioQuality        struct {
		EventCount        int     `json:"event_count"`
		SampleCount       int64   `json:"sample_count"`
		DurationMS        int     `json:"duration_ms"`
		PeakDBFSMax       float64 `json:"peak_dbfs_max"`
		RMSDBFSP95        float64 `json:"rms_dbfs_p95"`
		ClippedPercentMax float64 `json:"clipped_percent_max"`
		DCOffsetMaxAbs    float64 `json:"dc_offset_max_abs"`
	} `json:"tts_audio_quality"`
	TurnWindow        string `json:"turn_window"`
	FirstAudibleBasis string `json:"first_audible_basis"`
	ContinuityBasis   string `json:"continuity_basis"`
	GeneratedAt       string `json:"generated_at"`
}

func TestPhysicalAcceptanceMetricsCommandSummarizesRecentTurnContextInLatestWindow(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"llm_request","elapsed_ms":1100,"fields":{"recent_turn_count":0,"memory_count":0,"prompt_text_length":120}}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1400}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":3000}
{"timestamp":"2026-06-08T00:01:00Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":4000}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":5000}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"llm_request","elapsed_ms":5100,"fields":{"recent_turn_count":1,"memory_count":0,"prompt_text_length":180}}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":5600}
{"timestamp":"2026-06-08T00:01:03Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":7000}
{"timestamp":"2026-06-08T00:02:00Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"listen_start","elapsed_ms":8000}
{"timestamp":"2026-06-08T00:02:01Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"speech_final","elapsed_ms":9000}
{"timestamp":"2026-06-08T00:02:01Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"llm_request","elapsed_ms":9100,"fields":{"recent_turn_count":2,"memory_count":0,"prompt_text_length":220}}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"first_downlink_audio_sent","elapsed_ms":9600}
{"timestamp":"2026-06-08T00:02:03Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"turn_complete","elapsed_ms":11000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "2",
		"--latest-turns", "2",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.LLMRequestTurns != 2 || summary.LLMRecentContextTurns != 2 {
		t.Fatalf("continuity turn counts = %+v, want latest two LLM turns with recent context", summary)
	}
	if summary.MaxRecentTurnCount != 2 {
		t.Fatalf("max recent turn count = %d, want 2", summary.MaxRecentTurnCount)
	}
	if !summary.ContinuityContextOK {
		t.Fatalf("continuity_context_ok = false, want true for latest-window recent-turn context")
	}
	if summary.ContinuityBasis != "llm_request.fields.recent_turn_count > 0" {
		t.Fatalf("continuity basis = %q, want recent-turn-count basis", summary.ContinuityBasis)
	}
}

func TestPhysicalAcceptanceMetricsCommandRejectsTooFewAudioTurns(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"turn_complete","elapsed_ms":3000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "stackchan-s3-main",
		"--min-turns", "1",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-acceptance-metrics code = 0, want audio turn failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "audio_turns") {
		t.Fatalf("stderr = %q, want audio_turns failure", stderr.String())
	}
}

func TestPhysicalAcceptanceMetricsCommandRequiresExplicitDevice(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--min-turns", "1",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-acceptance-metrics code = 0, want missing-device failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--device is required") || !strings.Contains(stderr.String(), "Device-Id/MAC") {
		t.Fatalf("stderr = %q, want explicit runtime device guidance", stderr.String())
	}
}

func TestPhysicalAcceptanceMetricsCommandLimitsBodyRateToLatestTurns(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1400}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":1450,"fields":{"channel":"led","reason":"speaking","result":"failed"}}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":3000}
{"timestamp":"2026-06-08T00:01:00Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":4000}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":5000}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":5600}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"stackchan_body_dispatch","elapsed_ms":5650,"fields":{"channel":"motion","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"stackchan_body_dispatch","elapsed_ms":5900,"fields":{"channel":"led","reason":"idle_start","result":"failed"},"error_code":"MCP_TOOL_TIMEOUT"}
{"timestamp":"2026-06-08T00:01:03Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":7000}
{"timestamp":"2026-06-08T00:02:00Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"listen_start","elapsed_ms":8000}
{"timestamp":"2026-06-08T00:02:01Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"speech_final","elapsed_ms":9000}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"first_downlink_audio_sent","elapsed_ms":9300}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"stackchan_body_dispatch","elapsed_ms":9350,"fields":{"channel":"led","reason":"speaking","result":"sent"}}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"stackchan_body_dispatch","elapsed_ms":9600,"fields":{"channel":"led","reason":"idle_start","result":"failed"},"error_code":"stackchan_old_generation"}
{"timestamp":"2026-06-08T00:02:03Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"turn_complete","elapsed_ms":10000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "2",
		"--latest-turns", "2",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.CompletedTurns != 2 || summary.AudioTurns != 2 {
		t.Fatalf("summary counts = %+v, want latest two completed/audio turns", summary)
	}
	if summary.BodyMCPToolSuccessRate != 1 {
		t.Fatalf("body MCP success rate = %v, want latest-turn window to exclude old failure", summary.BodyMCPToolSuccessRate)
	}
	if summary.P50FirstAudibleMS != 300 || summary.P95FirstAudibleMS != 600 {
		t.Fatalf("latencies = %+v, want latest-window p50=300 p95=600", summary)
	}
}

func TestPhysicalAcceptanceMetricsCommandDerivesBargeInStopLatencyFromTrace(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1400}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"tts_stop_sent","elapsed_ms":1500,"fields":{"reason":"listen_start","stop_latency_ms":37}}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":3000,"error_code":"barge_in","fields":{"reason":"listen_start"}}
{"timestamp":"2026-06-08T00:01:00Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":4000}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":5000}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":5600}
{"timestamp":"2026-06-08T00:01:03Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":7000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "1",
		"--latest-turns", "1",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.BargeInStopLatencyMS != 37 {
		t.Fatalf("barge latency = %d, want trace-derived 37", summary.BargeInStopLatencyMS)
	}
}

func TestPhysicalAcceptanceMetricsCommandIgnoresStaleBargeInOutsideLatestWindow(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1400}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"tts_stop_sent","elapsed_ms":1500,"fields":{"reason":"listen_start","stop_latency_ms":41}}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":3000,"error_code":"barge_in","fields":{"reason":"listen_start"}}
{"timestamp":"2026-06-08T00:00:04Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":4000}
{"timestamp":"2026-06-08T00:00:05Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":5000}
{"timestamp":"2026-06-08T00:00:06Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":5600}
{"timestamp":"2026-06-08T00:00:07Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":7000}
{"timestamp":"2026-06-08T00:01:00Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"listen_start","elapsed_ms":8000}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"speech_final","elapsed_ms":9000}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"first_downlink_audio_sent","elapsed_ms":9600}
{"timestamp":"2026-06-08T00:01:03Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"turn_complete","elapsed_ms":11000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "1",
		"--latest-turns", "1",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.BargeInStopLatencyMS != 0 {
		t.Fatalf("barge latency = %d, want stale trace ignored", summary.BargeInStopLatencyMS)
	}
}

func TestPhysicalAcceptanceMetricsCommandFiltersTurnsSinceTimestamp(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1400}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":1500,"fields":{"channel":"led","reason":"thinking_start","result":"failed"},"error_code":"MCP_TOOL_TIMEOUT"}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":3000}
{"timestamp":"2026-06-08T00:10:01Z","trace_id":"trace-interrupt","session_id":"sess-interrupt","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"tts_stop_sent","elapsed_ms":10,"fields":{"reason":"listen_start","stop_latency_ms":29}}
{"timestamp":"2026-06-08T00:10:01Z","trace_id":"trace-interrupt","session_id":"sess-interrupt","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":12,"error_code":"barge_in","fields":{"reason":"listen_start"}}
{"timestamp":"2026-06-08T00:10:02Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:10:03Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:10:04Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":1500}
{"timestamp":"2026-06-08T00:10:04Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"stackchan_body_dispatch","elapsed_ms":1600,"fields":{"channel":"led","reason":"speaking","result":"sent"}}
{"timestamp":"2026-06-08T00:10:05Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":3000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "1",
		"--since", "2026-06-08T00:10:00Z",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.CompletedTurns != 1 || summary.AudioTurns != 1 {
		t.Fatalf("summary counts = %+v, want only post-since turn", summary)
	}
	if summary.BargeInStopLatencyMS != 29 {
		t.Fatalf("barge latency = %d, want post-since interruption latency 29", summary.BargeInStopLatencyMS)
	}
	if summary.BodyMCPToolSuccessRate != 1 {
		t.Fatalf("body rate = %v, want old failure excluded", summary.BodyMCPToolSuccessRate)
	}
	if summary.P50FirstAudibleMS != 500 || summary.P95FirstAudibleMS != 500 {
		t.Fatalf("latencies = %+v, want post-since first-audible 500", summary)
	}
}

func TestPhysicalAcceptanceMetricsCommandDerivesAbortBargeInInsideSinceWindow(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:10:01Z","trace_id":"trace-interrupt","session_id":"sess-interrupt","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"abort_received","elapsed_ms":10,"fields":{"old_generation":1,"new_generation":2}}
{"timestamp":"2026-06-08T00:10:01Z","trace_id":"trace-interrupt","session_id":"sess-interrupt","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"tts_stop_sent","elapsed_ms":10,"fields":{"reason":"abort","stop_latency_ms":31}}
{"timestamp":"2026-06-08T00:10:01Z","trace_id":"trace-interrupt","session_id":"sess-interrupt","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":12,"error_code":"canceled","fields":{"reason":"abort"}}
{"timestamp":"2026-06-08T00:10:02Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:10:03Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:10:04Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":1500}
{"timestamp":"2026-06-08T00:10:05Z","trace_id":"trace-new","session_id":"sess-new","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":3000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "1",
		"--since", "2026-06-08T00:10:00Z",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.BargeInStopLatencyMS != 31 {
		t.Fatalf("barge latency = %d, want abort stop latency 31", summary.BargeInStopLatencyMS)
	}
}

func TestPhysicalAcceptanceMetricsCommandCountsCameraToolCallsInAcceptanceWindow(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"llm_tool_call","elapsed_ms":2000,"error_code":"tool_not_allowed","fields":{"tool_name":"self.camera.take_photo","skipped":true}}
{"timestamp":"2026-06-08T00:10:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:10:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:10:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"llm_tool_call","elapsed_ms":1200,"error_code":"tool_not_allowed","fields":{"tool_name":"self.camera.take_photo","skipped":true}}
{"timestamp":"2026-06-08T00:10:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":1500}
{"timestamp":"2026-06-08T00:10:03Z","trace_id":"trace-1","session_id":"sess-1","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":3000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "1",
		"--since", "2026-06-08T00:10:00Z",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.CameraToolCallCount != 1 {
		t.Fatalf("camera tool call count = %d, want only in-window camera call counted", summary.CameraToolCallCount)
	}
	if !summary.UnexpectedCamera {
		t.Fatalf("unexpected_camera_triggered = false, want trace-derived true")
	}
}

func TestPhysicalAcceptanceMetricsCommandSummarizesTTSAudioQualityInLatestWindow(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1500}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"tts_audio_quality","elapsed_ms":1600,"fields":{"sample_count":24000,"duration_ms":1000,"peak_dbfs":0,"rms_dbfs":-3,"clipped_percent":99,"silence_percent":0,"dc_offset":0.5}}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"trace-old","session_id":"sess-old","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":3000}
{"timestamp":"2026-06-08T00:01:00Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":4000}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"speech_final","elapsed_ms":5000}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"first_downlink_audio_sent","elapsed_ms":5600}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"tts_audio_quality","elapsed_ms":5700,"fields":{"sample_count":14400,"duration_ms":600,"peak_dbfs":-1.5,"rms_dbfs":-18.4,"clipped_percent":0.1,"silence_percent":12.5,"dc_offset":0.002}}
{"timestamp":"2026-06-08T00:01:03Z","trace_id":"trace-2","session_id":"sess-2","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"turn_complete","elapsed_ms":7000}
{"timestamp":"2026-06-08T00:02:00Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"listen_start","elapsed_ms":8000}
{"timestamp":"2026-06-08T00:02:01Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"speech_final","elapsed_ms":9000}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"first_downlink_audio_sent","elapsed_ms":9600}
{"timestamp":"2026-06-08T00:02:02Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"tts_audio_quality","elapsed_ms":9700,"fields":{"sample_count":9600,"duration_ms":400,"peak_dbfs":-2.2,"rms_dbfs":-22.8,"clipped_percent":0,"silence_percent":18.5,"dc_offset":-0.004}}
{"timestamp":"2026-06-08T00:02:03Z","trace_id":"trace-3","session_id":"sess-3","device_id":"44:1b:f6:e2:74:50","generation":3,"event":"turn_complete","elapsed_ms":11000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "2",
		"--latest-turns", "2",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.TTSAudioQuality.EventCount != 2 {
		t.Fatalf("tts quality event count = %d, want latest-window 2", summary.TTSAudioQuality.EventCount)
	}
	if summary.TTSAudioQuality.SampleCount != 24000 || summary.TTSAudioQuality.DurationMS != 1000 {
		t.Fatalf("tts quality counts = %+v, want latest-window sample/duration sum", summary.TTSAudioQuality)
	}
	if summary.TTSAudioQuality.ClippedPercentMax != 0.1 {
		t.Fatalf("clipped max = %v, want old clipped event excluded", summary.TTSAudioQuality.ClippedPercentMax)
	}
	if summary.TTSAudioQuality.DCOffsetMaxAbs != 0.004 {
		t.Fatalf("dc offset max abs = %v, want 0.004", summary.TTSAudioQuality.DCOffsetMaxAbs)
	}
	if summary.TTSAudioQuality.PeakDBFSMax != -1.5 || summary.TTSAudioQuality.RMSDBFSP95 != -18.4 {
		t.Fatalf("tts quality dbfs = %+v, want latest-window max/p95", summary.TTSAudioQuality)
	}
}

func TestPhysicalAcceptanceMetricsCommandSegmentsLongXiaozhiSessionByListenStart(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T00:00:00Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-08T00:00:01Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":1000}
{"timestamp":"2026-06-08T00:00:02Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":1300}
{"timestamp":"2026-06-08T00:00:03Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":3000}
{"timestamp":"2026-06-08T00:01:00Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"listen_start","elapsed_ms":4000}
{"timestamp":"2026-06-08T00:01:01Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"speech_final","elapsed_ms":5000}
{"timestamp":"2026-06-08T00:01:02Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"first_downlink_audio_sent","elapsed_ms":5600}
{"timestamp":"2026-06-08T00:01:03Z","trace_id":"sess_1:1","session_id":"sess_1","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"turn_complete","elapsed_ms":7000}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-acceptance-metrics",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--min-turns", "2",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-acceptance-metrics code = %d, stderr = %s", code, stderr.String())
	}
	var summary acceptanceMetricsTestSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.CompletedTurns != 2 || summary.AudioTurns != 2 {
		t.Fatalf("summary counts = %+v, want two long-session turns", summary)
	}
	if summary.P50FirstAudibleMS != 300 || summary.P95FirstAudibleMS != 600 {
		t.Fatalf("latencies = %+v, want p50=300 p95=600", summary)
	}
}

func writeAcceptanceMetricsTrace(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "turns.jsonl")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	return path
}
