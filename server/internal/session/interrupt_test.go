package session

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/observability"
	"stackchan-gateway/internal/providers"
)

func TestAbortDuringLLMPreventsTTSRequest(t *testing.T) {
	countingTTS := &countingTTSProvider{}
	loop, downlink, llm, _ := newInterruptLoop(t, &blockingLLMProvider{}, countingTTS)
	ctx := context.Background()

	startListeningAndFeedAudio(t, loop, ctx)
	done := runListenStop(t, loop, ctx)
	waitForSignal(t, llm.started, "LLM start")

	if _, err := loop.HandleAbort(ctx); err != nil {
		t.Fatalf("HandleAbort() error = %v", err)
	}
	waitForDone(t, done, "listen stop after abort")

	if countingTTS.StreamCount() != 0 {
		t.Fatalf("TTS stream count = %d, want 0", countingTTS.StreamCount())
	}
	if downlink.CountTTSStop() != 0 {
		t.Fatalf("tts stop count = %d, want 0 before TTS start", downlink.CountTTSStop())
	}
}

func TestAbortDuringTTSStopsOldGenerationAudio(t *testing.T) {
	tts := newBlockingTTSProvider()
	loop, downlink, _, _ := newInterruptLoop(t, immediateLLMProvider{}, tts)
	ctx := context.Background()

	startListeningAndFeedAudio(t, loop, ctx)
	done := runListenStop(t, loop, ctx)
	waitForSignal(t, tts.firstFrameSent, "first TTS frame")
	waitForBinaryCount(t, downlink, 1)

	if _, err := loop.HandleAbort(ctx); err != nil {
		t.Fatalf("HandleAbort() error = %v", err)
	}
	tts.ReleaseExtraFrame()
	waitForDone(t, done, "listen stop after TTS abort")

	if downlink.CountBinary() != 1 {
		t.Fatalf("binary frame count = %d, want only first old-generation frame", downlink.CountBinary())
	}
	if downlink.CountTTSStop() != 1 {
		t.Fatalf("tts stop count = %d, want 1", downlink.CountTTSStop())
	}
}

func TestNewListenStartDuringSpeakingSendsSingleTTSStop(t *testing.T) {
	tts := newBlockingTTSProvider()
	loop, downlink, _, _ := newInterruptLoop(t, immediateLLMProvider{}, tts)
	ctx := context.Background()

	startListeningAndFeedAudio(t, loop, ctx)
	done := runListenStop(t, loop, ctx)
	waitForSignal(t, tts.firstFrameSent, "first TTS frame")
	waitForBinaryCount(t, downlink, 1)

	turn, err := loop.HandleListenStart(ctx)
	if err != nil {
		t.Fatalf("HandleListenStart() during speaking error = %v", err)
	}
	if turn.State != StateListening {
		t.Fatalf("new listen state = %s, want %s", turn.State, StateListening)
	}
	if turn.Generation != 2 {
		t.Fatalf("new listen generation = %d, want 2", turn.Generation)
	}

	tts.ReleaseExtraFrame()
	waitForDone(t, done, "old listen stop after barge-in")

	if downlink.CountTTSStop() != 1 {
		t.Fatalf("tts stop count = %d, want 1", downlink.CountTTSStop())
	}
	if downlink.CountBinary() != 1 {
		t.Fatalf("binary frame count = %d, want only old first frame", downlink.CountBinary())
	}
}

