package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providerprobe"
)

type ecsPackageManifest struct {
	SchemaVersion     string   `json:"schema_version"`
	GeneratedAtUnixMS int64    `json:"generated_at_unix_ms"`
	ConfigPath        string   `json:"config_path"`
	DefaultProfile    string   `json:"default_profile"`
	ServiceUser       string   `json:"service_user"`
	InstallDir        string   `json:"install_dir"`
	ConfigDest        string   `json:"config_dest"`
	EnvFile           string   `json:"env_file"`
	RequiredEnvNames  []string `json:"required_env_names"`
	OptionalEnvNames  []string `json:"optional_env_names"`
	SourceRef         string   `json:"source_ref"`
	SourceState       string   `json:"source_state"`
}

type ecsPackageOptions struct {
	ConfigPath  string
	OutputDir   string
	ServiceUser string
	InstallDir  string
	ConfigDest  string
	EnvFile     string
}

var ecsEnvNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var ecsPackageForbiddenPattern = regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|xox[baprs]-[0-9A-Za-z-]{20,}|-----BEGIN (RSA |OPENSSH |EC )?PRIVATE KEY-----|Authorization: Bearer [A-Za-z0-9._~+/-]{16,}`)

func runECSPackage(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs-package", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config used to derive runtime env names")
	outputDir := flags.String("output-dir", "./var/ecs-packages/stackchan-gateway-runtime", "ECS runtime package output directory")
	serviceUser := flags.String("service-user", "stackchan", "Linux service user for systemd")
	installDir := flags.String("install-dir", "/opt/stackchan-gateway", "directory containing the stackchan-gateway binary and preflight script on ECS")
	configDest := flags.String("config-dest", "/etc/stackchan-gateway/stackchan-gateway.yaml", "gateway config path on ECS")
	envFile := flags.String("env-file", "/etc/stackchan-gateway/gateway.env", "root-readable environment file path on ECS")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	options := ecsPackageOptions{
		ConfigPath:  strings.TrimSpace(*configPath),
		OutputDir:   strings.TrimSpace(*outputDir),
		ServiceUser: strings.TrimSpace(*serviceUser),
		InstallDir:  strings.TrimRight(strings.TrimSpace(*installDir), "/"),
		ConfigDest:  strings.TrimSpace(*configDest),
		EnvFile:     strings.TrimSpace(*envFile),
	}
	if err := validateECSPackageOptions(options); err != nil {
		fmt.Fprintf(stderr, "ecs-package failed: %v\n", err)
		return 2
	}

	cfg, err := loadECSPackageConfig(options.ConfigPath)
	if err != nil {
		fmt.Fprintf(stderr, "ecs-package failed: %v\n", err)
		return 1
	}
	requiredEnvNames, optionalEnvNames, err := ecsPackageEnvNames(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "ecs-package failed: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(options.OutputDir, 0o700); err != nil {
		fmt.Fprintf(stderr, "ecs-package failed: %v\n", err)
		return 1
	}
	if err := os.Chmod(options.OutputDir, 0o700); err != nil {
		fmt.Fprintf(stderr, "ecs-package failed: chmod %s: %v\n", options.OutputDir, err)
		return 1
	}
	if err := ensureECSPackageDirClean(options.OutputDir); err != nil {
		fmt.Fprintf(stderr, "ecs-package failed: %v\n", err)
		return 1
	}

	sourceRef, sourceState := detectProviderProbePackageSource()
	manifest := ecsPackageManifest{
		SchemaVersion:     "stackchan_ecs_runtime_package_v1",
		GeneratedAtUnixMS: time.Now().UnixMilli(),
		ConfigPath:        options.ConfigPath,
		DefaultProfile:    strings.TrimSpace(cfg.Providers.DefaultProfile),
		ServiceUser:       options.ServiceUser,
		InstallDir:        options.InstallDir,
		ConfigDest:        options.ConfigDest,
		EnvFile:           options.EnvFile,
		RequiredEnvNames:  requiredEnvNames,
		OptionalEnvNames:  optionalEnvNames,
		SourceRef:         sourceRef,
		SourceState:       sourceState,
	}

	files := []struct {
		Name    string
		Content string
		Mode    os.FileMode
	}{
		{Name: "manifest.json", Content: formatECSPackageManifest(manifest), Mode: 0o600},
		{Name: "stackchan-gateway.service", Content: formatECSPackageSystemdUnit(manifest), Mode: 0o600},
		{Name: "preflight.sh", Content: formatECSPackagePreflight(manifest), Mode: 0o700},
		{Name: "gateway.env.example", Content: formatECSPackageEnvExample(manifest), Mode: 0o600},
		{Name: "README.md", Content: formatECSPackageReadme(manifest), Mode: 0o600},
	}
	for _, file := range files {
		if err := writeProviderProbePackageFile(filepath.Join(options.OutputDir, file.Name), file.Content, file.Mode); err != nil {
			fmt.Fprintf(stderr, "ecs-package failed: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "ecs runtime package: %s\n", options.OutputDir)
	return 0
}

func runECSPackageValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs-package-validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	packageDir := flags.String("package-dir", "./var/ecs-packages/stackchan-gateway-runtime", "ECS runtime package directory to validate")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	dir := strings.TrimSpace(*packageDir)
	if dir == "" {
		fmt.Fprintln(stderr, "ecs-package-validate failed: --package-dir is required")
		return 2
	}
	if err := validateECSPackageDir(dir); err != nil {
		fmt.Fprintf(stderr, "ecs-package-validate failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "ecs runtime package OK: %s\n", dir)
	return 0
}

func runECSPreflightDryRun(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs-preflight-dry-run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	packageDir := flags.String("package-dir", "./var/ecs-packages/stackchan-gateway-runtime", "validated ECS runtime package directory")
	configPath := flags.String("config", "", "runtime gateway config path; defaults to manifest config_dest")
	envFile := flags.String("env-file", "", "private runtime env file path; defaults to manifest env_file")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	dir := strings.TrimSpace(*packageDir)
	if dir == "" {
		fmt.Fprintln(stderr, "ecs-preflight-dry-run failed: --package-dir is required")
		return 2
	}
	if err := validateECSPackageDir(dir); err != nil {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: %v\n", err)
		return 1
	}
	manifest, err := readECSPackageManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: %v\n", err)
		return 1
	}

	runtimeConfigPath := strings.TrimSpace(*configPath)
	if runtimeConfigPath == "" {
		runtimeConfigPath = strings.TrimSpace(manifest.ConfigDest)
	}
	runtimeEnvFile := strings.TrimSpace(*envFile)
	if runtimeEnvFile == "" {
		runtimeEnvFile = strings.TrimSpace(manifest.EnvFile)
	}
	envValues, err := loadECSRuntimeEnvFile(runtimeEnvFile)
	if err != nil {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: %v\n", err)
		return 1
	}
	if err := requireECSRuntimeEnvNames(manifest.RequiredEnvNames, envValues); err != nil {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: %v\n", err)
		return 1
	}
	lookup := ecsRuntimeEnvLookup(envValues)
	cfg, err := gatewayconfig.LoadFile(runtimeConfigPath, lookup)
	if err != nil {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: %v\n", err)
		return 1
	}
	defaultProfile := strings.TrimSpace(cfg.Providers.DefaultProfile)
	if defaultProfile != strings.TrimSpace(manifest.DefaultProfile) {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: config default profile %q does not match package manifest default profile %q\n", defaultProfile, strings.TrimSpace(manifest.DefaultProfile))
		return 1
	}
	profile, ok := cfg.Providers.Profiles[defaultProfile]
	if !ok {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: profile %q not found\n", defaultProfile)
		return 1
	}
	result, err := inspectVoiceProfile(defaultProfile, profile, providerprobe.NewRegistryFromEnv(lookup))
	if err != nil {
		fmt.Fprintf(stderr, "ecs-preflight-dry-run failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "ecs preflight dry-run OK: package=%s config=%s env_file=%s profile=%s asr=%s asr_model=%s llm=%s llm_model=%s tts=%s tts_model=%s tts_voice=%s\n",
		dir,
		runtimeConfigPath,
		runtimeEnvFile,
		result.Profile,
		result.ASRProvider,
		result.ASRModel,
		result.LLMProvider,
		result.LLMModel,
		result.TTSProvider,
		result.TTSModel,
		result.TTSVoice,
	)
	return 0
}

func validateECSPackageOptions(options ecsPackageOptions) error {
	for label, value := range map[string]string{
		"--config":       options.ConfigPath,
		"--output-dir":   options.OutputDir,
		"--service-user": options.ServiceUser,
		"--install-dir":  options.InstallDir,
		"--config-dest":  options.ConfigDest,
		"--env-file":     options.EnvFile,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s must not contain newlines", label)
		}
	}
	if !strings.HasPrefix(options.InstallDir, "/") {
		return fmt.Errorf("--install-dir must be an absolute path")
	}
	if !strings.HasPrefix(options.ConfigDest, "/") {
		return fmt.Errorf("--config-dest must be an absolute path")
	}
	if !strings.HasPrefix(options.EnvFile, "/") {
		return fmt.Errorf("--env-file must be an absolute path")
	}
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`).MatchString(options.ServiceUser) {
		return fmt.Errorf("--service-user may contain only letters, digits, underscore, dot or dash")
	}
	return nil
}

