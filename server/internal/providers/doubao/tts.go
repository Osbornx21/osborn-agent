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
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDTTS             = "doubao-tts"
	DefaultTTSEndpoint        = "wss://ai-gateway.vei.volces.com/v1/realtime"
	DefaultTTSModel           = "doubao-tts"
	DefaultTTSVoice           = "zh_female_kailangjiejie_moon_bigtts"
	DefaultTTSAudioFormat     = "pcm"
	DefaultTTSSampleRateHz    = 16000
	DefaultTTSChannels        = 1
	DefaultTTSFrameDurationMS = 60
	SourceDocURLTTS           = "https://www.volcengine.com/docs/6893/1527770"
	defaultTTSEventIDPrefix   = "event_doubao_tts_"
)

var (
	ErrMissingTTSText      = errors.New("doubao tts request text is required")
	ErrMissingTTSConverter = providers.NewProviderConfigurationError("doubao tts requires an explicit provider audio to xiaozhi opus converter")
)

type TTSConverter interface {
	ConvertToOpusFrames(ctx context.Context, input TTSConversionInput) ([][]byte, error)
}

type TTSConversionInput struct {
	Audio           []byte
	Format          string
	SampleRateHz    int
	Channels        int
	FrameDurationMS int
	Text            string
}

type TTSOptions struct {
	EndpointURL     string
	APIKey          string
	ResourceID      string
	Model           string
	Voice           string
	AudioFormat     string
	SampleRateHz    int
	Channels        int
	FrameDurationMS int
	Dialer          *websocket.Dialer
	Converter       TTSConverter
	EventIDFactory  func() string
}

type TTS struct {
	endpointURL     string
	apiKey          string
	resourceID      string
	model           string
	voice           string
	audioFormat     string
	sampleRateHz    int
	channels        int
	frameDurationMS int
	dialer          *websocket.Dialer
	converter       TTSConverter
	eventIDFactory  func() string
}

func NewTTS(options TTSOptions) *TTS {
	endpointURL := strings.TrimSpace(options.EndpointURL)
	if endpointURL == "" {
		endpointURL = DefaultTTSEndpoint
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = DefaultTTSModel
	}
	voice := strings.TrimSpace(options.Voice)
	if voice == "" {
		voice = DefaultTTSVoice
	}
	audioFormat := strings.ToLower(strings.TrimSpace(options.AudioFormat))
	if audioFormat == "" {
		audioFormat = DefaultTTSAudioFormat
	}
	sampleRateHz := options.SampleRateHz
	if sampleRateHz == 0 {
		sampleRateHz = DefaultTTSSampleRateHz
	}
	channels := options.Channels
	if channels == 0 {
		channels = DefaultTTSChannels
	}
	frameDurationMS := options.FrameDurationMS
	if frameDurationMS == 0 {
		frameDurationMS = DefaultTTSFrameDurationMS
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	eventIDFactory := options.EventIDFactory
	if eventIDFactory == nil {
		eventIDFactory = newTTSEventID
	}

	return &TTS{
		endpointURL:     endpointURL,
		apiKey:          strings.TrimSpace(options.APIKey),
		resourceID:      strings.TrimSpace(options.ResourceID),
		model:           model,
		voice:           voice,
		audioFormat:     audioFormat,
		sampleRateHz:    sampleRateHz,
		channels:        channels,
		frameDurationMS: frameDurationMS,
		dialer:          dialer,
		converter:       options.Converter,
		eventIDFactory:  eventIDFactory,
	}
}

func (p *TTS) ProviderID() string {
	return ProviderIDTTS
}

func (p *TTS) ModelID() string {
	return p.model
}

func (p *TTS) VoiceID() string {
	return p.voice
}

func (p *TTS) SourceDocURL() string {
	return SourceDocURLTTS
}

func (p *TTS) SourceDocCheckedAt() string {
	return SourceDocCheckedAt
}

func (p *TTS) ValidateProviderConfig() error {
	if p.apiKey == "" {
		return ErrMissingAPIKey
	}
	if p.converter == nil {
		return ErrMissingTTSConverter
	}
	return nil
}

func (p *TTS) Stream(ctx context.Context, req providers.TTSRequest) (<-chan providers.TTSFrame, error) {
	if p.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, ErrMissingTTSText
	}
	if p.converter == nil {
		return nil, ErrMissingTTSConverter
	}

	conn, err := p.dial(ctx)
	if err != nil {
		return nil, err
	}

	voice := p.voice
	if strings.TrimSpace(req.Voice) != "" {
		voice = strings.TrimSpace(req.Voice)
	}

	if err := writeTTSJSON(conn, newTTSSessionUpdate(p.model, voice, p.audioFormat, p.sampleRateHz)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := waitForTTSStarted(conn, p.apiKey); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := writeTTSJSON(conn, newTTSTextAppend(p.nextEventID(), req.Text)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := writeTTSJSON(conn, newTTSTextDone(p.nextEventID())); err != nil {
		_ = conn.Close()
		return nil, err
	}

	out := make(chan providers.TTSFrame)
	go readTTSFrames(ctx, conn, out, ttsReadOptions{
		apiKey:          p.apiKey,
		request:         req,
		audioFormat:     p.audioFormat,
		sampleRateHz:    p.sampleRateHz,
		channels:        p.channels,
		frameDurationMS: p.frameDurationMS,
		converter:       p.converter,
	})
	return out, nil
}

func (p *TTS) dial(ctx context.Context) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+p.apiKey)
	if p.resourceID != "" {
		header.Set("X-Api-Resource-Id", p.resourceID)
	}
	conn, resp, err := p.dialer.DialContext(ctx, p.endpointWithModel(), header)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			return nil, decodeTTSHandshakeError(resp, p.apiKey)
		}
		return nil, fmt.Errorf("doubao tts websocket dial: %s", sanitizeProviderMessage(err.Error(), p.apiKey))
	}
	return conn, nil
}