func TestNewListenStartStopsOldTTSBeforeProviderResolve(t *testing.T) {
	tts := newBlockingTTSProvider()
	resolver := newBlockingSecondResolveProviderSet(immediateLLMProvider{}, tts)
	downlink := &recordingDownlink{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:          New("sess_interrupt_resolve", "stackchan-s3-main", "client-1"),
		ProviderResolver: resolver,
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	ctx := context.Background()
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	startListeningAndFeedAudio(t, loop, ctx)
	done := runListenStop(t, loop, ctx)
	waitForSignal(t, tts.firstFrameSent, "first TTS frame")
	waitForBinaryCount(t, downlink, 1)

	secondListenDone := make(chan error, 1)
	go func() {
		_, err := loop.HandleListenStart(ctx)
		secondListenDone <- err
	}()
	waitForSignal(t, resolver.secondStarted, "second provider resolve")
	defer resolver.Release()

	waitForTTSStopCount(t, downlink, 1, "barge-in before provider resolve returns")
	resolver.Release()
	waitForDone(t, secondListenDone, "second listen start")
	tts.ReleaseExtraFrame()
	waitForDone(t, done, "old listen stop after resolver-delayed barge-in")
}

func TestListenStartProviderResolveFailureResetsForNextListen(t *testing.T) {
	resolver := &failingOnceResolver{
		err: errors.New("provider profile unavailable"),
		set: VoiceProviderSet{
			Profile: "recovered",
			ASRName: "mock-asr",
			LLMName: "test-llm",
			TTSName: "test-tts",
			ASR:     providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
			LLM:     immediateLLMProvider{},
			TTS:     &countingTTSProvider{frameCount: 1},
		},
	}
	traceOutput := &lockedBuffer{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:          New("sess_listen_start_resolve_failure", "stackchan-s3-main", "client-1"),
		ProviderResolver: resolver,
		Downlink:         &recordingDownlink{},
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:    observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput, RedactSecrets: true}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	ctx := context.Background()
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	if _, err := loop.HandleListenStart(ctx); err == nil {
		t.Fatalf("HandleListenStart() error = nil, want provider resolve error")
	}
	if got := loop.session.State(); got != StateInterrupted {
		t.Fatalf("session state after provider failure = %s, want %s", got, StateInterrupted)
	}
	if got := loop.session.CurrentGeneration(); got != 2 {
		t.Fatalf("generation after provider failure = %d, want 2", got)
	}

	turn, err := loop.HandleListenStart(ctx)
	if err != nil {
		t.Fatalf("second HandleListenStart() error = %v", err)
	}
	if turn.Generation != 2 || turn.State != StateListening {
		t.Fatalf("second listen turn = %+v, want generation 2 listening", turn)
	}
	events := decodeTraceEvents(t, traceOutput.Bytes())
	if !traceHasTurnCompleteError(events, "provider_profile_resolve_failed") {
		t.Fatalf("trace missing provider_profile_resolve_failed turn_complete; events=%v", traceEventNames(events))
	}
}

func TestListenStartASRStartFailureResetsForNextListen(t *testing.T) {
	session := New("sess_listen_start_asr_failure", "stackchan-s3-main", "client-1")
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:  session,
		ASR:      failingStartASRProvider{err: errors.New("asr start failed")},
		LLM:      immediateLLMProvider{},
		TTS:      &countingTTSProvider{frameCount: 1},
		Downlink: &recordingDownlink{},
		Pacer:    audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	ctx := context.Background()
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	if _, err := loop.HandleListenStart(ctx); err == nil {
		t.Fatalf("HandleListenStart() error = nil, want ASR start error")
	}
	if got := session.State(); got != StateInterrupted {
		t.Fatalf("session state after ASR failure = %s, want %s", got, StateInterrupted)
	}
	if got := session.CurrentGeneration(); got != 2 {
		t.Fatalf("generation after ASR failure = %d, want 2", got)
	}

	if err := loop.AcceptOpus(audio.NewOpusFrame([]byte{0x01}, 16000, 60, time.Now())); !errors.Is(err, ErrASRStreamNotStarted) {
		t.Fatalf("AcceptOpus() after ASR failure = %v, want ErrASRStreamNotStarted", err)
	}
}

func TestNewListenStartDuringSpeakingTracesBargeInStopLatency(t *testing.T) {
	tts := newBlockingTTSProvider()
	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       New("sess_interrupt_trace", "stackchan-s3-main", "client-1"),
		ASR:           providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
		LLM:           immediateLLMProvider{},
		TTS:           tts,
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput, RedactSecrets: true}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	ctx := context.Background()
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	startListeningAndFeedAudio(t, loop, ctx)
	done := runListenStop(t, loop, ctx)
	waitForSignal(t, tts.firstFrameSent, "first TTS frame")
	waitForBinaryCount(t, downlink, 1)

	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() during speaking error = %v", err)
	}
	tts.ReleaseExtraFrame()
	waitForDone(t, done, "old listen stop after barge-in")

	events := decodeTraceEvents(t, traceOutput.Bytes())
	stopEvent := findTraceEventByFields(t, events, "tts_stop_sent", map[string]any{
		"reason": "listen_start",
	})
	latencyMS := traceFieldInt(stopEvent.Fields, "stop_latency_ms")
	if latencyMS <= 0 || latencyMS > 350 {
		t.Fatalf("stop_latency_ms = %d, want 1..350; event=%+v", latencyMS, stopEvent)
	}
}