func validateECSPackageDir(packageDir string) error {
	if err := validateECSPackageFileSet(packageDir); err != nil {
		return err
	}
	manifest, err := readECSPackageManifest(filepath.Join(packageDir, "manifest.json"))
	if err != nil {
		return err
	}
	if manifest.SchemaVersion != "stackchan_ecs_runtime_package_v1" {
		return fmt.Errorf("manifest schema_version %q is not supported", manifest.SchemaVersion)
	}
	if err := validateECSPackageOptions(ecsPackageOptions{
		ConfigPath:  valueOrPlaceholderPath(manifest.ConfigPath),
		OutputDir:   packageDir,
		ServiceUser: manifest.ServiceUser,
		InstallDir:  manifest.InstallDir,
		ConfigDest:  manifest.ConfigDest,
		EnvFile:     manifest.EnvFile,
	}); err != nil {
		return fmt.Errorf("manifest runtime options are invalid: %w", err)
	}
	if strings.TrimSpace(manifest.DefaultProfile) == "" {
		return fmt.Errorf("manifest default_profile is required")
	}
	if err := validateECSPackageEnvNameList("required_env_names", manifest.RequiredEnvNames); err != nil {
		return err
	}
	if err := validateECSPackageEnvNameList("optional_env_names", manifest.OptionalEnvNames); err != nil {
		return err
	}
	envTemplatePath := filepath.Join(packageDir, "gateway.env.example")
	envNames, err := validateECSPackageEnvTemplate(envTemplatePath)
	if err != nil {
		return err
	}
	for _, envName := range append([]string{}, manifest.RequiredEnvNames...) {
		if !envNames[envName] {
			return fmt.Errorf("gateway.env.example missing required env name %s", envName)
		}
	}
	for _, envName := range manifest.OptionalEnvNames {
		if !envNames[envName] {
			return fmt.Errorf("gateway.env.example missing optional env name %s", envName)
		}
	}
	if err := validateECSPackageSystemd(filepath.Join(packageDir, "stackchan-gateway.service"), manifest); err != nil {
		return err
	}
	if err := validateECSPackagePreflight(filepath.Join(packageDir, "preflight.sh"), manifest); err != nil {
		return err
	}
	for _, name := range ecsPackageRequiredFileNames() {
		if err := validateECSPackageContent(filepath.Join(packageDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func loadECSRuntimeEnvFile(path string) (map[string]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("--env-file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat env file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("env file must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("env file permissions must not be readable or writable by group/world")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read env file: %w", err)
	}
	values := make(map[string]string)
	for index, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "export ") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		}
		name, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return nil, fmt.Errorf("env file line %d must be NAME=value", index+1)
		}
		envName := strings.TrimSpace(name)
		if !ecsEnvNamePattern.MatchString(envName) {
			return nil, fmt.Errorf("env file line %d has unsafe env name %q", index+1, envName)
		}
		if _, exists := values[envName]; exists {
			return nil, fmt.Errorf("env file contains duplicate env name %s", envName)
		}
		parsedValue, err := parseECSRuntimeEnvValue(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("env file line %d has unsupported quoting for %s", index+1, envName)
		}
		values[envName] = parsedValue
	}
	return values, nil
}

