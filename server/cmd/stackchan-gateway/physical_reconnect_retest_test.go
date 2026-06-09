package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPhysicalReconnectRetestIgnoresPreRestartOffsetTimestamp(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T03:54:49+08:00","trace_id":"sess_3:1","session_id":"sess_3","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"hello_received","elapsed_ms":0}
{"timestamp":"2026-06-08T03:54:59+08:00","trace_id":"sess_3:2","session_id":"sess_3","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":0}`)
	restartStart := time.Date(2026, 6, 7, 20, 12, 20, 0, time.UTC)

	summary, err := buildPhysicalReconnectRetestSummary(tracePath, "44:1b:f6:e2:74:50", restartStart)
	if err != nil {
		t.Fatalf("build summary: %v", err)
	}

	if summary.DeviceHelloAfterRestartOK {
		t.Fatalf("device_hello_after_restart_ok = true, want false for pre-restart +08:00 timestamp")
	}
	if summary.GatewayRestartReconnectOK {
		t.Fatalf("gateway_restart_reconnect_ok = true, want false without post-restart hello")
	}
}

func TestPhysicalReconnectRetestAcceptsPostRestartOffsetTimestamp(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T04:12:21+08:00","trace_id":"sess_4:1","session_id":"sess_4","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"hello_received","elapsed_ms":0}
{"timestamp":"2026-06-08T04:12:25+08:00","trace_id":"sess_4:2","session_id":"sess_4","device_id":"44:1b:f6:e2:74:50","generation":2,"event":"listen_start","elapsed_ms":0}`)
	restartStart := time.Date(2026, 6, 7, 20, 12, 20, 0, time.UTC)

	summary, err := buildPhysicalReconnectRetestSummary(tracePath, "44:1b:f6:e2:74:50", restartStart)
	if err != nil {
		t.Fatalf("build summary: %v", err)
	}

	if !summary.DeviceHelloAfterRestartOK {
		t.Fatalf("device_hello_after_restart_ok = false, want true for post-restart hello")
	}
	if !summary.GatewayRestartReconnectOK {
		t.Fatalf("gateway_restart_reconnect_ok = false, want true after post-restart hello")
	}
	if summary.HelloEvent == nil || summary.HelloEvent.TraceID != "sess_4:1" {
		t.Fatalf("hello_event = %+v, want sess_4:1", summary.HelloEvent)
	}
	if summary.ListenEventAfterRestart == nil || summary.ListenEventAfterRestart.TraceID != "sess_4:2" {
		t.Fatalf("listen_event_after_restart = %+v, want sess_4:2", summary.ListenEventAfterRestart)
	}
}

func TestPhysicalReconnectRetestCommandFailsWithoutPostRestartHello(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T03:54:49+08:00","trace_id":"sess_3:1","session_id":"sess_3","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"hello_received","elapsed_ms":0}`)
	reportPath := filepath.Join(t.TempDir(), "reconnect.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-reconnect-retest",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--restart-start", "2026-06-07T20:12:20Z",
		"--report", reportPath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("physical-reconnect-retest code = 0, want failure without post-restart hello; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no device hello_received event after restart") {
		t.Fatalf("stderr = %q, want reconnect failure", stderr.String())
	}
	report := readPhysicalReconnectReport(t, reportPath)
	if report.GatewayRestartReconnectOK {
		t.Fatalf("report gateway_restart_reconnect_ok = true, want false")
	}
}

func TestPhysicalReconnectRetestCommandPassesWithPostRestartHello(t *testing.T) {
	tracePath := writeAcceptanceMetricsTrace(t, `{"timestamp":"2026-06-08T04:12:21+08:00","trace_id":"sess_4:1","session_id":"sess_4","device_id":"44:1b:f6:e2:74:50","generation":1,"event":"hello_received","elapsed_ms":0}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"physical-reconnect-retest",
		"--trace-file", tracePath,
		"--device", "44:1b:f6:e2:74:50",
		"--restart-start", "2026-06-07T20:12:20Z",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("physical-reconnect-retest code = %d, stderr = %s", code, stderr.String())
	}
	report := readPhysicalReconnectReportBytes(t, stdout.Bytes())
	if !report.GatewayRestartReconnectOK {
		t.Fatalf("report gateway_restart_reconnect_ok = false, want true")
	}
	if report.HelloEvent == nil || report.HelloEvent.TraceID != "sess_4:1" {
		t.Fatalf("report hello_event = %+v, want sess_4:1", report.HelloEvent)
	}
}

func readPhysicalReconnectReport(t *testing.T, path string) physicalReconnectRetestSummary {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	return readPhysicalReconnectReportBytes(t, data)
}

func readPhysicalReconnectReportBytes(t *testing.T, data []byte) physicalReconnectRetestSummary {
	t.Helper()
	var report physicalReconnectRetestSummary
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parse reconnect report: %v\n%s", err, string(data))
	}
	return report
}
