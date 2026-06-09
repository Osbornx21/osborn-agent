package providers

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"stackchan-gateway/internal/audio"
)

func TestProbeRunnerMeasuresMockLLMWithoutReturningText(t *testing.T) {
	registry := NewRegistry(MockConfig{
		LLMFirstTokenDelayMS: 1,
	})
	runner := NewProbeRunner(registry)

	result, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "mock",
		Modality:   ProbeModalityLLM,
		Text:       "secret prompt should not be echoed",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	if !result.OK {
		t.Fatalf("OK = false, result = %+v", result)
	}
	if result.ProviderID != "mock" || result.Modality != ProbeModalityLLM {
		t.Fatalf("result identity = %+v", result)
	}
	if result.FirstTokenMS <= 0 || result.TotalMS < result.FirstTokenMS {
		t.Fatalf("latency fields = %+v", result)
	}
	if result.OutputTextBytes <= 0 {
		t.Fatalf("OutputTextBytes = %d, want positive", result.OutputTextBytes)
	}
	if result.Text != "" || result.RawPayload != "" {
		t.Fatalf("probe leaked text or raw payload: %+v", result)
	}
}

func TestProbeRunnerMeasuresMockTTSFirstAudio(t *testing.T) {
	registry := NewRegistry(MockConfig{
		TTSFirstFrameDelayMS: 1,
		TTSFrameCount:        2,
	})
	runner := NewProbeRunner(registry)

	result, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "mock",
		Modality:   ProbeModalityTTS,
		Text:       "hello",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	if !result.OK {
		t.Fatalf("OK = false, result = %+v", result)
	}
	if result.FirstAudioMS <= 0 || result.TotalMS < result.FirstAudioMS {
		t.Fatalf("latency fields = %+v", result)
	}
	if result.AudioFrames != 2 || result.AudioBytes <= 0 {
		t.Fatalf("audio fields = %+v", result)
	}
}

func TestProbeRunnerMeasuresMockASRFinal(t *testing.T) {
	registry := NewRegistry(MockConfig{
		ASRFinalDelayMS: 1,
	})
	runner := NewProbeRunner(registry)

	result, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "mock",
		Modality:   ProbeModalityASR,
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	if !result.OK {
		t.Fatalf("OK = false, result = %+v", result)
	}
	if result.FirstTranscriptMS <= 0 || result.TotalMS < result.FirstTranscriptMS {
		t.Fatalf("latency fields = %+v", result)
	}
	if result.InputAudioFrames <= 0 || result.InputAudioBytes <= 0 {
		t.Fatalf("input audio fields = %+v", result)
	}
	if result.TranscriptTextBytes <= 0 {
		t.Fatalf("TranscriptTextBytes = %d, want positive", result.TranscriptTextBytes)
	}
	if result.Text != "" || result.RawPayload != "" {
		t.Fatalf("probe leaked text or raw payload: %+v", result)
	}
}

func TestProbeRunnerReportsASRWithoutFinalTranscript(t *testing.T) {
	registry := NewRegistry(MockConfig{})
	registry.RegisterASR("empty-asr", func() (ASRProvider, error) {
		return emptyASRProvider{}, nil
	})
	runner := NewProbeRunner(registry)

	_, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "empty-asr",
		Modality:   ProbeModalityASR,
		Timeout:    time.Second,
	})
	if !errors.Is(err, ErrProbeNoFinalTranscript) {
		t.Fatalf("Probe() error = %v, want ErrProbeNoFinalTranscript", err)
	}
}

func TestProbeRunnerReportsUnknownProvider(t *testing.T) {
	runner := NewProbeRunner(NewRegistry(MockConfig{}))

	_, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "missing-llm",
		Modality:   ProbeModalityLLM,
		Timeout:    time.Second,
	})
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Probe() error = %v, want ErrProviderNotFound", err)
	}
}

