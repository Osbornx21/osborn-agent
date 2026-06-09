package providers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"stackchan-gateway/internal/audio"
)

const (
	ProbeModalityASR = "asr"
	ProbeModalityLLM = "llm"
	ProbeModalityTTS = "tts"

	defaultProbeTimeout = 5 * time.Second
)

var (
	ErrUnsupportedProbeModality = errors.New("unsupported provider probe modality")
	ErrProbeNoFinalTranscript   = errors.New("provider asr probe produced no final transcript")
)

type ProbeRequest struct {
	ProviderID string
	Modality   string
	Text       string
	Voice      string
	OpusFrames [][]byte
	Timeout    time.Duration
	SessionID  string
	DeviceID   string
	Generation int64
}

type ProbeResult struct {
	ProviderID          string `json:"provider_id"`
	Modality            string `json:"modality"`
	OK                  bool   `json:"ok"`
	ProviderModelID     string `json:"provider_model_id,omitempty"`
	ProviderVoiceID     string `json:"provider_voice_id,omitempty"`
	FirstTranscriptMS   int64  `json:"first_transcript_ms,omitempty"`
	FirstTokenMS        int64  `json:"first_token_ms,omitempty"`
	FirstAudioMS        int64  `json:"first_audio_ms,omitempty"`
	TotalMS             int64  `json:"total_ms"`
	TranscriptTextBytes int    `json:"transcript_text_bytes,omitempty"`
	InputAudioFrames    int    `json:"input_audio_frames,omitempty"`
	InputAudioBytes     int    `json:"input_audio_bytes,omitempty"`
	OutputTextBytes     int    `json:"output_text_bytes,omitempty"`
	AudioFrames         int    `json:"audio_frames,omitempty"`
	AudioBytes          int    `json:"audio_bytes,omitempty"`
	ProviderError       string `json:"provider_error,omitempty"`
	ProviderHTTPStatus  int    `json:"provider_http_status,omitempty"`
	ProviderErrorCode   string `json:"provider_error_code,omitempty"`
	StartedAtUnixMS     int64  `json:"started_at_unix_ms"`
	FinishedAtUnixMS    int64  `json:"finished_at_unix_ms"`

	Text       string `json:"-"`
	RawPayload string `json:"-"`
}

type ProbeRunner struct {
	registry *Registry
}

func NewProbeRunner(registry *Registry) *ProbeRunner {
	return &ProbeRunner{registry: registry}
}

func (r *ProbeRunner) Probe(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	if r == nil || r.registry == nil {
		return ProbeResult{}, fmt.Errorf("provider probe registry is not configured")
	}

	timeout := request.Timeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	providerID := normalizeProviderName(request.ProviderID)
	modality := strings.ToLower(strings.TrimSpace(request.Modality))
	startedAt := time.Now()
	result := ProbeResult{
		ProviderID:      providerID,
		Modality:        modality,
		StartedAtUnixMS: startedAt.UnixMilli(),
	}

	finish := func() ProbeResult {
		finishedAt := time.Now()
		result.TotalMS = elapsedMillis(startedAt, finishedAt)
		result.FinishedAtUnixMS = finishedAt.UnixMilli()
		result.Text = ""
		result.RawPayload = ""
		return result
	}

	switch modality {
	case ProbeModalityASR:
		if err := r.probeASR(probeCtx, request, startedAt, &result); err != nil {
			applyProbeErrorMetadata(&result, err)
			return finish(), err
		}
	case ProbeModalityLLM:
		if err := r.probeLLM(probeCtx, request, startedAt, &result); err != nil {
			applyProbeErrorMetadata(&result, err)
			return finish(), err
		}
	case ProbeModalityTTS:
		if err := r.probeTTS(probeCtx, request, startedAt, &result); err != nil {
			applyProbeErrorMetadata(&result, err)
			return finish(), err
		}
	default:
		result.ProviderError = ErrUnsupportedProbeModality.Error()
		return finish(), fmt.Errorf("%w: %s", ErrUnsupportedProbeModality, modality)
	}

	result.OK = true
	return finish(), nil
}

