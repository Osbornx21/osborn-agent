package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"stackchan-gateway/internal/providerprobe"
)

func runProviderProbeGate(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	minRuns := flags.Int("min-runs", 1, "minimum runs required for every provider summary row")
	minSuccessPct := flags.Int("min-success-percent", 1, "minimum success percent required for every provider summary row")
	requiredProfiles := flags.String("require-profiles", "", "comma-separated profiles that must be present")
	requiredModalities := flags.String("require-modalities", "", "comma-separated modalities that must have successful summaries")
	fallbackModality := flags.String("require-fallback-modality", "", "modality that must have at least two successful providers")
	sourceRef := flags.String("source-ref", "unspecified", "safe source revision label for evidence provenance")
	sourceState := flags.String("source-state", "unspecified", "safe source state label for evidence provenance")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	reportPaths := flags.Args()
	if len(reportPaths) == 0 {
		fmt.Fprintln(stderr, "provider-probe-gate failed: at least one report path is required")
		return 2
	}
	effectiveMinRuns := *minRuns
	if effectiveMinRuns <= 0 {
		effectiveMinRuns = 1
	}
	effectiveMinSuccessPct := *minSuccessPct
	if effectiveMinSuccessPct <= 0 {
		effectiveMinSuccessPct = 1
	}
	requiredProfileList := parseProviderProbeProfiles(*requiredProfiles)
	requiredModalityList := parseProviderProbeProfiles(*requiredModalities)
	fallbackModalityValue := strings.TrimSpace(*fallbackModality)

	rows, err := providerprobe.LoadValidatedReportSummaries(reportPaths)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-gate failed: %v\n", err)
		return 1
	}
	result, err := providerprobe.EvaluateReportGate(rows, providerprobe.ReportGateOptions{
		MinRuns:            effectiveMinRuns,
		MinSuccessPct:      effectiveMinSuccessPct,
		RequiredProfiles:   requiredProfileList,
		RequiredModalities: requiredModalityList,
		FallbackModality:   fallbackModalityValue,
	})
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-gate failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "provider-probe gate OK: rows=%d profiles=%d providers=%d min_runs=%d min_success_percent=%d required_profiles=%s required_modalities=%s fallback_modality=%s source_ref=%s source_state=%s\n",
		result.Rows,
		len(result.Profiles),
		len(result.Providers),
		effectiveMinRuns,
		effectiveMinSuccessPct,
		strings.Join(requiredProfileList, ","),
		strings.Join(requiredModalityList, ","),
		fallbackModalityValue,
		safeGateToken(*sourceRef, "unspecified"),
		safeGateToken(*sourceState, "unspecified"),
	)
	return 0
}

func safeGateToken(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return strings.NewReplacer(" ", "_", "\t", "_", "\n", "_", "\r", "_", "=", "_").Replace(trimmed)
}
