package providerprobe

import (
	"fmt"
	"sort"
	"strings"
)

type ReportGateOptions struct {
	MinRuns            int
	MinSuccessPct      int
	RequiredProfiles   []string
	RequiredModalities []string
	FallbackModality   string
}

type ReportGateResult struct {
	Rows      int
	Profiles  []string
	Providers []string
}

func EvaluateReportGate(rows []ReportSummaryRow, options ReportGateOptions) (ReportGateResult, error) {
	if len(rows) == 0 {
		return ReportGateResult{}, fmt.Errorf("provider probe gate requires at least one report summary row")
	}

	minRuns := options.MinRuns
	if minRuns <= 0 {
		minRuns = 1
	}
	minSuccessPct := options.MinSuccessPct
	if minSuccessPct <= 0 {
		minSuccessPct = 1
	}
	if minSuccessPct > 100 {
		return ReportGateResult{}, fmt.Errorf("min success percent must be <= 100")
	}

	profiles := map[string]struct{}{}
	providers := map[string]struct{}{}
	successfulModalities := map[string]struct{}{}
	successfulProvidersByModality := map[string]map[string]struct{}{}

	for _, row := range rows {
		profile := strings.TrimSpace(row.Profile)
		providerID := strings.TrimSpace(row.ProviderID)
		modality := strings.TrimSpace(row.Modality)
		if profile != "" {
			profiles[profile] = struct{}{}
		}
		if providerID != "" {
			providers[providerID] = struct{}{}
		}
		if row.Runs < minRuns {
			return ReportGateResult{}, fmt.Errorf("provider probe gate failed: %s/%s in profile %s has %d runs, want at least %d", providerID, modality, profile, row.Runs, minRuns)
		}
		if row.Successes <= 0 {
			return ReportGateResult{}, fmt.Errorf("provider probe gate failed: %s/%s in profile %s has no successful probes%s", providerID, modality, profile, gateErrorDetails(row))
		}
		if row.Successes*100 < row.Runs*minSuccessPct {
			return ReportGateResult{}, fmt.Errorf("provider probe gate failed: %s/%s in profile %s success rate %d/%d is below %d%%%s", providerID, modality, profile, row.Successes, row.Runs, minSuccessPct, gateErrorDetails(row))
		}
		if !rowHasRequiredLatency(row) {
			return ReportGateResult{}, fmt.Errorf("provider probe gate failed: %s/%s in profile %s lacks required first latency summary", providerID, modality, profile)
		}
		successfulModalities[modality] = struct{}{}
		byProvider := successfulProvidersByModality[modality]
		if byProvider == nil {
			byProvider = map[string]struct{}{}
			successfulProvidersByModality[modality] = byProvider
		}
		byProvider[providerID] = struct{}{}
	}

	for _, profile := range normalizedList(options.RequiredProfiles) {
		if _, ok := profiles[profile]; !ok {
			return ReportGateResult{}, fmt.Errorf("provider probe gate failed: required profile %s is missing", profile)
		}
	}
	for _, modality := range normalizedList(options.RequiredModalities) {
		if _, ok := successfulModalities[modality]; !ok {
			return ReportGateResult{}, fmt.Errorf("provider probe gate failed: required modality %s has no successful summary", modality)
		}
	}
	if fallbackModality := strings.TrimSpace(options.FallbackModality); fallbackModality != "" {
		successfulProviders := successfulProvidersByModality[fallbackModality]
		if len(successfulProviders) < 2 {
			return ReportGateResult{}, fmt.Errorf("provider probe gate failed: fallback modality %s needs at least two successful providers, got %d", fallbackModality, len(successfulProviders))
		}
	}

	return ReportGateResult{
		Rows:      len(rows),
		Profiles:  sortedKeys(profiles),
		Providers: sortedKeys(providers),
	}, nil
}

func gateErrorDetails(row ReportSummaryRow) string {
	if len(row.ErrorClasses) == 0 {
		return ""
	}
	return "; errors=" + strings.Join(row.ErrorClasses, ",")
}

func rowHasRequiredLatency(row ReportSummaryRow) bool {
	switch row.Modality {
	case "asr":
		return row.FirstTranscriptP50MS > 0 && row.FirstTranscriptP95MS > 0
	case "llm":
		return row.FirstTokenP50MS > 0 && row.FirstTokenP95MS > 0
	case "tts":
		return row.FirstAudioP50MS > 0 && row.FirstAudioP95MS > 0
	default:
		return false
	}
}

func normalizedList(values []string) []string {
	var normalized []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				normalized = append(normalized, trimmed)
			}
		}
	}
	return normalized
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