func (p *TTS) endpointWithModel() string {
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

func (p *TTS) nextEventID() string {
	if p.eventIDFactory == nil {
		return newTTSEventID()
	}
	id := strings.TrimSpace(p.eventIDFactory())
	if id == "" {
		return newTTSEventID()
	}
	return id
}

type ttsReadOptions struct {
	apiKey          string
	request         providers.TTSRequest
	audioFormat     string
	sampleRateHz    int
	channels        int
	frameDurationMS int
	converter       TTSConverter
}

func readTTSFrames(ctx context.Context, conn *websocket.Conn, out chan<- providers.TTSFrame, options ttsReadOptions) {
	defer close(out)
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	frameDuration := time.Duration(options.frameDurationMS) * time.Millisecond
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}

		event, err := decodeTTSServerEvent(payload, options.apiKey)
		if err != nil {
			return
		}
		if event.AudioDelta != "" {
			audioBytes, err := base64.StdEncoding.DecodeString(event.AudioDelta)
			if err != nil {
				return
			}
			opusFrames, err := options.converter.ConvertToOpusFrames(ctx, TTSConversionInput{
				Audio:           audioBytes,
				Format:          options.audioFormat,
				SampleRateHz:    options.sampleRateHz,
				Channels:        options.channels,
				FrameDurationMS: options.frameDurationMS,
				Text:            options.request.Text,
			})
			if err != nil {
				return
			}
			for _, opus := range opusFrames {
				if len(opus) == 0 {
					continue
				}
				frame := providers.TTSFrame{
					Generation: options.request.Generation,
					Opus:       copyBytes(opus),
					TextSpan:   options.request.Text,
					Duration:   frameDuration,
					CreatedAt:  time.Now(),
				}
				select {
				case <-ctx.Done():
					return
				case out <- frame:
				}
			}
		}
		if event.Terminal {
			return
		}
	}
}

func waitForTTSStarted(conn *websocket.Conn, apiKey string) error {
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read doubao tts tts_session.updated: %s", sanitizeProviderMessage(err.Error(), apiKey))
	}
	if messageType != websocket.TextMessage {
		return fmt.Errorf("doubao tts unexpected start message type: %d", messageType)
	}
	event, err := decodeTTSServerEvent(payload, apiKey)
	if err != nil {
		return err
	}
	if event.Terminal {
		return errors.New("doubao tts task ended before tts_session.updated")
	}
	if event.Event != "tts_session.updated" {
		return fmt.Errorf("doubao tts unexpected start event: %s", event.Event)
	}
	return nil
}

