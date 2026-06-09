package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providerprobe"
)

const (
	defaultProviderProbeProfiles = "siliconflow-dashscope-voice,siliconflow-llm,moonshot-llm,stepfun-llm,doubao-llm,dashscope-cosyvoice"
	defaultProbeMainToken        = "provider-probe-main-token"
	defaultProbeAdminToken       = "provider-probe-admin-token"
)

func runProviderProbeMatrix(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-matrix", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config")
	profilesCSV := flags.String("profiles", defaultProviderProbeProfiles, "comma-separated provider profile ids")
	runs := flags.Int("runs", 20, "number of probe runs per profile")
	outputDir := flags.String("output-dir", "./var/reports", "provider probe report output directory")
	timeoutMS := flags.Int("timeout-ms", 5000, "timeout per provider probe in milliseconds")
	runDelayMS := flags.Int("run-delay-ms", 0, "delay between probe runs in milliseconds; recorded in reports")
	text := flags.String("text", "Say hello in one short sentence.", "probe text; stored only as byte length in reports")
	asrOpusFixture := flags.String("asr-opus-fixture", "", "optional ASR xiaozhi Opus frame fixture JSON path")
	envFile := flags.String("env-file", "", "optional shell env file with provider keys")
	allowFailedProfiles := flags.Bool("allow-failed-profiles", false, "return success after writing validated reports even when a profile has zero successful probes")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *runs <= 0 {
		fmt.Fprintln(stderr, "provider-probe-matrix failed: --runs must be positive")
		return 2
	}
	if *timeoutMS <= 0 {
		fmt.Fprintln(stderr, "provider-probe-matrix failed: --timeout-ms must be positive")
		return 2
	}
	if *runDelayMS < 0 {
		fmt.Fprintln(stderr, "provider-probe-matrix failed: --run-delay-ms must not be negative")
		return 2
	}
	profiles := parseProviderProbeProfiles(*profilesCSV)
	if len(profiles) == 0 {
		fmt.Fprintln(stderr, "provider-probe-matrix failed: --profiles must include at least one profile")
		return 2
	}

	lookup, err := buildProviderProbeMatrixLookup(*envFile, gatewayconfig.OSLookupEnv)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-matrix failed: %v\n", err)
		return 1
	}
	cfg, err := gatewayconfig.LoadFile(*configPath, lookup)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-matrix failed: %v\n", err)
		return 1
	}

	requiresSemanticASRFixture := false
	for _, profile := range profiles {
		if providerProfileRequiresASRFixture(cfg, profile) {
			requiresSemanticASRFixture = true
			break
		}
	}

	var asrOpusFrames [][]byte
	if strings.TrimSpace(*asrOpusFixture) != "" {
		frames, err := providerprobe.LoadASROpusFixture(*asrOpusFixture)
		if err != nil {
			fmt.Fprintf(stderr, "provider-probe-matrix failed: %v\n", err)
			return 1
		}
		if requiresSemanticASRFixture {
			if _, err := providerprobe.ValidateASROpusFramesForSemanticProbe(frames); err != nil {
				fmt.Fprintf(stderr, "provider-probe-matrix failed: %v\n", err)
				return 1
			}
		}
		asrOpusFrames = frames
	}

	registry := providerprobe.NewRegistryFromEnv(lookup)
	failed := false
	for _, profile := range profiles {
		if providerProfileRequiresASRFixture(cfg, profile) && len(asrOpusFrames) == 0 {
			fmt.Fprintf(stderr, "provider-probe-matrix failed: profile %s requires --asr-opus-fixture with real spoken xiaozhi Opus frames\n", profile)
			return 2
		}

		report, err := providerprobe.RunReport(context.Background(), providerprobe.ReportOptions{
			Config:        cfg,
			Registry:      registry,
			Profile:       profile,
			Runs:          *runs,
			Text:          *text,
			ASROpusFrames: asrOpusFrames,
			Timeout:       time.Duration(*timeoutMS) * time.Millisecond,
			RunDelay:      time.Duration(*runDelayMS) * time.Millisecond,
		})
		if err != nil {
			fmt.Fprintf(stderr, "provider-probe-matrix failed: profile %s: %v\n", profile, err)
			return 1
		}
		path, err := providerprobe.WriteReport(report, *outputDir, time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "provider-probe-matrix failed: profile %s: %v\n", profile, err)
			return 1
		}
		if err := providerprobe.ValidateReportFile(path); err != nil {
			fmt.Fprintf(stderr, "provider-probe-matrix failed: profile %s: %v\n", profile, err)
			return 1
		}
		fmt.Fprintf(stdout, "profile=%s report=%s successes=%d failures=%d\n", profile, path, report.Successes, report.Failures)
		if report.Successes == 0 {
			fmt.Fprintf(stderr, "provider-probe-matrix failed: profile %s completed with no successful probes; report: %s\n", profile, path)
			failed = true
		}
	}

	fmt.Fprintf(stdout, "validated_reports=%d\n", len(profiles))
	if failed && !*allowFailedProfiles {
		return 1
	}
	return 0
}

