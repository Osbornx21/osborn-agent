package providerrouter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/session"
)

var (
	ErrDeviceNotFound  = errors.New("device not found")
	ErrProfileNotFound = errors.New("provider profile not found")
)

type Status struct {
	DeviceID       string `json:"device_id"`
	DefaultProfile string `json:"default_profile"`
	ActiveProfile  string `json:"active_profile"`
	Override       bool   `json:"override"`
	OverrideSource string `json:"override_source,omitempty"`
	ASRProvider    string `json:"asr_provider"`
	LLMProvider    string `json:"llm_provider"`
	TTSProvider    string `json:"tts_provider"`
}

type ProfileCatalog struct {
	DefaultProfile       string        `json:"default_profile"`
	AutoFallbackEnabled  bool          `json:"auto_fallback_enabled"`
	AutoFallbackProfiles []string      `json:"auto_fallback_profiles,omitempty"`
	Profiles             []ProfileInfo `json:"profiles"`
	Devices              []Status      `json:"devices"`
}

type ProfileInfo struct {
	Profile              string `json:"profile"`
	Default              bool   `json:"default"`
	AutoFallback         bool   `json:"auto_fallback"`
	VoiceRuntime         bool   `json:"voice_runtime"`
	SharedDefaultVoiceIO bool   `json:"shared_default_voice_io,omitempty"`
	LLMOnlyFallback      bool   `json:"llm_only_fallback,omitempty"`
	ASRProvider          string `json:"asr_provider,omitempty"`
	LLMProvider          string `json:"llm_provider,omitempty"`
	TTSProvider          string `json:"tts_provider,omitempty"`
}

type Controller interface {
	ListProfiles(ctx context.Context) (ProfileCatalog, error)
	GetDeviceProfile(ctx context.Context, deviceID string) (Status, error)
	SetDeviceProfile(ctx context.Context, deviceID string, profile string) (Status, error)
	ClearDeviceProfile(ctx context.Context, deviceID string) (Status, error)
}

type Router struct {
	cfg      *gatewayconfig.Config
	registry *providers.Registry

	mu              sync.RWMutex
	manualOverrides map[string]string
	autoOverrides   map[string]string
	outcomes        map[string]deviceOutcomeState
}

type deviceOutcomeState struct {
	ConsecutiveYellow int
	ConsecutiveErrors int
}

func New(cfg *gatewayconfig.Config, registry *providers.Registry) *Router {
	return &Router{
		cfg:             cfg,
		registry:        registry,
		manualOverrides: make(map[string]string),
		autoOverrides:   make(map[string]string),
		outcomes:        make(map[string]deviceOutcomeState),
	}
}

func (r *Router) ResolveVoiceProviders(ctx context.Context, deviceID string) (session.VoiceProviderSet, error) {
	_ = ctx
	profileName, _, err := r.activeProfileName(deviceID)
	if err != nil {
		return session.VoiceProviderSet{}, err
	}
	profile, err := r.profileConfig(profileName)
	if err != nil {
		return session.VoiceProviderSet{}, err
	}
	return r.buildProviderSet(profileName, profile)
}

func (r *Router) GetDeviceProfile(ctx context.Context, deviceID string) (Status, error) {
	_ = ctx
	profileName, override, source, err := r.activeProfile(deviceID)
	if err != nil {
		return Status{}, err
	}
	return r.status(deviceID, profileName, override, source)
}