func (r *ProbeRunner) probeASR(ctx context.Context, request ProbeRequest, startedAt time.Time, result *ProbeResult) error {
	provider, err := r.registry.ASRProvider(request.ProviderID)
	if err != nil {
		return err
	}
	result.ProviderModelID = providerModelID(provider)

	stream, err := provider.Start(ctx, ASRStartRequest{
		SessionID:  request.SessionID,
		DeviceID:   request.DeviceID,
		Generation: request.Generation,
		StartedAt:  startedAt,
	})
	if err != nil {
		return err
	}
	defer stream.Close()

	frames := request.OpusFrames
	if len(frames) == 0 {
		frames = defaultASRProbeOpusFrames()
	}
	for _, payload := range frames {
		frame := audio.NewOpusFrame(payload, audio.DefaultSampleRateHz, audio.DefaultFrameDurationMS, time.Now())
		if err := stream.AcceptOpus(frame); err != nil {
			return err
		}
		result.InputAudioFrames++
		result.InputAudioBytes += len(payload)
	}
	if err := stream.Finish(); err != nil {
		return err
	}

	for {
		select {
		case event, ok := <-stream.Events():
			if !ok {
				return ErrProbeNoFinalTranscript
			}
			if event.Text != "" && result.FirstTranscriptMS == 0 {
				result.FirstTranscriptMS = elapsedMillis(startedAt, time.Now())
			}
			if event.IsFinal {
				if event.Text == "" {
					return ErrProbeNoFinalTranscript
				}
				result.TranscriptTextBytes += len([]byte(event.Text))
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func applyProbeErrorMetadata(result *ProbeResult, err error) {
	result.ProviderError = classifyProviderProbeError(err)
	status, code := providerErrorMetadata(err)
	result.ProviderHTTPStatus = status
	result.ProviderErrorCode = sanitizeProviderErrorCode(code)
}

func providerErrorMetadata(err error) (int, string) {
	for current := err; current != nil; current = errors.Unwrap(current) {
		value := reflect.ValueOf(current)
		if !value.IsValid() {
			continue
		}
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				continue
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			continue
		}

		status := providerErrorStatus(value)
		code := providerErrorCode(value)
		if status != 0 || code != "" {
			return status, code
		}
	}
	return 0, ""
}

func providerErrorStatus(value reflect.Value) int {
	field := value.FieldByName("StatusCode")
	if !field.IsValid() || !field.CanInt() {
		return 0
	}
	status := int(field.Int())
	if status < 100 || status > 599 {
		return 0
	}
	return status
}

func providerErrorCode(value reflect.Value) string {
	field := value.FieldByName("Code")
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

func sanitizeProviderErrorCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	const maxProviderErrorCodeLen = 128
	var builder strings.Builder
	lastWasUnderscore := false
	for _, char := range code {
		if builder.Len() >= maxProviderErrorCodeLen {
			break
		}
		if isSafeProviderErrorCodeChar(char) {
			builder.WriteRune(char)
			lastWasUnderscore = false
			continue
		}
		if !lastWasUnderscore && builder.Len() < maxProviderErrorCodeLen {
			builder.WriteByte('_')
			lastWasUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func isSafeProviderErrorCodeChar(char rune) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9') ||
		char == '.' ||
		char == '_' ||
		char == ':' ||
		char == '-'
}

func (r *ProbeRunner) probeLLM(ctx context.Context, request ProbeRequest, startedAt time.Time, result *ProbeResult) error {
	provider, err := r.registry.LLMProvider(request.ProviderID)
	if err != nil {
		return err
	}
	result.ProviderModelID = providerModelID(provider)

	text := strings.TrimSpace(request.Text)
	if text == "" {
		text = "Say hello in one short sentence."
	}
	chunks, err := provider.Stream(ctx, LLMRequest{
		SessionID:  request.SessionID,
		DeviceID:   request.DeviceID,
		Generation: request.Generation,
		Text:       text,
		CreatedAt:  startedAt,
	})
	if err != nil {
		return err
	}

	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return nil
			}
			if chunk.Text != "" {
				if result.FirstTokenMS == 0 {
					result.FirstTokenMS = elapsedMillis(startedAt, time.Now())
				}
				result.OutputTextBytes += len([]byte(chunk.Text))
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (r *ProbeRunner) probeTTS(ctx context.Context, request ProbeRequest, startedAt time.Time, result *ProbeResult) error {
	provider, err := r.registry.TTSProvider(request.ProviderID)
	if err != nil {
		return err
	}
	result.ProviderModelID = providerModelID(provider)
	result.ProviderVoiceID = providerVoiceID(provider)

	text := strings.TrimSpace(request.Text)
	if text == "" {
		text = "你好，我准备好了。"
	}
	frames, err := provider.Stream(ctx, TTSRequest{
		SessionID:  request.SessionID,
		DeviceID:   request.DeviceID,
		Generation: request.Generation,
		Text:       text,
		Voice:      request.Voice,
		CreatedAt:  startedAt,
	})
	if err != nil {
		return err
	}

	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return nil
			}
			if result.FirstAudioMS == 0 {
				result.FirstAudioMS = elapsedMillis(startedAt, time.Now())
			}
			result.AudioFrames++
			result.AudioBytes += len(frame.Opus)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func defaultASRProbeOpusFrames() [][]byte {
	return [][]byte{
		{0xf8, 0xff, 0xfe},
	}
}

type modelIDProvider interface {
	ModelID() string
}

type voiceIDProvider interface {
	VoiceID() string
}

func providerModelID(provider any) string {
	withModelID, ok := provider.(modelIDProvider)
	if !ok {
		return ""
	}
	return withModelID.ModelID()
}

func providerVoiceID(provider any) string {
	withVoiceID, ok := provider.(voiceIDProvider)
	if !ok {
		return ""
	}
	return withVoiceID.VoiceID()
}

func elapsedMillis(startedAt time.Time, finishedAt time.Time) int64 {
	elapsed := finishedAt.Sub(startedAt)
	if elapsed <= 0 {
		return 0
	}
	millis := elapsed.Milliseconds()
	if millis <= 0 {
		return 1
	}
	return millis
}

func classifyProviderProbeError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrProviderNotFound):
		return "provider_not_found"
	case errors.Is(err, ErrProviderConfiguration):
		return "provider_config_error"
	case errors.Is(err, ErrUnsupportedProbeModality):
		return "unsupported_modality"
	case errors.Is(err, ErrProbeNoFinalTranscript):
		return "no_final_transcript"
	default:
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return "network_error"
		}
		return "provider_error"
	}
}
