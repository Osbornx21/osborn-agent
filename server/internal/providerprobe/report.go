package providerprobe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"stackchan-gateway/internal/audio"
	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/providers/anthropic"
	"stackchan-gateway/internal/providers/dashscope"
	"stackchan-gateway/internal/providers/deepseek"
	"stackchan-gateway/internal/providers/doubao"
	"stackchan-gateway/internal/providers/minimax"
	"stackchan-gateway/internal/providers/moonshot"
	"stackchan-gateway/internal/providers/siliconflow"
	"stackchan-gateway/internal/providers/stepfun"
)

var (
	ErrProfileNotFound   = errors.New("profile not found")
	ErrNoSupportedProbes = errors.New("profile has no supported asr, llm or tts providers")
)

type ReportOptions struct {
	Config        *gatewayconfig.Config
	Registry      *providers.Registry
	Profile       string
	Runs          int
	Text          string
	ASROpusFrames [][]byte
	Timeout       time.Duration
	RunDelay      time.Duration
	StartedAt     time.Time
}

type Report struct {
	Profile         string         `json:"profile"`
	Runs            int            `json:"runs"`
	TimeoutMS       int64          `json:"timeout_ms"`
	RunDelayMS      int64          `json:"run_delay_ms,omitempty"`
	PromptTextBytes int            `json:"prompt_text_bytes"`
	StartedAtUnixMS int64          `json:"started_at_unix_ms"`
	FinishedUnixMS  int64          `json:"finished_at_unix_ms"`
	Results         []RunResult    `json:"results"`
	Summaries       []Summary      `json:"summaries"`
	Successes       int            `json:"successes"`
	Failures        int            `json:"failures"`
	Skipped         []SkippedProbe `json:"skipped,omitempty"`
}

type RunResult struct {
	Run        int                   `json:"run"`
	ProviderID string                `json:"provider_id"`
	Modality   string                `json:"modality"`
	Result     providers.ProbeResult `json:"result"`
	ErrorClass string                `json:"error_class,omitempty"`
}

type Summary struct {
	ProviderID           string `json:"provider_id"`
	Modality             string `json:"modality"`
	Runs                 int    `json:"runs"`
	Successes            int    `json:"successes"`
	Failures             int    `json:"failures"`
	FirstTranscriptP50MS int64  `json:"first_transcript_p50_ms,omitempty"`
	FirstTranscriptP95MS int64  `json:"first_transcript_p95_ms,omitempty"`
	FirstTokenP50MS      int64  `json:"first_token_p50_ms,omitempty"`
	FirstTokenP95MS      int64  `json:"first_token_p95_ms,omitempty"`
	FirstAudioP50MS      int64  `json:"first_audio_p50_ms,omitempty"`
	FirstAudioP95MS      int64  `json:"first_audio_p95_ms,omitempty"`
	TotalP50MS           int64  `json:"total_p50_ms,omitempty"`
	TotalP95MS           int64  `json:"total_p95_ms,omitempty"`
}

type SkippedProbe struct {
	ProviderID string `json:"provider_id"`
	Modality   string `json:"modality"`
	Reason     string `json:"reason"`
}

type probeTarget struct {
	providerID string
	modality   string
}

