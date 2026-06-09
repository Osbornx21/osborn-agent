package providerrouter

import (
	"context"
	"errors"
	"testing"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/session"
)

func TestRouterResolvesDefaultAndDeviceOverrideProfiles(t *testing.T) {
	cfg := testConfig()
	router := New(cfg, providers.NewRegistry(providers.MockConfig{}))

	first, err := router.ResolveVoiceProviders(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("ResolveVoiceProviders() error = %v", err)
	}
	if first.Profile != "primary" {
		t.Fatalf("default profile = %q, want primary", first.Profile)
	}

	status, err := router.SetDeviceProfile(context.Background(), "stackchan-s3-main", "fallback")
	if err != nil {
		t.Fatalf("SetDeviceProfile() error = %v", err)
	}
	if !status.Override || status.ActiveProfile != "fallback" {
		t.Fatalf("status = %+v, want fallback override", status)
	}

	second, err := router.ResolveVoiceProviders(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("ResolveVoiceProviders() after override error = %v", err)
	}
	if second.Profile != "fallback" {
		t.Fatalf("override profile = %q, want fallback", second.Profile)
	}

	status, err = router.ClearDeviceProfile(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("ClearDeviceProfile() error = %v", err)
	}
	if status.Override || status.ActiveProfile != "primary" {
		t.Fatalf("status = %+v, want primary default", status)
	}
}

func TestRouterRejectsUnknownDeviceAndProfile(t *testing.T) {
	router := New(testConfig(), providers.NewRegistry(providers.MockConfig{}))

	if _, err := router.GetDeviceProfile(context.Background(), "missing-device"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("GetDeviceProfile() error = %v, want ErrDeviceNotFound", err)
	}
	if _, err := router.SetDeviceProfile(context.Background(), "stackchan-s3-main", "missing-profile"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("SetDeviceProfile() error = %v, want ErrProfileNotFound", err)
	}
}

func TestRouterRejectsLLMOnlyProfileForVoiceRuntime(t *testing.T) {
	router := New(testConfig(), providers.NewRegistry(providers.MockConfig{}))

	_, err := router.SetDeviceProfile(context.Background(), "stackchan-s3-main", "llm-only")
	if !errors.Is(err, providers.ErrProviderConfiguration) {
		t.Fatalf("SetDeviceProfile(llm-only) error = %v, want provider configuration error", err)
	}
}

func TestRouterAutoFallbackAfterConsecutiveYellowTurns(t *testing.T) {
	cfg := testConfig()
	cfg.Providers.AutoFallback = gatewayconfig.ProviderAutoFallbackConfig{
		Enabled:            true,
		Profiles:           []string{"fallback"},
		YellowFirstAudioMS: 1000,
		ConsecutiveYellow:  2,
		ConsecutiveErrors:  2,
	}
	router := New(cfg, providers.NewRegistry(providers.MockConfig{}))

	router.ObserveVoiceTurn(context.Background(), providersOutcome("primary", false, 1200))
	status, err := router.GetDeviceProfile(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceProfile() error = %v", err)
	}
	if status.ActiveProfile != "primary" || status.Override {
		t.Fatalf("status after one yellow = %+v, want default primary", status)
	}

	router.ObserveVoiceTurn(context.Background(), providersOutcome("primary", false, 1500))
	status, err = router.GetDeviceProfile(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceProfile() after fallback error = %v", err)
	}
	if status.ActiveProfile != "fallback" || !status.Override || status.OverrideSource != "auto" {
		t.Fatalf("status after two yellow = %+v, want auto fallback", status)
	}
}

func TestRouterAutoFallbackAfterConsecutiveErrors(t *testing.T) {
	cfg := testConfig()
	cfg.Providers.AutoFallback = gatewayconfig.ProviderAutoFallbackConfig{
		Enabled:           true,
		Profiles:          []string{"fallback"},
		ConsecutiveErrors: 2,
	}
	router := New(cfg, providers.NewRegistry(providers.MockConfig{}))

	router.ObserveVoiceTurn(context.Background(), providersOutcome("primary", true, 0))
	router.ObserveVoiceTurn(context.Background(), providersOutcome("primary", true, 0))

	status, err := router.GetDeviceProfile(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceProfile() error = %v", err)
	}
	if status.ActiveProfile != "fallback" || status.OverrideSource != "auto" {
		t.Fatalf("status = %+v, want auto fallback after errors", status)
	}
}

func TestRouterManualOverridePreventsAutoFallback(t *testing.T) {
	cfg := testConfig()
	cfg.Providers.AutoFallback = gatewayconfig.ProviderAutoFallbackConfig{
		Enabled:            true,
		Profiles:           []string{"fallback"},
		YellowFirstAudioMS: 1000,
		ConsecutiveYellow:  1,
	}
	router := New(cfg, providers.NewRegistry(providers.MockConfig{}))
	if _, err := router.SetDeviceProfile(context.Background(), "stackchan-s3-main", "primary"); err != nil {
		t.Fatalf("SetDeviceProfile() error = %v", err)
	}

	router.ObserveVoiceTurn(context.Background(), providersOutcome("primary", false, 2000))

	status, err := router.GetDeviceProfile(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceProfile() error = %v", err)
	}
	if status.ActiveProfile != "primary" || status.OverrideSource != "manual" {
		t.Fatalf("status = %+v, want manual primary override", status)
	}
}