func parseProviderProbeProfiles(value string) []string {
	var profiles []string
	for _, part := range strings.Split(value, ",") {
		profile := strings.TrimSpace(part)
		if profile == "" {
			continue
		}
		profiles = append(profiles, profile)
	}
	return profiles
}

func providerProfileRequiresASRFixture(cfg *gatewayconfig.Config, profileID string) bool {
	if cfg == nil {
		return false
	}
	profile, ok := cfg.Providers.Profiles[profileID]
	if !ok {
		return false
	}
	asr := strings.TrimSpace(profile.ASR)
	return asr != "" && asr != "mock"
}

func buildProviderProbeMatrixLookup(envFile string, base gatewayconfig.LookupEnv) (gatewayconfig.LookupEnv, error) {
	if base == nil {
		base = gatewayconfig.OSLookupEnv
	}
	fileEnv := map[string]string{}
	if strings.TrimSpace(envFile) != "" {
		parsed, err := parseProviderProbeEnvFile(envFile)
		if err != nil {
			return nil, err
		}
		fileEnv = parsed
	}

	raw := func(name string) string {
		if value, ok := base(name); ok && strings.TrimSpace(value) != "" {
			return value
		}
		return fileEnv[name]
	}

	values := map[string]string{}
	setFirst := func(target string, names ...string) {
		if value := raw(target); strings.TrimSpace(value) != "" {
			values[target] = value
			return
		}
		for _, name := range names {
			if value := raw(name); strings.TrimSpace(value) != "" {
				values[target] = value
				return
			}
		}
	}

	setFirst("DASHSCOPE_API_KEY", "A21_LAB_DASHSCOPE_API_KEY", "A21_DASHSCOPE_API_KEY")
	setFirst("ARK_API_KEY", "A21_LAB_VOLCENGINE_ARK_API_KEY", "A21_LAB_ARK_API_KEY", "A21_LAB_VOLCENGINE_API_KEY")
	setFirst("DOUBAO_API_KEY", "A21_LAB_DOUBAO_API_KEY", "A21_LAB_DOUBAO_VOICE_API_KEY", "A21_LAB_VOLCENGINE_API_KEY")
	setFirst("MINIMAX_API_KEY", "A21_LAB_MINIMAX_API_KEY", "A21_MINIMAX_API_KEY")
	setFirst("STEPFUN_API_KEY", "A21_LAB_STEPFUN_API_KEY", "A21_STEPFUN_API_KEY")
	setFirst("SILICONFLOW_API_KEY", "A21_LAB_SILICONFLOW_API_KEY", "A21_SILICONFLOW_API_KEY")
	setFirst("MOONSHOT_API_KEY", "A21_LAB_MOONSHOT_API_KEY", "A21_MOONSHOT_API_KEY")
	setFirst("DEEPSEEK_API_KEY", "A21_LAB_DEEPSEEK_API_KEY", "A21_DEEPSEEK_API_KEY")
	setFirst("ANTHROPIC_API_KEY", "A21_LAB_ANTHROPIC_API_KEY", "A21_ANTHROPIC_API_KEY")
	setFirst("STACKCHAN_MAIN_AUTH_TOKEN")
	setFirst("STACKCHAN_ADMIN_TOKEN")
	if strings.TrimSpace(values["STACKCHAN_MAIN_AUTH_TOKEN"]) == "" {
		values["STACKCHAN_MAIN_AUTH_TOKEN"] = defaultProbeMainToken
	}
	if strings.TrimSpace(values["STACKCHAN_ADMIN_TOKEN"]) == "" {
		values["STACKCHAN_ADMIN_TOKEN"] = defaultProbeAdminToken
	}

	return func(name string) (string, bool) {
		if value, ok := values[name]; ok {
			return value, true
		}
		if value, ok := base(name); ok {
			return value, true
		}
		value, ok := fileEnv[name]
		return value, ok
	}, nil
}

func parseProviderProbeEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read provider env file: %w", err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("parse provider env file line %d: expected KEY=value", lineNumber)
		}
		key := strings.TrimSpace(parts[0])
		if !isProviderEnvName(key) {
			return nil, fmt.Errorf("parse provider env file line %d: invalid env name", lineNumber)
		}
		values[key] = trimProviderEnvValue(strings.TrimSpace(parts[1]))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read provider env file: %w", err)
	}
	return values, nil
}

func isProviderEnvName(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case index > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func trimProviderEnvValue(value string) string {
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}
