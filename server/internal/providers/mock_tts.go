package providers

import (
	"context"
	"time"
)

const mockTTSFrameDuration = 60 * time.Millisecond

type MockTTSProvider struct {
	config MockConfig
}

func NewMockTTSProvider(config MockConfig) *MockTTSProvider {
	return &MockTTSProvider{config: config}
}

func (p *MockTTSProvider) Stream(ctx context.Context, req TTSRequest) (<-chan TTSFrame, error) {
	out := make(chan TTSFrame)
	go func() {
		defer close(out)

		if !waitForContext(ctx, p.config.TTSFirstFrameDelay()) {
			return
		}

		frameCount := p.config.EffectiveTTSFrameCount()
		for index := 0; index < frameCount; index++ {
			frame := TTSFrame{
				Generation: req.Generation,
				Opus:       mockOpusPayload(req.Generation, index),
				TextSpan:   req.Text,
				Duration:   mockTTSFrameDuration,
				CreatedAt:  time.Now(),
			}
			if !sendTTSFrame(ctx, out, frame) {
				return
			}
		}
	}()

	return out, nil
}

func mockOpusPayload(generation int64, index int) []byte {
	return []byte{
		0xf8,
		0xff,
		0xfe,
		byte(generation),
		byte(index),
		byte(generation >> 8),
	}
}

func sendTTSFrame(ctx context.Context, out chan<- TTSFrame, frame TTSFrame) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- frame:
		return true
	}
}
