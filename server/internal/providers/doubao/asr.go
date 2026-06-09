package doubao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDASR           = "doubao-asr"
	DefaultASREndpoint      = "wss://ai-gateway.vei.volces.com/v1/realtime"
	DefaultASRModel         = "bigmodel"
	DefaultASRSampleRateHz  = 16000
	DefaultASRBits          = 16
	DefaultASRChannels      = 1
	SourceDocURLASR         = "https://www.volcengine.com/docs/6893/1527759"
	DefaultASRInputFormat   = "pcm"
	DefaultASRInputCodec    = "raw"
	defaultASREventIDPrefix = "event_doubao_asr_"
)

var (
	ErrMissingASRDecoder        = providers.NewProviderConfigurationError("doubao asr requires an explicit opus to pcm decoder")
	ErrUnsupportedASRAudioFrame = errors.New("doubao asr audio frame is unsupported")
)

type OpusDecoder interface {
	DecodeOpus(frame audio.Frame) ([]byte, error)
}

type ASROptions struct {
	EndpointURL    string
	APIKey         string
	ResourceID     string
	Model          string
	SampleRateHz   int
	Bits           int
	Channels       int
	Dialer         *websocket.Dialer
	OpusDecoder    OpusDecoder
	EventIDFactory func() string
}

type ASR struct {
	endpointURL    string
	apiKey         string
	resourceID     string
	model          string
	sampleRateHz   int
	bits           int
	channels       int
	dialer         *websocket.Dialer
	decoder        OpusDecoder
	eventIDFactory func() string
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
	sampleRateHz := options.SampleRateHz
	if sampleRateHz == 0 {
		sampleRateHz = DefaultASRSampleRateHz
	}
	bits := options.Bits
	if bits == 0 {
		bits = DefaultASRBits
	}
	channels := options.Channels
	if channels == 0 {
		channels = DefaultASRChannels
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	eventIDFactory := options.EventIDFactory
	if eventIDFactory == nil {
		eventIDFactory = newEventID
	}

	return &ASR{
		endpointURL:    endpointURL,
		apiKey:         strings.TrimSpace(options.APIKey),
		resourceID:     strings.TrimSpace(options.ResourceID),
		model:          model,
		sampleRateHz:   sampleRateHz,
		bits:           bits,
		channels:       channels,
		dialer:         dialer,
		decoder:        options.OpusDecoder,
		eventIDFactory: eventIDFactory,
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
	if p.decoder == nil {
		return ErrMissingASRDecoder
	}
	return nil
}

func (p *ASR) Start(ctx context.Context, req providers.ASRStartRequest) (providers.ASRStream, error) {
	if p.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if p.decoder == nil {
		return nil, ErrMissingASRDecoder
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+p.apiKey)
	if p.resourceID != "" {
		header.Set("X-Api-Resource-Id", p.resourceID)
	}

	conn, resp, err := p.dialer.DialContext(ctx, p.endpointWithModel(), header)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			return nil, decodeASRHandshakeError(resp, p.apiKey)
		}
		return nil, fmt.Errorf("doubao asr websocket dial: %s", sanitizeProviderMessage(err.Error(), p.apiKey))
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := &ASRStream{
		conn:           conn,
		ctx:            streamCtx,
		cancel:         cancel,
		apiKey:         p.apiKey,
		model:          p.model,
		sampleRateHz:   p.sampleRateHz,
		bits:           p.bits,
		channels:       p.channels,
		decoder:        p.decoder,
		eventIDFactory: p.eventIDFactory,
		events:         make(chan providers.ASREvent, 8),
	}

	if err := stream.writeJSON(newASRSessionUpdate(p.model, p.sampleRateHz, p.bits, p.channels)); err != nil {
		_ = stream.Close()
		return nil, err
	}
	if err := stream.waitForStarted(); err != nil {
		_ = stream.Close()
		return nil, err
	}

	go stream.closeConnOnContextDone()
	go stream.readLoop()
	return stream, nil
}

func (p *ASR) endpointWithModel() string {
	parsed, err := url.Parse(p.endpointURL)
	if err != nil {
		separator := "?"
		if strings.Contains(p.endpointURL, "?") {
			separator = "&"
		}
		return p.endpointURL + separator + "model=" + url.QueryEscape(p.model)
	}
	query := parsed.Query()
	query.Set("model", p.model)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

type ASRStream struct {
	conn           *websocket.Conn
	ctx            context.Context
	cancel         context.CancelFunc
	apiKey         string
	model          string
	sampleRateHz   int
	bits           int
	channels       int
	decoder        OpusDecoder
	eventIDFactory func() string
	events         chan providers.ASREvent

	mu       sync.Mutex
	closed   bool
	finished bool
}

func (s *ASRStream) AcceptOpus(frame audio.Frame) error {
	if frame.Format != audio.FormatOpus || frame.SampleRateHz != s.sampleRateHz || frame.Channels != s.channels {
		return ErrUnsupportedASRAudioFrame
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
	return s.writeJSONLocked(newASRAudioAppend(s.nextEventID(), pcm))
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
	if err := s.writeJSONLocked(newASRCommit(s.nextEventID())); err != nil {
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
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (s *ASRStream) waitForStarted() error {
	_, payload, err := s.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read doubao asr transcription_session.updated: %s", sanitizeProviderMessage(err.Error(), s.apiKey))
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
		return errors.New("doubao asr task ended before transcription_session.updated")
	}

	var envelope doubaoASRServerEvent
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("decode doubao asr transcription_session.updated: %w", err)
	}
	if envelope.Type != "transcription_session.updated" {
		return fmt.Errorf("doubao asr unexpected start event: %s", envelope.Type)
	}
	return nil
}

func (s *ASRStream) closeConnOnContextDone() {
	<-s.ctx.Done()
	_ = s.Close()
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

func (s *ASRStream) nextEventID() string {
	if s.eventIDFactory == nil {
		return newEventID()
	}
	id := strings.TrimSpace(s.eventIDFactory())
	if id == "" {
		return newEventID()
	}
	return id
}

func DecodeASRServerEvent(data []byte) (providers.ASREvent, bool, bool, error) {
	return decodeASRServerEvent(data, "")
}

func decodeASRServerEvent(data []byte, apiKey string) (providers.ASREvent, bool, bool, error) {
	var event doubaoASRServerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return providers.ASREvent{}, false, false, fmt.Errorf("decode doubao asr server event: %w", err)
	}

	switch event.Type {
	case "transcription_session.updated":
		return providers.ASREvent{}, false, false, nil
	case "conversation.item.input_audio_transcription.result":
		if strings.TrimSpace(event.Transcript) == "" {
			return providers.ASREvent{}, false, false, nil
		}
		now := time.Now()
		return providers.ASREvent{
			Type:       providers.ASREventPartial,
			Text:       event.Transcript,
			IsFinal:    false,
			StartedAt:  now,
			FinishedAt: now,
		}, true, false, nil
	case "conversation.item.input_audio_transcription.completed":
		if strings.TrimSpace(event.Transcript) == "" {
			return providers.ASREvent{}, false, true, nil
		}
		now := time.Now()
		return providers.ASREvent{
			Type:       providers.ASREventFinal,
			Text:       event.Transcript,
			IsFinal:    true,
			StartedAt:  now,
			FinishedAt: now,
		}, true, true, nil
	case "error":
		return providers.ASREvent{}, false, true, &ProviderError{
			Code:    firstNonEmpty(event.Code, event.Error.Code, event.Error.Type, "error"),
			Message: sanitizeProviderMessage(firstNonEmpty(event.Message, event.Error.Message, "provider task failed"), apiKey),
		}
	default:
		return providers.ASREvent{}, false, false, nil
	}
}

func newASRSessionUpdate(model string, sampleRateHz int, bits int, channels int) doubaoASRSessionUpdate {
	return doubaoASRSessionUpdate{
		Type: "transcription_session.update",
		Session: doubaoASRSession{
			InputAudioFormat:     DefaultASRInputFormat,
			InputAudioCodec:      DefaultASRInputCodec,
			InputAudioSampleRate: sampleRateHz,
			InputAudioBits:       bits,
			InputAudioChannel:    channels,
			InputAudioTranscription: doubaoASRTranscription{
				Model: model,
			},
		},
	}
}

func newASRAudioAppend(eventID string, pcm []byte) doubaoASRAudioAppend {
	return doubaoASRAudioAppend{
		Type:    "input_audio_buffer.append",
		EventID: eventID,
		Audio:   base64.StdEncoding.EncodeToString(pcm),
	}
}

func newASRCommit(eventID string) doubaoASRCommit {
	return doubaoASRCommit{
		Type:    "input_audio_buffer.commit",
		EventID: eventID,
	}
}

type doubaoASRSessionUpdate struct {
	Type    string           `json:"type"`
	Session doubaoASRSession `json:"session"`
}

type doubaoASRSession struct {
	InputAudioFormat        string                 `json:"input_audio_format"`
	InputAudioCodec         string                 `json:"input_audio_codec"`
	InputAudioSampleRate    int                    `json:"input_audio_sample_rate"`
	InputAudioBits          int                    `json:"input_audio_bits"`
	InputAudioChannel       int                    `json:"input_audio_channel"`
	InputAudioTranscription doubaoASRTranscription `json:"input_audio_transcription"`
}

type doubaoASRTranscription struct {
	Model string `json:"model"`
}

type doubaoASRAudioAppend struct {
	Type    string `json:"type"`
	EventID string `json:"event_id,omitempty"`
	Audio   string `json:"audio"`
}

type doubaoASRCommit struct {
	Type    string `json:"type"`
	EventID string `json:"event_id,omitempty"`
}

type doubaoASRServerEvent struct {
	Type       string `json:"type"`
	EventID    string `json:"event_id"`
	ItemID     string `json:"item_id"`
	Transcript string `json:"transcript"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Error      struct {
		Code    string `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeASRHandshakeError(resp *http.Response, apiKey string) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload doubaoErrorPayload
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

func newEventID() string {
	return fmt.Sprintf("%s%d", defaultASREventIDPrefix, time.Now().UnixNano())
}
