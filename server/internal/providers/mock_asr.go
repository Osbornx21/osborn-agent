package providers

import (
	"context"
	"errors"
	"sync"
	"time"

	"stackchan-gateway/internal/audio"
)

var (
	ErrStreamClosed   = errors.New("provider stream closed")
	ErrStreamFinished = errors.New("provider stream already finished")
)

type MockASRProvider struct {
	config MockConfig
}

func NewMockASRProvider(config MockConfig) *MockASRProvider {
	return &MockASRProvider{config: config}
}

func (p *MockASRProvider) Start(ctx context.Context, req ASRStartRequest) (ASRStream, error) {
	if req.StartedAt.IsZero() {
		req.StartedAt = time.Now()
	}
	if req.FinalText == "" {
		req.FinalText = p.config.EffectiveASRFinalText()
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := &mockASRStream{
		ctx:              streamCtx,
		cancel:           cancel,
		req:              req,
		delay:            p.config.ASRFinalDelay(),
		autoFinalOnAudio: p.config.ASRAutoFinalOnAudio,
		events:           make(chan ASREvent, 1),
		finalCh:          make(chan struct{}),
		finalOnce:        sync.Once{},
		closeOnce:        sync.Once{},
	}
	go stream.run()

	return stream, nil
}

type mockASRStream struct {
	ctx              context.Context
	cancel           context.CancelFunc
	req              ASRStartRequest
	delay            time.Duration
	autoFinalOnAudio bool
	events           chan ASREvent
	finalCh          chan struct{}

	mu        sync.Mutex
	closed    bool
	finished  bool
	frameSeen int

	finalOnce sync.Once
	closeOnce sync.Once
}

func (s *mockASRStream) AcceptOpus(frame audio.Frame) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStreamClosed
	}
	if s.finished {
		s.mu.Unlock()
		return ErrStreamFinished
	}
	s.frameSeen++
	shouldAutoFinal := s.autoFinalOnAudio && s.frameSeen == 1
	s.mu.Unlock()

	if shouldAutoFinal {
		s.triggerFinal()
	}
	return nil
}

func (s *mockASRStream) Finish() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStreamClosed
	}
	if s.finished {
		s.mu.Unlock()
		return nil
	}
	s.finished = true
	s.mu.Unlock()

	s.triggerFinal()
	return nil
}

func (s *mockASRStream) Events() <-chan ASREvent {
	return s.events
}

func (s *mockASRStream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	s.closeOnce.Do(s.cancel)
	return nil
}

func (s *mockASRStream) triggerFinal() {
	s.finalOnce.Do(func() {
		close(s.finalCh)
	})
}

func (s *mockASRStream) run() {
	defer close(s.events)

	select {
	case <-s.ctx.Done():
		return
	case <-s.finalCh:
	}

	timer := time.NewTimer(s.delay)
	defer timer.Stop()

	select {
	case <-s.ctx.Done():
		return
	case <-timer.C:
	}

	event := ASREvent{
		Type:       ASREventFinal,
		Text:       s.req.FinalText,
		IsFinal:    true,
		StartedAt:  s.req.StartedAt,
		FinishedAt: time.Now(),
	}

	select {
	case <-s.ctx.Done():
	case s.events <- event:
	}
}