func parseECSRuntimeEnvValue(value string) (string, error) {
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'"), nil
	}
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return parsed, nil
	}
	return value, nil
}

func requireECSRuntimeEnvNames(required []string, values map[string]string) error {
	for _, name := range required {
		envName := strings.TrimSpace(name)
		if envName == "" {
			continue
		}
		if value := strings.TrimSpace(values[envName]); value == "" {
			return fmt.Errorf("missing required env %s", envName)
		}
	}
	return nil
}

func ecsRuntimeEnvLookup(values map[string]string) gatewayconfig.LookupEnv {
	return func(name string) (string, bool) {
		value, ok := values[strings.TrimSpace(name)]
		return value, ok
	}
}

func valueOrPlaceholderPath(value string) string {
	if strings.TrimSpace(value) == "" {
		return "./placeholder.yaml"
	}
	return value
}

func validateECSPackageFileSet(packageDir string) error {
	allowed := make(map[string]struct{})
	for _, name := range ecsPackageRequiredFileNames() {
		allowed[name] = struct{}{}
	}
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", packageDir, err)
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return fmt.Errorf("output dir contains unexpected entry %q; ECS package files must be flat", entry.Name())
		}
		if _, ok := allowed[entry.Name()]; !ok {
			return fmt.Errorf("output dir contains unexpected entry %q; ECS package must contain only fixed safe files", entry.Name())
		}
		seen[entry.Name()] = struct{}{}
	}
	for _, name := range ecsPackageRequiredFileNames() {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("ECS package missing required file %s", name)
		}
	}
	return nil
}

