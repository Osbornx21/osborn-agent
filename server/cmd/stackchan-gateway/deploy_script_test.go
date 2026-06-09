package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPhysicalFinalAcceptanceDryRunPrintsContinuitySummaryFields(t *testing.T) {
	cmd := exec.Command("bash", "../../deploy/aliyun/physical-final-acceptance.sh",
		"--since", "2026-06-09T00:00:00+08:00",
		"--gateway-commit", "4b64da997bfb",
		"--led-retest-report", "/var/lib/a21-air/acceptance/led.json",
		"--reconnect-report", "/var/lib/a21-air/acceptance/reconnect.json",
		"--audio-playback-ok",
		"--screen-text-ok",
		"--head-control-ok",
		"--led-lifecycle-ok",
		"--no-unexpected-camera-trigger",
		"--wifi-reconnect-ok",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("physical-final-acceptance dry-run failed: %v\n%s", err, string(output))
	}
	body := string(output)
	for _, want := range []string{
		"llm_request_turns",
		"llm_recent_context_turns",
		"max_recent_turn_count",
		"continuity_context_ok",
		"continuity_basis",
		"tts_audio_quality",
		"event_count",
		"clipped_percent_max",
		"dc_offset_max_abs",
		"1445\\ speaking-only\\ wake\\ barge-in",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"prompt_text",
		"transcript",
		"generated_text",
		"recent_turns",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("dry-run output contains forbidden %q:\n%s", forbidden, body)
		}
	}
}
