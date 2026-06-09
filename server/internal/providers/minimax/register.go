package minimax

import "stackchan-gateway/internal/providers"

type LLMRegistrar interface {
	RegisterLLM(name string, factory providers.LLMFactory)
}

type TTSRegistrar interface {
	RegisterTTS(name string, factory providers.TTSFactory)
}

func RegisterLLM(registry LLMRegistrar, options LLMOptions) {
	registry.RegisterLLM(ProviderIDLLM, func() (providers.LLMProvider, error) {
		return NewLLM(options), nil
	})
}

func RegisterTTS(registry TTSRegistrar, options TTSOptions) {
	registry.RegisterTTS(ProviderIDTTS, func() (providers.TTSProvider, error) {
		return NewTTS(options), nil
	})
}
