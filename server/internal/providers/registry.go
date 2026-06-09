package providers

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrProviderNotFound      = errors.New("provider not found")
	ErrProviderConfiguration = errors.New("provider configuration error")
)

func NewProviderConfigurationError(message string) error {
	return fmt.Errorf("%w: %s", ErrProviderConfiguration, message)
}

type MockConfig struct {
	ASRFinalDelayMS      int    `yaml:"asr_final_delay_ms"`
	ASRAutoFinalOnAudio  bool   `yaml:"asr_auto_final_on_audio"`
	ASRFinalText         string `yaml:"asr_final_text"`
	LLMFirstTokenDelayMS int    `yaml:"llm_first_token_delay_ms"`
	TTSFirstFrameDelayMS int    `yaml:"tts_first_frame_delay_ms"`
	TTSFrameCount        int    `yaml:"tts_frame_count"`
}

func (c MockConfig) ASRFinalDelay() time.Duration {
	return durationFromMillis(c.ASRFinalDelayMS, 120)
}

func (c MockConfig) EffectiveASRFinalText() string {
	text := strings.TrimSpace(c.ASRFinalText)
	if text == "" {
		return "你好，我是 StackChan。"
	}
	return text
}

func (c MockConfig) LLMFirstTokenDelay() time.Duration {
	return durationFromMillis(c.LLMFirstTokenDelayMS, 80)
}

func (c MockConfig) TTSFirstFrameDelay() time.Duration {
	return durationFromMillis(c.TTSFirstFrameDelayMS, 100)
}

func (c MockConfig) EffectiveTTSFrameCount() int {
	if c.TTSFrameCount <= 0 {
		return 8
	}
	return c.TTSFrameCount
}

type Registry struct {
	mockConfig   MockConfig
	asrFactories map[string]ASRFactory
	llmFactories map[string]LLMFactory
	ttsFactories map[string]TTSFactory
}

type ASRFactory func() (ASRProvider, error)
type LLMFactory func() (LLMProvider, error)
type TTSFactory func() (TTSProvider, error)

func NewRegistry(mockConfig MockConfig) *Registry {
	registry := &Registry{
		mockConfig:   mockConfig,
		asrFactories: make(map[string]ASRFactory),
		llmFactories: make(map[string]LLMFactory),
		ttsFactories: make(map[string]TTSFactory),
	}
	registry.RegisterASR(ProviderMock, func() (ASRProvider, error) {
		return NewMockASRProvider(registry.mockConfig), nil
	})
	registry.RegisterLLM(ProviderMock, func() (LLMProvider, error) {
		return NewMockLLMProvider(registry.mockConfig), nil
	})
	registry.RegisterTTS(ProviderMock, func() (TTSProvider, error) {
		return NewMockTTSProvider(registry.mockConfig), nil
	})
	return registry
}

func (r *Registry) RegisterASR(name string, factory ASRFactory) {
	if factory == nil {
		return
	}
	r.asrFactories[normalizeProviderName(name)] = factory
}

func (r *Registry) RegisterLLM(name string, factory LLMFactory) {
	if factory == nil {
		return
	}
	r.llmFactories[normalizeProviderName(name)] = factory
}

func (r *Registry) RegisterTTS(name string, factory TTSFactory) {
	if factory == nil {
		return
	}
	r.ttsFactories[normalizeProviderName(name)] = factory
}

func (r *Registry) ASRProvider(name string) (ASRProvider, error) {
	factory, ok := r.asrFactories[normalizeProviderName(name)]
	if !ok {
		return nil, unknownProvider(name)
	}
	return factory()
}

func (r *Registry) LLMProvider(name string) (LLMProvider, error) {
	factory, ok := r.llmFactories[normalizeProviderName(name)]
	if !ok {
		return nil, unknownProvider(name)
	}
	return factory()
}

func (r *Registry) TTSProvider(name string) (TTSProvider, error) {
	factory, ok := r.ttsFactories[normalizeProviderName(name)]
	if !ok {
		return nil, unknownProvider(name)
	}
	return factory()
}

func durationFromMillis(value int, fallback int) time.Duration {
	if value <= 0 {
		value = fallback
	}
	return time.Duration(value) * time.Millisecond
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func unknownProvider(name string) error {
	return fmt.Errorf("%w: %s", ErrProviderNotFound, name)
}