func (r *Router) ListProfiles(ctx context.Context) (ProfileCatalog, error) {
	_ = ctx
	if r == nil || r.cfg == nil {
		return ProfileCatalog{}, fmt.Errorf("%w: config not loaded", ErrProfileNotFound)
	}

	defaultProfile := r.defaultProfile()
	defaultProfileConfig, hasDefaultProfileConfig := r.cfg.Providers.Profiles[defaultProfile]
	fallbackProfiles := normalizedFallbackProfiles(r.cfg.Providers.AutoFallback.Profiles)
	fallbackSet := make(map[string]struct{}, len(fallbackProfiles))
	for _, profile := range fallbackProfiles {
		fallbackSet[profile] = struct{}{}
	}

	profileEntries := make([]profileCatalogEntry, 0, len(r.cfg.Providers.Profiles))
	for name, profile := range r.cfg.Providers.Profiles {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			profileEntries = append(profileEntries, profileCatalogEntry{Name: trimmed, Config: profile})
		}
	}
	sort.Slice(profileEntries, func(i, j int) bool {
		return profileEntries[i].Name < profileEntries[j].Name
	})

	profiles := make([]ProfileInfo, 0, len(profileEntries))
	for _, entry := range profileEntries {
		name := entry.Name
		profile := entry.Config
		asr := strings.TrimSpace(profile.ASR)
		llm := strings.TrimSpace(profile.LLM)
		tts := strings.TrimSpace(profile.TTS)
		_, fallback := fallbackSet[name]
		sharedDefaultVoiceIO := fallback && hasDefaultProfileConfig && profileSharesDefaultVoiceIO(profile, defaultProfileConfig)
		llmOnlyFallback := sharedDefaultVoiceIO && llm != "" && llm != strings.TrimSpace(defaultProfileConfig.LLM)
		profiles = append(profiles, ProfileInfo{
			Profile:              name,
			Default:              name == defaultProfile,
			AutoFallback:         fallback,
			VoiceRuntime:         asr != "" && llm != "" && tts != "",
			SharedDefaultVoiceIO: sharedDefaultVoiceIO,
			LLMOnlyFallback:      llmOnlyFallback,
			ASRProvider:          asr,
			LLMProvider:          llm,
			TTSProvider:          tts,
		})
	}

	deviceIDs := make([]string, 0, len(r.cfg.Devices))
	for _, device := range r.cfg.Devices {
		if deviceID := strings.TrimSpace(device.DeviceID); deviceID != "" {
			deviceIDs = append(deviceIDs, deviceID)
		}
	}
	sort.Strings(deviceIDs)

	devices := make([]Status, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		profileName, override, source, err := r.activeProfile(deviceID)
		if err != nil {
			return ProfileCatalog{}, err
		}
		status, err := r.status(deviceID, profileName, override, source)
		if err != nil {
			return ProfileCatalog{}, err
		}
		devices = append(devices, status)
	}

	return ProfileCatalog{
		DefaultProfile:       defaultProfile,
		AutoFallbackEnabled:  r.cfg.Providers.AutoFallback.Enabled,
		AutoFallbackProfiles: fallbackProfiles,
		Profiles:             profiles,
		Devices:              devices,
	}, nil
}

func (r *Router) ObserveVoiceTurn(ctx context.Context, outcome session.VoiceProviderOutcome) {
	_ = ctx
	if r == nil || r.cfg == nil || !r.cfg.Providers.AutoFallback.Enabled {
		return
	}
	deviceID := strings.TrimSpace(outcome.DeviceID)
	if deviceID == "" {
		return
	}
	activeProfile, _, source, err := r.activeProfile(deviceID)
	if err != nil || source == "manual" {
		return
	}
	if strings.TrimSpace(outcome.Profile) != activeProfile {
		return
	}

	target := r.recordOutcomeAndSelectFallback(deviceID, outcome)
	if target == "" {
		return
	}
	profile, err := r.profileConfig(target)
	if err != nil {
		return
	}
	if _, err := r.buildProviderSet(target, profile); err != nil {
		return
	}

	r.mu.Lock()
	r.autoOverrides[deviceID] = target
	delete(r.outcomes, deviceID)
	r.mu.Unlock()
}

func (r *Router) SetDeviceProfile(ctx context.Context, deviceID string, profileName string) (Status, error) {
	_ = ctx
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		return Status{}, fmt.Errorf("%w: empty profile", ErrProfileNotFound)
	}
	if err := r.ensureDevice(deviceID); err != nil {
		return Status{}, err
	}
	profile, err := r.profileConfig(profileName)
	if err != nil {
		return Status{}, err
	}
	if _, err := r.buildProviderSet(profileName, profile); err != nil {
		return Status{}, err
	}

	r.mu.Lock()
	r.manualOverrides[deviceID] = profileName
	delete(r.autoOverrides, deviceID)
	delete(r.outcomes, deviceID)
	r.mu.Unlock()

	return r.status(deviceID, profileName, true, "manual")
}

