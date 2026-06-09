package providerprobe

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providers"
)

func TestRunReportIncludesASRLLMAndTTSSummaries(t *testing.T) {
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
		Profile:  "mock-all",
		Registry: registry,
		Runs:     2,
		Timeout:  time.Second,
		Text:     "do not echo this prompt",
	})
	if err != nil {
		t.Fatalf("RunReport() error = %v", err)
	}

	if report.Successes != 6 || report.Failures != 0 {
		t.Fatalf("success/failure counts = %d/%d", report.Successes, report.Failures)
	}
	if len(report.Summaries) != 3 {
		t.Fatalf("summaries len = %d, want asr/llm/tts", len(report.Summaries))
	}

	asr := requireSummary(t, report.Summaries, "mock", providers.ProbeModalityASR)
	if asr.FirstTranscriptP50MS <= 0 || asr.FirstTranscriptP95MS <= 0 {
		t.Fatalf("asr summary = %+v", asr)
	}
}

func TestRunReportRecordsAndAppliesRunDelay(t *testing.T) {
	cfg := &gatewayconfig.Config{
		Providers: gatewayconfig.ProvidersConfig{
			Profiles: map[string]gatewayconfig.ProviderProfileConfig{
				"mock-llm": {
					LLM: "mock",
				},
			},
		},
	}

	started := time.Now()
	report, err := RunReport(context.Background(), ReportOptions{
		Config:   cfg,
		Profile:  "mock-llm",
		Runs:     2,
		Timeout:  time.Second,
		RunDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RunReport() error = %v", err)
	}

	if report.RunDelayMS != 20 {
		t.Fatalf("RunDelayMS = %d, want 20", report.RunDelayMS)
	}
	if elapsed := time.Since(started); elapsed < 15*time.Millisecond {
		t.Fatalf("RunReport() elapsed = %s, want evidence of run delay", elapsed)
	}
}

func TestNewRegistryFromEnvAcceptsA21LegacyVoiceKeysWithoutLegacyModelFallback(t *testing.T) {
	registry := NewRegistryFromEnv(func(name string) (string, bool) {
		values := map[string]string{
			"A21_LAB_DASHSCOPE_API_KEY":   "legacy-dashscope-key",
			"A21_LAB_SILICONFLOW_API_KEY": "legacy-siliconflow-key",
			"A21_SILICONFLOW_MODEL":       "legacy-siliconflow-model",
		}
		value, ok := values[name]
		return value, ok
	})

	asr, err := registry.ASRProvider("dashscope-asr")
	if err != nil {
		t.Fatalf("ASRProvider() error = %v", err)
	}
	if err := providers.ValidateProviderConfig(asr); err != nil {
		t.Fatalf("dashscope ASR ValidateProviderConfig() error = %v", err)
	}

	llm, err := registry.LLMProvider("siliconflow-llm")
	if err != nil {
		t.Fatalf("LLMProvider() error = %v", err)
	}
	if err := providers.ValidateProviderConfig(llm); err != nil {
		t.Fatalf("siliconflow LLM ValidateProviderConfig() error = %v", err)
	}
	modelProvider, ok := llm.(interface{ ModelID() string })
	if !ok {
		t.Fatal("siliconflow LLM does not expose ModelID()")
	}
	if got := modelProvider.ModelID(); got == "legacy-siliconflow-model" {
		t.Fatalf("siliconflow LLM adopted legacy model fallback %q; voice path model overrides must use current gateway env names", got)
	}

	tts, err := registry.TTSProvider("dashscope-tts")
	if err != nil {
		t.Fatalf("TTSProvider() error = %v", err)
	}
	if err := providers.ValidateProviderConfig(tts); err != nil {
		t.Fatalf("dashscope TTS ValidateProviderConfig() error = %v", err)
	}
}

func TestNewRegistryFromEnvAppliesDashScopeTTSTuning(t *testing.T) {
	registry := NewRegistryFromEnv(func(name string) (string, bool) {
		values := map[string]string{
			"DASHSCOPE_API_KEY":    "dashscope-key",
			"DASHSCOPE_TTS_VOLUME": "68",
			"DASHSCOPE_TTS_RATE":   "1.08",
			"DASHSCOPE_TTS_PITCH":  "0.96",
		}
		value, ok := values[name]
		return value, ok
	})

	tts, err := registry.TTSProvider("dashscope-tts")
	if err != nil {
		t.Fatalf("TTSProvider() error = %v", err)
	}
	tuned, ok := tts.(interface {
		VolumeLevel() int
		SpeechRate() float64
		SpeechPitch() float64
	})
	if !ok {
		t.Fatalf("dashscope TTS provider does not expose tuning accessors: %T", tts)
	}
	if tuned.VolumeLevel() != 68 || tuned.SpeechRate() != 1.08 || tuned.SpeechPitch() != 0.96 {
		t.Fatalf("tts tuning = volume %d rate %.2f pitch %.2f, want 68/1.08/0.96", tuned.VolumeLevel(), tuned.SpeechRate(), tuned.SpeechPitch())
	}
}

func TestOpusEncoderTuningFromEnvBoundsValues(t *testing.T) {
	tuning := opusEncoderTuningFromEnv(func(name string) (string, bool) {
		values := map[string]string{
			"A21_OPUS_DOWNLINK_BITRATE_BPS": "48000",
			"A21_OPUS_DOWNLINK_COMPLEXITY":  "8",
		}
		value, ok := values[name]
		return value, ok
	})

	if tuning.BitrateBPS != 48000 || tuning.Complexity != 8 {
		t.Fatalf("tuning = %+v, want bitrate 48000 complexity 8", tuning)
	}

	fallback := opusEncoderTuningFromEnv(func(name string) (string, bool) {
		values := map[string]string{
			"A21_OPUS_DOWNLINK_BITRATE_BPS": "4000",
			"A21_OPUS_DOWNLINK_COMPLEXITY":  "99",
		}
		value, ok := values[name]
		return value, ok
	})
	if fallback.BitrateBPS != 0 || fallback.Complexity != 0 {
		t.Fatalf("fallback tuning = %+v, want zero values for default tuning", fallback)
	}
}

func TestNewRegistryFromEnvAllowsDashScopeTTSVolumeZero(t *testing.T) {
	registry := NewRegistryFromEnv(func(name string) (string, bool) {
		values := map[string]string{
			"DASHSCOPE_API_KEY":    "dashscope-key",
			"DASHSCOPE_TTS_VOLUME": "0",
		}
		value, ok := values[name]
		return value, ok
	})

	tts, err := registry.TTSProvider("dashscope-tts")
	if err != nil {
		t.Fatalf("TTSProvider() error = %v", err)
	}
	tuned, ok := tts.(interface{ VolumeLevel() int })
	if !ok {
		t.Fatalf("dashscope TTS provider does not expose volume accessor: %T", tts)
	}
	if tuned.VolumeLevel() != 0 {
		t.Fatalf("tts volume = %d, want explicit env volume 0", tuned.VolumeLevel())
	}
}

func TestValidateReportAcceptsGeneratedReport(t *testing.T) {
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
		Profile:  "mock-all",
		Registry: registry,
		Runs:     2,
		Timeout:  time.Second,
		Text:     "do not echo this prompt",
	})
	if err != nil {
		t.Fatalf("RunReport() error = %v", err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if err := ValidateReportJSON(data); err != nil {
		t.Fatalf("ValidateReportJSON() error = %v", err)
	}
}

func TestValidateReportRejectsUnsafeFields(t *testing.T) {
	data := []byte(`{
		"profile": "dashscope-cosyvoice",
		"runs": 1,
		"timeout_ms": 5000,
		"prompt_text_bytes": 12,
		"started_at_unix_ms": 1,
		"finished_at_unix_ms": 2,
		"successes": 1,
		"failures": 0,
		"results": [{
			"run": 1,
			"provider_id": "dashscope-asr",
			"modality": "asr",
			"result": {
				"provider_id": "dashscope-asr",
				"modality": "asr",
				"ok": true,
				"first_transcript_ms": 10,
				"total_ms": 20,
				"transcript_text_bytes": 6,
				"input_audio_frames": 1,
				"input_audio_bytes": 3,
				"started_at_unix_ms": 1,
				"finished_at_unix_ms": 2,
				"raw_payload": "this must never be accepted"
			}
		}],
		"summaries": [{
			"provider_id": "dashscope-asr",
			"modality": "asr",
			"runs": 1,
			"successes": 1,
			"failures": 0,
			"first_transcript_p50_ms": 10,
			"first_transcript_p95_ms": 10,
			"total_p50_ms": 20,
			"total_p95_ms": 20
		}]
	}`)

	if err := ValidateReportJSON(data); err == nil {
		t.Fatal("ValidateReportJSON() error = nil, want unsafe field rejection")
	}
}

func TestValidateReportRejectsSecretLikeStrings(t *testing.T) {
	report := validValidationReport()
	report.Results[0].Result.ProviderError = "request failed with Authorization: Bearer " + "sk-" + strings.Repeat("1", 24)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if err := ValidateReportJSON(data); err == nil {
		t.Fatal("ValidateReportJSON() error = nil, want secret-like string rejection")
	}
}

func TestValidateReportAcceptsSafeProviderErrorMetadata(t *testing.T) {
	report := validValidationReport()
	report.Successes = 0
	report.Failures = 1
	report.Results[0].ErrorClass = "provider_error"
	report.Results[0].Result.OK = false
	report.Results[0].Result.ProviderError = "provider_error"
	report.Results[0].Result.ProviderHTTPStatus = 401
	report.Results[0].Result.ProviderErrorCode = "invalid_request_error"
	report.Summaries[0].Successes = 0
	report.Summaries[0].Failures = 1
	report.Summaries[0].FirstTranscriptP50MS = 0
	report.Summaries[0].FirstTranscriptP95MS = 0
	report.Summaries[0].TotalP50MS = 0
	report.Summaries[0].TotalP95MS = 0
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if err := ValidateReportJSON(data); err != nil {
		t.Fatalf("ValidateReportJSON() error = %v", err)
	}
}

func TestValidateReportAcceptsProviderConfigurationError(t *testing.T) {
	report := validValidationReport()
	report.Successes = 0
	report.Failures = 1
	report.Results[0].ErrorClass = "provider_config_error"
	report.Results[0].Result.OK = false
	report.Results[0].Result.ProviderError = "provider_config_error"
	report.Summaries[0].Successes = 0
	report.Summaries[0].Failures = 1
	report.Summaries[0].FirstTranscriptP50MS = 0
	report.Summaries[0].FirstTranscriptP95MS = 0
	report.Summaries[0].TotalP50MS = 0
	report.Summaries[0].TotalP95MS = 0
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if err := ValidateReportJSON(data); err != nil {
		t.Fatalf("ValidateReportJSON() error = %v", err)
	}
}

func TestValidateReportRejectsUnsafeProviderErrorMetadata(t *testing.T) {
	report := validValidationReport()
	report.Successes = 0
	report.Failures = 1
	report.Results[0].ErrorClass = "provider_error"
	report.Results[0].Result.OK = false
	report.Results[0].Result.ProviderError = "provider_error"
	report.Results[0].Result.ProviderHTTPStatus = 401
	report.Results[0].Result.ProviderErrorCode = "invalid request"
	report.Summaries[0].Successes = 0
	report.Summaries[0].Failures = 1
	report.Summaries[0].FirstTranscriptP50MS = 0
	report.Summaries[0].FirstTranscriptP95MS = 0
	report.Summaries[0].TotalP50MS = 0
	report.Summaries[0].TotalP95MS = 0
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if err := ValidateReportJSON(data); err == nil {
		t.Fatal("ValidateReportJSON() error = nil, want unsafe provider metadata rejection")
	}
}

func TestValidateReportRejectsInvalidProviderHTTPStatus(t *testing.T) {
	report := validValidationReport()
	report.Successes = 0
	report.Failures = 1
	report.Results[0].ErrorClass = "provider_error"
	report.Results[0].Result.OK = false
	report.Results[0].Result.ProviderError = "provider_error"
	report.Results[0].Result.ProviderHTTPStatus = 99
	report.Results[0].Result.ProviderErrorCode = "invalid_request_error"
	report.Summaries[0].Successes = 0
	report.Summaries[0].Failures = 1
	report.Summaries[0].FirstTranscriptP50MS = 0
	report.Summaries[0].FirstTranscriptP95MS = 0
	report.Summaries[0].TotalP50MS = 0
	report.Summaries[0].TotalP95MS = 0
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if err := ValidateReportJSON(data); err == nil {
		t.Fatal("ValidateReportJSON() error = nil, want invalid provider_http_status rejection")
	}
}

func TestValidateReportRejectsSuccessfulLLMWithoutFirstToken(t *testing.T) {
	report := validValidationReport()
	report.Results[0].Modality = providers.ProbeModalityLLM
	report.Results[0].Result.Modality = providers.ProbeModalityLLM
	report.Results[0].Result.FirstTranscriptMS = 0
	report.Results[0].Result.TranscriptTextBytes = 0
	report.Results[0].Result.InputAudioFrames = 0
	report.Results[0].Result.InputAudioBytes = 0
	report.Results[0].Result.FirstTokenMS = 0
	report.Results[0].Result.OutputTextBytes = 8
	report.Summaries[0].Modality = providers.ProbeModalityLLM
	report.Summaries[0].FirstTranscriptP50MS = 0
	report.Summaries[0].FirstTranscriptP95MS = 0
	report.Summaries[0].FirstTokenP50MS = 0
	report.Summaries[0].FirstTokenP95MS = 0
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if err := ValidateReportJSON(data); err == nil {
		t.Fatal("ValidateReportJSON() error = nil, want missing first token rejection")
	}
}

func TestSafeErrorClassDetectsNetworkErrors(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "https://api.example.invalid", Err: errors.New("tls handshake failed")}

	if got := safeErrorClass(err); got != "network_error" {
		t.Fatalf("safeErrorClass() = %q, want network_error", got)
	}
}

