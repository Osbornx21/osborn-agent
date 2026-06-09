package session

import "testing"

func TestSessionHelloToIdleStartsGenerationOne(t *testing.T) {
	s := New("sess_1", "stackchan-s3-main", "client-1")

	if s.State() != StateConnected {
		t.Fatalf("initial state = %s, want %s", s.State(), StateConnected)
	}
	if s.CurrentGeneration() != 0 {
		t.Fatalf("initial generation = %d, want 0", s.CurrentGeneration())
	}

	if err := s.ReceiveHello(); err != nil {
		t.Fatalf("ReceiveHello() error = %v", err)
	}
	if s.State() != StateHelloReceived {
		t.Fatalf("state = %s, want %s", s.State(), StateHelloReceived)
	}

	turn, err := s.ServerHelloSent()
	if err != nil {
		t.Fatalf("ServerHelloSent() error = %v", err)
	}
	if turn.State != StateIdle {
		t.Fatalf("turn state = %s, want %s", turn.State, StateIdle)
	}
	if turn.Generation != 1 {
		t.Fatalf("generation = %d, want 1", turn.Generation)
	}
}

func TestSessionListenStopAndFirstTTSFrameTransitions(t *testing.T) {
	s := readySession(t)

	turn, err := s.StartListening()
	if err != nil {
		t.Fatalf("StartListening() error = %v", err)
	}
	if turn.State != StateListening {
		t.Fatalf("state = %s, want %s", turn.State, StateListening)
	}

	turn, err = s.StopListening()
	if err != nil {
		t.Fatalf("StopListening() error = %v", err)
	}
	if turn.State != StateProcessing {
		t.Fatalf("state = %s, want %s", turn.State, StateProcessing)
	}

	turn, err = s.BeginSpeaking(turn.Generation)
	if err != nil {
		t.Fatalf("BeginSpeaking() error = %v", err)
	}
	if turn.State != StateSpeaking {
		t.Fatalf("state = %s, want %s", turn.State, StateSpeaking)
	}
}

func TestSessionSpeakingAbortTransitionsToInterruptedAndRejectsOldGeneration(t *testing.T) {
	s := speakingSession(t)
	oldGeneration := s.CurrentGeneration()

	turn := s.Abort()

	if turn.State != StateInterrupted {
		t.Fatalf("state = %s, want %s", turn.State, StateInterrupted)
	}
	if turn.Generation != oldGeneration+1 {
		t.Fatalf("generation = %d, want %d", turn.Generation, oldGeneration+1)
	}
	if s.AcceptsGeneration(oldGeneration) {
		t.Fatalf("old generation %d accepted after abort", oldGeneration)
	}
	if !s.AcceptsGeneration(turn.Generation) {
		t.Fatalf("current generation %d rejected after abort", turn.Generation)
	}
}

func TestSessionNewListenWhileProcessingIncrementsGeneration(t *testing.T) {
	s := readySession(t)

	if _, err := s.StartListening(); err != nil {
		t.Fatalf("StartListening() error = %v", err)
	}
	turn, err := s.StopListening()
	if err != nil {
		t.Fatalf("StopListening() error = %v", err)
	}
	oldGeneration := turn.Generation

	turn, err = s.StartListening()
	if err != nil {
		t.Fatalf("StartListening() while processing error = %v", err)
	}
	if turn.State != StateListening {
		t.Fatalf("state = %s, want %s", turn.State, StateListening)
	}
	if turn.Generation != oldGeneration+1 {
		t.Fatalf("generation = %d, want %d", turn.Generation, oldGeneration+1)
	}
}

func TestSessionNewListenWhileSpeakingIncrementsGeneration(t *testing.T) {
	s := speakingSession(t)
	oldGeneration := s.CurrentGeneration()

	turn, err := s.StartListening()
	if err != nil {
		t.Fatalf("StartListening() while speaking error = %v", err)
	}
	if turn.State != StateListening {
		t.Fatalf("state = %s, want %s", turn.State, StateListening)
	}
	if turn.Generation != oldGeneration+1 {
		t.Fatalf("generation = %d, want %d", turn.Generation, oldGeneration+1)
	}
}

func TestSessionNewListenAfterCompletedTurnIncrementsGeneration(t *testing.T) {
	s := speakingSession(t)
	oldGeneration := s.CurrentGeneration()

	turn, err := s.CompleteSpeaking(oldGeneration)
	if err != nil {
		t.Fatalf("CompleteSpeaking() error = %v", err)
	}
	if turn.State != StateIdle {
		t.Fatalf("state = %s, want %s", turn.State, StateIdle)
	}

	turn, err = s.StartListening()
	if err != nil {
		t.Fatalf("StartListening() after idle complete error = %v", err)
	}
	if turn.State != StateListening {
		t.Fatalf("state = %s, want %s", turn.State, StateListening)
	}
	if turn.Generation != oldGeneration+1 {
		t.Fatalf("generation = %d, want %d", turn.Generation, oldGeneration+1)
	}
}

