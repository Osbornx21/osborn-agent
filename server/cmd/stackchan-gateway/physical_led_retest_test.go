package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhysicalLEDRetestCommandValidatesTraceAndVisualConfirmation(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":180,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-07T13:00:04Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}
{"timestamp":"2026-06-07T13:00:05Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"turn_complete","elapsed_ms":5200}`)
	reportPath := filepath.Join(t.TempDir(), "led-retest.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--gateway-commit", "952cc27",
		"--visual-green-confirmed",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-led-retest code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "physical led retest OK") {
		t.Fatalf("stdout = %q, want OK summary", stdout.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report physicalLEDRetestReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if report.SchemaVersion != physicalLEDRetestSchemaVersion || !report.ServerTraceOK || !report.VisualGreenConfirmed {
		t.Fatalf("report = %+v, want schema and confirmations", report)
	}
	if report.TraceID != "trace-1" || report.LEDDispatchElapsedMS != 180 || report.SpeechFinalElapsedMS != 4100 {
		t.Fatalf("report trace fields = %+v, want trace-1/180/4100", report)
	}
	if strings.Contains(string(data), `"red"`) || strings.Contains(string(data), `"green":168`) || strings.Contains(string(data), `"blue"`) {
		t.Fatalf("report leaked raw LED values: %s", string(data))
	}
}

func TestPhysicalLEDRetestCommandRejectsMissingVisualConfirmation(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":180,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-07T13:00:04Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--device", "stackchan-s3-main",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-led-retest code = 0, want visual confirmation failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "visual-green-confirmed") {
		t.Fatalf("stderr = %q, want visual confirmation failure", stderr.String())
	}
}

func TestPhysicalLEDRetestCommandRejectsMissingLEDDispatch(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:04Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--device", "stackchan-s3-main",
		"--visual-green-confirmed",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-led-retest code = 0, want missing led failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "listen-start led dispatch") {
		t.Fatalf("stderr = %q, want missing led dispatch failure", stderr.String())
	}
}

func TestPhysicalLEDRetestCommandRejectsLatestTurnMissingLEDDispatch(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T12:59:00Z","trace_id":"trace-old","session_id":"sess-old","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T12:59:01Z","trace_id":"trace-old","session_id":"sess-old","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T12:59:01Z","trace_id":"trace-old","session_id":"sess-old","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":180,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-07T12:59:04Z","trace_id":"trace-old","session_id":"sess-old","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}
{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-latest","session_id":"sess-latest","device_id":"stackchan-s3-main","generation":2,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-latest","session_id":"sess-latest","device_id":"stackchan-s3-main","generation":2,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:04Z","trace_id":"trace-latest","session_id":"sess-latest","device_id":"stackchan-s3-main","generation":2,"event":"speech_final","elapsed_ms":4100}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--device", "stackchan-s3-main",
		"--visual-green-confirmed",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-led-retest code = 0, want latest turn missing led failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "trace-latest") || !strings.Contains(stderr.String(), "listen-start led dispatch") {
		t.Fatalf("stderr = %q, want latest trace missing led dispatch failure", stderr.String())
	}
}

func TestPhysicalLEDRetestCommandUsesExplicitTraceIDWhenAutoListenTailIsIncomplete(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T12:59:00Z","trace_id":"trace-observed","session_id":"sess-observed","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T12:59:01Z","trace_id":"trace-observed","session_id":"sess-observed","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T12:59:01Z","trace_id":"trace-observed","session_id":"sess-observed","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":180,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-07T12:59:04Z","trace_id":"trace-observed","session_id":"sess-observed","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}
{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-auto-tail","session_id":"sess-auto-tail","device_id":"stackchan-s3-main","generation":2,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-auto-tail","session_id":"sess-auto-tail","device_id":"stackchan-s3-main","generation":2,"event":"first_uplink_audio","elapsed_ms":120}`)
	reportPath := filepath.Join(t.TempDir(), "led-retest.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--trace-id", "trace-observed",
		"--visual-green-confirmed",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-led-retest code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report physicalLEDRetestReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if report.TraceID != "trace-observed" || report.Generation != 1 {
		t.Fatalf("report = %+v, want explicit observed trace", report)
	}
}

func TestPhysicalLEDRetestCommandUsesExplicitListenStartSequenceWithinLongSession(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T13:00:00Z","sequence":1,"trace_id":"trace-session","session_id":"sess-session","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","sequence":2,"trace_id":"trace-session","session_id":"sess-session","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:04Z","sequence":3,"trace_id":"trace-session","session_id":"sess-session","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}
{"timestamp":"2026-06-07T13:00:05Z","sequence":4,"trace_id":"trace-session","session_id":"sess-session","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":5000}
{"timestamp":"2026-06-07T13:00:06Z","sequence":5,"trace_id":"trace-session","session_id":"sess-session","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":5120}
{"timestamp":"2026-06-07T13:00:06Z","sequence":6,"trace_id":"trace-session","session_id":"sess-session","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":5180,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-07T13:00:09Z","sequence":7,"trace_id":"trace-session","session_id":"sess-session","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":9100}`)
	reportPath := filepath.Join(t.TempDir(), "led-retest.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--trace-id", "trace-session",
		"--listen-start-sequence", "4",
		"--visual-green-confirmed",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-led-retest code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report physicalLEDRetestReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if report.TraceID != "trace-session" || report.ListenStartSequence != 4 || report.LEDDispatchElapsedMS != 5180 {
		t.Fatalf("report = %+v, want explicit second listen segment", report)
	}
}

func TestPhysicalLEDRetestCommandRejectsLEDDispatchAfterSpeechFinal(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:04Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}
{"timestamp":"2026-06-07T13:00:05Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":5200,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--device", "stackchan-s3-main",
		"--visual-green-confirmed",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-led-retest code = 0, want late led dispatch failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "before speech_final") {
		t.Fatalf("stderr = %q, want led-before-speech-final failure", stderr.String())
	}
}

