package providerprobe

import (
	"strings"
	"testing"
)

func TestEvaluateReportGatePassesRequiredProfilesModalitiesAndFallback(t *testing.T) {
	rows := []ReportSummaryRow{
		{
			Profile:         "stepfun-llm",
			ProviderID:      "stepfun-llm",
			Modality:        "llm",
			Runs:            20,
			Successes:       19,
			Failures:        1,
			FirstTokenP50MS: 100,
			FirstTokenP95MS: 200,
			TotalP50MS:      300,
			TotalP95MS:      400,
		},
		{
			Profile:         "deepseek-llm",
			ProviderID:      "deepseek-llm",
			Modality:        "llm",
			Runs:            20,
			Successes:       18,
			Failures:        2,
			FirstTokenP50MS: 120,
			FirstTokenP95MS: 240,
			TotalP50MS:      320,
			TotalP95MS:      440,
		},
		{
			Profile:              "dashscope-cosyvoice",
			ProviderID:           "dashscope-asr",
			Modality:             "asr",
			Runs:                 20,
			Successes:            20,
			FirstTranscriptP50MS: 200,
			FirstTranscriptP95MS: 500,
			TotalP50MS:           260,
			TotalP95MS:           560,
		},
		{
			Profile:         "dashscope-cosyvoice",
			ProviderID:      "dashscope-tts",
			Modality:        "tts",
			Runs:            20,
			Successes:       20,
			FirstAudioP50MS: 300,
			FirstAudioP95MS: 650,
			TotalP50MS:      900,
			TotalP95MS:      1200,
		},
	}

	result, err := EvaluateReportGate(rows, ReportGateOptions{
		MinRuns:            20,
		MinSuccessPct:      80,
		RequiredProfiles:   []string{"stepfun-llm,deepseek-llm,dashscope-cosyvoice"},
		RequiredModalities: []string{"asr,llm,tts"},
		FallbackModality:   "llm",
	})
	if err != nil {
		t.Fatalf("EvaluateReportGate() error = %v", err)
	}
	if result.Rows != 4 {
		t.Fatalf("rows = %d, want 4", result.Rows)
	}
	if len(result.Profiles) != 3 || len(result.Providers) != 4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestEvaluateReportGateFailsWhenFallbackProviderMissing(t *testing.T) {
	rows := []ReportSummaryRow{{
		Profile:         "stepfun-llm",
		ProviderID:      "stepfun-llm",
		Modality:        "llm",
		Runs:            20,
		Successes:       20,
		FirstTokenP50MS: 100,
		FirstTokenP95MS: 200,
	}}

	_, err := EvaluateReportGate(rows, ReportGateOptions{
		MinRuns:          20,
		MinSuccessPct:    80,
		FallbackModality: "llm",
	})

	if err == nil {
		t.Fatal("EvaluateReportGate() error = nil, want missing fallback failure")
	}
}

func TestEvaluateReportGateIncludesSafeErrorsWhenProviderHasNoSuccesses(t *testing.T) {
	rows := []ReportSummaryRow{{
		Profile:      "deepseek-llm",
		ProviderID:   "deepseek-llm",
		Modality:     "llm",
		Runs:         3,
		Failures:     3,
		ErrorClasses: []string{"provider_error:http_402:invalid_request_error"},
	}}

	_, err := EvaluateReportGate(rows, ReportGateOptions{MinRuns: 3, MinSuccessPct: 80})

	if err == nil {
		t.Fatal("EvaluateReportGate() error = nil, want no-success failure")
	}
	if !strings.Contains(err.Error(), "provider_error:http_402:invalid_request_error") {
		t.Fatalf("error = %q, want safe error detail", err.Error())
	}
}

func TestEvaluateReportGateFailsWhenLatencyMissing(t *testing.T) {
	rows := []ReportSummaryRow{{
		Profile:    "stepfun-llm",
		ProviderID: "stepfun-llm",
		Modality:   "llm",
		Runs:       20,
		Successes:  20,
	}}

	_, err := EvaluateReportGate(rows, ReportGateOptions{MinRuns: 20, MinSuccessPct: 80})

	if err == nil {
		t.Fatal("EvaluateReportGate() error = nil, want missing latency failure")
	}
}
