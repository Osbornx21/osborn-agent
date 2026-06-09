package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stackchan-gateway/internal/providerprobe"
)

func TestASRFixtureValidateCommandAcceptsSemanticFixture(t *testing.T) {
	fixturePath := filepath.Join(t.TempDir(), "spoken-opus.json")
	if err := providerprobe.WriteASROpusFixture(fixturePath, variedCommandASROpusFrames(12, 16)); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"asr-fixture-validate",
		"--fixture", fixturePath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("asr-fixture-validate code = %d, stderr = %s", code, stderr.String())
	}
	for _, want := range []string{"asr fixture OK:", "frames=12", "duration_ms=720", "unique_payloads=12"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "ICEi") {
		t.Fatalf("stdout leaked fixture payload: %s", stdout.String())
	}
}

func TestASRFixtureValidateCommandRejectsPlaceholderFixture(t *testing.T) {
	fixturePath := filepath.Join(t.TempDir(), "spoken-opus.json")
	if err := os.WriteFile(fixturePath, []byte(`{
		"format": "xiaozhi_opus_frames_v1",
		"sample_rate_hz": 16000,
		"frame_duration_ms": 60,
		"frames": [
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffe"}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"asr-fixture-validate",
		"--fixture", fixturePath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("asr-fixture-validate code = 0, want placeholder failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "semantic provider probes") {
		t.Fatalf("stderr = %q, want semantic fixture failure", stderr.String())
	}
	if strings.Contains(stderr.String(), "f8fffe") {
		t.Fatalf("stderr leaked fixture payload: %s", stderr.String())
	}
}

func TestASRFixtureValidateCommandRejectsMissingFixture(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"asr-fixture-validate"}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("asr-fixture-validate code = 0, want missing fixture failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--fixture is required") {
		t.Fatalf("stderr = %q, want missing fixture failure", stderr.String())
	}
}

func variedCommandASROpusFrames(count int, size int) [][]byte {
	frames := make([][]byte, count)
	for index := range frames {
		frame := make([]byte, size)
		for offset := range frame {
			frame[offset] = byte(0x20 + index + offset)
		}
		frames[index] = frame
	}
	return frames
}
