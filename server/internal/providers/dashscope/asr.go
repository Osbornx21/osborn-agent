package dashscope

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDASR               = "dashscope-asr"
	DefaultASREndpoint          = "wss://dashscope.aliyuncs.com/api-ws/v1/inference"
	DefaultASRModel             = "fun-asr-realtime"
	DefaultASRAudioFormat       = "pcm"
	DefaultASRSampleRateHz      = 16000
	SourceDocURLASR             = "https://help.aliyun.com/zh/model-studio/fun-asr-realtime-websocket-api"
	SourceDocURLASRClientEvents = "https://help.aliyun.com/zh/model-studio/fun-asr-client-events"
	SourceDocURLASRServerEvents = "https://help.aliyun.com/zh/model-studio/fun-asr-server-events"
)

var (
	ErrUnsupportedASRAudioFrame  = errors.New("dashscope asr audio frame is unsupported")
	ErrUnsupportedASRAudioFormat = providers.NewProviderConfigurationError("dashscope asr currently requires pcm provider audio")
	ErrMissingASRDecoder         = providers.NewProviderConfigurationError("dashscope asr requires an explicit xiaozhi opus to pcm decoder")
)

type OpusDecoder interface {
	DecodeOpus(audio.Frame) ([]byte, error)
}

type OpusDecoderFactory interface {
	NewOpusDecoder() (OpusDecoder, error)
}

type OpusDecoderFactoryFunc func() (OpusDecoder, error)

func (f OpusDecoderFactoryFunc) NewOpusDecoder() (OpusDecoder, error) {
	return f()
}

type ASROptions struct {
	EndpointURL        string
	APIKey             string
	WorkspaceID        string
	Model              string
	AudioFormat        string
	SampleRateHz       int
	OpusDecoderFactory OpusDecoderFactory
	Dialer             *websocket.Dialer
	TaskIDFactory      func() string
}

type ASR struct {
	endpointURL    string
	apiKey         string
	workspaceID    string
	model          string
	audioFormat    string
	sampleRateHz   int
	decoderFactory OpusDecoderFactory
	dialer         *websocket.Dialer
	taskIDFactory  func() string
}

func NewASR(options ASROptions) *ASR {
	endpointURL := strings.TrimSpace(options.EndpointURL)
	if endpointURL == "" {
		endpointURL = DefaultASREndpoint
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = DefaultASRModel
	}
	audioFormat := strings.ToLower(strings.TrimSpace(options.AudioFormat))
	if audioFormat == "" {
		audioFormat = DefaultASRAudioFormat
	}
	sampleRateHz := options.SampleRateHz
	if sampleRateHz == 0 {
		sampleRateHz = DefaultASRSampleRateHz
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	taskIDFactory := options.TaskIDFactory
	if taskIDFactory == nil {
		taskIDFactory = newTaskID
	}

	return &ASR{
		endpointURL:    endpointURL,
		apiKey:         strings.TrimSpace(options.APIKey),
		workspaceID:    strings.TrimSpace(options.WorkspaceID),
		model:          model,
		audioFormat:    audioFormat,
		sampleRateHz:   sampleRateHz,
		decoderFactory: options.OpusDecoderFactory,
		dialer:         dialer,
		taskIDFactory:  taskIDFactory,
	}
}

func (p *ASR) ProviderID() string {
	return ProviderIDASR
}

func (p *ASR) ModelID() string {
	return p.model
}

func (p *ASR) SourceDocURL() string {
	return SourceDocURLASR
}

func (p *ASR) SourceDocCheckedAt() string {
	return SourceDocCheckedAt
}

func (p *ASR) ValidateProviderConfig() error {
	if p.apiKey == "" {
		return ErrMissingAPIKey
	}
	if p.audioFormat != "pcm" {
		return ErrUnsupportedASRAudioFormat
	}
	decoder, err := p.newOpusDecoder()
	if err != nil {
		return err
	}
	closeOpusDecoder(decoder)
	return nil
}

func (p *ASR) Start(ctx context.Context, req providers.ASRStartRequest) (providers.ASRStream, error) {
	if p.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if p.audioFormat != "pcm" {
		return nil, ErrUnsupportedASRAudioFormat
	}
	decoder, err := p.newOpusDecoder()
	if err != nil {
		return nil, err
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+p.apiKey)
	if p.workspaceID != "" {
		header.Set("X-DashScope-WorkSpace", p.workspaceID)
	}

	conn, resp, err := p.dialer.DialContext(ctx, p.endpointURL, header)
	if err != nil {
		closeOpusDecoder(decoder)
		if resp != nil {
			defer resp.Body.Close()
			return nil, decodeASRHandshakeError(resp, p.apiKey)
		}
		return nil, fmt.Errorf("dashscope asr websocket dial: %s", sanitizeProviderMessage(err.Error(), p.apiKey))
	}

	streamCtx, cancel := context.WithCancel(ctx)
	taskID := strings.TrimSpace(p.taskIDFactory())
	if taskID == "" {
		taskID = newTaskID()
	}
	stream := &ASRStream{
		conn:         conn,
		ctx:          streamCtx,
		cancel:       cancel,
		taskID:       taskID,
		apiKey:       p.apiKey,
		audioFormat:  p.audioFormat,
		sampleRateHz: p.sampleRateHz,
		decoder:      decoder,
		events:       make(chan providers.ASREvent, 8),
	}

	if err := stream.writeJSON(newASRRunTask(taskID, p.model, p.audioFormat, p.sampleRateHz)); err != nil {
		_ = stream.Close()
		return nil, err
	}
	if err := stream.waitForStarted(); err != nil {
		_ = stream.Close()
		return nil, err
	}

	go stream.readLoop()
	return stream, nil
}

func (p *ASR) newOpusDecoder() (OpusDecoder, error) {
	if p.decoderFactory == nil {
		return nil, ErrMissingASRDecoder
	}
	decoder, err := p.decoderFactory.NewOpusDecoder()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMissingASRDecoder, err)
	}
	if decoder == nil {
		return nil, ErrMissingASRDecoder
	}
	return decoder, nil
}

