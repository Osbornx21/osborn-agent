package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providerprobe"
	"stackchan-gateway/internal/providers"
)

type modelIDProvider interface {
	ModelID() string
}

type voiceIDProvider interface {
	VoiceID() string
}

func runVoiceProfileCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("voice-profile-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config")
	profileName := flags.String("profile", "", "provider profile id; defaults to providers.default_profile")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	cfg, err := gatewayconfig.LoadFile(*configPath, gatewayconfig.OSLookupEnv)
	if err != nil {
		fmt.Fprintf(stderr, "voice-profile-check failed: %v\n", err)
		return 1
	}
	selectedProfile := strings.TrimSpace(*profileName)
	if selectedProfile == "" {
		selectedProfile = strings.TrimSpace(cfg.Providers.DefaultProfile)
	}
	profile, ok := cfg.Providers.Profiles[selectedProfile]
	if !ok {
		fmt.Fprintf(stderr, "voice-profile-check failed: profile %q not found\n", selectedProfile)
		return 1
	}
	result, err := inspectVoiceProfile(selectedProfile, profile, providerprobe.NewRegistryFromEnv(gatewayconfig.OSLookupEnv))
	if err != nil {
		fmt.Fprintf(stderr, "voice-profile-check failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "voice profile OK: profile=%s asr=%s asr_model=%s llm=%s llm_model=%s tts=%s tts_model=%s tts_voice=%s\n",
		result.Profile,
		result.ASRProvider,
		result.ASRModel,
		result.LLMProvider,
		result.LLMModel,
		result.TTSProvider,
		result.TTSModel,
		result.TTSVoice,
	)
	return 0
}

type voiceProfileInspection struct {
	Profile     string
	ASRProvider string
	ASRModel    string
	LLMProvider string
	LLMModel    string
	TTSProvider string
	TTSModel    string
	TTSVoice    string
}

func inspectVoiceProfile(profileName string, profile gatewayconfig.ProviderProfileConfig, registry *providers.Registry) (voiceProfileInspection, error) {
	if registry == nil {
		return voiceProfileInspection{}, fmt.Errorf("provider registry is required")
	}
	inspection := voiceProfileInspection{
		Profile:     strings.TrimSpace(profileName),
		ASRProvider: strings.TrimSpace(profile.ASR),
		LLMProvider: strings.TrimSpace(profile.LLM),
		TTSProvider: strings.TrimSpace(profile.TTS),
	}
	if inspection.ASRProvider == "" || inspection.LLMProvider == "" || inspection.TTSProvider == "" {
		return voiceProfileInspection{}, fmt.Errorf("voice profile %q must include ASR, LLM and TTS providers", inspection.Profile)
	}
	for _, providerID := range []string{inspection.ASRProvider, inspection.LLMProvider, inspection.TTSProvider} {
		if providerID == providers.ProviderMock {
			return voiceProfileInspection{}, fmt.Errorf("voice profile %q must not use mock provider %q", inspection.Profile, providerID)
		}
	}

	asr, err := registry.ASRProvider(inspection.ASRProvider)
	if err != nil {
		return voiceProfileInspection{}, err
	}
	if err := providers.ValidateProviderConfig(asr); err != nil {
		return voiceProfileInspection{}, err
	}
	inspection.ASRModel = providerModelID(asr)

	llm, err := registry.LLMProvider(inspection.LLMProvider)
	if err != nil {
		return voiceProfileInspection{}, err
	}
	if err := providers.ValidateProviderConfig(llm); err != nil {
		return voiceProfileInspection{}, err
	}
	inspection.LLMModel = providerModelID(llm)
	if err := validateRealtimeVoiceLLMModel(inspection.LLMModel); err != nil {
		return voiceProfileInspection{}, err
	}

	tts, err := registry.TTSProvider(inspection.TTSProvider)
	if err != nil {
		return voiceProfileInspection{}, err
	}
	if err := providers.ValidateProviderConfig(tts); err != nil {
		return voiceProfileInspection{}, err
	}
	inspection.TTSModel = providerModelID(tts)
	inspection.TTSVoice = providerVoiceID(tts)
	return inspection, nil
}

func providerModelID(provider any) string {
	withModelID, ok := provider.(modelIDProvider)
	if !ok {
		return "unknown"
	}
	model := strings.TrimSpace(withModelID.ModelID())
	if model == "" {
		return "unknown"
	}
	return model
}

func providerVoiceID(provider any) string {
	withVoiceID, ok := provider.(voiceIDProvider)
	if !ok {
		return "unknown"
	}
	voice := strings.TrimSpace(withVoiceID.VoiceID())
	if voice == "" {
		return "unknown"
	}
	return voice
}

func validateRealtimeVoiceLLMModel(modelID string) error {
	model := strings.TrimSpace(modelID)
	if model == "" || model == "unknown" {
		return fmt.Errorf("LLM model id is required for voice profile preflight")
	}
	tokens := modelIDTokens(model)
	for _, forbidden := range realtimeVoiceForbiddenModelTokens() {
		if tokens[forbidden] {
			return fmt.Errorf("LLM model %q is not allowed on the realtime voice path: token %q indicates reasoning/code/vision/pro-class behavior; choose a flash/instruct low-latency model with current probe evidence", model, forbidden)
		}
	}
	return nil
}

func realtimeVoiceForbiddenModelTokens() []string {
	return []string{
		"r1",
		"qwq",
		"think",
		"thinking",
		"reason",
		"reasoner",
		"reasoning",
		"coder",
		"code",
		"vl",
		"vision",
		"omni",
		"pro",
	}
}

func modelIDTokens(modelID string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range strings.FieldsFunc(strings.ToLower(modelID), func(r rune) bool {
		switch r {
		case '/', '-', '_', '.', ':', '@', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	}) {
		token = strings.TrimSpace(token)
		if token != "" {
			tokens[token] = true
		}
	}
	return tokens
}