func (r *Router) ClearDeviceProfile(ctx context.Context, deviceID string) (Status, error) {
	_ = ctx
	if err := r.ensureDevice(deviceID); err != nil {
		return Status{}, err
	}

	r.mu.Lock()
	delete(r.manualOverrides, deviceID)
	delete(r.autoOverrides, deviceID)
	delete(r.outcomes, deviceID)
	r.mu.Unlock()

	return r.status(deviceID, r.defaultProfile(), false, "")
}

func (r *Router) activeProfileName(deviceID string) (string, bool, error) {
	profile, override, _, err := r.activeProfile(deviceID)
	return profile, override, err
}

func (r *Router) activeProfile(deviceID string) (string, bool, string, error) {
	if err := r.ensureDevice(deviceID); err != nil {
		return "", false, "", err
	}

	r.mu.RLock()
	manual := strings.TrimSpace(r.manualOverrides[deviceID])
	auto := strings.TrimSpace(r.autoOverrides[deviceID])
	r.mu.RUnlock()
	if manual != "" {
		return manual, true, "manual", nil
	}
	if auto != "" {
		return auto, true, "auto", nil
	}
	return r.defaultProfile(), false, "", nil
}

func (r *Router) status(deviceID string, profileName string, override bool, source string) (Status, error) {
	profile, err := r.profileConfig(profileName)
	if err != nil {
		return Status{}, err
	}
	return Status{
		DeviceID:       deviceID,
		DefaultProfile: r.defaultProfile(),
		ActiveProfile:  profileName,
		Override:       override,
		OverrideSource: source,
		ASRProvider:    strings.TrimSpace(profile.ASR),
		LLMProvider:    strings.TrimSpace(profile.LLM),
		TTSProvider:    strings.TrimSpace(profile.TTS),
	}, nil
}

func (r *Router) recordOutcomeAndSelectFallback(deviceID string, outcome session.VoiceProviderOutcome) string {
	yellow := outcome.FirstAudibleLatencyMS >= int64(r.yellowFirstAudioMS()) && outcome.FirstAudibleLatencyMS > 0

	r.mu.Lock()
	state := r.outcomes[deviceID]
	switch {
	case outcome.Error:
		state.ConsecutiveErrors++
		state.ConsecutiveYellow = 0
	case yellow:
		state.ConsecutiveYellow++
		state.ConsecutiveErrors = 0
	default:
		state = deviceOutcomeState{}
	}
	r.outcomes[deviceID] = state

	shouldFallback := state.ConsecutiveErrors >= r.consecutiveErrors() || state.ConsecutiveYellow >= r.consecutiveYellow()
	r.mu.Unlock()

	if !shouldFallback {
		return ""
	}
	return r.nextFallbackProfile(strings.TrimSpace(outcome.Profile))
}

func (r *Router) nextFallbackProfile(current string) string {
	if r == nil || r.cfg == nil {
		return ""
	}
	for _, profile := range r.cfg.Providers.AutoFallback.Profiles {
		profile = strings.TrimSpace(profile)
		if profile != "" && profile != current {
			return profile
		}
	}
	return ""
}

func (r *Router) yellowFirstAudioMS() int {
	if r == nil || r.cfg == nil || r.cfg.Providers.AutoFallback.YellowFirstAudioMS <= 0 {
		return 3000
	}
	return r.cfg.Providers.AutoFallback.YellowFirstAudioMS
}

func (r *Router) consecutiveYellow() int {
	if r == nil || r.cfg == nil || r.cfg.Providers.AutoFallback.ConsecutiveYellow <= 0 {
		return 3
	}
	return r.cfg.Providers.AutoFallback.ConsecutiveYellow
}

