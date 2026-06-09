package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"stackchan-gateway/internal/audio"
)

func TestRegistryReturnsMockProvidersByName(t *testing.T) {
	registry := NewRegistry(MockConfig{})

	if _, err := registry.ASRProvider("mock"); err != nil {
		t.Fatalf("ASRProvider(mock) error = %v", err)
	}
	if _, err := registry.LLMProvider(" MOCK "); err != nil {
		t.Fatalf("LLMProvider(mock) error = %v", err)
	}
	if _, err := registry.TTSProvider("Mock"); err != nil {
		t.Fatalf("TTSProvider(mock) error = %v", err)
	}

	if _, err := registry.ASRProvider("dashscope"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("ASRProvider(dashscope) error = %v, want ErrProviderNotFound", err)
	}
}

func TestRegistryAllowsExternalLLMFactories(t *testing.T) {
	registry := NewRegistry(MockConfig{})
	want := NewMockLLMProvider(MockConfig{LLMFirstTokenDelayMS: 1})

	registry.RegisterLLM("dashscope-llm", func() (LLMProvider, error) {
		return want, nil
	})

	got, err := registry.LLMProvider(" DASHSCOPE-LLM ")
	if err != nil {
		t.Fatalf("LLMProvider(dashscope-llm) error = %v", err)
	}
	if got != want {
		t.Fatalf("LLMProvider returned %#v, want %#v", got, want)
	}
}

func TestMockASRFinalArrivesAfterFinish(t *testing.T) {
	provider := NewMockASRProvider(MockConfig{ASRFinalDelayMS: 5})
	stream, err := provider.Start(context.Background(), ASRStartRequest{
		SessionID:  "sess_test",
		DeviceID:   "stackchan-s3-main",
		Generation: 7,
		StartedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer stream.Close()

	frame := audio.NewOpusFrame([]byte{0x01, 0x02}, 16000, 60, time.Now())
	if err := stream.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}

	select {
	case event, ok := <-stream.Events():
		t.Fatalf("event before Finish() = %+v, ok = %v", event, ok)
	case <-time.After(10 * time.Millisecond):
	}

	if err := stream.Finish(); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("events closed before final event")
		}
		if event.Type != ASREventFinal || !event.IsFinal {
			t.Fatalf("event = %+v, want final", event)
		}
		if event.Text != "你好，我是 StackChan。" {
			t.Fatalf("event text = %q", event.Text)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for ASR final event")
	}
}

func TestMockASRUsesConfiguredFinalText(t *testing.T) {
	provider := NewMockASRProvider(MockConfig{
		ASRFinalDelayMS: 1,
		ASRFinalText:    "切到字节模型。",
	})
	stream, err := provider.Start(context.Background(), ASRStartRequest{
		SessionID:  "sess_test",
		DeviceID:   "stackchan-s3-main",
		Generation: 7,
		StartedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer stream.Close()

	frame := audio.NewOpusFrame([]byte{0x01, 0x02}, 16000, 60, time.Now())
	if err := stream.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}
	if err := stream.Finish(); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("events closed before final event")
		}
		if event.Text != "切到字节模型。" {
			t.Fatalf("event text = %q, want configured final text", event.Text)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for ASR final event")
	}
}

func TestMockASRFinalCanArriveAfterAudioWithoutFinish(t *testing.T) {
	provider := NewMockASRProvider(MockConfig{ASRFinalDelayMS: 5, ASRAutoFinalOnAudio: true})
	stream, err := provider.Start(context.Background(), ASRStartRequest{
		SessionID:  "sess_test",
		DeviceID:   "stackchan-s3-main",
		Generation: 7,
		StartedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer stream.Close()

	frame := audio.NewOpusFrame([]byte{0x01, 0x02}, 16000, 60, time.Now())
	if err := stream.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("events closed before final event")
		}
		if event.Type != ASREventFinal || !event.IsFinal {
			t.Fatalf("event = %+v, want final", event)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for ASR final event without Finish()")
	}
}

func TestMockLLMChunksAreOrdered(t *testing.T) {
	provider := NewMockLLMProvider(MockConfig{LLMFirstTokenDelayMS: 1})
	chunks, err := provider.Stream(context.Background(), LLMRequest{
		SessionID:  "sess_test",
		DeviceID:   "stackchan-s3-main",
		Generation: 3,
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var got []LLMChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}

	if len(got) != 2 {
		t.Fatalf("chunks len = %d, want 2", len(got))
	}
	if got[0].Text != "你好，" || got[0].IsFinal {
		t.Fatalf("first chunk = %+v", got[0])
	}
	if got[1].Text != "我准备好了。" || !got[1].IsFinal {
		t.Fatalf("second chunk = %+v", got[1])
	}
}

func TestMockTTSFramesCarryRequestedGeneration(t *testing.T) {
	provider := NewMockTTSProvider(MockConfig{
		TTSFirstFrameDelayMS: 1,
		TTSFrameCount:        3,
	})
	frames, err := provider.Stream(context.Background(), TTSRequest{
		SessionID:  "sess_test",
		DeviceID:   "stackchan-s3-main",
		Generation: 42,
		Text:       "你好，我准备好了。",
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	count := 0
	for frame := range frames {
		count++
		if frame.Generation != 42 {
			t.Fatalf("frame generation = %d, want 42", frame.Generation)
		}
		if len(frame.Opus) == 0 {
			t.Fatalf("frame Opus payload is empty")
		}
		if frame.Duration != mockTTSFrameDuration {
			t.Fatalf("frame duration = %s, want %s", frame.Duration, mockTTSFrameDuration)
		}
	}

	if count != 3 {
		t.Fatalf("frame count = %d, want 3", count)
	}
}

func TestContextCancellationStopsStreams(t *testing.T) {
	t.Run("asr", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		provider := NewMockASRProvider(MockConfig{ASRFinalDelayMS: 50})
		stream, err := provider.Start(ctx, ASRStartRequest{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if err := stream.Finish(); err != nil {
			t.Fatalf("Finish() error = %v", err)
		}
		cancel()

		select {
		case _, ok := <-stream.Events():
			if ok {
				t.Fatal("ASR stream emitted event after cancellation")
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timed out waiting for ASR stream to close")
		}
	})

	t.Run("llm", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		provider := NewMockLLMProvider(MockConfig{LLMFirstTokenDelayMS: 50})
		chunks, err := provider.Stream(ctx, LLMRequest{})
		if err != nil {
			t.Fatalf("Stream() error = %v", err)
		}
		cancel()

		select {
		case _, ok := <-chunks:
			if ok {
				t.Fatal("LLM stream emitted chunk after cancellation")
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timed out waiting for LLM stream to close")
		}
	})

	t.Run("tts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		provider := NewMockTTSProvider(MockConfig{TTSFirstFrameDelayMS: 50})
		frames, err := provider.Stream(ctx, TTSRequest{})
		if err != nil {
			t.Fatalf("Stream() error = %v", err)
		}
		cancel()

		select {
		case _, ok := <-frames:
			if ok {
				t.Fatal("TTS stream emitted frame after cancellation")
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timed out waiting for TTS stream to close")
		}
	})
}