func TestPhysicalLEDRetestCommandRejectsLEDOverwriteBeforeSpeechFinal(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":180,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-07T13:00:02Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_expression_cue_dispatch","elapsed_ms":220,"fields":{"scope":"lifecycle","trigger":"listening","cue":"attentive","result":"sent","has_led":true}}
{"timestamp":"2026-06-07T13:00:04Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--device", "stackchan-s3-main",
		"--visual-green-confirmed",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-led-retest code = 0, want LED overwrite failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "overwritten before speech_final") {
		t.Fatalf("stderr = %q, want overwrite failure", stderr.String())
	}
}

func TestPhysicalLEDRetestCommandRejectsUnsafeNotes(t *testing.T) {
	tracePath := writePhysicalLEDTrace(t, `{"timestamp":"2026-06-07T13:00:00Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"listen_start","elapsed_ms":0}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"first_uplink_audio","elapsed_ms":120}
{"timestamp":"2026-06-07T13:00:01Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"stackchan_body_dispatch","elapsed_ms":180,"fields":{"channel":"led","reason":"listen_start","result":"sent"}}
{"timestamp":"2026-06-07T13:00:04Z","trace_id":"trace-1","session_id":"sess-1","device_id":"stackchan-s3-main","generation":1,"event":"speech_final","elapsed_ms":4100}`)
	reportPath := filepath.Join(t.TempDir(), "led-retest.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-led-retest",
		"--trace-file", tracePath,
		"--report", reportPath,
		"--device", "stackchan-s3-main",
		"--visual-green-confirmed",
		"--notes", "transcript: hello",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-led-retest code = 0, want unsafe notes failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "notes contain unsafe") {
		t.Fatalf("stderr = %q, want unsafe notes failure", stderr.String())
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatalf("report path err = %v, want no report written", err)
	}
}

func writePhysicalLEDTrace(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "turns.jsonl")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	return path
}