func RunReport(ctx context.Context, options ReportOptions) (Report, error) {
	if options.Config == nil {
		return Report{}, fmt.Errorf("config is required")
	}
	registry := options.Registry
	if registry == nil {
		registry = providers.NewRegistry(providers.MockConfig{})
	}

	profileID := strings.TrimSpace(options.Profile)
	profile, ok := options.Config.Providers.Profiles[profileID]
	if !ok {
		return Report{}, fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
	}

	targets := probeTargetsForProfile(profile)
	if len(targets) == 0 {
		return Report{}, ErrNoSupportedProbes
	}

	runs := options.Runs
	if runs <= 0 {
		runs = 1
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	runDelay := options.RunDelay
	if runDelay < 0 {
		return Report{}, fmt.Errorf("run delay must not be negative")
	}
	startedAt := options.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	text := strings.TrimSpace(options.Text)
	if text == "" {
		text = "Say hello in one short sentence."
	}

	runner := providers.NewProbeRunner(registry)
	report := Report{
		Profile:         profileID,
		Runs:            runs,
		TimeoutMS:       timeout.Milliseconds(),
		RunDelayMS:      runDelay.Milliseconds(),
		PromptTextBytes: len([]byte(text)),
		StartedAtUnixMS: startedAt.UnixMilli(),
	}

	for runIndex := 1; runIndex <= runs; runIndex++ {
		for _, target := range targets {
			result, err := runner.Probe(ctx, providers.ProbeRequest{
				ProviderID: target.providerID,
				Modality:   target.modality,
				Text:       text,
				OpusFrames: options.ASROpusFrames,
				Timeout:    timeout,
				Generation: int64(runIndex),
			})
			result.Text = ""
			result.RawPayload = ""

			runResult := RunResult{
				Run:        runIndex,
				ProviderID: target.providerID,
				Modality:   target.modality,
				Result:     result,
			}
			if err != nil {
				runResult.ErrorClass = safeErrorClass(err)
				report.Failures++
			} else {
				report.Successes++
			}
			report.Results = append(report.Results, runResult)
		}
		if runDelay > 0 && runIndex < runs {
			select {
			case <-ctx.Done():
				return Report{}, ctx.Err()
			case <-time.After(runDelay):
			}
		}
	}

	report.Summaries = summarize(report.Results)
	report.FinishedUnixMS = time.Now().UnixMilli()
	return report, nil
}

func WriteReport(report Report, outputDir string, now time.Time) (string, error) {
	if strings.TrimSpace(outputDir) == "" {
		outputDir = "./var/reports"
	}
	if now.IsZero() {
		now = time.Now()
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", fmt.Errorf("create provider probe output dir: %w", err)
	}

	path := filepath.Join(outputDir, fmt.Sprintf("provider-probe-%s.json", now.Format("20060102-150405")))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		path = filepath.Join(outputDir, fmt.Sprintf("provider-probe-%s-%09d.json", now.Format("20060102-150405"), now.Nanosecond()))
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	if err != nil {
		return "", fmt.Errorf("create provider probe report: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return "", fmt.Errorf("write provider probe report: %w", err)
	}
	return path, nil
}

func NewRegistryFromEnv(lookup gatewayconfig.LookupEnv) *providers.Registry {
	return NewRegistryFromEnvWithMockConfig(lookup, providers.MockConfig{})
}

func NewRegistryFromEnvWithMockConfig(lookup gatewayconfig.LookupEnv, mockConfig providers.MockConfig) *providers.Registry {
	if lookup == nil {
		lookup = gatewayconfig.OSLookupEnv
	}
	registry := providers.NewRegistry(mockConfig)

	dashscope.RegisterLLM(registry, dashscope.LLMOptions{
		BaseURL: env(lookup, "DASHSCOPE_LLM_BASE_URL"),
		APIKey:  dashscopeAPIKey(lookup),
		Model:   env(lookup, "DASHSCOPE_LLM_MODEL"),
	})
	dashscope.RegisterASR(registry, dashscope.ASROptions{
		EndpointURL: env(lookup, "DASHSCOPE_ASR_ENDPOINT"),
		APIKey:      dashscopeAPIKey(lookup),
		WorkspaceID: env(lookup, "DASHSCOPE_WORKSPACE_ID"),
		Model:       env(lookup, "DASHSCOPE_ASR_MODEL"),
		OpusDecoderFactory: dashscope.OpusDecoderFactoryFunc(func() (dashscope.OpusDecoder, error) {
			return audio.NewLibOpusPCMDecoder(audio.DefaultSampleRateHz, audio.DefaultChannels)
		}),
	})
	ttsVolume, ttsVolumeSet := boundedEnvInt(lookup, "DASHSCOPE_TTS_VOLUME", 0, 100)
	opusTuning := opusEncoderTuningFromEnv(lookup)
	dashscope.RegisterTTS(registry, dashscope.TTSOptions{
		EndpointURL: env(lookup, "DASHSCOPE_TTS_ENDPOINT"),
		APIKey:      dashscopeAPIKey(lookup),
		WorkspaceID: env(lookup, "DASHSCOPE_WORKSPACE_ID"),
		Model:       env(lookup, "DASHSCOPE_TTS_MODEL"),
		Voice:       env(lookup, "DASHSCOPE_TTS_VOICE"),
		Volume:      ttsVolume,
		VolumeSet:   ttsVolumeSet,
		Rate:        boundedEnvFloat(lookup, "DASHSCOPE_TTS_RATE", 0.5, 2.0),
		Pitch:       boundedEnvFloat(lookup, "DASHSCOPE_TTS_PITCH", 0.5, 2.0),
		OpusEncoder: audio.OpusPCMEncoderFactoryFunc(func(sampleRateHz int, channels int, frameDurationMS int) (audio.OpusPCMEncoder, error) {
			return audio.NewLibOpusPCMEncoderWithTuning(sampleRateHz, channels, frameDurationMS, opusTuning)
		}),
	})

	doubao.RegisterLLM(registry, doubao.LLMOptions{
		BaseURL: firstEnv(lookup, "ARK_LLM_BASE_URL", "DOUBAO_LLM_BASE_URL"),
		APIKey:  firstEnv(lookup, "ARK_API_KEY", "DOUBAO_ARK_API_KEY"),
		Model:   firstEnv(lookup, "ARK_LLM_MODEL", "DOUBAO_LLM_MODEL"),
	})
	doubao.RegisterASR(registry, doubao.ASROptions{
		EndpointURL: env(lookup, "DOUBAO_ASR_ENDPOINT"),
		APIKey:      firstEnv(lookup, "DOUBAO_API_KEY", "DOUBAO_VOICE_API_KEY"),
		ResourceID:  env(lookup, "DOUBAO_ASR_RESOURCE_ID"),
		Model:       env(lookup, "DOUBAO_ASR_MODEL"),
	})
	doubao.RegisterTTS(registry, doubao.TTSOptions{
		EndpointURL: env(lookup, "DOUBAO_TTS_ENDPOINT"),
		APIKey:      firstEnv(lookup, "DOUBAO_API_KEY", "DOUBAO_VOICE_API_KEY"),
		ResourceID:  env(lookup, "DOUBAO_TTS_RESOURCE_ID"),
		Model:       env(lookup, "DOUBAO_TTS_MODEL"),
		Voice:       env(lookup, "DOUBAO_TTS_VOICE"),
	})

	minimax.RegisterLLM(registry, minimax.LLMOptions{
		BaseURL: env(lookup, "MINIMAX_LLM_BASE_URL"),
		APIKey:  env(lookup, "MINIMAX_API_KEY"),
		Model:   env(lookup, "MINIMAX_LLM_MODEL"),
	})
	minimax.RegisterTTS(registry, minimax.TTSOptions{
		EndpointURL: env(lookup, "MINIMAX_TTS_ENDPOINT"),
		APIKey:      env(lookup, "MINIMAX_API_KEY"),
		Model:       env(lookup, "MINIMAX_TTS_MODEL"),
		Voice:       env(lookup, "MINIMAX_TTS_VOICE"),
	})

	stepfun.RegisterLLM(registry, stepfun.LLMOptions{
		BaseURL: firstEnv(lookup, "STEPFUN_LLM_BASE_URL", "STEP_LLM_BASE_URL"),
		APIKey:  firstEnv(lookup, "STEPFUN_API_KEY", "STEP_API_KEY"),
		Model:   env(lookup, "STEPFUN_LLM_MODEL"),
	})
	siliconflow.RegisterLLM(registry, siliconflow.LLMOptions{
		BaseURL: env(lookup, "SILICONFLOW_LLM_BASE_URL"),
		APIKey:  siliconflowAPIKey(lookup),
		Model:   env(lookup, "SILICONFLOW_LLM_MODEL"),
	})
	moonshot.RegisterLLM(registry, moonshot.LLMOptions{
		BaseURL: env(lookup, "MOONSHOT_LLM_BASE_URL"),
		APIKey:  env(lookup, "MOONSHOT_API_KEY"),
		Model:   env(lookup, "MOONSHOT_LLM_MODEL"),
	})
	deepseek.RegisterLLM(registry, deepseek.LLMOptions{
		BaseURL: env(lookup, "DEEPSEEK_LLM_BASE_URL"),
		APIKey:  env(lookup, "DEEPSEEK_API_KEY"),
		Model:   env(lookup, "DEEPSEEK_LLM_MODEL"),
	})
	anthropic.RegisterLLM(registry, anthropic.LLMOptions{
		BaseURL: env(lookup, "ANTHROPIC_LLM_BASE_URL"),
		APIKey:  env(lookup, "ANTHROPIC_API_KEY"),
		Model:   env(lookup, "ANTHROPIC_LLM_MODEL"),
	})

	return registry
}

func probeTargetsForProfile(profile gatewayconfig.ProviderProfileConfig) []probeTarget {
	var targets []probeTarget
	if providerID := strings.TrimSpace(profile.ASR); providerID != "" {
		targets = append(targets, probeTarget{providerID: providerID, modality: providers.ProbeModalityASR})
	}
	if providerID := strings.TrimSpace(profile.LLM); providerID != "" {
		targets = append(targets, probeTarget{providerID: providerID, modality: providers.ProbeModalityLLM})
	}
	if providerID := strings.TrimSpace(profile.TTS); providerID != "" {
		targets = append(targets, probeTarget{providerID: providerID, modality: providers.ProbeModalityTTS})
	}
	return targets
}

func summarize(results []RunResult) []Summary {
	type bucket struct {
		providerID       string
		modality         string
		runs             int
		failures         int
		firstTranscripts []int64
		firstTokens      []int64
		firstAudios      []int64
		totalLatency     []int64
	}
	buckets := make(map[string]*bucket)
	for _, result := range results {
		key := result.ProviderID + "\x00" + result.Modality
		current, ok := buckets[key]
		if !ok {
			current = &bucket{
				providerID: result.ProviderID,
				modality:   result.Modality,
			}
			buckets[key] = current
		}
		current.runs++
		if !result.Result.OK {
			current.failures++
			continue
		}
		if result.Result.FirstTranscriptMS > 0 {
			current.firstTranscripts = append(current.firstTranscripts, result.Result.FirstTranscriptMS)
		}
		if result.Result.FirstTokenMS > 0 {
			current.firstTokens = append(current.firstTokens, result.Result.FirstTokenMS)
		}
		if result.Result.FirstAudioMS > 0 {
			current.firstAudios = append(current.firstAudios, result.Result.FirstAudioMS)
		}
		if result.Result.TotalMS > 0 {
			current.totalLatency = append(current.totalLatency, result.Result.TotalMS)
		}
	}

	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	summaries := make([]Summary, 0, len(keys))
	for _, key := range keys {
		current := buckets[key]
		summary := Summary{
			ProviderID: current.providerID,
			Modality:   current.modality,
			Runs:       current.runs,
			Successes:  current.runs - current.failures,
			Failures:   current.failures,
			TotalP50MS: percentile(current.totalLatency, 50),
			TotalP95MS: percentile(current.totalLatency, 95),
		}
		switch current.modality {
		case providers.ProbeModalityASR:
			summary.FirstTranscriptP50MS = percentile(current.firstTranscripts, 50)
			summary.FirstTranscriptP95MS = percentile(current.firstTranscripts, 95)
		case providers.ProbeModalityLLM:
			summary.FirstTokenP50MS = percentile(current.firstTokens, 50)
			summary.FirstTokenP95MS = percentile(current.firstTokens, 95)
		case providers.ProbeModalityTTS:
			summary.FirstAudioP50MS = percentile(current.firstAudios, 50)
			summary.FirstAudioP95MS = percentile(current.firstAudios, 95)
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func percentile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	rank := int(math.Ceil(float64(p)/100*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func safeErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, providers.ErrProviderNotFound):
		return "provider_not_found"
	case errors.Is(err, providers.ErrProviderConfiguration):
		return "provider_config_error"
	case errors.Is(err, providers.ErrUnsupportedProbeModality):
		return "unsupported_modality"
	case errors.Is(err, providers.ErrProbeNoFinalTranscript):
		return "no_final_transcript"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return "network_error"
		}
		return "provider_error"
	}
}

func env(lookup gatewayconfig.LookupEnv, name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

func firstEnv(lookup gatewayconfig.LookupEnv, names ...string) string {
	for _, name := range names {
		if value := env(lookup, name); value != "" {
			return value
		}
	}
	return ""
}

func boundedEnvInt(lookup gatewayconfig.LookupEnv, name string, min int, max int) (int, bool) {
	value := env(lookup, name)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min || parsed > max {
		return 0, false
	}
	return parsed, true
}

func boundedEnvFloat(lookup gatewayconfig.LookupEnv, name string, min float64, max float64) float64 {
	value := env(lookup, name)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < min || parsed > max {
		return 0
	}
	return parsed
}

func opusEncoderTuningFromEnv(lookup gatewayconfig.LookupEnv) audio.LibOpusSpeechTuning {
	bitrate, _ := boundedEnvInt(lookup, "A21_OPUS_DOWNLINK_BITRATE_BPS", 24000, 96000)
	complexity, _ := boundedEnvInt(lookup, "A21_OPUS_DOWNLINK_COMPLEXITY", 1, 10)
	return audio.LibOpusSpeechTuning{
		BitrateBPS: bitrate,
		Complexity: complexity,
	}
}

func dashscopeAPIKey(lookup gatewayconfig.LookupEnv) string {
	return firstEnv(lookup, "DASHSCOPE_API_KEY", "A21_LAB_DASHSCOPE_API_KEY", "A21_DASHSCOPE_API_KEY")
}

func siliconflowAPIKey(lookup gatewayconfig.LookupEnv) string {
	return firstEnv(lookup, "SILICONFLOW_API_KEY", "A21_LAB_SILICONFLOW_API_KEY", "A21_SILICONFLOW_API_KEY")
}
