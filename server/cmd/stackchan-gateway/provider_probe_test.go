package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"stackchan-gateway/internal/providerprobe"
	"stackchan-gateway/internal/providers"
)

func TestProviderProbeCommandWritesSanitizedReport(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	outputDir := t.TempDir()
	fixturePath := filepath.Join(t.TempDir(), "spoken-opus.json")
	if err := os.WriteFile(fixturePath, []byte(`{
		"format": "xiaozhi_opus_frames_v1",
		"sample_rate_hz": 16000,
		"frame_duration_ms": 60,
		"frames": [
			{"payload_hex": "f8fffe"},
			{"payload_hex": "f8fffd"}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write ASR fixture: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "cn-low-latency-cascade",
		"--runs", "2",
		"--output-dir", outputDir,
		"--timeout-ms", "1000",
		"--text", "do not echo this prompt",
		"--asr-opus-fixture", fixturePath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %s", code, stderr.String())
	}

	reportPath := strings.TrimSpace(stdout.String())
	if reportPath == "" {
		t.Fatal("stdout is empty, want report path")
	}
	if filepath.Dir(reportPath) != outputDir {
		t.Fatalf("report dir = %q, want %q", filepath.Dir(reportPath), outputDir)
	}
	if !strings.HasPrefix(filepath.Base(reportPath), "provider-probe-") || !strings.HasSuffix(reportPath, ".json") {
		t.Fatalf("report path = %q, want provider-probe-*.json", reportPath)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("report is not valid JSON: %s", string(data))
	}
	if bytes.Contains(data, []byte("do not echo this prompt")) ||
		bytes.Contains(data, []byte("main-token")) ||
		bytes.Contains(data, []byte("admin-token")) ||
		bytes.Contains(data, []byte("f8fffe")) ||
		bytes.Contains(data, []byte("f8fffd")) {
		t.Fatalf("report leaked prompt or token: %s", string(data))
	}

	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report["profile"] != "cn-low-latency-cascade" {
		t.Fatalf("profile = %v", report["profile"])
	}
	if report["runs"] != float64(2) {
		t.Fatalf("runs = %v", report["runs"])
	}
	if summaries, ok := report["summaries"].([]any); !ok || len(summaries) != 3 {
		t.Fatalf("summaries = %#v, want asr, llm and tts summaries", report["summaries"])
	}
	results, ok := report["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("results = %#v", report["results"])
	}
	firstResult, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("first result = %#v", results[0])
	}
	probeResult, ok := firstResult["result"].(map[string]any)
	if !ok {
		t.Fatalf("nested result = %#v", firstResult["result"])
	}
	if probeResult["input_audio_frames"] != float64(2) || probeResult["input_audio_bytes"] != float64(6) {
		t.Fatalf("ASR input fields = %#v", probeResult)
	}
}

func TestProviderProbeCommandRejectsMissingProfile(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "missing-profile",
		"--runs", "1",
		"--output-dir", t.TempDir(),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run() code = 0, want failure; stdout = %s", stdout.String())
	}
	if strings.Contains(stderr.String(), "main-token") || strings.Contains(stderr.String(), "admin-token") {
		t.Fatalf("stderr leaked token: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "profile not found") {
		t.Fatalf("stderr = %q, want profile not found", stderr.String())
	}
}

func TestProviderProbeCommandRejectsRealASRWithoutFixture(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "dashscope-cosyvoice",
		"--runs", "1",
		"--output-dir", t.TempDir(),
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe code = 0, want missing ASR fixture failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "requires --asr-opus-fixture") {
		t.Fatalf("stderr = %q, want ASR fixture failure", stderr.String())
	}
}

func TestProviderProbeCommandRejectsPlaceholderASRFixtureForRealASR(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

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
		"provider-probe",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "dashscope-cosyvoice",
		"--runs", "1",
		"--output-dir", t.TempDir(),
		"--asr-opus-fixture", fixturePath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe code = 0, want placeholder ASR fixture failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "semantic provider probes") {
		t.Fatalf("stderr = %q, want semantic fixture failure", stderr.String())
	}
	if strings.Contains(stderr.String(), "f8fffe") {
		t.Fatalf("stderr leaked fixture payload: %s", stderr.String())
	}
}

func TestProviderProbeValidateCommandAcceptsSanitizedReport(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	outputDir := t.TempDir()
	var probeStdout bytes.Buffer
	var probeStderr bytes.Buffer

	code := run([]string{
		"provider-probe",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "cn-low-latency-cascade",
		"--runs", "1",
		"--output-dir", outputDir,
		"--timeout-ms", "1000",
	}, &probeStdout, &probeStderr)
	if code != 0 {
		t.Fatalf("provider-probe code = %d, stderr = %s", code, probeStderr.String())
	}

	reportPath := strings.TrimSpace(probeStdout.String())
	var validateStdout bytes.Buffer
	var validateStderr bytes.Buffer
	code = run([]string{
		"provider-probe-validate",
		"--report", reportPath,
	}, &validateStdout, &validateStderr)
	if code != 0 {
		t.Fatalf("provider-probe-validate code = %d, stderr = %s", code, validateStderr.String())
	}
	if !strings.Contains(validateStdout.String(), "provider-probe report OK:") {
		t.Fatalf("validate stdout = %q", validateStdout.String())
	}
}

func TestProviderProbeValidateCommandRejectsMissingReport(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"provider-probe-validate"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run() code = 0, want failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--report is required") {
		t.Fatalf("stderr = %q, want missing report", stderr.String())
	}
}

func TestProviderProbeSummaryCommandOutputsSanitizedMarkdown(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	outputDir := t.TempDir()
	var probeStdout bytes.Buffer
	var probeStderr bytes.Buffer
	code := run([]string{
		"provider-probe",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "cn-low-latency-cascade",
		"--runs", "1",
		"--output-dir", outputDir,
		"--timeout-ms", "1000",
		"--text", "do not echo this prompt",
	}, &probeStdout, &probeStderr)
	if code != 0 {
		t.Fatalf("provider-probe code = %d, stderr = %s", code, probeStderr.String())
	}

	var summaryStdout bytes.Buffer
	var summaryStderr bytes.Buffer
	code = run([]string{
		"provider-probe-summary",
		strings.TrimSpace(probeStdout.String()),
	}, &summaryStdout, &summaryStderr)
	if code != 0 {
		t.Fatalf("provider-probe-summary code = %d, stderr = %s", code, summaryStderr.String())
	}
	markdown := summaryStdout.String()
	if !strings.Contains(markdown, "| Source | Profile | Provider | Modality |") ||
		!strings.Contains(markdown, "cn-low-latency-cascade") ||
		!strings.Contains(markdown, "mock") {
		t.Fatalf("summary markdown missing expected fields:\n%s", markdown)
	}
	if strings.Contains(markdown, "do not echo this prompt") ||
		strings.Contains(markdown, "main-token") ||
		strings.Contains(markdown, "admin-token") {
		t.Fatalf("summary markdown leaked sensitive material:\n%s", markdown)
	}
}

func TestProviderProbeSummaryCommandRejectsMissingPath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"provider-probe-summary"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run() code = 0, want missing path failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "at least one report path is required") {
		t.Fatalf("stderr = %q, want missing path failure", stderr.String())
	}
}

func TestProviderProbeGateCommandAcceptsValidatedReport(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	reportPath := writeMockProviderProbeReport(t, 2)
	var gateStdout bytes.Buffer
	var gateStderr bytes.Buffer
	code := run([]string{
		"provider-probe-gate",
		"--min-runs", "2",
		"--min-success-percent", "100",
		"--require-profiles", "cn-low-latency-cascade",
		"--require-modalities", "asr,llm,tts",
		reportPath,
	}, &gateStdout, &gateStderr)
	if code != 0 {
		t.Fatalf("provider-probe-gate code = %d, stderr = %s", code, gateStderr.String())
	}
	if !strings.Contains(gateStdout.String(), "provider-probe gate OK:") {
		t.Fatalf("gate stdout = %q", gateStdout.String())
	}
	for _, want := range []string{
		"min_runs=2",
		"min_success_percent=100",
		"required_profiles=cn-low-latency-cascade",
		"required_modalities=asr,llm,tts",
		"source_ref=unspecified",
		"source_state=unspecified",
	} {
		if !strings.Contains(gateStdout.String(), want) {
			t.Fatalf("gate stdout = %q, want %q", gateStdout.String(), want)
		}
	}
}

func TestProviderProbeGateCommandRejectsMissingFallback(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	reportPath := writeMockProviderProbeReport(t, 1)
	var gateStdout bytes.Buffer
	var gateStderr bytes.Buffer
	code := run([]string{
		"provider-probe-gate",
		"--require-fallback-modality", "llm",
		reportPath,
	}, &gateStdout, &gateStderr)
	if code == 0 {
		t.Fatalf("provider-probe-gate code = 0, want fallback failure; stdout = %s", gateStdout.String())
	}
	if !strings.Contains(gateStderr.String(), "fallback modality llm needs at least two successful providers") {
		t.Fatalf("gate stderr = %q, want fallback failure", gateStderr.String())
	}
}

func TestProviderProbeGateCommandPrintsEffectiveDefaultThresholds(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	reportPath := writeMockProviderProbeReport(t, 1)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"provider-probe-gate",
		"--min-runs", "0",
		"--min-success-percent", "0",
		reportPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("provider-probe-gate code = %d, stderr = %s", code, stderr.String())
	}
	for _, want := range []string{"min_runs=1", "min_success_percent=1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("gate stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestProviderProbeGateCommandRejectsMissingPath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"provider-probe-gate"}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-gate code = 0, want missing path failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "at least one report path is required") {
		t.Fatalf("stderr = %q, want missing path failure", stderr.String())
	}
}

func TestProviderProbePackageCommandWritesSafeExecutionPackage(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "probe-package")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "run-provider-probes.sh"), []byte("old script"), 0o644); err != nil {
		t.Fatalf("write old script: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-package",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--output-dir", outputDir,
		"--profiles", defaultProviderProbeProfiles,
		"--runs", "20",
		"--timeout-ms", "8000",
		"--run-delay-ms", "250",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("provider-probe-package code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "provider-probe package:") {
		t.Fatalf("stdout = %q, want package path", stdout.String())
	}

	manifestData, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["schema_version"] != "stackchan_provider_probe_package_v1" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest["profiles"] != defaultProviderProbeProfiles || manifest["runs"] != float64(20) {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest["run_delay_ms"] != float64(250) {
		t.Fatalf("manifest run_delay_ms = %v, want 250", manifest["run_delay_ms"])
	}
	if strings.TrimSpace(fmt.Sprint(manifest["source_ref"])) == "" || strings.TrimSpace(fmt.Sprint(manifest["source_state"])) == "" {
		t.Fatalf("manifest missing source provenance: %#v", manifest)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("package entries = %d, want 4", len(entries))
	}

	dirInfo, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("stat output dir: %v", err)
	}
	if runtime.GOOS != "windows" && dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("output dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}

	scriptPath := filepath.Join(outputDir, "run-provider-probes.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat script: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("script mode = %o, want 0700", info.Mode().Perm())
	}
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	script := string(scriptData)
	for _, want := range []string{
		"PROVIDER_ENV_FILE",
		"ASR_OPUS_FIXTURE",
		"ASR_FIXTURE_ARG_ENABLED",
		"ASR fixture not found:",
		"Capture one with:",
		"--advertise-url",
		"asr_fixture_path_allowed_without_git",
		"/var/lib/a21-air/fixtures/asr/",
		"git check-ignore -q --",
		"ASR fixture is not ignored by git:",
		"asr-fixture-validate",
		"provider-probe-matrix",
		"--allow-failed-profiles",
		"--run-delay-ms",
		"provider-probe-summary",
		"provider-probe-gate",
		"GATE_STATUS",
		"provider probe gate failed; diagnostic written",
		"provider-probe-diagnostics-",
		"provider-probe-diagnostics-validate",
		"provider probe diagnostics:",
		"provider-probe-evidence-validate",
		"provider-probe-evidence-summary",
		"provider-probe-evidence-summary.md",
		"provider probe promotion summary:",
		"provider-probe-evidence-",
		"COPYFILE_DISABLE=1 tar",
		"GOPROXY",
		"goproxy.cn",
		"GOSUMDB",
		"sum.golang.google.cn",
		"PROVIDER_PROBE_SKIP_SELF_TEST",
		"SOURCE_REF",
		"SOURCE_STATE",
		"--source-ref",
		"--source-state",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	for _, forbidden := range []string{
		"Authorization: Bearer",
		"payload_base64",
		"DASHSCOPE_API_KEY",
		"STEPFUN_API_KEY",
		"DEEPSEEK_API_KEY",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("script leaked forbidden token %q:\n%s", forbidden, script)
		}
	}

	powerShellPath := filepath.Join(outputDir, "run-provider-probes.ps1")
	powerShellInfo, err := os.Stat(powerShellPath)
	if err != nil {
		t.Fatalf("stat PowerShell runner: %v", err)
	}
	if runtime.GOOS != "windows" && powerShellInfo.Mode().Perm() != 0o700 {
		t.Fatalf("PowerShell runner mode = %o, want 0700", powerShellInfo.Mode().Perm())
	}
	powerShellData, err := os.ReadFile(powerShellPath)
	if err != nil {
		t.Fatalf("read PowerShell runner: %v", err)
	}
	powerShell := string(powerShellData)
	for _, want := range []string{
		"PROVIDER_ENV_FILE",
		"ASR_OPUS_FIXTURE",
		"5080lab",
		"ASR fixture not found:",
		"Capture one with:",
		"--advertise-url",
		"Test-AsrFixturePathAllowedWithoutGit",
		"/var/lib/a21-air/fixtures/asr/",
		"git check-ignore -q --",
		"ASR fixture is not ignored by git:",
		"asr-fixture-validate",
		"provider-probe-matrix",
		"--allow-failed-profiles",
		"--run-delay-ms",
		"Write-TextFileUTF8",
		"System.Text.UTF8Encoding",
		"provider-probe-summary",
		"provider-probe-gate",
		"GateStatus",
		"PreviousErrorActionPreference",
		"$ErrorActionPreference = 'Continue'",
		"--require-fallback-modality=$FallbackModality",
		"provider probe gate failed; diagnostic written",
		"provider-probe-diagnostics-",
		"provider-probe-diagnostics-validate",
		"provider probe diagnostics:",
		"provider-probe-evidence-validate",
		"provider-probe-evidence-summary",
		"provider-probe-evidence-summary.md",
		"provider probe promotion summary:",
		"provider-probe-evidence-",
		"tar -czf",
		"GOPROXY",
		"goproxy.cn",
		"GOSUMDB",
		"sum.golang.google.cn",
		"PROVIDER_PROBE_SKIP_SELF_TEST",
		"$SourceRef",
		"$SourceState",
		"--source-ref",
		"--source-state",
	} {
		if !strings.Contains(powerShell, want) {
			t.Fatalf("PowerShell runner missing %q:\n%s", want, powerShell)
		}
	}
	for _, forbidden := range []string{
		"Authorization: Bearer",
		"payload_base64",
		"DASHSCOPE_API_KEY",
		"STEPFUN_API_KEY",
		"DEEPSEEK_API_KEY",
		"Tee-Object",
	} {
		if strings.Contains(powerShell, forbidden) {
			t.Fatalf("PowerShell runner leaked forbidden token %q:\n%s", forbidden, powerShell)
		}
	}

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readme), "5080lab or Aliyun ECS") ||
		!strings.Contains(string(readme), "Run On Windows / 5080lab") ||
		!strings.Contains(string(readme), "run-provider-probes.ps1") ||
		!strings.Contains(string(readme), "STACKCHAN_MAIN_AUTH_TOKEN") ||
		!strings.Contains(string(readme), "STACKCHAN_ADMIN_TOKEN is not required for capture") ||
		!strings.Contains(string(readme), "raw token or Bearer <token>") ||
		!strings.Contains(string(readme), "Device-Id and Client-Id headers must match") ||
		!strings.Contains(string(readme), "auth-failed") ||
		!strings.Contains(string(readme), "header presence") ||
		!strings.Contains(string(readme), "device_id") ||
		!strings.Contains(string(readme), "client_id") ||
		!strings.Contains(string(readme), "prints a safe ready line") ||
		!strings.Contains(string(readme), "connect_url") ||
		!strings.Contains(string(readme), "auth_env") ||
		!strings.Contains(string(readme), "--advertise-url") ||
		!strings.Contains(string(readme), "asr-fixture-validate") ||
		!strings.Contains(string(readme), "ASR_OPUS_FIXTURE") ||
		!strings.Contains(string(readme), "/var/lib/a21-air/fixtures/asr/") ||
		!strings.Contains(string(readme), "capture fails before serving") ||
		!strings.Contains(string(readme), "path must match the capture WebSocket path") ||
		!strings.Contains(string(readme), "must not include user info") ||
		!strings.Contains(string(readme), "Source ref:") ||
		!strings.Contains(string(readme), "Source state:") ||
		!strings.Contains(string(readme), "fails before any provider call if the fixture is missing") {
		t.Fatalf("README missing run location guidance:\n%s", string(readme))
	}
}

func TestProviderProbePackageCommandAcceptsExplicitSourceProvenance(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "probe-package")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-package",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--output-dir", outputDir,
		"--profiles", "siliconflow-llm,moonshot-llm",
		"--runs", "20",
		"--timeout-ms", "8000",
		"--run-delay-ms", "1000",
		"--require-modalities", "llm",
		"--gate-fallback-modality", "llm",
		"--source-ref", "9e8ed63bd03c",
		"--source-state", "clean",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("provider-probe-package code = %d, stderr = %s", code, stderr.String())
	}

	manifestData, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["source_ref"] != "9e8ed63bd03c" || manifest["source_state"] != "clean" {
		t.Fatalf("manifest source provenance = %#v", manifest)
	}

	for _, name := range []string{"run-provider-probes.sh", "run-provider-probes.ps1", "README.md"} {
		data, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(data)
		if !strings.Contains(text, "9e8ed63bd03c") || !strings.Contains(text, "clean") {
			t.Fatalf("%s missing explicit source provenance:\n%s", name, text)
		}
	}
}

func TestProviderProbePackageDetectsASRFixtureRequirementFromProfileConfig(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "probe-package")
	configPath := writeProviderProbePackageConfig(t, `
providers:
  default_profile: "custom-asr-profile"
  profiles:
    custom-asr-profile:
      asr: "dashscope-asr"
      llm: "mock"
      tts: ""
`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-package",
		"--config", configPath,
		"--output-dir", outputDir,
		"--profiles", "custom-asr-profile",
		"--runs", "20",
		"--timeout-ms", "8000",
		"--require-modalities", "llm",
		"--gate-fallback-modality", "llm",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("provider-probe-package code = %d, stderr = %s", code, stderr.String())
	}

	manifestData, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["requires_asr_fixture"] != true {
		t.Fatalf("manifest requires_asr_fixture = %v, want true; manifest = %#v", manifest["requires_asr_fixture"], manifest)
	}
	if manifest["config_path"] != configPath {
		t.Fatalf("manifest config_path = %v, want %q; manifest = %#v", manifest["config_path"], configPath, manifest)
	}

	for _, name := range []string{"run-provider-probes.sh", "run-provider-probes.ps1", "README.md"} {
		data, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(data), "Requires ASR fixture: true") &&
			!strings.Contains(string(data), "REQUIRES_ASR_FIXTURE=1") &&
			!strings.Contains(string(data), "$RequiresAsrFixture = $true") {
			t.Fatalf("%s missing ASR fixture requirement from profile config:\n%s", name, string(data))
		}
		if !strings.Contains(string(data), "--config") &&
			!strings.Contains(string(data), "CONFIG_PATH=") &&
			!strings.Contains(string(data), "$ConfigPath") &&
			!strings.Contains(string(data), "PROVIDER_PROBE_CONFIG") {
			t.Fatalf("%s missing generated config propagation:\n%s", name, string(data))
		}
	}
}

func TestProviderProbePackageRejectsMissingProfileEvenWhenASRIsRequired(t *testing.T) {
	configPath := writeProviderProbePackageConfig(t, `
providers:
  default_profile: "known-profile"
  profiles:
    known-profile:
      asr: ""
      llm: "mock"
      tts: ""
`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-package",
		"--config", configPath,
		"--output-dir", filepath.Join(t.TempDir(), "probe-package"),
		"--profiles", "missing-profile",
		"--runs", "20",
		"--timeout-ms", "8000",
		"--require-modalities", "asr,llm,tts",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-package code = 0, want missing profile failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "profile missing-profile not found") {
		t.Fatalf("stderr = %q, want missing profile failure", stderr.String())
	}
}

func TestProviderProbePackageBashRunnerPersistsGateFailureDiagnostic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash runner execution is covered on Unix-like hosts")
	}
	outputDir := filepath.Join(t.TempDir(), "probe-package")
	serverRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("server root: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-package",
		"--config", filepath.Join(serverRoot, "configs/stackchan-gateway.example.yaml"),
		"--output-dir", outputDir,
		"--profiles", "deepseek-llm",
		"--runs", "1",
		"--timeout-ms", "1000",
		"--require-modalities", "llm",
		"--gate-fallback-modality", "",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("provider-probe-package code = %d, stderr = %s", code, stderr.String())
	}

	envPath := filepath.Join(t.TempDir(), "provider.env")
	if err := os.WriteFile(envPath, []byte("# intentionally empty\n"), 0o600); err != nil {
		t.Fatalf("write provider env: %v", err)
	}
	reportBase := filepath.Join(t.TempDir(), "reports")
	cmd := exec.Command("bash", filepath.Join(outputDir, "run-provider-probes.sh"))
	cmd.Dir = serverRoot
	cmd.Env = append(withoutEnv(os.Environ(), []string{
		"DEEPSEEK_API_KEY",
		"A21_LAB_DEEPSEEK_API_KEY",
		"A21_DEEPSEEK_API_KEY",
	}), "PROVIDER_ENV_FILE="+envPath, "BASE_REPORT_DIR="+reportBase, "RUN_ID=failed-gate-test", "PROVIDER_PROBE_SKIP_SELF_TEST=1")

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("bash runner succeeded, want gate failure; output = %s", string(output))
	}
	gatePath := filepath.Join(reportBase, "failed-gate-test", "provider-probe-gate.txt")
	gateData, readErr := os.ReadFile(gatePath)
	if readErr != nil {
		t.Fatalf("read gate diagnostic: %v; output = %s", readErr, string(output))
	}
	gateText := string(gateData)
	for _, want := range []string{"provider-probe-gate failed", "provider_config_error"} {
		if !strings.Contains(gateText, want) {
			t.Fatalf("gate diagnostic = %q, want %q; output = %s", gateText, want, string(output))
		}
	}
	if _, err := os.Stat(filepath.Join(reportBase, "failed-gate-test", "provider-probe-summary.md")); err != nil {
		t.Fatalf("summary was not written before gate failure: %v", err)
	}
	diagnosticsPath := filepath.Join(reportBase, "provider-probe-diagnostics-failed-gate-test.tgz")
	entries := listTarGzipEntries(t, diagnosticsPath)
	for _, want := range []string{
		"provider-probe-summary.md",
		"provider-probe-gate.txt",
	} {
		if !entries[want] {
			t.Fatalf("diagnostic archive entries = %#v, want %s", entries, want)
		}
	}
	reports, err := filepath.Glob(filepath.Join(reportBase, "failed-gate-test", "provider-probe-*.json"))
	if err != nil {
		t.Fatalf("glob reports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %#v, want one report before gate failure", reports)
	}
	if !entries[filepath.Base(reports[0])] {
		t.Fatalf("diagnostic archive entries = %#v, want report %s", entries, filepath.Base(reports[0]))
	}
}

func TestProviderProbePackageCommandRejectsInvalidGatePercent(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-package",
		"--output-dir", t.TempDir(),
		"--gate-min-success-percent", "101",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-package code = 0, want invalid gate failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "between 1 and 100") {
		t.Fatalf("stderr = %q, want invalid gate percent failure", stderr.String())
	}
}

func TestProviderProbePackageCommandRejectsDirtyOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "provider.env"), []byte("secret-ish"), 0o600); err != nil {
		t.Fatalf("write unexpected file: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"provider-probe-package",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--output-dir", outputDir,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-package code = 0, want dirty output dir failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected entry") {
		t.Fatalf("stderr = %q, want dirty output dir failure", stderr.String())
	}
}

func TestProviderProbeEvidenceValidateCommandAcceptsSafeArchive(t *testing.T) {
	reportName, reportData := writeCommandEvidenceReportData(t)
	archivePath := filepath.Join(t.TempDir(), "provider-probe-evidence-test.tgz")
	writeEvidenceArchiveForCommandTest(t, archivePath, map[string][]byte{
		reportName:                  reportData,
		"provider-probe-summary.md": []byte("safe generated summary\n"),
		"provider-probe-gate.txt":   []byte(validCommandEvidenceGateText()),
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"provider-probe-evidence-validate",
		"--archive", archivePath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("provider-probe-evidence-validate code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "provider-probe evidence OK:") {
		t.Fatalf("stdout = %q, want evidence OK", stdout.String())
	}
}

func TestProviderProbeEvidenceValidateCommandRejectsGateWithoutThresholds(t *testing.T) {
	reportName, reportData := writeCommandEvidenceReportData(t)
	archivePath := filepath.Join(t.TempDir(), "provider-probe-evidence-test.tgz")
	writeEvidenceArchiveForCommandTest(t, archivePath, map[string][]byte{
		reportName:                  reportData,
		"provider-probe-summary.md": []byte("safe generated summary\n"),
		"provider-probe-gate.txt":   []byte("provider-probe gate OK: rows=3 profiles=1 providers=1\n"),
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"provider-probe-evidence-validate",
		"--archive", archivePath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-evidence-validate code = 0, want missing threshold failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "missing min_runs") {
		t.Fatalf("stderr = %q, want missing gate threshold", stderr.String())
	}
}

func TestProviderProbeEvidenceValidateCommandRejectsMissingArchive(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"provider-probe-evidence-validate"}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-evidence-validate code = 0, want missing archive failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--archive is required") {
		t.Fatalf("stderr = %q, want missing archive failure", stderr.String())
	}
}

func TestProviderProbeEvidenceSummaryCommandOutputsSafeMarkdown(t *testing.T) {
	reportName, reportData := writeCommandEvidenceReportData(t)
	archivePath := filepath.Join(t.TempDir(), "provider-probe-evidence-test.tgz")
	writeEvidenceArchiveForCommandTest(t, archivePath, map[string][]byte{
		reportName:                  reportData,
		"provider-probe-summary.md": []byte("this remote summary is validated but not trusted for promotion\n"),
		"provider-probe-gate.txt":   []byte(validCommandEvidenceGateText()),
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"provider-probe-evidence-summary",
		"--archive", archivePath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("provider-probe-evidence-summary code = %d, stderr = %s", code, stderr.String())
	}
	markdown := stdout.String()
	for _, want := range []string{"# Provider Probe Evidence", "SHA256:", "cn-low-latency-cascade", "mock", filepath.Base(archivePath)} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	for _, forbidden := range []string{"do not echo this prompt", "main-token", "admin-token", "remote summary is validated"} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("markdown leaked %q:\n%s", forbidden, markdown)
		}
	}
}

func TestProviderProbeDiagnosticsValidateCommandAcceptsSafeFailureArchive(t *testing.T) {
	reportName, reportData := writeCommandDiagnosticsReportData(t)
	archivePath := filepath.Join(t.TempDir(), "provider-probe-diagnostics-test.tgz")
	writeEvidenceArchiveForCommandTest(t, archivePath, map[string][]byte{
		reportName:                  reportData,
		"provider-probe-summary.md": []byte("safe generated summary\n"),
		"provider-probe-gate.txt":   []byte("provider-probe-gate failed: provider probe gate failed: deepseek-llm/llm in profile deepseek-llm has no successful probes; errors=provider_config_error\n"),
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"provider-probe-diagnostics-validate",
		"--archive", archivePath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("provider-probe-diagnostics-validate code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "provider-probe diagnostics OK:") {
		t.Fatalf("stdout = %q, want diagnostics OK", stdout.String())
	}
}

func TestProviderProbeDiagnosticsValidateCommandRejectsPassingGate(t *testing.T) {
	reportName, reportData := writeCommandEvidenceReportData(t)
	archivePath := filepath.Join(t.TempDir(), "provider-probe-diagnostics-test.tgz")
	writeEvidenceArchiveForCommandTest(t, archivePath, map[string][]byte{
		reportName:                  reportData,
		"provider-probe-summary.md": []byte("safe generated summary\n"),
		"provider-probe-gate.txt":   []byte(validCommandEvidenceGateText()),
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"provider-probe-diagnostics-validate",
		"--archive", archivePath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-diagnostics-validate code = 0, want passing gate failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "passing gate") {
		t.Fatalf("stderr = %q, want passing gate rejection", stderr.String())
	}
}

func TestProviderProbeDiagnosticsValidateCommandRejectsMissingArchive(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"provider-probe-diagnostics-validate"}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-diagnostics-validate code = 0, want missing archive failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--archive is required") {
		t.Fatalf("stderr = %q, want missing archive failure", stderr.String())
	}
}

func writeCommandEvidenceReportData(t *testing.T) (string, []byte) {
	t.Helper()
	report := providerprobe.Report{
		Profile:         "cn-low-latency-cascade",
		Runs:            1,
		TimeoutMS:       1000,
		PromptTextBytes: 12,
		StartedAtUnixMS: 1000,
		FinishedUnixMS:  1300,
		Successes:       2,
		Results: []providerprobe.RunResult{{
			Run:        1,
			ProviderID: "mock-llm-a",
			Modality:   providers.ProbeModalityLLM,
			Result: providers.ProbeResult{
				ProviderID:       "mock-llm-a",
				Modality:         providers.ProbeModalityLLM,
				OK:               true,
				ProviderModelID:  "mock-model-a",
				FirstTokenMS:     50,
				TotalMS:          100,
				OutputTextBytes:  10,
				StartedAtUnixMS:  1000,
				FinishedAtUnixMS: 1100,
			},
		}, {
			Run:        1,
			ProviderID: "mock-llm-b",
			Modality:   providers.ProbeModalityLLM,
			Result: providers.ProbeResult{
				ProviderID:       "mock-llm-b",
				Modality:         providers.ProbeModalityLLM,
				OK:               true,
				ProviderModelID:  "mock-model-b",
				FirstTokenMS:     60,
				TotalMS:          120,
				OutputTextBytes:  12,
				StartedAtUnixMS:  1000,
				FinishedAtUnixMS: 1120,
			},
		}},
		Summaries: []providerprobe.Summary{{
			ProviderID:      "mock-llm-a",
			Modality:        providers.ProbeModalityLLM,
			Runs:            1,
			Successes:       1,
			FirstTokenP50MS: 50,
			FirstTokenP95MS: 50,
			TotalP50MS:      100,
			TotalP95MS:      100,
		}, {
			ProviderID:      "mock-llm-b",
			Modality:        providers.ProbeModalityLLM,
			Runs:            1,
			Successes:       1,
			FirstTokenP50MS: 60,
			FirstTokenP95MS: 60,
			TotalP50MS:      120,
			TotalP95MS:      120,
		}},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal command evidence report: %v", err)
	}
	if err := providerprobe.ValidateReportJSON(data); err != nil {
		t.Fatalf("validate command evidence report: %v", err)
	}
	return "provider-probe-20260606-120000.json", data
}

func writeCommandDiagnosticsReportData(t *testing.T) (string, []byte) {
	t.Helper()
	report := providerprobe.Report{
		Profile:         "deepseek-llm",
		Runs:            1,
		TimeoutMS:       1000,
		PromptTextBytes: 12,
		StartedAtUnixMS: 1000,
		FinishedUnixMS:  1200,
		Failures:        1,
		Results: []providerprobe.RunResult{{
			Run:        1,
			ProviderID: "deepseek-llm",
			Modality:   providers.ProbeModalityLLM,
			ErrorClass: "provider_config_error",
			Result: providers.ProbeResult{
				ProviderID:       "deepseek-llm",
				Modality:         providers.ProbeModalityLLM,
				OK:               false,
				ProviderError:    "provider_config_error",
				StartedAtUnixMS:  1000,
				FinishedAtUnixMS: 1100,
			},
		}},
		Summaries: []providerprobe.Summary{{
			ProviderID: "deepseek-llm",
			Modality:   providers.ProbeModalityLLM,
			Runs:       1,
			Failures:   1,
		}},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal command diagnostics report: %v", err)
	}
	if err := providerprobe.ValidateReportJSON(data); err != nil {
		t.Fatalf("validate command diagnostics report: %v", err)
	}
	return "provider-probe-20260606-120000.json", data
}

func TestProviderProbeEvidenceSummaryCommandRejectsMissingArchive(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"provider-probe-evidence-summary"}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("provider-probe-evidence-summary code = 0, want missing archive failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--archive is required") {
		t.Fatalf("stderr = %q, want missing archive failure", stderr.String())
	}
}

func writeMockProviderProbeReport(t *testing.T, runs int) string {
	t.Helper()
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	outputDir := t.TempDir()
	var probeStdout bytes.Buffer
	var probeStderr bytes.Buffer
	code := run([]string{
		"provider-probe",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "cn-low-latency-cascade",
		"--runs", strconv.Itoa(runs),
		"--output-dir", outputDir,
		"--timeout-ms", "1000",
		"--text", "do not echo this prompt",
	}, &probeStdout, &probeStderr)
	if code != 0 {
		t.Fatalf("provider-probe code = %d, stderr = %s", code, probeStderr.String())
	}
	return strings.TrimSpace(probeStdout.String())
}

func writeProviderProbePackageConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stackchan-gateway.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write provider probe package config: %v", err)
	}
	return path
}

func validCommandEvidenceGateText() string {
	return "provider-probe gate OK: rows=2 profiles=1 providers=2 min_runs=1 min_success_percent=80 required_profiles=cn-low-latency-cascade required_modalities=llm fallback_modality=llm source_ref=test-source source_state=clean\n"
}

func withoutEnv(values []string, names []string) []string {
	blocked := map[string]struct{}{}
	for _, name := range names {
		blocked[name] = struct{}{}
	}
	var filtered []string
	for _, value := range values {
		name := strings.SplitN(value, "=", 2)[0]
		if _, skip := blocked[name]; skip {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func listTarGzipEntries(t *testing.T, path string) map[string]bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	entries := map[string]bool{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		entries[header.Name] = true
	}
	return entries
}

func writeEvidenceArchiveForCommandTest(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, data := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(data)),
		}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}
