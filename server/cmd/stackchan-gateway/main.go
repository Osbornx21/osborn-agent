package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"stackchan-gateway/internal/app"
	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providerprobe"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "provider-probe" {
		return runProviderProbe(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-validate" {
		return runProviderProbeValidate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-matrix" {
		return runProviderProbeMatrix(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-summary" {
		return runProviderProbeSummary(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-gate" {
		return runProviderProbeGate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-package" {
		return runProviderProbePackage(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-evidence-validate" {
		return runProviderProbeEvidenceValidate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-evidence-summary" {
		return runProviderProbeEvidenceSummary(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "provider-probe-diagnostics-validate" {
		return runProviderProbeDiagnosticsValidate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "acceptance" {
		return runAcceptance(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "physical-acceptance-report" {
		return runPhysicalAcceptanceReport(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "physical-acceptance-metrics" {
		return runPhysicalAcceptanceMetrics(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "acoustic-devices" {
		return runAcousticDevices(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "acoustic-capture" {
		return runAcousticCapture(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "physical-led-retest" {
		return runPhysicalLEDRetest(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "physical-reconnect-retest" {
		return runPhysicalReconnectRetest(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "asr-fixture-capture" {
		return runASRFixtureCapture(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "device-provisioning-check" {
		return runDeviceProvisioningCheck(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "asr-fixture-validate" {
		return runASRFixtureValidate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "voice-profile-check" {
		return runVoiceProfileCheck(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "feishu-smoke" {
		return runFeishuSmoke(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "ecs-package" {
		return runECSPackage(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "ecs-package-validate" {
		return runECSPackageValidate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "ecs-preflight-dry-run" {
		return runECSPreflightDryRun(args[1:], stdout, stderr)
	}
	return runGateway(args, stdout, stderr)
}

func runGateway(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("stackchan-gateway", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	gateway, err := app.New(app.Options{
		ConfigPath: *configPath,
		Logger:     slog.New(slog.NewTextHandler(stdout, nil)),
	})
	if err != nil {
		fmt.Fprintf(stderr, "failed to create gateway: %v\n", err)
		return 1
	}

	if err := gateway.Run(ctx); err != nil {
		fmt.Fprintf(stderr, "gateway stopped with error: %v\n", err)
		return 1
	}
	return 0
}

func runProviderProbeValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reportPath := flags.String("report", "", "provider probe report JSON path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *reportPath == "" {
		fmt.Fprintln(stderr, "provider-probe-validate failed: --report is required")
		return 2
	}
	if err := providerprobe.ValidateReportFile(*reportPath); err != nil {
		fmt.Fprintf(stderr, "provider-probe-validate failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "provider-probe report OK: %s\n", *reportPath)
	return 0
}

func runProviderProbeSummary(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-summary", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	reportPaths := flags.Args()
	if len(reportPaths) == 0 {
		fmt.Fprintln(stderr, "provider-probe-summary failed: at least one report path is required")
		return 2
	}
	rows, err := providerprobe.LoadValidatedReportSummaries(reportPaths)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-summary failed: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, providerprobe.FormatReportSummaryMarkdown(rows))
	return 0
}

func runProviderProbe(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config")
	profile := flags.String("profile", "", "provider profile id")
	runs := flags.Int("runs", 1, "number of probe runs")
	outputDir := flags.String("output-dir", "./var/reports", "provider probe report output directory")
	timeoutMS := flags.Int("timeout-ms", 5000, "timeout per provider probe in milliseconds")
	runDelayMS := flags.Int("run-delay-ms", 0, "delay between probe runs in milliseconds; recorded in reports")
	text := flags.String("text", "Say hello in one short sentence.", "probe text; stored only as byte length in reports")
	asrOpusFixture := flags.String("asr-opus-fixture", "", "optional ASR xiaozhi Opus frame fixture JSON path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *profile == "" {
		fmt.Fprintln(stderr, "provider-probe failed: --profile is required")
		return 2
	}
	if *runs <= 0 {
		fmt.Fprintln(stderr, "provider-probe failed: --runs must be positive")
		return 2
	}
	if *timeoutMS <= 0 {
		fmt.Fprintln(stderr, "provider-probe failed: --timeout-ms must be positive")
		return 2
	}
	if *runDelayMS < 0 {
		fmt.Fprintln(stderr, "provider-probe failed: --run-delay-ms must not be negative")
		return 2
	}

	cfg, err := gatewayconfig.LoadFile(*configPath, gatewayconfig.OSLookupEnv)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe failed: %v\n", err)
		return 1
	}
	requiresSemanticASRFixture := providerProfileRequiresASRFixture(cfg, *profile)
	var asrOpusFrames [][]byte
	if strings.TrimSpace(*asrOpusFixture) == "" {
		if requiresSemanticASRFixture {
			fmt.Fprintf(stderr, "provider-probe failed: profile %s requires --asr-opus-fixture with real spoken xiaozhi Opus frames\n", *profile)
			return 2
		}
	} else {
		frames, err := providerprobe.LoadASROpusFixture(*asrOpusFixture)
		if err != nil {
			fmt.Fprintf(stderr, "provider-probe failed: %v\n", err)
			return 1
		}
		if requiresSemanticASRFixture {
			if _, err := providerprobe.ValidateASROpusFramesForSemanticProbe(frames); err != nil {
				fmt.Fprintf(stderr, "provider-probe failed: %v\n", err)
				return 1
			}
		}
		asrOpusFrames = frames
	}

	report, err := providerprobe.RunReport(context.Background(), providerprobe.ReportOptions{
		Config:        cfg,
		Registry:      providerprobe.NewRegistryFromEnv(gatewayconfig.OSLookupEnv),
		Profile:       *profile,
		Runs:          *runs,
		Text:          *text,
		ASROpusFrames: asrOpusFrames,
		Timeout:       time.Duration(*timeoutMS) * time.Millisecond,
		RunDelay:      time.Duration(*runDelayMS) * time.Millisecond,
	})
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe failed: %v\n", err)
		return 1
	}
	path, err := providerprobe.WriteReport(report, *outputDir, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe failed: %v\n", err)
		return 1
	}
	if err := providerprobe.ValidateReportFile(path); err != nil {
		fmt.Fprintf(stderr, "provider-probe failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	if report.Successes == 0 {
		fmt.Fprintf(stderr, "provider-probe completed with no successful probes; report: %s\n", path)
		return 1
	}
	return 0
}