func TestProbeRunnerReportsProviderConfigurationErrors(t *testing.T) {
	registry := NewRegistry(MockConfig{})
	registry.RegisterLLM("config-missing-llm", func() (LLMProvider, error) {
		return failingLLMProvider{err: ErrProviderConfiguration}, nil
	})
	runner := NewProbeRunner(registry)

	result, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "config-missing-llm",
		Modality:   ProbeModalityLLM,
		Timeout:    time.Second,
	})
	if err == nil {
		t.Fatal("Probe() error = nil, want configuration error")
	}

	if result.ProviderError != "provider_config_error" {
		t.Fatalf("ProviderError = %q, want provider_config_error", result.ProviderError)
	}
	if result.ProviderHTTPStatus != 0 || result.ProviderErrorCode != "" {
		t.Fatalf("provider config error carried HTTP metadata: %+v", result)
	}
}

func TestProbeRunnerReportsSafeProviderErrorMetadata(t *testing.T) {
	registry := NewRegistry(MockConfig{})
	registry.RegisterLLM("failing-llm", func() (LLMProvider, error) {
		return failingLLMProvider{
			err: &providerErrorMetadataTest{
				StatusCode: 401,
				Code:       "invalid request: api key\nbad",
				Message:    "private-message-must-not-enter-report",
			},
		}, nil
	})
	runner := NewProbeRunner(registry)

	result, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "failing-llm",
		Modality:   ProbeModalityLLM,
		Timeout:    time.Second,
	})
	if err == nil {
		t.Fatal("Probe() error = nil, want provider error")
	}

	if result.OK {
		t.Fatalf("OK = true, result = %+v", result)
	}
	if result.ProviderError != "provider_error" {
		t.Fatalf("ProviderError = %q, want provider_error", result.ProviderError)
	}
	if result.ProviderHTTPStatus != 401 {
		t.Fatalf("ProviderHTTPStatus = %d, want 401", result.ProviderHTTPStatus)
	}
	if result.ProviderErrorCode != "invalid_request:_api_key_bad" {
		t.Fatalf("ProviderErrorCode = %q", result.ProviderErrorCode)
	}
	if strings.Contains(result.ProviderErrorCode, " ") || strings.Contains(result.ProviderErrorCode, "\n") {
		t.Fatalf("ProviderErrorCode is not sanitized: %q", result.ProviderErrorCode)
	}
	if strings.Contains(result.ProviderErrorCode, "private-message") {
		t.Fatalf("ProviderErrorCode leaked message: %q", result.ProviderErrorCode)
	}
}

type failingLLMProvider struct {
	err error
}

func (p failingLLMProvider) Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	_ = ctx
	_ = req
	return nil, p.err
}

type providerErrorMetadataTest struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *providerErrorMetadataTest) Error() string {
	return e.Message
}

type emptyASRProvider struct{}

func (p emptyASRProvider) Start(ctx context.Context, req ASRStartRequest) (ASRStream, error) {
	_ = ctx
	_ = req
	return &emptyASRStream{events: make(chan ASREvent)}, nil
}

type emptyASRStream struct {
	events chan ASREvent
}

func (s *emptyASRStream) AcceptOpus(frame audio.Frame) error {
	_ = frame
	return nil
}

func (s *emptyASRStream) Finish() error {
	close(s.events)
	return nil
}

func (s *emptyASRStream) Events() <-chan ASREvent {
	return s.events
}

func (s *emptyASRStream) Close() error {
	return nil
}

func TestProbeRunnerRejectsUnsupportedModality(t *testing.T) {
	runner := NewProbeRunner(NewRegistry(MockConfig{}))

	_, err := runner.Probe(context.Background(), ProbeRequest{
		ProviderID: "mock",
		Modality:   "video",
		Timeout:    time.Second,
	})
	if !errors.Is(err, ErrUnsupportedProbeModality) {
		t.Fatalf("Probe() error = %v, want ErrUnsupportedProbeModality", err)
	}
}

func TestClassifyProviderProbeErrorDetectsNetworkErrors(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "https://api.example.invalid", Err: errors.New("tls handshake failed")}

	if got := classifyProviderProbeError(err); got != "network_error" {
		t.Fatalf("classifyProviderProbeError() = %q, want network_error", got)
	}
}