func TestSafeErrorClassDetectsProviderConfigurationErrors(t *testing.T) {
	if got := safeErrorClass(providers.ErrProviderConfiguration); got != "provider_config_error" {
		t.Fatalf("safeErrorClass() = %q, want provider_config_error", got)
	}
}

func TestSafeErrorClassDetectsASRWithoutFinalTranscript(t *testing.T) {
	if got := safeErrorClass(providers.ErrProbeNoFinalTranscript); got != "no_final_transcript" {
		t.Fatalf("safeErrorClass() = %q, want no_final_transcript", got)
	}
}

func requireSummary(t *testing.T, summaries []Summary, providerID string, modality string) Summary {
	t.Helper()

	for _, summary := range summaries {
		if summary.ProviderID == providerID && summary.Modality == modality {
			return summary
		}
	}
	t.Fatalf("missing summary provider=%s modality=%s in %+v", providerID, modality, summaries)
	return Summary{}
}

func validValidationReport() Report {
	return Report{
		Profile:         "dashscope-cosyvoice",
		Runs:            1,
		TimeoutMS:       5000,
		PromptTextBytes: 12,
		StartedAtUnixMS: 1,
		FinishedUnixMS:  2,
		Successes:       1,
		Failures:        0,
		Results: []RunResult{{
			Run:        1,
			ProviderID: "dashscope-asr",
			Modality:   providers.ProbeModalityASR,
			Result: providers.ProbeResult{
				ProviderID:          "dashscope-asr",
				Modality:            providers.ProbeModalityASR,
				OK:                  true,
				ProviderModelID:     "paraformer-realtime-v2",
				FirstTranscriptMS:   10,
				TotalMS:             20,
				TranscriptTextBytes: 6,
				InputAudioFrames:    1,
				InputAudioBytes:     3,
				StartedAtUnixMS:     1,
				FinishedAtUnixMS:    2,
			},
		}},
		Summaries: []Summary{{
			ProviderID:           "dashscope-asr",
			Modality:             providers.ProbeModalityASR,
			Runs:                 1,
			Successes:            1,
			Failures:             0,
			FirstTranscriptP50MS: 10,
			FirstTranscriptP95MS: 10,
			TotalP50MS:           20,
			TotalP95MS:           20,
		}},
	}
}