type ASRStream struct {
	conn         *websocket.Conn
	ctx          context.Context
	cancel       context.CancelFunc
	taskID       string
	apiKey       string
	audioFormat  string
	sampleRateHz int
	decoder      OpusDecoder
	events       chan providers.ASREvent

	mu       sync.Mutex
	closed   bool
	finished bool
}

func (s *ASRStream) AcceptOpus(frame audio.Frame) error {
	if frame.Format != audio.FormatOpus || frame.SampleRateHz != s.sampleRateHz || frame.Channels != audio.DefaultChannels {
		return ErrUnsupportedASRAudioFrame
	}
	if s.audioFormat != "pcm" {
		return ErrUnsupportedASRAudioFormat
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return providers.ErrStreamClosed
	}
	if s.finished {
		return providers.ErrStreamFinished
	}
	if s.conn == nil {
		return providers.ErrStreamClosed
	}
	if s.decoder == nil {
		return ErrMissingASRDecoder
	}
	pcm, err := s.decoder.DecodeOpus(frame)
	if err != nil {
		return err
	}
	if len(pcm) == 0 {
		return ErrUnsupportedASRAudioFrame
	}
	return s.conn.WriteMessage(websocket.BinaryMessage, pcm)
}

func (s *ASRStream) Finish() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return providers.ErrStreamClosed
	}
	if s.finished {
		return nil
	}
	if s.conn == nil {
		return providers.ErrStreamClosed
	}
	if err := s.writeJSONLocked(newASRFinishTask(s.taskID)); err != nil {
		return err
	}
	s.finished = true
	return nil
}

func (s *ASRStream) Events() <-chan providers.ASREvent {
	return s.events
}

func (s *ASRStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cancel := s.cancel
	conn := s.conn
	decoder := s.decoder
	s.decoder = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	closeOpusDecoder(decoder)
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func closeOpusDecoder(decoder OpusDecoder) {
	if closer, ok := decoder.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func (s *ASRStream) waitForStarted() error {
	_, payload, err := s.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read dashscope asr task-started: %s", sanitizeProviderMessage(err.Error(), s.apiKey))
	}

	event, ok, terminal, err := decodeASRServerEvent(payload, s.apiKey)
	if err != nil {
		return err
	}
	if ok {
		select {
		case s.events <- event:
		default:
		}
	}
	if terminal {
		return errors.New("dashscope asr task ended before task-started")
	}

	var envelope dashscopeASRServerEvent
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("decode dashscope asr task-started: %w", err)
	}
	if envelope.Header.Event != "task-started" {
		return fmt.Errorf("dashscope asr unexpected start event: %s", envelope.Header.Event)
	}
	return nil
}

func (s *ASRStream) readLoop() {
	defer close(s.events)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		messageType, payload, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}

		event, ok, terminal, err := decodeASRServerEvent(payload, s.apiKey)
		if err != nil {
			return
		}
		if ok {
			select {
			case <-s.ctx.Done():
				return
			case s.events <- event:
			}
		}
		if terminal {
			return
		}
	}
}

func (s *ASRStream) writeJSON(value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSONLocked(value)
}

