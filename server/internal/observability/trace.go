package observability

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const redactedValue = "[REDACTED]"

type TraceRecorder interface {
	Record(ctx context.Context, event TraceEvent) error
}

type TraceEvent struct {
	Timestamp  time.Time      `json:"timestamp"`
	Sequence   uint64         `json:"sequence"`
	TraceID    string         `json:"trace_id"`
	SessionID  string         `json:"session_id"`
	DeviceID   string         `json:"device_id"`
	Generation int64          `json:"generation"`
	Event      string         `json:"event"`
	ElapsedMS  int64          `json:"elapsed_ms"`
	Provider   string         `json:"provider,omitempty"`
	ErrorCode  string         `json:"error_code,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type TraceRecorderOptions struct {
	Writer        io.Writer
	Now           func() time.Time
	RedactSecrets bool
}

type FileTraceRecorderOptions struct {
	Path          string
	Now           func() time.Time
	RedactSecrets bool
}

type Recorder struct {
	mu            sync.Mutex
	writer        io.Writer
	closer        io.Closer
	now           func() time.Time
	redactSecrets bool
	traces        map[string]traceState
}

type traceState struct {
	start    time.Time
	lastMS   int64
	sequence uint64
	started  bool
}

func NewTraceRecorder(options TraceRecorderOptions) *Recorder {
	writer := options.Writer
	if writer == nil {
		writer = io.Discard
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &Recorder{
		writer:        writer,
		now:           now,
		redactSecrets: options.RedactSecrets,
		traces:        make(map[string]traceState),
	}
}

func NewFileTraceRecorder(options FileTraceRecorderOptions) (*Recorder, error) {
	path := strings.TrimSpace(options.Path)
	if path == "" {
		return nil, errors.New("trace jsonl path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	recorder := NewTraceRecorder(TraceRecorderOptions{
		Writer:        file,
		Now:           options.Now,
		RedactSecrets: options.RedactSecrets,
	})
	recorder.closer = file
	return recorder, nil
}

func (r *Recorder) Record(_ context.Context, event TraceEvent) error {
	if strings.TrimSpace(event.Event) == "" {
		return errors.New("trace event name is required")
	}
	if event.TraceID == "" {
		event.TraceID = TraceID(event.SessionID, event.Generation)
	}
	now := r.now()
	event.Timestamp = now

	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.traces[event.TraceID]
	if !state.started {
		state.start = now
		state.started = true
	}
	elapsedMS := event.ElapsedMS
	if elapsedMS <= 0 {
		elapsedMS = now.Sub(state.start).Milliseconds()
	}
	if elapsedMS < state.lastMS {
		elapsedMS = state.lastMS
	}
	state.lastMS = elapsedMS
	state.sequence++
	event.ElapsedMS = elapsedMS
	event.Sequence = state.sequence
	r.traces[event.TraceID] = state

	if r.redactSecrets {
		event.Fields = RedactFields(event.Fields)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := r.writer.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

func TraceID(sessionID string, generation int64) string {
	if sessionID == "" && generation == 0 {
		return "trace:unknown"
	}
	return sessionID + ":" + strconv.FormatInt(generation, 10)
}

func RedactFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	redacted := make(map[string]any, len(fields))
	for key, value := range fields {
		if IsSecretKey(key) {
			redacted[key] = redactedValue
			continue
		}
		redacted[key] = RedactValue(value)
	}
	return redacted
}

func RedactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return RedactFields(typed)
	case map[string]string:
		redacted := make(map[string]string, len(typed))
		for key, value := range typed {
			if IsSecretKey(key) {
				redacted[key] = redactedValue
			} else {
				redacted[key] = value
			}
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for index, item := range typed {
			redacted[index] = RedactValue(item)
		}
		return redacted
	default:
		return value
	}
}

func IsSecretKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	secretMarkers := []string{
		"authorization",
		"api_key",
		"apikey",
		"access_key",
		"secret",
		"token",
		"password",
		"passwd",
		"bearer",
	}
	for _, marker := range secretMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func RedactedValue() string {
	return redactedValue
}
