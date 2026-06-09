package app

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/session"
)

func TestXiaozhiIdleKeepaliveSendsNeutralLLMOnlyWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sender := &recordingKeepaliveSender{messages: make(chan any, 4)}
	var state atomic.Value
	state.Store(session.StateProcessing)
	startXiaozhiIdleKeepalive(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), sender, "sess_keepalive", 5*time.Millisecond, func() session.State {
		return state.Load().(session.State)
	})

	select {
	case msg := <-sender.messages:
		t.Fatalf("unexpected keepalive while processing: %#v", msg)
	case <-time.After(25 * time.Millisecond):
	}

	state.Store(session.StateIdle)
	select {
	case msg := <-sender.messages:
		llm, ok := msg.(xiaozhi.LLMMessage)
		if !ok {
			t.Fatalf("keepalive message = %#v, want xiaozhi.LLMMessage", msg)
		}
		if llm.Type != xiaozhi.MessageTypeLLM || llm.SessionID != "sess_keepalive" || llm.Emotion != "neutral" {
			t.Fatalf("keepalive message = %+v, want llm neutral for session", llm)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for idle keepalive")
	}
}

type recordingKeepaliveSender struct {
	messages chan any
}

func (s *recordingKeepaliveSender) SendJSON(_ context.Context, msg any) error {
	s.messages <- msg
	return nil
}