type TTSServerEvent struct {
	Event      string
	AudioDelta string
	Terminal   bool
}

func DecodeTTSServerEvent(data []byte) (TTSServerEvent, error) {
	return decodeTTSServerEvent(data, "")
}

func decodeTTSServerEvent(data []byte, apiKey string) (TTSServerEvent, error) {
	var event doubaoTTSServerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return TTSServerEvent{}, fmt.Errorf("decode doubao tts server event: %w", err)
	}

	switch event.Type {
	case "tts_session.updated":
		return TTSServerEvent{Event: event.Type}, nil
	case "response.audio.delta":
		return TTSServerEvent{
			Event:      event.Type,
			AudioDelta: event.Delta,
		}, nil
	case "response.audio.done":
		return TTSServerEvent{
			Event:    event.Type,
			Terminal: true,
		}, nil
	case "error":
		return TTSServerEvent{}, &ProviderError{
			Code:    firstNonEmpty(event.Code, event.Error.Code, event.Error.Type, "error"),
			Message: sanitizeProviderMessage(firstNonEmpty(event.Message, event.Error.Message, "provider task failed"), apiKey),
		}
	default:
		return TTSServerEvent{Event: event.Type}, nil
	}
}

func newTTSSessionUpdate(model string, voice string, audioFormat string, sampleRateHz int) doubaoTTSSessionUpdate {
	return doubaoTTSSessionUpdate{
		Type: "tts_session.update",
		Session: doubaoTTSSession{
			Voice:                 voice,
			OutputAudioFormat:     audioFormat,
			OutputAudioSampleRate: sampleRateHz,
			TextToSpeech: doubaoTextToSpeech{
				Model: model,
			},
		},
	}
}

func newTTSTextAppend(eventID string, text string) doubaoTTSTextAppend {
	return doubaoTTSTextAppend{
		Type:    "input_text.append",
		EventID: eventID,
		Delta:   text,
	}
}

func newTTSTextDone(eventID string) doubaoTTSTextDone {
	return doubaoTTSTextDone{
		Type:    "input_text.done",
		EventID: eventID,
	}
}

type doubaoTTSSessionUpdate struct {
	Type    string           `json:"type"`
	Session doubaoTTSSession `json:"session"`
}

type doubaoTTSSession struct {
	Voice                 string             `json:"voice"`
	OutputAudioFormat     string             `json:"output_audio_format"`
	OutputAudioSampleRate int                `json:"output_audio_sample_rate"`
	TextToSpeech          doubaoTextToSpeech `json:"text_to_speech"`
}

type doubaoTextToSpeech struct {
	Model string `json:"model"`
}

type doubaoTTSTextAppend struct {
	Type    string `json:"type"`
	EventID string `json:"event_id,omitempty"`
	Delta   string `json:"delta"`
}

type doubaoTTSTextDone struct {
	Type    string `json:"type"`
	EventID string `json:"event_id,omitempty"`
}

type doubaoTTSServerEvent struct {
	Type    string `json:"type"`
	EventID string `json:"event_id"`
	ItemID  string `json:"item_id"`
	Delta   string `json:"delta"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   struct {
		Code    string `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeTTSJSON(conn *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func decodeTTSHandshakeError(resp *http.Response, apiKey string) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload doubaoErrorPayload
	_ = json.Unmarshal(data, &payload)
	message := firstNonEmpty(payload.Message, strings.TrimSpace(string(data)), "provider websocket handshake failed")
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       firstNonEmpty(payload.Code, http.StatusText(resp.StatusCode), "websocket_handshake_failed"),
		Message:    sanitizeProviderMessage(message, apiKey),
	}
}

func copyBytes(input []byte) []byte {
	output := make([]byte, len(input))
	copy(output, input)
	return output
}

func newTTSEventID() string {
	return fmt.Sprintf("%s%d", defaultTTSEventIDPrefix, time.Now().UnixNano())
}