func TestSessionNewListenAfterAbortUsesInterruptedGeneration(t *testing.T) {
	s := speakingSession(t)
	oldGeneration := s.CurrentGeneration()

	interrupted := s.Abort()
	if interrupted.Generation != oldGeneration+1 {
		t.Fatalf("abort generation = %d, want %d", interrupted.Generation, oldGeneration+1)
	}

	turn, err := s.StartListening()
	if err != nil {
		t.Fatalf("StartListening() after abort error = %v", err)
	}
	if turn.Generation != interrupted.Generation {
		t.Fatalf("generation = %d, want interrupted generation %d", turn.Generation, interrupted.Generation)
	}
}

func TestSessionProviderFatalResetIncrementsGeneration(t *testing.T) {
	s := speakingSession(t)
	oldGeneration := s.CurrentGeneration()

	turn := s.ProviderFatalReset()

	if turn.State != StateInterrupted {
		t.Fatalf("state = %s, want %s", turn.State, StateInterrupted)
	}
	if turn.Generation != oldGeneration+1 {
		t.Fatalf("generation = %d, want %d", turn.Generation, oldGeneration+1)
	}
}

func TestSessionProviderFatalResetForGenerationOnlyResetsCurrent(t *testing.T) {
	s := speakingSession(t)
	currentGeneration := s.CurrentGeneration()

	turn, didReset := s.ProviderFatalResetForGeneration(currentGeneration - 1)
	if didReset {
		t.Fatalf("ProviderFatalResetForGeneration(stale) didReset = true")
	}
	if turn.Generation != currentGeneration || turn.State != StateSpeaking {
		t.Fatalf("stale reset turn = %+v, want current speaking generation %d", turn, currentGeneration)
	}

	turn, didReset = s.ProviderFatalResetForGeneration(currentGeneration)
	if !didReset {
		t.Fatalf("ProviderFatalResetForGeneration(current) didReset = false")
	}
	if turn.Generation != currentGeneration+1 || turn.State != StateInterrupted {
		t.Fatalf("current reset turn = %+v, want interrupted generation %d", turn, currentGeneration+1)
	}
}

func TestSessionBeginSpeakingRejectsOldGeneration(t *testing.T) {
	s := readySession(t)
	if _, err := s.StartListening(); err != nil {
		t.Fatalf("StartListening() error = %v", err)
	}
	turn, err := s.StopListening()
	if err != nil {
		t.Fatalf("StopListening() error = %v", err)
	}

	_, err = s.BeginSpeaking(turn.Generation - 1)

	if err == nil {
		t.Fatal("BeginSpeaking(old generation) error = nil, want error")
	}
	if s.State() != StateProcessing {
		t.Fatalf("state after stale generation = %s, want %s", s.State(), StateProcessing)
	}
}

func TestSessionCompleteListeningNoSpeechReturnsIdleWithoutGenerationBump(t *testing.T) {
	s := readySession(t)
	turn, err := s.StartListening()
	if err != nil {
		t.Fatalf("StartListening() error = %v", err)
	}

	completed, err := s.CompleteListeningNoSpeech(turn.Generation)
	if err != nil {
		t.Fatalf("CompleteListeningNoSpeech() error = %v", err)
	}
	if completed.State != StateIdle {
		t.Fatalf("state = %s, want %s", completed.State, StateIdle)
	}
	if completed.Generation != turn.Generation {
		t.Fatalf("generation = %d, want %d", completed.Generation, turn.Generation)
	}
}

func TestManagerReconnectCreatesNewSessionAndIncrementsGeneration(t *testing.T) {
	manager := NewManager()
	first := manager.CreateSession("stackchan-s3-main", "client-1")
	if err := first.ReceiveHello(); err != nil {
		t.Fatalf("first ReceiveHello() error = %v", err)
	}
	firstTurn, err := first.ServerHelloSent()
	if err != nil {
		t.Fatalf("first ServerHelloSent() error = %v", err)
	}
	if firstTurn.Generation != 1 {
		t.Fatalf("first generation = %d, want 1", firstTurn.Generation)
	}

	second := manager.CreateSession("stackchan-s3-main", "client-2")
	if first.State() != StateClosed {
		t.Fatalf("first state = %s, want %s", first.State(), StateClosed)
	}
	if err := second.ReceiveHello(); err != nil {
		t.Fatalf("second ReceiveHello() error = %v", err)
	}
	secondTurn, err := second.ServerHelloSent()
	if err != nil {
		t.Fatalf("second ServerHelloSent() error = %v", err)
	}

	if secondTurn.Generation != 2 {
		t.Fatalf("second generation = %d, want 2", secondTurn.Generation)
	}
}

func readySession(t *testing.T) *Session {
	t.Helper()

	s := New("sess_1", "stackchan-s3-main", "client-1")
	if err := s.ReceiveHello(); err != nil {
		t.Fatalf("ReceiveHello() error = %v", err)
	}
	if _, err := s.ServerHelloSent(); err != nil {
		t.Fatalf("ServerHelloSent() error = %v", err)
	}
	return s
}

func speakingSession(t *testing.T) *Session {
	t.Helper()

	s := readySession(t)
	if _, err := s.StartListening(); err != nil {
		t.Fatalf("StartListening() error = %v", err)
	}
	turn, err := s.StopListening()
	if err != nil {
		t.Fatalf("StopListening() error = %v", err)
	}
	if _, err := s.BeginSpeaking(turn.Generation); err != nil {
		t.Fatalf("BeginSpeaking() error = %v", err)
	}
	return s
}
