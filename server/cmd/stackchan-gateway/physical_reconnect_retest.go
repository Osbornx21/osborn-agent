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

	"stackchan-gateway/internal/observability"
)

const physicalReconnectRetestSchemaVersion = "a21_air_physical_reconnect_retest_v1"

type physicalReconnectRetestSummary struct {
	SchemaVersion             string                  `json:"schema_version"`
	DeviceID                  string                  `json:"device_id"`
	RestartStart              string                  `json:"restart_start"`
	DeviceHelloAfterRestartOK bool                    `json:"device_hello_after_restart_ok"`
	ListenStartAfterRestartOK bool                    `json:"listen_start_after_restart_ok"`
	GatewayRestartReconnectOK bool                    `json:"gateway_restart_reconnect_ok"`
	HelloEvent                *physicalReconnectEvent `json:"hello_event"`
	ListenEventAfterRestart   *physicalReconnectEvent `json:"listen_event_after_restart"`
	GeneratedAt               string                  `json:"generated_at"`
}

type physicalReconnectEvent struct {
	Timestamp  string `json:"timestamp"`
	TraceID    string `json:"trace_id"`
	SessionID  string `json:"session_id"`
	Generation int64  `json:"generation"`
	Event      string `json:"event"`
}

func runPhysicalReconnectRetest(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("physical-reconnect-retest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tracePath := flags.String("trace-file", "", "trace JSONL file to inspect")
	deviceID := flags.String("device", "", "expected runtime Xiaozhi Device-Id/MAC")
	restartStartRaw := flags.String("restart-start", "", "gateway restart start time as RFC3339")
	reportPath := flags.String("report", "", "optional JSON report path to write")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*tracePath) == "" {
		fmt.Fprintln(stderr, "physical-reconnect-retest failed: --trace-file is required")
		return 2
	}
	if strings.TrimSpace(*deviceID) == "" {
		fmt.Fprintln(stderr, "physical-reconnect-retest failed: --device is required")
		return 2
	}
	restartStart, err := parsePhysicalReconnectRestartStart(*restartStartRaw)
	if err != nil {
		fmt.Fprintf(stderr, "physical-reconnect-retest failed: %v\n", err)
		return 2
	}

	summary, err := buildPhysicalReconnectRetestSummary(*tracePath, strings.TrimSpace(*deviceID), restartStart)
	if err != nil {
		fmt.Fprintf(stderr, "physical-reconnect-retest failed: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "physical-reconnect-retest failed: marshal summary: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*reportPath) != "" {
		if err := os.MkdirAll(filepath.Dir(*reportPath), 0o755); err != nil {
			fmt.Fprintf(stderr, "physical-reconnect-retest failed: create report dir: %v\n", err)
			return 1
		}
		if err := os.WriteFile(*reportPath, append(data, '\n'), 0o600); err != nil {
			fmt.Fprintf(stderr, "physical-reconnect-retest failed: write report: %v\n", err)
			return 1
		}
	}
	fmt.Fprintln(stdout, string(data))
	if !summary.GatewayRestartReconnectOK {
		fmt.Fprintln(stderr, "physical-reconnect-retest failed: no device hello_received event after restart")
		return 1
	}
	return 0
}

func parsePhysicalReconnectRestartStart(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("--restart-start is required")
	}
	restartStart, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("--restart-start must be an RFC3339 timestamp")
	}
	return restartStart, nil
}

func buildPhysicalReconnectRetestSummary(tracePath string, deviceID string, restartStart time.Time) (physicalReconnectRetestSummary, error) {
	if strings.TrimSpace(deviceID) == "" {
		return physicalReconnectRetestSummary{}, errors.New("device id is required")
	}
	if restartStart.IsZero() {
		return physicalReconnectRetestSummary{}, errors.New("restart start is required")
	}
	events, err := loadTraceEvents(tracePath)
	if err != nil {
		return physicalReconnectRetestSummary{}, err
	}

	var hello *physicalReconnectEvent
	var listen *physicalReconnectEvent
	for _, event := range events {
		if event.DeviceID != deviceID {
			continue
		}
		if event.Timestamp.IsZero() || event.Timestamp.Before(restartStart) {
			continue
		}
		switch event.Event {
		case "hello_received":
			candidate := physicalReconnectEventFromTrace(event)
			hello = &candidate
		case "listen_start":
			candidate := physicalReconnectEventFromTrace(event)
			listen = &candidate
		}
	}

	return physicalReconnectRetestSummary{
		SchemaVersion:             physicalReconnectRetestSchemaVersion,
		DeviceID:                  deviceID,
		RestartStart:              restartStart.UTC().Format(time.RFC3339),
		DeviceHelloAfterRestartOK: hello != nil,
		ListenStartAfterRestartOK: listen != nil,
		GatewayRestartReconnectOK: hello != nil,
		HelloEvent:                hello,
		ListenEventAfterRestart:   listen,
		GeneratedAt:               time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func physicalReconnectEventFromTrace(event observability.TraceEvent) physicalReconnectEvent {
	timestamp := ""
	if !event.Timestamp.IsZero() {
		timestamp = event.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return physicalReconnectEvent{
		Timestamp:  timestamp,
		TraceID:    event.TraceID,
		SessionID:  event.SessionID,
		Generation: event.Generation,
		Event:      event.Event,
	}
}
