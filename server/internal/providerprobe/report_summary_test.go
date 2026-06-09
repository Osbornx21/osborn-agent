package providerprobe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providers"
)

func TestFormatReportSummaryMarkdownUsesOnlySafeFields(t *testing.T) {
	cfg := &gatewayconfig.Config{
		Providers: gatewayconfig.ProvidersConfig{
			Profiles: map[string]gatewayconfig.ProviderProfileConfig{
				"mock-all": {
					ASR: "mock",
					LLM: "mock",
					TTS: "mock",
				},
			},
		},
	}
	registry := providers.NewRegistry(providers.MockConfig{
		ASRFinalDelayMS:      1,
		LLMFirstTokenDelayMS: 1,
		TTSFirstFrameDelayMS: 1,
		TTSFrameCount:        1,
	})
	report, err := RunReport(context.Background(), ReportOptions{
		Config:   cfg,
		Registry: registry,
		Profile:  "mock-all",
		Runs:     1,
		Text:     "do not echo this prompt",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("RunReport() error = %v", err)
	}
	report.Results = append(report.Results, RunResult{
		Run:        1,
		ProviderID: "network-provider",
		Modality:   providers.ProbeModalityLLM,
		Result: providers.ProbeResult{
			ProviderID:       "network-provider",
			Modality:         providers.ProbeModalityLLM,
			OK:               false,
			TotalMS:          1,
			StartedAtUnixMS:  report.StartedAtUnixMS,
			FinishedAtUnixMS: report.FinishedUnixMS,
			ProviderError:    "network_error",
		},
		ErrorClass: "network_error",
	})
	report.Failures++
	report.Summaries = append(report.Summaries, Summary{
		ProviderID: "network-provider",
		Modality:   providers.ProbeModalityLLM,
		Runs:       1,
		Successes:  0,
		Failures:   1,
	})
	report.Results = append(report.Results, RunResult{
		Run:        1,
		ProviderID: "provider-error",
		Modality:   providers.ProbeModalityLLM,
		Result: providers.ProbeResult{
			ProviderID:         "provider-error",
			Modality:           providers.ProbeModalityLLM,
			OK:                 false,
			TotalMS:            1,
			StartedAtUnixMS:    report.StartedAtUnixMS,
			FinishedAtUnixMS:   report.FinishedUnixMS,
			ProviderError:      "provider_error",
			ProviderHTTPStatus: 401,
			ProviderErrorCode:  "invalid_request_error",
		},
		ErrorClass: "provider_error",
	})
	report.Failures++
	report.Summaries = append(report.Summaries, Summary{
		ProviderID: "provider-error",
		Modality:   providers.ProbeModalityLLM,
		Runs:       1,
		Successes:  0,
		Failures:   1,
	})

	rows := SummarizeReport("provider-probe-test.json", report)
	markdown := FormatReportSummaryMarkdown(rows)

	for _, want := range []string{"mock-all", "mock", "asr", "llm", "tts", "network_error", "provider_error:http_401:invalid_request_error"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	for _, forbidden := range []string{"do not echo this prompt", "raw_payload", "prompt_text"} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("markdown leaked %q:\n%s", forbidden, markdown)
		}
	}
}

func TestLoadValidatedReportSummariesRejectsUnsafeReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsafe.json")
	if err := os.WriteFile(path, []byte(`{
		"profile": "unsafe",
		"runs": 1,
		"timeout_ms": 5000,
		"prompt_text_bytes": 12,
		"started_at_unix_ms": 1,
		"finished_at_unix_ms": 2,
		"successes": 1,
		"failures": 0,
		"results": [{
			"run": 1,
			"provider_id": "mock",
			"modality": "llm",
			"result": {
				"provider_id": "mock",
				"modality": "llm",
				"ok": true,
				"first_token_ms": 1,
				"total_ms": 2,
				"output_text_bytes": 4,
				"started_at_unix_ms": 1,
				"finished_at_unix_ms": 2,
				"text": "leaked"
			}
		}],
		"summaries": [{
			"provider_id": "mock",
			"modality": "llm",
			"runs": 1,
			"successes": 1,
			"failures": 0,
			"first_token_p50_ms": 1,
			"first_token_p95_ms": 1,
			"total_p50_ms": 2,
			"total_p95_ms": 2
		}]
	}`), 0o600); err != nil {
		t.Fatalf("write unsafe report: %v", err)
	}

	if _, err := LoadValidatedReportSummaries([]string{path}); err == nil {
		t.Fatal("LoadValidatedReportSummaries() error = nil, want unsafe report rejection")
	}
}