func (r *Router) consecutiveErrors() int {
	if r == nil || r.cfg == nil || r.cfg.Providers.AutoFallback.ConsecutiveErrors <= 0 {
		return 2
	}
	return r.cfg.Providers.AutoFallback.ConsecutiveErrors
}

func (r *Router) buildProviderSet(profileName string, profile gatewayconfig.ProviderProfileConfig) (session.VoiceProviderSet, error) {
	asrName := strings.TrimSpace(profile.ASR)
	llmName := strings.TrimSpace(profile.LLM)
	ttsName := strings.TrimSpace(profile.TTS)
	if asrName == "" || llmName == "" || ttsName == "" {
		return session.VoiceProviderSet{}, providers.NewProviderConfigurationError("provider profile must include asr, llm and tts")
	}

	asr, err := r.registry.ASRProvider(asrName)
	if err != nil {
		return session.VoiceProviderSet{}, fmt.Errorf("asr provider: %w", err)
	}
	if err := providers.ValidateProviderConfig(asr); err != nil {
		return session.VoiceProviderSet{}, fmt.Errorf("asr provider: %w", err)
	}
	llm, err := r.registry.LLMProvider(llmName)
	if err != nil {
		return session.VoiceProviderSet{}, fmt.Errorf("llm provider: %w", err)
	}
	if err := providers.ValidateProviderConfig(llm); err != nil {
		return session.VoiceProviderSet{}, fmt.Errorf("llm provider: %w", err)
	}
	tts, err := r.registry.TTSProvider(ttsName)
	if err != nil {
		return session.VoiceProviderSet{}, fmt.Errorf("tts provider: %w", err)
	}
	if err := providers.ValidateProviderConfig(tts); err != nil {
		return session.VoiceProviderSet{}, fmt.Errorf("tts provider: %w", err)
	}

	return session.VoiceProviderSet{
		Profile: profileName,
		ASRName: asrName,
		LLMName: llmName,
		TTSName: ttsName,
		ASR:     asr,
		LLM:     llm,
		TTS:     tts,
	}, nil
}

func (r *Router) profileConfig(profileName string) (gatewayconfig.ProviderProfileConfig, error) {
	if r == nil || r.cfg == nil {
		return gatewayconfig.ProviderProfileConfig{}, fmt.Errorf("%w: config not loaded", ErrProfileNotFound)
	}
	profile, ok := r.cfg.Providers.Profiles[strings.TrimSpace(profileName)]
	if !ok {
		return gatewayconfig.ProviderProfileConfig{}, fmt.Errorf("%w: %s", ErrProfileNotFound, profileName)
	}
	return profile, nil
}

func (r *Router) ensureDevice(deviceID string) error {
	if r == nil || r.cfg == nil {
		return fmt.Errorf("%w: config not loaded", ErrDeviceNotFound)
	}
	if _, ok := r.cfg.DeviceByID(strings.TrimSpace(deviceID)); !ok {
		return fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
	}
	return nil
}

func (r *Router) defaultProfile() string {
	if r == nil || r.cfg == nil {
		return ""
	}
	return strings.TrimSpace(r.cfg.Providers.DefaultProfile)
}

func normalizedFallbackProfiles(profiles []string) []string {
	normalized := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		profile = strings.TrimSpace(profile)
		if profile != "" {
			normalized = append(normalized, profile)
		}
	}
	return normalized
}

func profileSharesDefaultVoiceIO(profile gatewayconfig.ProviderProfileConfig, defaultProfile gatewayconfig.ProviderProfileConfig) bool {
	asr := strings.TrimSpace(profile.ASR)
	tts := strings.TrimSpace(profile.TTS)
	return asr != "" &&
		tts != "" &&
		asr == strings.TrimSpace(defaultProfile.ASR) &&
		tts == strings.TrimSpace(defaultProfile.TTS)
}

type profileCatalogEntry struct {
	Name   string
	Config gatewayconfig.ProviderProfileConfig
}
