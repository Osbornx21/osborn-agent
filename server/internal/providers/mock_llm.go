package providers

import (
	"context"
	"time"
)

type MockLLMProvider struct {
	config MockConfig
}

func NewMockLLMProvider(config MockConfig) *MockLLMProvider {
	return &MockLLMProvider{config: config}
}

func (p *MockLLMProvider) Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	out := make(chan LLMChunk)
	go func() {
		defer close(out)

		if !waitForContext(ctx, p.config.LLMFirstTokenDelay()) {
			return
		}

		chunks := []LLMChunk{
			{
				Text:      "你好，",
				Emotion:   "warm",
				CreatedAt: time.Now(),
			},
			{
				Text:      "我准备好了。",
				Emotion:   "ready",
				IsFinal:   true,
				CreatedAt: time.Now(),
			},
		}

		for _, chunk := range chunks {
			if !sendLLMChunk(ctx, out, chunk) {
				return
			}
		}
	}()

	return out, nil
}

func sendLLMChunk(ctx context.Context, out chan<- LLMChunk, chunk LLMChunk) bool {
	if chunk.CreatedAt.IsZero() {
		chunk.CreatedAt = time.Now()
	}
	select {
	case <-ctx.Done():
		return false
	case out <- chunk:
		return true
	}
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
