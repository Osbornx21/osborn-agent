package session

import "fmt"

type State string

const (
	StateConnected     State = "connected"
	StateHelloReceived State = "hello_received"
	StateIdle          State = "idle"
	StateListening     State = "listening"
	StateProcessing    State = "processing"
	StateSpeaking      State = "speaking"
	StateInterrupted   State = "interrupted"
	StateClosed        State = "closed"
)

type TransitionError struct {
	From   State
	Event  string
	Reason string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid transition from %s on %s: %s", e.From, e.Event, e.Reason)
}

func invalidTransition(from State, event string, reason string) error {
	return &TransitionError{From: from, Event: event, Reason: reason}
}