func TestAbortAfterNormalTTSStopDoesNotDuplicateStop(t *testing.T) {
	loop, downlink, _, _ := newInterruptLoop(t, immediateLLMProvider{}, &countingTTSProvider{frameCount: 1})
	ctx := context.Background()

	startListeningAndFeedAudio(t, loop, ctx)
	if err := loop.HandleListenStop(ctx); err != nil {
		t.Fatalf("HandleListenStop() error = %v", err)
	}
	if downlink.CountTTSStop() != 1 {
		t.Fatalf("normal tts stop count = %d, want 1", downlink.CountTTSStop())
	}

	if _, err := loop.HandleAbort(ctx); err != nil {
		t.Fatalf("HandleAbort() after complete error = %v", err)
	}
	if downlink.CountTTSStop() != 1 {
		t.Fatalf("post-abort tts stop count = %d, want still 1", downlink.CountTTSStop())
	}
}

func TestTTSStreamErrorAfterStartSendsStop(t *testing.T) {
	loop, downlink, _, _ := newInterruptLoop(t, immediateLLMProvider{}, failingTTSProvider{})
	ctx := context.Background()

	startListeningAndFeedAudio(t, loop, ctx)
	err := loop.HandleListenStop(ctx)
	if err == nil {
		t.Fatalf("HandleListenStop() error = nil, want TTS stream error")
	}
	if downlink.CountTTSStop() != 1 {
		t.Fatalf("tts stop count = %d, want stop after tts/start on provider error", downlink.CountTTSStop())
	}
}

func TestAbortTraceIncludesAbortReceivedAndTurnComplete(t *testing.T) {
	var traceOutput bytes.Buffer
	downlink := &recordingDownlink{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       New("sess_abort_trace", "stackchan-s3-main", "client-1"),
		ASR:           providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
		LLM:           immediateLLMProvider{},
		TTS:           &countingTTSProvider{frameCount: 1},
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: &traceOutput}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	ctx := context.Background()
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	if _, err := loop.HandleAbort(ctx); err != nil {
		t.Fatalf("HandleAbort() error = %v", err)
	}

	events := decodeTraceEvents(t, traceOutput.Bytes())
	assertTraceContainsEvents(t, events, []string{"abort_received", "turn_complete"})
	if !traceHasTurnCompleteError(events, "aborted") {
		t.Fatalf("trace missing aborted turn_complete; events=%v", traceEventNames(events))
	}
}

func newInterruptLoop(t *testing.T, llm providers.LLMProvider, tts providers.TTSProvider) (*VoiceLoop, *recordingDownlink, *blockingLLMProvider, providers.TTSProvider) {
	t.Helper()

	blocking, _ := llm.(*blockingLLMProvider)
	if blocking != nil && blocking.started == nil {
		blocking.started = make(chan struct{})
	}

	asr := providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1})
	downlink := &recordingDownlink{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:  New("sess_interrupt", "stackchan-s3-main", "client-1"),
		ASR:      asr,
		LLM:      llm,
		TTS:      tts,
		Downlink: downlink,
		Pacer:    audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(context.Background()); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	return loop, downlink, blocking, tts
}

func startListeningAndFeedAudio(t *testing.T, loop *VoiceLoop, ctx context.Context) {
	t.Helper()

	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	frame := audio.NewOpusFrame([]byte{0x01, 0x02}, 16000, 60, time.Now())
	if err := loop.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}
}

