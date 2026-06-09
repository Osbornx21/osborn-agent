package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	gatewayconfig "stackchan-gateway/internal/config"
)

type providerProbePackageManifest struct {
	SchemaVersion        string `json:"schema_version"`
	GeneratedAtUnixMS    int64  `json:"generated_at_unix_ms"`
	ConfigPath           string `json:"config_path"`
	Profiles             string `json:"profiles"`
	Runs                 int    `json:"runs"`
	TimeoutMS            int    `json:"timeout_ms"`
	RunDelayMS           int    `json:"run_delay_ms"`
	RequiredModalities   string `json:"required_modalities"`
	GateMinSuccessPct    int    `json:"gate_min_success_percent"`
	GateFallbackModality string `json:"gate_fallback_modality"`
	SourceRef            string `json:"source_ref"`
	SourceState          string `json:"source_state"`
	RequiresASRFixture   bool   `json:"requires_asr_fixture"`
}

func runProviderProbePackage(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-package", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config for profile ASR fixture detection")
	outputDir := flags.String("output-dir", "./var/probe-packages/provider-probe-package", "provider probe execution package output directory")
	profiles := flags.String("profiles", defaultProviderProbeProfiles, "comma-separated provider profile ids")
	runs := flags.Int("runs", 20, "number of probe runs per profile")
	timeoutMS := flags.Int("timeout-ms", 5000, "timeout per provider probe in milliseconds")
	runDelayMS := flags.Int("run-delay-ms", 0, "delay between probe runs in milliseconds; recorded in reports")
	requiredModalities := flags.String("require-modalities", "asr,llm,tts", "comma-separated modalities required by provider-probe-gate")
	gateMinSuccessPct := flags.Int("gate-min-success-percent", 80, "minimum success percent for provider-probe-gate")
	gateFallbackModality := flags.String("gate-fallback-modality", "llm", "modality that must have at least two successful providers")
	sourceRefOverride := flags.String("source-ref", "", "safe source revision label for evidence provenance; overrides git detection when set")
	sourceStateOverride := flags.String("source-state", "", "safe source state label for evidence provenance; overrides git detection when set")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*outputDir) == "" {
		fmt.Fprintln(stderr, "provider-probe-package failed: --output-dir is required")
		return 2
	}
	if len(parseProviderProbeProfiles(*profiles)) == 0 {
		fmt.Fprintln(stderr, "provider-probe-package failed: --profiles must include at least one profile")
		return 2
	}
	if *runs <= 0 {
		fmt.Fprintln(stderr, "provider-probe-package failed: --runs must be positive")
		return 2
	}
	if *timeoutMS <= 0 {
		fmt.Fprintln(stderr, "provider-probe-package failed: --timeout-ms must be positive")
		return 2
	}
	if *runDelayMS < 0 {
		fmt.Fprintln(stderr, "provider-probe-package failed: --run-delay-ms must not be negative")
		return 2
	}
	if *gateMinSuccessPct <= 0 || *gateMinSuccessPct > 100 {
		fmt.Fprintln(stderr, "provider-probe-package failed: --gate-min-success-percent must be between 1 and 100")
		return 2
	}
	requiresASRFixture, err := providerProbePackageRequiresASRFixture(*configPath, *profiles, *requiredModalities)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(*outputDir, 0o700); err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: %v\n", err)
		return 1
	}
	if err := os.Chmod(*outputDir, 0o700); err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: chmod %s: %v\n", *outputDir, err)
		return 1
	}
	if err := ensureProviderProbePackageDirClean(*outputDir); err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: %v\n", err)
		return 1
	}
	sourceRef, sourceState := detectProviderProbePackageSource()
	if strings.TrimSpace(*sourceRefOverride) != "" {
		sourceRef = strings.TrimSpace(*sourceRefOverride)
	}
	if strings.TrimSpace(*sourceStateOverride) != "" {
		sourceState = strings.TrimSpace(*sourceStateOverride)
	}
	manifest := providerProbePackageManifest{
		SchemaVersion:        "stackchan_provider_probe_package_v1",
		GeneratedAtUnixMS:    time.Now().UnixMilli(),
		ConfigPath:           strings.TrimSpace(*configPath),
		Profiles:             strings.TrimSpace(*profiles),
		Runs:                 *runs,
		TimeoutMS:            *timeoutMS,
		RunDelayMS:           *runDelayMS,
		RequiredModalities:   strings.TrimSpace(*requiredModalities),
		GateMinSuccessPct:    *gateMinSuccessPct,
		GateFallbackModality: strings.TrimSpace(*gateFallbackModality),
		SourceRef:            sourceRef,
		SourceState:          sourceState,
		RequiresASRFixture:   requiresASRFixture,
	}
	if err := writeProviderProbePackageFile(filepath.Join(*outputDir, "manifest.json"), formatProviderProbePackageManifest(manifest), 0o600); err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: %v\n", err)
		return 1
	}
	if err := writeProviderProbePackageFile(filepath.Join(*outputDir, "run-provider-probes.sh"), formatProviderProbePackageScript(manifest), 0o700); err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: %v\n", err)
		return 1
	}
	if err := writeProviderProbePackageFile(filepath.Join(*outputDir, "run-provider-probes.ps1"), formatProviderProbePackagePowerShell(manifest), 0o700); err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: %v\n", err)
		return 1
	}
	if err := writeProviderProbePackageFile(filepath.Join(*outputDir, "README.md"), formatProviderProbePackageReadme(manifest), 0o600); err != nil {
		fmt.Fprintf(stderr, "provider-probe-package failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "provider-probe package: %s\n", *outputDir)
	return 0
}

func providerProbePackageRequiresASRFixture(configPath string, profilesCSV string, requiredModalitiesCSV string) (bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("read config %q: %w", configPath, err)
	}
	var cfg gatewayconfig.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("parse config %q: %w", configPath, err)
	}
	requiresASRFixture := false
	for _, profileID := range parseProviderProbeProfiles(profilesCSV) {
		profile, ok := cfg.Providers.Profiles[profileID]
		if !ok {
			return false, fmt.Errorf("profile %s not found in config %s", profileID, configPath)
		}
		if strings.TrimSpace(profile.ASR) != "" {
			requiresASRFixture = true
		}
	}
	for _, modality := range parseProviderProbeProfiles(requiredModalitiesCSV) {
		if strings.EqualFold(modality, "asr") {
			requiresASRFixture = true
		}
	}
	return requiresASRFixture, nil
}