func ecsPackageRequiredFileNames() []string {
	return []string{"README.md", "gateway.env.example", "manifest.json", "preflight.sh", "stackchan-gateway.service"}
}

func readECSPackageManifest(path string) (ecsPackageManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ecsPackageManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest ecsPackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ecsPackageManifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest, nil
}

func validateECSPackageEnvNameList(label string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		envName := strings.TrimSpace(value)
		if envName == "" {
			return fmt.Errorf("manifest %s contains an empty env name", label)
		}
		if !ecsEnvNamePattern.MatchString(envName) {
			return fmt.Errorf("manifest %s contains unsafe env name %q", label, envName)
		}
		if _, ok := seen[envName]; ok {
			return fmt.Errorf("manifest %s contains duplicate env name %s", label, envName)
		}
		seen[envName] = struct{}{}
	}
	return nil
}

func validateECSPackageEnvTemplate(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gateway.env.example: %w", err)
	}
	result := make(map[string]bool)
	for lineNumber, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return nil, fmt.Errorf("gateway.env.example line %d must be NAME= with an empty value", lineNumber+1)
		}
		envName := strings.TrimSpace(name)
		if !ecsEnvNamePattern.MatchString(envName) {
			return nil, fmt.Errorf("gateway.env.example line %d has unsafe env name %q", lineNumber+1, envName)
		}
		if strings.TrimSpace(value) != "" {
			return nil, fmt.Errorf("gateway.env.example must leave values empty for %s", envName)
		}
		result[envName] = true
	}
	return result, nil
}