func runListenStop(t *testing.T, loop *VoiceLoop, ctx context.Context) <-chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- loop.HandleListenStop(ctx)
	}()
	return done
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitForDone(t *testing.T, ch <-chan error, label string) {
	t.Helper()

	select {
	case err := <-ch:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s returned error: %v", label, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitForBinaryCount(t *testing.T, downlink *recordingDownlink, want int) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if downlink.CountBinary() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("binary frame count = %d, want at least %d", downlink.CountBinary(), want)
}

func waitForTTSStopCount(t *testing.T, downlink *recordingDownlink, want int, label string) {
	t.Helper()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if downlink.CountTTSStop() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("tts stop count = %d, want at least %d for %s", downlink.CountTTSStop(), want, label)
}

func traceHasTurnCompleteError(events []observability.TraceEvent, errorCode string) bool {
	for _, event := range events {
		if event.Event == "turn_complete" && event.ErrorCode == errorCode {
			return true
		}
	}
	return false
}

func traceFieldInt(fields map[string]any, key string) int {
	switch value := fields[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

type blockingLLMProvider struct {
	started chan struct{}
}

func (p *blockingLLMProvider) Stream(ctx context.Context, req providers.LLMRequest) (<-chan providers.LLMChunk, error) {
	if p.started == nil {
		p.started = make(chan struct{})
	}
	out := make(chan providers.LLMChunk)
	go func() {
		defer close(out)
		close(p.started)
		<-ctx.Done()
	}()
	return out, nil
}

type immediateLLMProvider struct{}

func (p immediateLLMProvider) Stream(ctx context.Context, req providers.LLMRequest) (<-chan providers.LLMChunk, error) {
	out := make(chan providers.LLMChunk, 1)
	out <- providers.LLMChunk{Text: "我在。", IsFinal: true, CreatedAt: time.Now()}
	close(out)
	return out, nil
}

type failingTTSProvider struct{}

func (p failingTTSProvider) Stream(ctx context.Context, req providers.TTSRequest) (<-chan providers.TTSFrame, error) {
	return nil, errors.New("tts provider failed")
}

type blockingSecondResolveProviderSet struct {
	mu            sync.Mutex
	calls         int
	set           VoiceProviderSet
	secondStarted chan struct{}
	release       chan struct{}
	releaseOnce   sync.Once
}

func newBlockingSecondResolveProviderSet(llm providers.LLMProvider, tts providers.TTSProvider) *blockingSecondResolveProviderSet {
	return &blockingSecondResolveProviderSet{
		set: VoiceProviderSet{
			Profile: "blocking-resolver",
			ASRName: "mock-asr",
			LLMName: "test-llm",
			TTSName: "test-tts",
			ASR:     providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
			LLM:     llm,
			TTS:     tts,
		},
		secondStarted: make(chan struct{}),
		release:       make(chan struct{}),
	}
}

func (r *blockingSecondResolveProviderSet) ResolveVoiceProviders(ctx context.Context, deviceID string) (VoiceProviderSet, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()

	if call == 2 {
		close(r.secondStarted)
		select {
		case <-ctx.Done():
			return VoiceProviderSet{}, ctx.Err()
		case <-r.release:
		}
	}
	return r.set, nil
}

func (r *blockingSecondResolveProviderSet) Release() {
	r.releaseOnce.Do(func() {
		close(r.release)
	})
}

type failingOnceResolver struct {
	mu    sync.Mutex
	calls int
	err   error
	set   VoiceProviderSet
}

func (r *failingOnceResolver) ResolveVoiceProviders(ctx context.Context, deviceID string) (VoiceProviderSet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return VoiceProviderSet{}, r.err
	}
	return r.set, nil
}

type failingStartASRProvider struct {
	err error
}

func (p failingStartASRProvider) Start(context.Context, providers.ASRStartRequest) (providers.ASRStream, error) {
	return nil, p.err
}

type countingTTSProvider struct {
	mu         sync.Mutex
	streams    int
	frameCount int
}

func (p *countingTTSProvider) Stream(ctx context.Context, req providers.TTSRequest) (<-chan providers.TTSFrame, error) {
	p.mu.Lock()
	p.streams++
	frameCount := p.frameCount
	p.mu.Unlock()

	if frameCount <= 0 {
		frameCount = 1
	}

	out := make(chan providers.TTSFrame, frameCount)
	for i := 0; i < frameCount; i++ {
		out <- providers.TTSFrame{
			Generation: req.Generation,
			Opus:       []byte{0xf8, byte(i)},
			TextSpan:   req.Text,
			Duration:   60 * time.Millisecond,
			CreatedAt:  time.Now(),
		}
	}
	close(out)
	return out, nil
}

func (p *countingTTSProvider) StreamCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.streams
}

type blockingTTSProvider struct {
	firstFrameSent chan struct{}
	release        chan struct{}
	firstFrameOnce sync.Once
	releaseOnce    sync.Once
}

func newBlockingTTSProvider() *blockingTTSProvider {
	return &blockingTTSProvider{
		firstFrameSent: make(chan struct{}),
		release:        make(chan struct{}),
	}
}

func (p *blockingTTSProvider) Stream(ctx context.Context, req providers.TTSRequest) (<-chan providers.TTSFrame, error) {
	out := make(chan providers.TTSFrame)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
			return
		case out <- providers.TTSFrame{
			Generation: req.Generation,
			Opus:       []byte{0xf8, 0x01},
			TextSpan:   req.Text,
			Duration:   60 * time.Millisecond,
			CreatedAt:  time.Now(),
		}:
			p.firstFrameOnce.Do(func() {
				close(p.firstFrameSent)
			})
		}

		select {
		case <-ctx.Done():
			return
		case <-p.release:
		}

		select {
		case <-ctx.Done():
		case out <- providers.TTSFrame{
			Generation: req.Generation,
			Opus:       []byte{0xf8, 0x02},
			TextSpan:   req.Text,
			Duration:   60 * time.Millisecond,
			CreatedAt:  time.Now(),
		}:
		}
	}()
	return out, nil
}

func (p *blockingTTSProvider) ReleaseExtraFrame() {
	p.releaseOnce.Do(func() {
		close(p.release)
	})
}
