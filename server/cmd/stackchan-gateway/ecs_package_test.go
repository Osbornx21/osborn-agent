package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestECSPackageCommandWritesSafeRuntimePackage(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "ecs-package")
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-secret-should-not-appear")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-secret-should-not-appear")
	t.Setenv("DASHSCOPE_API_KEY", "dashscope-secret-should-not-appear")
	t.Setenv("SILICONFLOW_API_KEY", "siliconflow-secret-should-not-appear")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-package",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--output-dir", outputDir,
		"--service-user", "stackchan",
		"--install-dir", "/opt/stackchan-gateway",
		"--config-dest", "/etc/stackchan-gateway/stackchan-gateway.yaml",
		"--env-file", "/etc/stackchan-gateway/gateway.env",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ecs-package code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ecs runtime package:") {
		t.Fatalf("stdout = %q, want package path", stdout.String())
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("package entries = %d, want 5", len(entries))
	}
	for _, name := range []string{"README.md", "gateway.env.example", "manifest.json", "preflight.sh", "stackchan-gateway.service"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(outputDir, "preflight.sh"))
		if err != nil {
			t.Fatalf("stat preflight: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("preflight mode = %o, want 0700", info.Mode().Perm())
		}
	}

	manifestData, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["schema_version"] != "stackchan_ecs_runtime_package_v1" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest["default_profile"] != "siliconflow-dashscope-voice" {
		t.Fatalf("manifest default_profile = %v", manifest["default_profile"])
	}
	if manifest["service_user"] != "stackchan" ||
		manifest["install_dir"] != "/opt/stackchan-gateway" ||
		manifest["config_dest"] != "/etc/stackchan-gateway/stackchan-gateway.yaml" ||
		manifest["env_file"] != "/etc/stackchan-gateway/gateway.env" {
		t.Fatalf("manifest runtime paths = %#v", manifest)
	}
	envNames, ok := manifest["required_env_names"].([]any)
	if !ok {
		t.Fatalf("manifest required_env_names = %#v", manifest["required_env_names"])
	}
	for _, want := range []string{"STACKCHAN_MAIN_AUTH_TOKEN", "STACKCHAN_ADMIN_TOKEN", "DASHSCOPE_API_KEY", "SILICONFLOW_API_KEY"} {
		if !jsonArrayContains(envNames, want) {
			t.Fatalf("manifest required_env_names = %#v, want %s", envNames, want)
		}
	}

	unit := readCommandTestFile(t, filepath.Join(outputDir, "stackchan-gateway.service"))
	for _, want := range []string{
		"[Unit]",
		"After=network-online.target",
		"[Service]",
		"User=stackchan",
		"WorkingDirectory=/opt/stackchan-gateway",
		"EnvironmentFile=/etc/stackchan-gateway/gateway.env",
		"ExecStartPre=/opt/stackchan-gateway/preflight.sh",
		"ExecStart=/opt/stackchan-gateway/stackchan-gateway --config /etc/stackchan-gateway/stackchan-gateway.yaml",
		"Restart=always",
		"RestartSec=2",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("systemd unit missing %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "ReadWritePaths=") {
		t.Fatalf("systemd unit must not require precreated mutable data paths:\n%s", unit)
	}

	envExample := readCommandTestFile(t, filepath.Join(outputDir, "gateway.env.example"))
	for _, want := range []string{
		"STACKCHAN_MAIN_AUTH_TOKEN=",
		"STACKCHAN_ADMIN_TOKEN=",
		"DASHSCOPE_API_KEY=",
		"SILICONFLOW_API_KEY=",
		"# Optional model overrides for measured voice profiles",
		"SILICONFLOW_LLM_MODEL=",
		"DASHSCOPE_TTS_VOICE=",
		"DASHSCOPE_TTS_VOLUME=",
		"DASHSCOPE_TTS_RATE=",
		"DASHSCOPE_TTS_PITCH=",
		"A21_OPUS_DOWNLINK_BITRATE_BPS=",
		"A21_OPUS_DOWNLINK_COMPLEXITY=",
	} {
		if !strings.Contains(envExample, want) {
			t.Fatalf("env example missing %q:\n%s", want, envExample)
		}
	}

	preflight := readCommandTestFile(t, filepath.Join(outputDir, "preflight.sh"))
	for _, want := range []string{
		"set -euo pipefail",
		"STACKCHAN_GATEWAY_BIN",
		"STACKCHAN_GATEWAY_CONFIG",
		"STACKCHAN_GATEWAY_ENV_FILE",
		"voice-profile-check",
		"--config \"$CONFIG_PATH\"",
		"preflight OK",
	} {
		if !strings.Contains(preflight, want) {
			t.Fatalf("preflight missing %q:\n%s", want, preflight)
		}
	}

	readme := readCommandTestFile(t, filepath.Join(outputDir, "README.md"))
	for _, want := range []string{
		"Aliyun ECS",
		"production provider selection",
		"server is cloud-only",
		"serial is dev/admin/recovery only",
		"install -m 700 preflight.sh /opt/stackchan-gateway/preflight.sh",
		"install -m 600 gateway.env.example /etc/stackchan-gateway/gateway.env",
		"ecs-preflight-dry-run",
		"systemctl enable --now stackchan-gateway",
		"voice-profile-check",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing %q:\n%s", want, readme)
		}
	}

	combined := strings.Join([]string{string(manifestData), unit, envExample, preflight, readme}, "\n")
	for _, forbidden := range []string{
		"main-secret-should-not-appear",
		"admin-secret-should-not-appear",
		"dashscope-secret-should-not-appear",
		"siliconflow-secret-should-not-appear",
		"Authorization: Bearer",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("ecs package leaked forbidden value %q:\n%s", forbidden, combined)
		}
	}
}

func TestECSPackageCommandRejectsDirtyOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "gateway.env"), []byte("secret-ish"), 0o600); err != nil {
		t.Fatalf("write unexpected file: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-package",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--output-dir", outputDir,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("ecs-package code = 0, want dirty output dir failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected entry") {
		t.Fatalf("stderr = %q, want dirty output dir failure", stderr.String())
	}
}

func TestECSPackageValidateCommandAcceptsGeneratedPackage(t *testing.T) {
	outputDir := writeECSPackageForValidateTest(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-package-validate",
		"--package-dir", outputDir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ecs-package-validate code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ecs runtime package OK:") {
		t.Fatalf("stdout = %q, want package OK", stdout.String())
	}
}

func TestECSPackageValidateCommandRejectsUnexpectedFile(t *testing.T) {
	outputDir := writeECSPackageForValidateTest(t)
	if err := os.WriteFile(filepath.Join(outputDir, "gateway.env"), []byte("secret-ish"), 0o600); err != nil {
		t.Fatalf("write unexpected env: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-package-validate",
		"--package-dir", outputDir,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("ecs-package-validate code = 0, want unexpected file failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected entry") {
		t.Fatalf("stderr = %q, want unexpected entry failure", stderr.String())
	}
}

func TestECSPackageValidateCommandRejectsEnvTemplateValues(t *testing.T) {
	outputDir := writeECSPackageForValidateTest(t)
	envPath := filepath.Join(outputDir, "gateway.env.example")
	envTemplate := readCommandTestFile(t, envPath)
	envTemplate = strings.Replace(envTemplate, "DASHSCOPE_API_KEY=\n", "DASHSCOPE_API_KEY=actual-secret-should-not-print\n", 1)
	if err := os.WriteFile(envPath, []byte(envTemplate), 0o600); err != nil {
		t.Fatalf("write env template: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-package-validate",
		"--package-dir", outputDir,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("ecs-package-validate code = 0, want env value failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "gateway.env.example must leave values empty") {
		t.Fatalf("stderr = %q, want empty env template failure", stderr.String())
	}
	if strings.Contains(stderr.String(), "actual-secret-should-not-print") {
		t.Fatalf("stderr leaked env template value: %s", stderr.String())
	}
}

func TestECSPreflightDryRunCommandAcceptsPrivateEnvFile(t *testing.T) {
	outputDir := writeECSPackageForValidateTest(t)
	envPath := writeECSRuntimeEnvFile(t, 0o600, map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "main-secret-should-not-appear",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret-should-not-appear",
		"DASHSCOPE_API_KEY":         "dashscope-secret-should-not-appear",
		"SILICONFLOW_API_KEY":       "siliconflow-secret-should-not-appear",
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-preflight-dry-run",
		"--package-dir", outputDir,
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--env-file", envPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ecs-preflight-dry-run code = %d, stderr = %s", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"ecs preflight dry-run OK:",
		"profile=siliconflow-dashscope-voice",
		"asr=dashscope-asr",
		"llm=siliconflow-llm",
		"tts=dashscope-tts",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q:\n%s", want, output)
		}
	}
	combined := output + stderr.String()
	for _, forbidden := range []string{
		"main-secret-should-not-appear",
		"admin-secret-should-not-appear",
		"dashscope-secret-should-not-appear",
		"siliconflow-secret-should-not-appear",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("ecs-preflight-dry-run leaked secret value %q:\n%s", forbidden, combined)
		}
	}
}

func TestECSPreflightDryRunCommandRejectsMissingRequiredEnvWithoutLeakingValues(t *testing.T) {
	outputDir := writeECSPackageForValidateTest(t)
	envPath := writeECSRuntimeEnvFile(t, 0o600, map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "main-secret-should-not-appear",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret-should-not-appear",
		"DASHSCOPE_API_KEY":         "dashscope-secret-should-not-appear",
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-preflight-dry-run",
		"--package-dir", outputDir,
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--env-file", envPath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("ecs-preflight-dry-run code = 0, want missing env failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "missing required env SILICONFLOW_API_KEY") {
		t.Fatalf("stderr = %q, want missing SiliconFlow env", stderr.String())
	}
	for _, forbidden := range []string{
		"main-secret-should-not-appear",
		"admin-secret-should-not-appear",
		"dashscope-secret-should-not-appear",
	} {
		if strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("ecs-preflight-dry-run leaked secret value %q:\n%s", forbidden, stderr.String())
		}
	}
}

func TestECSPreflightDryRunCommandRejectsReadableByGroupOrWorldEnvFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not stable on Windows")
	}
	outputDir := writeECSPackageForValidateTest(t)
	envPath := writeECSRuntimeEnvFile(t, 0o644, map[string]string{
		"STACKCHAN_MAIN_AUTH_TOKEN": "main-secret-should-not-appear",
		"STACKCHAN_ADMIN_TOKEN":     "admin-secret-should-not-appear",
		"DASHSCOPE_API_KEY":         "dashscope-secret-should-not-appear",
		"SILICONFLOW_API_KEY":       "siliconflow-secret-should-not-appear",
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"ecs-preflight-dry-run",
		"--package-dir", outputDir,
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--env-file", envPath,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("ecs-preflight-dry-run code = 0, want loose env permission failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "env file permissions") {
		t.Fatalf("stderr = %q, want env permission failure", stderr.String())
	}
	if strings.Contains(stderr.String(), "secret-should-not-appear") {
		t.Fatalf("ecs-preflight-dry-run leaked secret value:\n%s", stderr.String())
	}
}

func writeECSPackageForValidateTest(t *testing.T) string {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), "ecs-package")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"ecs-package",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--output-dir", outputDir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ecs-package code = %d, stderr = %s", code, stderr.String())
	}
	return outputDir
}

func writeECSRuntimeEnvFile(t *testing.T, mode os.FileMode, values map[string]string) string {
	t.Helper()
	envPath := filepath.Join(t.TempDir(), "gateway.env")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(name)
		builder.WriteString("=")
		builder.WriteString(values[name])
		builder.WriteString("\n")
	}
	if err := os.WriteFile(envPath, []byte(builder.String()), mode); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(envPath, mode); err != nil {
			t.Fatalf("chmod env file: %v", err)
		}
	}
	return envPath
}

func jsonArrayContains(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func readCommandTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
