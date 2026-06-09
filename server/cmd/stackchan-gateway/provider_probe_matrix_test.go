package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stackchan-gateway/internal/providerprobe"
)

func TestProviderProbeMatrixCommandRunsMockProfile(t *testing.T) {
	outputDir := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-matrix",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profiles", "cn-low-latency-cascade",
		"--runs", "1",
		"--output-dir", outputDir,
		"--timeout-ms", "1000",
		"--run-delay-ms", "25",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "validated_reports=1") {
		t.Fatalf("stdout = %q, want validated report count", stdout.String())
	}
	reports, err := filepath.Glob(filepath.Join(outputDir, "provider-probe-*.json"))
	if err != nil {
		t.Fatalf("glob reports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %#v, want one report", reports)
	}
	reportData, err := os.ReadFile(reports[0])
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal(reportData, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report["run_delay_ms"] != float64(25) {
		t.Fatalf("run_delay_ms = %v, want 25", report["run_delay_ms"])
	}
}

func TestDefaultProviderProbeProfilesIncludeMoonshotFallbackCandidate(t *testing.T) {
	for _, want := range []string{"siliconflow-dashscope-voice", "siliconflow-llm", "moonshot-llm", "stepfun-llm", "doubao-llm", "dashscope-cosyvoice"} {
		if !strings.Contains(","+defaultProviderProbeProfiles+",", ","+want+",") {
			t.Fatalf("defaultProviderProbeProfiles = %q, want %s", defaultProviderProbeProfiles, want)
		}
	}
}

func TestProviderProbeMatrixCommandRejectsRealASRWithoutFixture(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-matrix",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profiles", "dashscope-cosyvoice",
		"--runs", "1",
		"--output-dir", t.TempDir(),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run() code = 0, want missing ASR fixture failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "requires --asr-opus-fixture") {
		t.Fatalf("stderr = %q, want ASR fixture failure", stderr.String())
	}
}

func TestProviderProbeMatrixCommandAllowFailedProfilesWritesValidatedReport(t *testing.T) {
	outputDir := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-matrix",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profiles", "deepseek-llm",
		"--runs", "1",
		"--output-dir", outputDir,
		"--timeout-ms", "1000",
		"--allow-failed-profiles",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "completed with no successful probes") {
		t.Fatalf("stderr = %q, want failed profile diagnostic", stderr.String())
	}
	reports, err := filepath.Glob(filepath.Join(outputDir, "provider-probe-*.json"))
	if err != nil {
		t.Fatalf("glob reports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %#v, want one failed-profile report", reports)
	}
	if err := providerprobe.ValidateReportFile(reports[0]); err != nil {
		t.Fatalf("ValidateReportFile() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "validated_reports=1") {
		t.Fatalf("stdout = %q, want validated report count", stdout.String())
	}
}

func TestProviderProbeMatrixCommandRejectsPlaceholderASRFixtureForRealASR(t *testing.T) {
	fixturePath := filepath.Join(t.TempDir(), "placeholder-opus.json")
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
		"provider-probe-matrix",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profiles", "dashscope-cosyvoice",
		"--runs", "1",
		"--output-dir", t.TempDir(),
		"--asr-opus-fixture", fixturePath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("run() code = 0, want placeholder ASR fixture failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "semantic provider probes") {
		t.Fatalf("stderr = %q, want semantic fixture failure", stderr.String())
	}
	if strings.Contains(stderr.String(), "f8fffe") {
		t.Fatalf("stderr leaked fixture payload: %s", stderr.String())
	}
}

func TestProviderProbeMatrixLookupBridgesLegacyEnvFile(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "provider.env")
	if err := os.WriteFile(envPath, []byte(strings.Join([]string{
		"export A21_LAB_DASHSCOPE_API_KEY=legacy-dashscope",
		"export A21_LAB_STEPFUN_API_KEY=legacy-stepfun",
		"export A21_LAB_SILICONFLOW_API_KEY=legacy-siliconflow",
		"export A21_LAB_MOONSHOT_API_KEY=legacy-moonshot",
		"export A21_LAB_DEEPSEEK_API_KEY=legacy-deepseek",
		"export A21_LAB_VOLCENGINE_ARK_API_KEY=legacy-ark",
		"export A21_LAB_VOLCENGINE_API_KEY=legacy-doubao",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	lookup, err := buildProviderProbeMatrixLookup(envPath, func(name string) (string, bool) {
		if name == "DEEPSEEK_API_KEY" {
			return "process-deepseek", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("buildProviderProbeMatrixLookup() error = %v", err)
	}

	cases := map[string]string{
		"DASHSCOPE_API_KEY":         "legacy-dashscope",
		"STEPFUN_API_KEY":           "legacy-stepfun",
		"SILICONFLOW_API_KEY":       "legacy-siliconflow",
		"MOONSHOT_API_KEY":          "legacy-moonshot",
		"DEEPSEEK_API_KEY":          "process-deepseek",
		"ARK_API_KEY":               "legacy-ark",
		"DOUBAO_API_KEY":            "legacy-doubao",
		"STACKCHAN_MAIN_AUTH_TOKEN": "provider-probe-main-token",
		"STACKCHAN_ADMIN_TOKEN":     "provider-probe-admin-token",
	}
	for name, want := range cases {
		got, ok := lookup(name)
		if !ok || got != want {
			t.Fatalf("lookup(%q) = %q/%v, want %q/true", name, got, ok, want)
		}
	}
}