func (s *ASRStream) writeJSONLocked(value any) error {
	if s.conn == nil {
		return providers.ErrStreamClosed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.conn.WriteMessage(websocket.TextMessage, data)
}

func DecodeASRServerEvent(data []byte) (providers.ASREvent, bool, bool, error) {
	return decodeASRServerEvent(data, "")
}

func decodeASRServerEvent(data []byte, apiKey string) (providers.ASREvent, bool, bool, error) {
	var event dashscopeASRServerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return providers.ASREvent{}, false, false, fmt.Errorf("decode dashscope asr server event: %w", err)
	}

	switch event.Header.Event {
	case "task-started":
		return providers.ASREvent{}, false, false, nil
	case "result-generated":
		sentence := event.Payload.Output.Sentence
		if sentence.Heartbeat || strings.TrimSpace(sentence.Text) == "" {
			return providers.ASREvent{}, false, false, nil
		}
		isFinal := sentence.SentenceEnd
		eventType := providers.ASREventPartial
		if isFinal {
			eventType = providers.ASREventFinal
		}
		now := time.Now()
		return providers.ASREvent{
			Type:       eventType,
			Text:       sentence.Text,
			IsFinal:    isFinal,
			StartedAt:  now.Add(-time.Duration(sentence.EndTime-sentence.BeginTime) * time.Millisecond),
			FinishedAt: now,
		}, true, false, nil
	case "task-finished":
		return providers.ASREvent{}, false, true, nil
	case "task-failed":
		return providers.ASREvent{}, false, true, &ProviderError{
			Code:    firstNonEmpty(event.Header.ErrorCode, "task-failed"),
			Message: sanitizeProviderMessage(firstNonEmpty(event.Header.ErrorMessage, "provider task failed"), apiKey),
		}
	default:
		return providers.ASREvent{}, false, false, nil
	}
}

func newASRRunTask(taskID string, model string, audioFormat string, sampleRateHz int) dashscopeASRClientEvent {
	return dashscopeASRClientEvent{
		Header: dashscopeASRClientHeader{
			Action:    "run-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: dashscopeASRClientPayload{
			TaskGroup: "audio",
			Task:      "asr",
			Function:  "recognition",
			Model:     model,
			Parameters: dashscopeASRParameters{
				Format:                     audioFormat,
				SampleRate:                 sampleRateHz,
				SemanticPunctuationEnabled: false,
			},
			Input: map[string]any{},
		},
	}
}

func newASRFinishTask(taskID string) dashscopeASRClientEvent {
	return dashscopeASRClientEvent{
		Header: dashscopeASRClientHeader{
			Action:    "finish-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: dashscopeASRClientPayload{
			Input: map[string]any{},
		},
	}
}

type dashscopeASRClientEvent struct {
	Header  dashscopeASRClientHeader  `json:"header"`
	Payload dashscopeASRClientPayload `json:"payload"`
}

type dashscopeASRClientHeader struct {
	Action    string `json:"action"`
	TaskID    string `json:"task_id"`
	Streaming string `json:"streaming"`
}

type dashscopeASRClientPayload struct {
	TaskGroup  string                 `json:"task_group,omitempty"`
	Task       string                 `json:"task,omitempty"`
	Function   string                 `json:"function,omitempty"`
	Model      string                 `json:"model,omitempty"`
	Parameters dashscopeASRParameters `json:"parameters,omitempty"`
	Input      map[string]any         `json:"input"`
}

type dashscopeASRParameters struct {
	Format                     string `json:"format"`
	SampleRate                 int    `json:"sample_rate"`
	SemanticPunctuationEnabled bool   `json:"semantic_punctuation_enabled"`
}

type dashscopeASRServerEvent struct {
	Header struct {
		Event        string `json:"event"`
		TaskID       string `json:"task_id"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"header"`
	Payload struct {
		Output struct {
			Sentence struct {
				BeginTime   int    `json:"begin_time"`
				EndTime     int    `json:"end_time"`
				Text        string `json:"text"`
				Heartbeat   bool   `json:"heartbeat"`
				SentenceEnd bool   `json:"sentence_end"`
				SentenceID  int    `json:"sentence_id"`
			} `json:"sentence"`
		} `json:"output"`
	} `json:"payload"`
}

func decodeASRHandshakeError(resp *http.Response, apiKey string) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload dashscopeErrorPayload
	_ = json.Unmarshal(data, &payload)
	message := firstNonEmpty(payload.Message, strings.TrimSpace(string(data)), "provider websocket handshake failed")
	if message == "" {
		message = "provider websocket handshake failed"
	}
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       firstNonEmpty(payload.Code, http.StatusText(resp.StatusCode), "websocket_handshake_failed"),
		Message:    sanitizeProviderMessage(message, apiKey),
	}
}

func newTaskID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("task_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]),
		hex.EncodeToString(bytes[4:6]),
		hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]),
		hex.EncodeToString(bytes[10:16]),
	)
}