func ensureProviderProbePackageDirClean(outputDir string) error {
	allowed := map[string]bool{
		"README.md":               true,
		"manifest.json":           true,
		"run-provider-probes.ps1": true,
		"run-provider-probes.sh":  true,
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", outputDir, err)
	}
	for _, entry := range entries {
		if allowed[entry.Name()] {
			continue
		}
		return fmt.Errorf("output dir contains unexpected entry %q; choose an empty directory", entry.Name())
	}
	return nil
}

func writeProviderProbePackageFile(path string, content string, mode os.FileMode) error {
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func detectProviderProbePackageSource() (string, string) {
	ref, err := gitOutput("rev-parse", "--short=12", "HEAD")
	if err != nil || strings.TrimSpace(ref) == "" {
		return "unavailable", "unavailable"
	}
	status, err := gitOutput("status", "--porcelain")
	if err != nil {
		return strings.TrimSpace(ref), "unavailable"
	}
	state := "clean"
	if strings.TrimSpace(status) != "" {
		state = "dirty"
	}
	return strings.TrimSpace(ref), state
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func formatProviderProbePackageManifest(manifest providerProbePackageManifest) string {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data) + "\n"
}

func formatProviderProbePackageScript(manifest providerProbePackageManifest) string {
	configPath := shellSingleQuote(manifest.ConfigPath)
	profiles := shellSingleQuote(manifest.Profiles)
	requiredModalities := shellSingleQuote(manifest.RequiredModalities)
	fallbackModality := shellSingleQuote(manifest.GateFallbackModality)
	sourceRef := shellSingleQuote(manifest.SourceRef)
	sourceState := shellSingleQuote(manifest.SourceState)
	requiresASRFixture := 0
	if manifest.RequiresASRFixture {
		requiresASRFixture = 1
	}
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

: "${PROVIDER_ENV_FILE:?set PROVIDER_ENV_FILE to the private provider env file path on this machine}"
CONFIG_PATH=${PROVIDER_PROBE_CONFIG:-%s}
PROFILES=%s
REQUIRED_MODALITIES=%s
FALLBACK_MODALITY=%s
SOURCE_REF=%s
SOURCE_STATE=%s
REQUIRES_ASR_FIXTURE=%d
RUN_DELAY_MS=%d
ASR_OPUS_FIXTURE="${ASR_OPUS_FIXTURE:-./var/fixtures/asr/spoken-opus.json}"
BASE_REPORT_DIR="${BASE_REPORT_DIR:-./var/reports}"
RUN_ID="${RUN_ID:-$(date -u +%%Y%%m%%dT%%H%%M%%SZ)}"
ASR_FIXTURE_ARG_ENABLED=0

asr_fixture_path_allowed_without_git() {
  local fixture="$1"
  case "$fixture" in
    *'/../'*|'../'*|*'/..'|*'/./'*)
      return 1
      ;;
  esac
  case "$fixture" in
    /var/lib/a21-air/fixtures/asr/*.json|var/fixtures/asr/*.json|server/var/fixtures/asr/*.json)
      return 0
      ;;
    ./var/fixtures/asr/*.json|./server/var/fixtures/asr/*.json)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOSUMDB="${GOSUMDB:-sum.golang.google.cn}"

if [[ "$REQUIRES_ASR_FIXTURE" == "1" ]]; then
  if [[ ! -f "$ASR_OPUS_FIXTURE" ]]; then
    echo "ASR fixture not found: $ASR_OPUS_FIXTURE" >&2
    echo "Capture one with: go run ./cmd/stackchan-gateway asr-fixture-capture --config ./configs/stackchan-gateway.example.yaml --listen 0.0.0.0:8080 --advertise-url ws://<reachable-host>:8080/xiaozhi/v1/ws --output ./var/fixtures/asr/spoken-opus.json" >&2
    exit 2
  fi
  if ! git check-ignore -q -- "$ASR_OPUS_FIXTURE" 2>/dev/null && ! asr_fixture_path_allowed_without_git "$ASR_OPUS_FIXTURE"; then
    echo "ASR fixture is not ignored by git: $ASR_OPUS_FIXTURE" >&2
    echo "Keep spoken ASR fixtures under server/var/fixtures/asr/ or /var/lib/a21-air/fixtures/asr/ before running provider probes." >&2
    exit 2
  fi
  go run ./cmd/stackchan-gateway asr-fixture-validate --fixture "$ASR_OPUS_FIXTURE"
  ASR_FIXTURE_ARG_ENABLED=1
fi

case "$BASE_REPORT_DIR" in
  /*) BASE_REPORT_DIR_ABS="$BASE_REPORT_DIR" ;;
  *) BASE_REPORT_DIR_ABS="$(pwd)/$BASE_REPORT_DIR" ;;
esac
REPORT_DIR="$BASE_REPORT_DIR_ABS/$RUN_ID"
EVIDENCE_TGZ="$BASE_REPORT_DIR_ABS/provider-probe-evidence-$RUN_ID.tgz"
DIAGNOSTICS_TGZ="$BASE_REPORT_DIR_ABS/provider-probe-diagnostics-$RUN_ID.tgz"

mkdir -p "$REPORT_DIR"
test -f "$PROVIDER_ENV_FILE"

if [[ "${PROVIDER_PROBE_SKIP_SELF_TEST:-0}" != "1" ]]; then
  go test ./cmd/stackchan-gateway ./internal/providerprobe ./internal/providers
fi

if [[ "$ASR_FIXTURE_ARG_ENABLED" == "1" ]]; then
  go run ./cmd/stackchan-gateway provider-probe-matrix \
    --env-file "$PROVIDER_ENV_FILE" \
    --config "$CONFIG_PATH" \
    --profiles "$PROFILES" \
    --runs %d \
    --timeout-ms %d \
    --run-delay-ms "$RUN_DELAY_MS" \
    --output-dir "$REPORT_DIR" \
    --allow-failed-profiles \
    --asr-opus-fixture "$ASR_OPUS_FIXTURE"
else
  go run ./cmd/stackchan-gateway provider-probe-matrix \
    --env-file "$PROVIDER_ENV_FILE" \
    --config "$CONFIG_PATH" \
    --profiles "$PROFILES" \
    --runs %d \
    --timeout-ms %d \
    --run-delay-ms "$RUN_DELAY_MS" \
    --output-dir "$REPORT_DIR" \
    --allow-failed-profiles
fi

go run ./cmd/stackchan-gateway provider-probe-summary "$REPORT_DIR"/provider-probe-*.json \
  | tee "$REPORT_DIR/provider-probe-summary.md"

set +e
go run ./cmd/stackchan-gateway provider-probe-gate \
  --min-runs %d \
  --min-success-percent %d \
  --require-profiles "$PROFILES" \
  --require-modalities "$REQUIRED_MODALITIES" \
  --require-fallback-modality "$FALLBACK_MODALITY" \
  --source-ref "$SOURCE_REF" \
  --source-state "$SOURCE_STATE" \
  "$REPORT_DIR"/provider-probe-*.json > "$REPORT_DIR/provider-probe-gate.txt" 2>&1
GATE_STATUS=$?
set -e
cat "$REPORT_DIR/provider-probe-gate.txt"
if [[ "$GATE_STATUS" -ne 0 ]]; then
  (
    cd "$REPORT_DIR"
    COPYFILE_DISABLE=1 tar -czf "$DIAGNOSTICS_TGZ" provider-probe-summary.md provider-probe-gate.txt provider-probe-*.json
  )
  go run ./cmd/stackchan-gateway provider-probe-diagnostics-validate --archive "$DIAGNOSTICS_TGZ"
  echo "provider probe gate failed; diagnostic written to $REPORT_DIR/provider-probe-gate.txt" >&2
  echo "provider probe diagnostics: $DIAGNOSTICS_TGZ" >&2
  exit "$GATE_STATUS"
fi

(
  cd "$REPORT_DIR"
  COPYFILE_DISABLE=1 tar -czf "$EVIDENCE_TGZ" provider-probe-summary.md provider-probe-gate.txt provider-probe-*.json
)

go run ./cmd/stackchan-gateway provider-probe-evidence-validate --archive "$EVIDENCE_TGZ"
go run ./cmd/stackchan-gateway provider-probe-evidence-summary --archive "$EVIDENCE_TGZ" \
  | tee "$REPORT_DIR/provider-probe-evidence-summary.md"

echo "provider probe evidence: $EVIDENCE_TGZ"
echo "provider probe promotion summary: $REPORT_DIR/provider-probe-evidence-summary.md"
`, configPath, profiles, requiredModalities, fallbackModality, sourceRef, sourceState, requiresASRFixture, manifest.RunDelayMS, manifest.Runs, manifest.TimeoutMS, manifest.Runs, manifest.TimeoutMS, manifest.Runs, manifest.GateMinSuccessPct)
}

func formatProviderProbePackagePowerShell(manifest providerProbePackageManifest) string {
	configPath := powerShellSingleQuote(manifest.ConfigPath)
	profiles := powerShellSingleQuote(manifest.Profiles)
	requiredModalities := powerShellSingleQuote(manifest.RequiredModalities)
	fallbackModality := powerShellSingleQuote(manifest.GateFallbackModality)
	sourceRef := powerShellSingleQuote(manifest.SourceRef)
	sourceState := powerShellSingleQuote(manifest.SourceState)
	requiresASRFixture := "$false"
	if manifest.RequiresASRFixture {
		requiresASRFixture = "$true"
	}
	return fmt.Sprintf(`# Provider probe runner for Windows PowerShell on 5080lab.
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($env:PROVIDER_ENV_FILE)) {
  throw 'set PROVIDER_ENV_FILE to the private provider env file path on this machine'
}

$ConfigPath = if ([string]::IsNullOrWhiteSpace($env:PROVIDER_PROBE_CONFIG)) { %s } else { $env:PROVIDER_PROBE_CONFIG }
$Profiles = %s
$RequiredModalities = %s
$FallbackModality = %s
$SourceRef = %s
$SourceState = %s
$RequiresAsrFixture = %s
$RunDelayMS = '%d'
$AsrOpusFixture = if ([string]::IsNullOrWhiteSpace($env:ASR_OPUS_FIXTURE)) { './var/fixtures/asr/spoken-opus.json' } else { $env:ASR_OPUS_FIXTURE }
$BaseReportDir = if ([string]::IsNullOrWhiteSpace($env:BASE_REPORT_DIR)) { './var/reports' } else { $env:BASE_REPORT_DIR }
$RunID = if ([string]::IsNullOrWhiteSpace($env:RUN_ID)) { (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ') } else { $env:RUN_ID }
$AsrFixtureArgs = @()

function Test-AsrFixturePathAllowedWithoutGit {
  param([string]$Path)
  if ([string]::IsNullOrWhiteSpace($Path)) {
    return $false
  }
  $Normalized = $Path.Replace('\', '/')
  if ($Normalized.Contains('/../') -or $Normalized.StartsWith('../') -or $Normalized.EndsWith('/..') -or $Normalized.Contains('/./')) {
    return $false
  }
  return (
    $Normalized.StartsWith('/var/lib/a21-air/fixtures/asr/') -or
    $Normalized.StartsWith('var/fixtures/asr/') -or
    $Normalized.StartsWith('./var/fixtures/asr/') -or
    $Normalized.StartsWith('server/var/fixtures/asr/') -or
    $Normalized.StartsWith('./server/var/fixtures/asr/')
  ) -and $Normalized.EndsWith('.json')
}

if ([string]::IsNullOrWhiteSpace($env:GOPROXY)) {
  $env:GOPROXY = 'https://goproxy.cn,direct'
}
if ([string]::IsNullOrWhiteSpace($env:GOSUMDB)) {
  $env:GOSUMDB = 'sum.golang.google.cn'
}

function Invoke-NativeChecked {
  param([scriptblock]$Command)
  & $Command
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
}

function Write-TextFileUTF8 {
  param(
    [string]$Path,
    [AllowNull()][object]$Value
  )
  if ($null -eq $Value) {
    $Text = ''
  } elseif ($Value -is [System.Array]) {
    $Text = (($Value | ForEach-Object { [string]$_ }) -join [Environment]::NewLine)
  } else {
    $Text = [string]$Value
  }
  if ($Text.Length -gt 0 -and -not $Text.EndsWith([Environment]::NewLine)) {
    $Text += [Environment]::NewLine
  }
  $Encoding = New-Object System.Text.UTF8Encoding -ArgumentList $false
  [System.IO.File]::WriteAllText($Path, $Text, $Encoding)
  [Console]::Out.Write($Text)
}

if ($RequiresAsrFixture) {
  if (-not (Test-Path -LiteralPath $AsrOpusFixture -PathType Leaf)) {
    throw "ASR fixture not found: $AsrOpusFixture. Capture one with: go run ./cmd/stackchan-gateway asr-fixture-capture --config ./configs/stackchan-gateway.example.yaml --listen 0.0.0.0:8080 --advertise-url ws://<reachable-host>:8080/xiaozhi/v1/ws --output ./var/fixtures/asr/spoken-opus.json"
  }
  & git check-ignore -q -- $AsrOpusFixture 2>$null
  if ($LASTEXITCODE -ne 0 -and -not (Test-AsrFixturePathAllowedWithoutGit -Path $AsrOpusFixture)) {
    throw "ASR fixture is not ignored by git: $AsrOpusFixture. Keep spoken ASR fixtures under server/var/fixtures/asr/ or /var/lib/a21-air/fixtures/asr/ before running provider probes."
  }
  Invoke-NativeChecked { go run ./cmd/stackchan-gateway asr-fixture-validate --fixture $AsrOpusFixture }
  $AsrFixtureArgs = @('--asr-opus-fixture', $AsrOpusFixture)
}

if ([System.IO.Path]::IsPathRooted($BaseReportDir)) {
  $BaseReportDirAbs = $BaseReportDir
} else {
  $BaseReportDirAbs = Join-Path (Get-Location) $BaseReportDir
}
$ReportDir = Join-Path $BaseReportDirAbs $RunID
$EvidenceTgz = Join-Path $BaseReportDirAbs "provider-probe-evidence-$RunID.tgz"
$DiagnosticsTgz = Join-Path $BaseReportDirAbs "provider-probe-diagnostics-$RunID.tgz"

New-Item -ItemType Directory -Force -Path $ReportDir | Out-Null
if (-not (Test-Path -LiteralPath $env:PROVIDER_ENV_FILE -PathType Leaf)) {
  throw "provider env file not found: $env:PROVIDER_ENV_FILE"
}

if ($env:PROVIDER_PROBE_SKIP_SELF_TEST -ne '1') {
  Invoke-NativeChecked { go test ./cmd/stackchan-gateway ./internal/providerprobe ./internal/providers }
}

$MatrixArgs = @(
  './cmd/stackchan-gateway', 'provider-probe-matrix',
  '--env-file', $env:PROVIDER_ENV_FILE,
  '--config', $ConfigPath,
  '--profiles', $Profiles,
  '--runs', '%d',
  '--timeout-ms', '%d',
  '--run-delay-ms', $RunDelayMS,
  '--output-dir', $ReportDir,
  '--allow-failed-profiles'
) + $AsrFixtureArgs
Invoke-NativeChecked { go run @MatrixArgs }

$ReportPaths = @(Get-ChildItem -LiteralPath $ReportDir -Filter 'provider-probe-*.json' | ForEach-Object { $_.FullName })
if ($ReportPaths.Count -eq 0) {
  throw "no provider-probe reports written under $ReportDir"
}

$SummaryText = & go run ./cmd/stackchan-gateway provider-probe-summary @ReportPaths
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-TextFileUTF8 -Path (Join-Path $ReportDir 'provider-probe-summary.md') -Value $SummaryText

$GateArgs = @(
  './cmd/stackchan-gateway', 'provider-probe-gate',
  '--min-runs', '%d',
  '--min-success-percent', '%d',
  '--require-profiles', $Profiles,
  '--require-modalities', $RequiredModalities,
  "--require-fallback-modality=$FallbackModality",
  '--source-ref', $SourceRef,
  '--source-state', $SourceState
) + $ReportPaths
$GatePath = Join-Path $ReportDir 'provider-probe-gate.txt'
$PreviousErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
try {
  $GateText = & go run @GateArgs 2>&1
  $GateStatus = $LASTEXITCODE
} finally {
  $ErrorActionPreference = $PreviousErrorActionPreference
}
Write-TextFileUTF8 -Path $GatePath -Value $GateText
if ($GateStatus -ne 0) {
  Push-Location $ReportDir
  try {
    Invoke-NativeChecked { tar -czf $DiagnosticsTgz provider-probe-summary.md provider-probe-gate.txt provider-probe-*.json }
  } finally {
    Pop-Location
  }
  Invoke-NativeChecked { go run ./cmd/stackchan-gateway provider-probe-diagnostics-validate --archive $DiagnosticsTgz }
  [Console]::Error.WriteLine("provider probe gate failed; diagnostic written to $GatePath")
  [Console]::Error.WriteLine("provider probe diagnostics: $DiagnosticsTgz")
  exit $GateStatus
}

Push-Location $ReportDir
try {
  Invoke-NativeChecked { tar -czf $EvidenceTgz provider-probe-summary.md provider-probe-gate.txt provider-probe-*.json }
} finally {
  Pop-Location
}

Invoke-NativeChecked { go run ./cmd/stackchan-gateway provider-probe-evidence-validate --archive $EvidenceTgz }
$PromotionText = & go run ./cmd/stackchan-gateway provider-probe-evidence-summary --archive $EvidenceTgz
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$PromotionPath = Join-Path $ReportDir 'provider-probe-evidence-summary.md'
Write-TextFileUTF8 -Path $PromotionPath -Value $PromotionText

Write-Host "provider probe evidence: $EvidenceTgz"
Write-Host "provider probe promotion summary: $PromotionPath"
`, configPath, profiles, requiredModalities, fallbackModality, sourceRef, sourceState, requiresASRFixture, manifest.RunDelayMS, manifest.Runs, manifest.TimeoutMS, manifest.Runs, manifest.GateMinSuccessPct)
}

func formatProviderProbePackageReadme(manifest providerProbePackageManifest) string {
	return fmt.Sprintf(`# Provider Probe Execution Package

This package is for running current Go provider probes on 5080lab or Aliyun ECS.

It does not contain secrets, provider env files, spoken Opus fixtures, raw audio, transcripts or generated text.

## Inputs

- Set PROVIDER_ENV_FILE to a private env file on the target machine.
- Set PROVIDER_PROBE_CONFIG when the gateway config is not %s on the target machine.
- Set ASR_OPUS_FIXTURE when the spoken fixture is not ./var/fixtures/asr/spoken-opus.json.
- Keep the fixture under server/var/fixtures/asr/ or the durable ECS path /var/lib/a21-air/fixtures/asr/ before running.
- Before physical StackChan capture, set STACKCHAN_MAIN_AUTH_TOKEN on the capture host to the token value. The gateway accepts device Authorization as either the raw token or Bearer <token>, matching xiaozhi-esp32's default Bearer prefix behavior. Device-Id and Client-Id headers must match the configured device_id stackchan-s3-main and client_id stackchan-s3-main-client. STACKCHAN_ADMIN_TOKEN is not required for capture because asr-fixture-capture does not start the admin listener.
- For new physical units, run device-provisioning-check before capture. It must report ready_for_capture=true with connected=true, hello=true, device_id_match=true and client_id_match=true. By default it prints only identity hashes and match booleans; raw Device-Id and Client-Id require explicit --show-device-identity for local pairing and must not be copied into reports.
- Capture spoken fixtures with asr-fixture-capture; it refuses to start when --output is neither ignored by git nor under /var/lib/a21-air/fixtures/asr/, prints a safe ready line with connect_url, device_id, client_id and auth_env, rejects short, tiny, low-diversity or repeated placeholder captures before writing, and prints only safe counts. auth_env is the environment variable name, not the token value. Never put the token in connect_url.
- If device auth fails during capture, asr-fixture-capture prints a safe auth-failed line with HTTP status, error code and header presence booleans only; it does not print Authorization, Device-Id or Client-Id header values.
- When asr-fixture-capture listens on 0.0.0.0 for physical StackChan capture, pass --advertise-url ws://<reachable-host>:<port>/<path> or wss://...; capture fails before serving when this URL is missing, and the advertised URL path must match the capture WebSocket path and must not include user info, query parameters or fragments.
- After capture, run go run ./cmd/stackchan-gateway asr-fixture-validate --fixture ./var/fixtures/asr/spoken-opus.json, or validate the durable ECS fixture under /var/lib/a21-air/fixtures/asr/. Then set ASR_OPUS_FIXTURE to that fixture path before running this package.
- When ASR is required, the runner fails before any provider call if the fixture is missing, neither git-ignored nor under /var/lib/a21-air/fixtures/asr/, or rejected by asr-fixture-validate.
- The runner executes a small Go self-test before provider calls by default; set PROVIDER_PROBE_SKIP_SELF_TEST=1 only for automation that has already run the same tests.

## Run On Linux / ECS

    chmod +x ./run-provider-probes.sh
    PROVIDER_ENV_FILE=/path/to/provider.env ./run-provider-probes.sh

## Run On Windows / 5080lab

    $env:PROVIDER_ENV_FILE = "C:\path\to\provider.env"
    powershell -ExecutionPolicy Bypass -File .\run-provider-probes.ps1

## Expected Gate

- Profiles: %s
- Config path: %s
- Runs per profile: %d
- Timeout per probe: %d ms
- Delay between runs: %d ms
- Required modalities: %s
- Gate minimum success: %d%%
- Fallback modality: %s
- Source ref: %s
- Source state: %s
- Requires ASR fixture: %t

The runner writes reports under BASE_REPORT_DIR/RUN_ID, creates a private evidence tarball under BASE_REPORT_DIR, validates that tarball with provider-probe-evidence-validate, and writes provider-probe-evidence-summary Markdown to provider-probe-evidence-summary.md for promotion. If the production gate fails, the runner creates provider-probe-diagnostics-$RUN_ID.tgz, validates it with provider-probe-diagnostics-validate, and exposes it for troubleshooting only; do not promote it as production evidence. Do not commit tarballs or raw report directories. Promote only validated summary/gate results into control docs.
`, manifest.ConfigPath, manifest.Profiles, manifest.ConfigPath, manifest.Runs, manifest.TimeoutMS, manifest.RunDelayMS, manifest.RequiredModalities, manifest.GateMinSuccessPct, manifest.GateFallbackModality, manifest.SourceRef, manifest.SourceState, manifest.RequiresASRFixture)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func powerShellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
