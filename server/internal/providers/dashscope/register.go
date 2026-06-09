package dashscope

import "stackchan-gateway/internal/providers"

type LLMRegistrar interface {
	RegisterLLM(name string, factory providers.LLMFactory)
}

type ASRRegistrar interface {
	RegisterASR(name string, factory providers.ASRFactory)
}

type TTSRegistrar interface {
	RegisterTTS(name string, factory providers.TTSFactory)
}

func RegisterLLM(registry LLMRegistrar, options LLMOptions) {
	registry.RegisterLLM(ProviderIDLLM, func() (providers.LLMProvider, error) {
		return NewLLM(options), nil
	})
}

func RegisterASR(registry ASRRegistrar, options ASROptions) {
	registry.RegisterASR(ProviderIDASR, func() (providers.ASRProvider, error) {
		return NewASR(options), nil
	})
}

func RegisterTTS(registry TTSRegistrar, options TTSOptions) {
	registry.RegisterTTS(ProviderIDTTS, func() (providers.TTSProvider, error) {
		return NewTTS(options), nil
	})
}
