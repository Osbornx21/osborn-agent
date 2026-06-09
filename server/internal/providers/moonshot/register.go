package moonshot

import "stackchan-gateway/internal/providers"

type LLMRegistrar interface {
	RegisterLLM(name string, factory providers.LLMFactory)
}

func RegisterLLM(registry LLMRegistrar, options LLMOptions) {
	registry.RegisterLLM(ProviderIDLLM, func() (providers.LLMProvider, error) {
		return NewLLM(options), nil
	})
}