func TestRouterListsProfileCatalogAndDeviceStatuses(t *testing.T) {
	cfg := testConfig()
	cfg.Providers.AutoFallback = gatewayconfig.ProviderAutoFallbackConfig{
		Enabled:  true,
		Profiles: []string{"fallback"},
	}
	router := New(cfg, providers.NewRegistry(providers.MockConfig{}))
	if _, err := router.SetDeviceProfile(context.Background(), "stackchan-s3-main", "fallback"); err != nil {
		t.Fatalf("SetDeviceProfile() error = %v", err)
	}

	catalog, err := router.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}

	if catalog.DefaultProfile != "primary" || !catalog.AutoFallbackEnabled {
		t.Fatalf("catalog defaults = %+v, want primary with auto fallback enabled", catalog)
	}
	if len(catalog.AutoFallbackProfiles) != 1 || catalog.AutoFallbackProfiles[0] != "fallback" {
		t.Fatalf("auto fallback profiles = %v, want [fallback]", catalog.AutoFallbackProfiles)
	}
	if len(catalog.Profiles) != 3 {
		t.Fatalf("profiles = %+v, want 3 configured profiles", catalog.Profiles)
	}
	if catalog.Profiles[0].Profile != "fallback" || !catalog.Profiles[0].AutoFallback || !catalog.Profiles[0].VoiceRuntime {
		t.Fatalf("first profile = %+v, want sorted fallback voice fallback profile", catalog.Profiles[0])
	}
	if catalog.Profiles[1].Profile != "llm-only" || catalog.Profiles[1].VoiceRuntime {
		t.Fatalf("second profile = %+v, want llm-only marked not voice-runtime", catalog.Profiles[1])
	}
	if catalog.Profiles[2].Profile != "primary" || !catalog.Profiles[2].Default {
		t.Fatalf("third profile = %+v, want default primary", catalog.Profiles[2])
	}
	if len(catalog.Devices) != 1 || catalog.Devices[0].ActiveProfile != "fallback" || catalog.Devices[0].OverrideSource != "manual" {
		t.Fatalf("devices = %+v, want manual fallback status", catalog.Devices)
	}
}

func TestRouterCatalogMarksLLMOnlyVoiceFallbackWhenASRAndTTSMatchDefault(t *testing.T) {
	cfg := &gatewayconfig.Config{
		Devices: []gatewayconfig.DeviceConfig{
			{DeviceID: "stackchan-s3-main", ClientID: "stackchan-s3-main-client"},
		},
		Providers: gatewayconfig.ProvidersConfig{
			DefaultProfile: "primary",
			AutoFallback: gatewayconfig.ProviderAutoFallbackConfig{
				Enabled:  true,
				Profiles: []string{"llm-fallback", "voice-io-fallback"},
			},
			Profiles: map[string]gatewayconfig.ProviderProfileConfig{
				"primary":           {ASR: "dashscope-asr", LLM: "siliconflow-llm", TTS: "dashscope-tts"},
				"llm-fallback":      {ASR: "dashscope-asr", LLM: "dashscope-llm", TTS: "dashscope-tts"},
				"voice-io-fallback": {ASR: "dashscope-asr", LLM: "dashscope-llm", TTS: "other-tts"},
			},
		},
	}
	router := New(cfg, providers.NewRegistry(providers.MockConfig{}))

	catalog, err := router.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}

	llmFallback := profileInfoByName(catalog.Profiles, "llm-fallback")
	if !llmFallback.SharedDefaultVoiceIO || !llmFallback.LLMOnlyFallback {
		t.Fatalf("llm-fallback = %+v, want shared voice IO and LLM-only fallback", llmFallback)
	}
	voiceIOFallback := profileInfoByName(catalog.Profiles, "voice-io-fallback")
	if voiceIOFallback.SharedDefaultVoiceIO || voiceIOFallback.LLMOnlyFallback {
		t.Fatalf("voice-io-fallback = %+v, want no shared voice IO policy markers", voiceIOFallback)
	}
	primary := profileInfoByName(catalog.Profiles, "primary")
	if primary.SharedDefaultVoiceIO || primary.LLMOnlyFallback {
		t.Fatalf("primary = %+v, want no fallback policy markers", primary)
	}
}

func providersOutcome(profile string, failed bool, firstAudibleMS int64) session.VoiceProviderOutcome {
	return session.VoiceProviderOutcome{
		DeviceID:              "stackchan-s3-main",
		Profile:               profile,
		Generation:            1,
		Error:                 failed,
		ErrorCode:             "test_error",
		FirstAudibleLatencyMS: firstAudibleMS,
	}
}

func profileInfoByName(profiles []ProfileInfo, name string) ProfileInfo {
	for _, profile := range profiles {
		if profile.Profile == name {
			return profile
		}
	}
	return ProfileInfo{}
}

func testConfig() *gatewayconfig.Config {
	return &gatewayconfig.Config{
		Devices: []gatewayconfig.DeviceConfig{
			{DeviceID: "stackchan-s3-main", ClientID: "stackchan-s3-main-client"},
		},
		Providers: gatewayconfig.ProvidersConfig{
			DefaultProfile: "primary",
			Profiles: map[string]gatewayconfig.ProviderProfileConfig{
				"primary":  {ASR: providers.ProviderMock, LLM: providers.ProviderMock, TTS: providers.ProviderMock},
				"fallback": {ASR: providers.ProviderMock, LLM: providers.ProviderMock, TTS: providers.ProviderMock},
				"llm-only": {LLM: providers.ProviderMock},
			},
		},
	}
}