func validateECSPackageSystemd(path string, manifest ecsPackageManifest) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read stackchan-gateway.service: %w", err)
	}
	unit := string(data)
	required := []string{
		"User=" + manifest.ServiceUser,
		"WorkingDirectory=" + manifest.InstallDir,
		"EnvironmentFile=" + manifest.EnvFile,
		"ExecStartPre=" + manifest.InstallDir + "/preflight.sh",
		"ExecStart=" + manifest.InstallDir + "/stackchan-gateway --config " + manifest.ConfigDest,
	}
	for _, want := range required {
		if !strings.Contains(unit, want) {
			return fmt.Errorf("stackchan-gateway.service missing manifest-aligned setting %q", want)
		}
	}
	return nil
}

func validateECSPackagePreflight(path string, manifest ecsPackageManifest) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat preflight.sh: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		return fmt.Errorf("preflight.sh must be executable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read preflight.sh: %w", err)
	}
	text := string(data)
	required := []string{
		`STACKCHAN_GATEWAY_BIN="${STACKCHAN_GATEWAY_BIN:-` + manifest.InstallDir + `/stackchan-gateway}"`,
		`CONFIG_PATH="${STACKCHAN_GATEWAY_CONFIG:-` + manifest.ConfigDest + `}"`,
		`ENV_FILE="${STACKCHAN_GATEWAY_ENV_FILE:-` + manifest.EnvFile + `}"`,
		`voice-profile-check --config "$CONFIG_PATH"`,
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			return fmt.Errorf("preflight.sh missing manifest-aligned check %q", want)
		}
	}
	return nil
}

func validateECSPackageContent(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	text := string(data)
	if ecsPackageForbiddenPattern.MatchString(text) {
		return fmt.Errorf("%s contains a forbidden secret-like pattern", filepath.Base(path))
	}
	for _, forbidden := range []string{"payload_base64", "transcript payload"} {
		if strings.Contains(text, forbidden) {
			return fmt.Errorf("%s contains forbidden package content marker %q", filepath.Base(path), forbidden)
		}
	}
	return nil
}

func loadECSPackageConfig(configPath string) (*gatewayconfig.Config, error) {
	cfg, err := gatewayconfig.LoadFile(configPath, placeholderECSPackageLookup)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func placeholderECSPackageLookup(name string) (string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if strings.Contains(upper, "BASE_URL") || strings.HasSuffix(upper, "_URL") || strings.HasSuffix(upper, "_WS_URL") {
		return "https://placeholder.invalid", true
	}
	return "placeholder-value", true
}

func ecsPackageEnvNames(cfg *gatewayconfig.Config) ([]string, []string, error) {
	required := make(map[string]struct{})
	optional := make(map[string]struct{})
	addRequired := func(name string) error {
		return addECSPackageEnvName(required, name)
	}
	addOptional := func(name string) error {
		return addECSPackageEnvName(optional, name)
	}

	if strings.TrimSpace(cfg.Server.AdminAddr) != "" {
		if err := addRequired(cfg.Server.AdminTokenEnv); err != nil {
			return nil, nil, err
		}
	}
	for _, device := range cfg.Devices {
		if err := addRequired(device.AuthTokenEnv); err != nil {
			return nil, nil, err
		}
	}

	defaultProfileID := strings.TrimSpace(cfg.Providers.DefaultProfile)
	defaultProfile, ok := cfg.Providers.Profiles[defaultProfileID]
	if !ok {
		return nil, nil, fmt.Errorf("providers.default_profile %q not found", defaultProfileID)
	}
	for _, providerID := range []string{defaultProfile.ASR, defaultProfile.LLM, defaultProfile.TTS} {
		envNames, optionalNames := ecsEnvNamesForProvider(providerID)
		for _, envName := range envNames {
			if err := addRequired(envName); err != nil {
				return nil, nil, err
			}
		}
		for _, envName := range optionalNames {
			if err := addOptional(envName); err != nil {
				return nil, nil, err
			}
		}
	}

	if cfg.Agent.V21.Enabled {
		if err := addRequired(cfg.Agent.V21.BaseURLEnv); err != nil {
			return nil, nil, err
		}
		if err := addRequired(cfg.Agent.V21.TokenEnv); err != nil {
			return nil, nil, err
		}
	}
	if cfg.Agent.OpenClaw.Enabled {
		if err := addRequired(cfg.Agent.OpenClaw.BaseURLEnv); err != nil {
			return nil, nil, err
		}
		if err := addRequired(cfg.Agent.OpenClaw.TokenEnv); err != nil {
			return nil, nil, err
		}
	}
	if cfg.Agent.Hermes.Enabled {
		if err := addRequired(cfg.Agent.Hermes.BaseURLEnv); err != nil {
			return nil, nil, err
		}
		if err := addRequired(cfg.Agent.Hermes.TokenEnv); err != nil {
			return nil, nil, err
		}
	}
	if cfg.Tools.HomeAssistant.Enabled {
		if err := addRequired(cfg.Tools.HomeAssistant.TokenEnv); err != nil {
			return nil, nil, err
		}
	}
	if cfg.Tools.Search.Enabled {
		if err := addRequired(cfg.Tools.Search.BaseURLEnv); err != nil {
			return nil, nil, err
		}
		if err := addRequired(cfg.Tools.Search.TokenEnv); err != nil {
			return nil, nil, err
		}
	}
	if cfg.Tools.Feishu.Enabled {
		if err := addRequired(cfg.Tools.Feishu.AppIDEnv); err != nil {
			return nil, nil, err
		}
		if err := addRequired(cfg.Tools.Feishu.AppSecretEnv); err != nil {
			return nil, nil, err
		}
		for _, target := range cfg.Tools.Feishu.AllowedTargets {
			if err := addRequired(target.ReceiveIDEnv); err != nil {
				return nil, nil, err
			}
		}
	}

	for envName := range required {
		delete(optional, envName)
	}
	return sortedEnvNames(required), sortedEnvNames(optional), nil
}

func addECSPackageEnvName(dst map[string]struct{}, name string) error {
	envName := strings.TrimSpace(name)
	if envName == "" {
		return nil
	}
	if !ecsEnvNamePattern.MatchString(envName) {
		return fmt.Errorf("unsafe env name %q in gateway config", envName)
	}
	dst[envName] = struct{}{}
	return nil
}

func sortedEnvNames(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func ecsEnvNamesForProvider(providerID string) ([]string, []string) {
	switch strings.TrimSpace(providerID) {
	case "", "mock":
		return nil, nil
	case "dashscope-asr":
		return []string{"DASHSCOPE_API_KEY"}, []string{"DASHSCOPE_ASR_ENDPOINT", "DASHSCOPE_ASR_MODEL", "DASHSCOPE_WORKSPACE_ID"}
	case "dashscope-llm":
		return []string{"DASHSCOPE_API_KEY"}, []string{"DASHSCOPE_LLM_BASE_URL", "DASHSCOPE_LLM_MODEL"}
	case "dashscope-tts":
		return []string{"DASHSCOPE_API_KEY"}, []string{"A21_OPUS_DOWNLINK_BITRATE_BPS", "A21_OPUS_DOWNLINK_COMPLEXITY", "DASHSCOPE_TTS_ENDPOINT", "DASHSCOPE_TTS_MODEL", "DASHSCOPE_TTS_VOICE", "DASHSCOPE_TTS_VOLUME", "DASHSCOPE_TTS_RATE", "DASHSCOPE_TTS_PITCH", "DASHSCOPE_WORKSPACE_ID"}
	case "siliconflow-llm":
		return []string{"SILICONFLOW_API_KEY"}, []string{"SILICONFLOW_LLM_BASE_URL", "SILICONFLOW_LLM_MODEL"}
	case "doubao-llm":
		return []string{"ARK_API_KEY"}, []string{"ARK_LLM_BASE_URL", "ARK_LLM_MODEL", "DOUBAO_LLM_BASE_URL", "DOUBAO_LLM_MODEL"}
	case "doubao-asr":
		return []string{"DOUBAO_API_KEY"}, []string{"DOUBAO_ASR_ENDPOINT", "DOUBAO_ASR_MODEL", "DOUBAO_ASR_RESOURCE_ID", "DOUBAO_VOICE_API_KEY"}
	case "doubao-tts":
		return []string{"DOUBAO_API_KEY"}, []string{"DOUBAO_TTS_ENDPOINT", "DOUBAO_TTS_MODEL", "DOUBAO_TTS_RESOURCE_ID", "DOUBAO_TTS_VOICE", "DOUBAO_VOICE_API_KEY"}
	case "minimax-llm":
		return []string{"MINIMAX_API_KEY"}, []string{"MINIMAX_LLM_BASE_URL", "MINIMAX_LLM_MODEL"}
	case "minimax-tts-ws":
		return []string{"MINIMAX_API_KEY"}, []string{"MINIMAX_TTS_ENDPOINT", "MINIMAX_TTS_MODEL", "MINIMAX_TTS_VOICE"}
	case "stepfun-llm":
		return []string{"STEPFUN_API_KEY"}, []string{"STEPFUN_LLM_BASE_URL", "STEPFUN_LLM_MODEL", "STEP_API_KEY", "STEP_LLM_BASE_URL"}
	case "moonshot-llm":
		return []string{"MOONSHOT_API_KEY"}, []string{"MOONSHOT_LLM_BASE_URL", "MOONSHOT_LLM_MODEL"}
	case "deepseek-llm":
		return []string{"DEEPSEEK_API_KEY"}, []string{"DEEPSEEK_LLM_BASE_URL", "DEEPSEEK_LLM_MODEL"}
	case "anthropic-llm":
		return []string{"ANTHROPIC_API_KEY"}, []string{"ANTHROPIC_LLM_BASE_URL", "ANTHROPIC_LLM_MODEL"}
	default:
		return nil, nil
	}
}

func ensureECSPackageDirClean(outputDir string) error {
	allowed := map[string]bool{
		"README.md":                 true,
		"gateway.env.example":       true,
		"manifest.json":             true,
		"preflight.sh":              true,
		"stackchan-gateway.service": true,
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

func formatECSPackageManifest(manifest ecsPackageManifest) string {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data) + "\n"
}

func formatECSPackageSystemdUnit(manifest ecsPackageManifest) string {
	return fmt.Sprintf(`[Unit]
Description=StackChan Xiaozhi Gateway
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
EnvironmentFile=%s
ExecStartPre=%s/preflight.sh
ExecStart=%s/stackchan-gateway --config %s
Restart=always
RestartSec=2
KillSignal=SIGTERM
TimeoutStopSec=10
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full

[Install]
WantedBy=multi-user.target
`,
		manifest.ServiceUser,
		manifest.InstallDir,
		manifest.EnvFile,
		manifest.InstallDir,
		manifest.InstallDir,
		manifest.ConfigDest,
	)
}

func formatECSPackagePreflight(manifest ecsPackageManifest) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

STACKCHAN_GATEWAY_BIN="${STACKCHAN_GATEWAY_BIN:-%s/stackchan-gateway}"
CONFIG_PATH="${STACKCHAN_GATEWAY_CONFIG:-%s}"
ENV_FILE="${STACKCHAN_GATEWAY_ENV_FILE:-%s}"

if [[ ! -x "$STACKCHAN_GATEWAY_BIN" ]]; then
  echo "stackchan gateway binary is not executable: $STACKCHAN_GATEWAY_BIN" >&2
  exit 2
fi
if [[ ! -r "$CONFIG_PATH" ]]; then
  echo "stackchan gateway config is not readable: $CONFIG_PATH" >&2
  exit 2
fi
if [[ ! -r "$ENV_FILE" ]]; then
  echo "stackchan gateway env file is not readable: $ENV_FILE" >&2
  exit 2
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

"$STACKCHAN_GATEWAY_BIN" voice-profile-check --config "$CONFIG_PATH"
echo "stackchan gateway ecs preflight OK: config=$CONFIG_PATH"
`,
		shellDoubleQuoteContent(manifest.InstallDir),
		shellDoubleQuoteContent(manifest.ConfigDest),
		shellDoubleQuoteContent(manifest.EnvFile),
	)
}

func shellDoubleQuoteContent(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "$", `\$`)
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	return escaped
}

func formatECSPackageEnvExample(manifest ecsPackageManifest) string {
	var builder strings.Builder
	builder.WriteString("# StackChan gateway ECS env template.\n")
	builder.WriteString("# Fill this file on ECS and install it as root-readable only. Do not commit real values.\n\n")
	builder.WriteString("# Required for the configured gateway and default voice profile.\n")
	for _, envName := range manifest.RequiredEnvNames {
		builder.WriteString(envName)
		builder.WriteString("=\n")
	}
	if len(manifest.OptionalEnvNames) > 0 {
		builder.WriteString("\n# Optional model overrides for measured voice profiles.\n")
		for _, envName := range manifest.OptionalEnvNames {
			builder.WriteString(envName)
			builder.WriteString("=\n")
		}
	}
	return builder.String()
}

func formatECSPackageReadme(manifest ecsPackageManifest) string {
	return fmt.Sprintf(
		"# StackChan Gateway ECS Runtime Package\n\n"+
			"This package prepares a repeatable Aliyun ECS runtime wrapper for the gateway. It does not include secrets, provider env files, the production config, audio fixtures, transcripts, generated text or the gateway binary.\n\n"+
			"This is not production provider selection. Provider selection still requires the spoken-fixture ASR/LLM/TTS evidence gates. The server is cloud-only for production; serial is dev/admin/recovery only.\n\n"+
			"## Files\n\n"+
			"- `stackchan-gateway.service`: systemd unit for the cloud gateway.\n"+
			"- `preflight.sh`: safe startup preflight that sources the private env file and runs `voice-profile-check`.\n"+
			"- `gateway.env.example`: env-name template only.\n"+
			"- `manifest.json`: safe provenance and runtime paths.\n\n"+
			"## Install On ECS\n\n"+
			"```bash\n"+
			"sudo install -d -m 755 %s\n"+
			"sudo install -d -m 700 /etc/stackchan-gateway\n"+
			"sudo install -d -m 700 /var/lib/stackchan-gateway /var/log/stackchan-gateway\n"+
			"sudo install -m 755 ./stackchan-gateway %s/stackchan-gateway\n"+
			"sudo install -m 700 preflight.sh %s/preflight.sh\n"+
			"sudo install -m 600 stackchan-gateway.service /etc/systemd/system/stackchan-gateway.service\n"+
			"sudo install -m 600 gateway.env.example %s\n"+
			"sudo install -m 600 ./stackchan-gateway.yaml %s\n"+
			"sudo systemctl daemon-reload\n"+
			"sudo %s/stackchan-gateway ecs-preflight-dry-run --package-dir . --config %s --env-file %s\n"+
			"sudo %s/preflight.sh\n"+
			"sudo systemctl enable --now stackchan-gateway\n"+
			"```\n\n"+
			"Edit `%s` before starting the service. Keep it root-readable only and place real `STACKCHAN_MAIN_AUTH_TOKEN`, `STACKCHAN_ADMIN_TOKEN`, DashScope and SiliconFlow values there. The dry-run validates the package, private env file, runtime config and default voice profile without starting systemd or running provider network probes.\n\n"+
			"## Checks\n\n"+
			"```bash\n"+
			"sudo systemctl status stackchan-gateway\n"+
			"curl -fsS http://127.0.0.1:8080/readyz\n"+
			"```\n\n"+
			"Source ref: `%s`\n\n"+
			"Source state: `%s`\n\n"+
			"Default profile: `%s`\n",
		manifest.InstallDir,
		manifest.InstallDir,
		manifest.InstallDir,
		manifest.EnvFile,
		manifest.ConfigDest,
		manifest.InstallDir,
		manifest.ConfigDest,
		manifest.EnvFile,
		manifest.InstallDir,
		manifest.EnvFile,
		manifest.SourceRef,
		manifest.SourceState,
		manifest.DefaultProfile,
	)
}
