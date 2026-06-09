package minimax

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDTTS             = "minimax-tts-ws"
	DefaultTTSEndpoint        = "wss://api.minimaxi.com/ws/v1/t2a_v2"
	DefaultTTSModel           = "speech-2.8-turbo"
	DefaultTTSVoice           = "male-qn-qingse"
	DefaultTTSAudioFormat     = "mp3"
	DefaultTTSSampleRateHz    = 32000
	DefaultTTSBitrate         = 128000
	DefaultTTSChannels        = 1
	DefaultTTSFrameDurationMS = 60
	SourceDocURLTTS           = "https://platform.minimaxi.com/docs/api-reference/speech-t2a-websocket"
)

var (
	ErrMissingTTSText      = errors.New("minimax tts request text is required")
	ErrMissingTTSConverter = providers.NewProviderConfigurationError("minimax tts requires an explicit provider audio to xiaozhi opus converter")
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
	Model           string
	Voice           string
	AudioFormat     string
	SampleRateHz    int
	Bitrate         int
	Channels        int
	FrameDurationMS int
	Speed           float64
	Volume          float64
	Pitch           float64
	Dialer          *websocket.Dialer
	Converter       TTSConverter
}

type TTS struct {
	endpointURL     string
	apiKey          string
	model           string
	voice           string
	audioFormat     string
	sampleRateHz    int
	bitrate         int
	channels        int
	frameDurationMS int
	speed           float64
	volume          float64
	pitch           float64
	dialer          *websocket.Dialer
	converter       TTSConverter
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
	bitrate := options.Bitrate
	if bitrate == 0 {
		bitrate = DefaultTTSBitrate
	}
	channels := options.Channels
	if channels == 0 {
		channels = DefaultTTSChannels
	}
	frameDurationMS := options.FrameDurationMS
	if frameDurationMS == 0 {
		frameDurationMS = DefaultTTSFrameDurationMS
	}
	speed := options.Speed
	if speed == 0 {
		speed = 1
	}
	volume := options.Volume
	if volume == 0 {
		volume = 1
	}
	pitch := options.Pitch
	dialer := options.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}

	return &TTS{
		endpointURL:     endpointURL,
		apiKey:          strings.TrimSpace(options.APIKey),
		model:           model,
		voice:           voice,
		audioFormat:     audioFormat,
		sampleRateHz:    sampleRateHz,
		bitrate:         bitrate,
		channels:        channels,
		frameDurationMS: frameDurationMS,
		speed:           speed,
		volume:          volume,
		pitch:           pitch,
		dialer:          dialer,
		converter:       options.Converter,
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
	if err := waitForTTSConnected(conn, p.apiKey); err != nil {
		_ = conn.Close()
		return nil, err
	}

	voice := p.voice
	if strings.TrimSpace(req.Voice) != "" {
		voice = strings.TrimSpace(req.Voice)
	}
	if err := writeTTSJSON(conn, newTTSTaskStart(p, voice)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := waitForTTSStarted(conn, p.apiKey); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := writeTTSJSON(conn, newTTSTaskContinue(req.Text)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := writeTTSJSON(conn, minimaxTTSTaskFinish{Event: "task_finish"}); err != nil {
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
	conn, resp, err := p.dialer.DialContext(ctx, p.endpointURL, header)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			return nil, decodeTTSHandshakeError(resp, p.apiKey)
		}
		return nil, fmt.Errorf("minimax tts websocket dial: %s", sanitizeProviderMessage(err.Error(), p.apiKey))
	}
	return conn, nil
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
		if event.AudioHex != "" {
			audioBytes, err := hex.DecodeString(strings.TrimSpace(event.AudioHex))
			if err != nil {
				return
			}
			format := firstNonEmpty(event.AudioFormat, options.audioFormat)
			sampleRateHz := firstNonZero(event.SampleRateHz, options.sampleRateHz)
			channels := firstNonZero(event.Channels, options.channels)
			opusFrames, err := options.converter.ConvertToOpusFrames(ctx, TTSConversionInput{
				Audio:           audioBytes,
				Format:          format,
				SampleRateHz:    sampleRateHz,
				Channels:        channels,
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

func waitForTTSConnected(conn *websocket.Conn, apiKey string) error {
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read minimax tts connected_success: %s", sanitizeProviderMessage(err.Error(), apiKey))
	}
	if messageType != websocket.TextMessage {
		return fmt.Errorf("minimax tts unexpected connected message type: %d", messageType)
	}
	event, err := decodeTTSServerEvent(payload, apiKey)
	if err != nil {
		return err
	}
	if event.Terminal || event.Event != "connected_success" {
		return fmt.Errorf("minimax tts unexpected connected event: %s", event.Event)
	}
	return nil
}

func waitForTTSStarted(conn *websocket.Conn, apiKey string) error {
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read minimax tts task_started: %s", sanitizeProviderMessage(err.Error(), apiKey))
	}
	if messageType != websocket.TextMessage {
		return fmt.Errorf("minimax tts unexpected start message type: %d", messageType)
	}
	event, err := decodeTTSServerEvent(payload, apiKey)
	if err != nil {
		return err
	}
	if event.Terminal {
		return errors.New("minimax tts task ended before task_started")
	}
	if event.Event != "task_started" {
		return fmt.Errorf("minimax tts unexpected start event: %s", event.Event)
	}
	return nil
}

type TTSServerEvent struct {
	Event        string
	AudioHex     string
	AudioFormat  string
	SampleRateHz int
	Channels     int
	Terminal     bool
}

func DecodeTTSServerEvent(data []byte) (TTSServerEvent, error) {
	return decodeTTSServerEvent(data, "")
}

func decodeTTSServerEvent(data []byte, apiKey string) (TTSServerEvent, error) {
	var event minimaxTTSServerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return TTSServerEvent{}, fmt.Errorf("decode minimax tts server event: %w", err)
	}

	if event.BaseResp.StatusCode != 0 {
		return TTSServerEvent{}, &ProviderError{
			Code:    fmt.Sprintf("%d", event.BaseResp.StatusCode),
			Message: sanitizeProviderMessage(firstNonEmpty(event.BaseResp.StatusMsg, "provider task failed"), apiKey),
		}
	}

	switch event.Event {
	case "connected_success", "task_started":
		return TTSServerEvent{Event: event.Event}, nil
	case "task_continued":
		return TTSServerEvent{
			Event:        event.Event,
			AudioHex:     event.Data.Audio,
			AudioFormat:  event.ExtraInfo.AudioFormat,
			SampleRateHz: event.ExtraInfo.AudioSampleRate,
			Channels:     event.ExtraInfo.AudioChannel,
			Terminal:     event.IsFinal,
		}, nil
	case "task_finished":
		return TTSServerEvent{
			Event:    event.Event,
			Terminal: true,
		}, nil
	case "task_failed":
		return TTSServerEvent{}, &ProviderError{
			Code:    fmt.Sprintf("%d", firstNonZero(event.BaseResp.StatusCode, 0)),
			Message: sanitizeProviderMessage(firstNonEmpty(event.BaseResp.StatusMsg, "provider task failed"), apiKey),
		}
	default:
		return TTSServerEvent{
			Event:        event.Event,
			AudioHex:     event.Data.Audio,
			AudioFormat:  event.ExtraInfo.AudioFormat,
			SampleRateHz: event.ExtraInfo.AudioSampleRate,
			Channels:     event.ExtraInfo.AudioChannel,
			Terminal:     event.IsFinal,
		}, nil
	}
}

func newTTSTaskStart(p *TTS, voice string) minimaxTTSTaskStart {
	return minimaxTTSTaskStart{
		Event: "task_start",
		Model: p.model,
		VoiceSetting: minimaxTTSVoiceSetting{
			VoiceID: voice,
			Speed:   p.speed,
			Volume:  p.volume,
			Pitch:   p.pitch,
		},
		AudioSetting: minimaxTTSAudioSetting{
			SampleRate: p.sampleRateHz,
			Bitrate:    p.bitrate,
			Format:     p.audioFormat,
			Channel:    p.channels,
		},
	}
}

func newTTSTaskContinue(text string) minimaxTTSTaskContinue {
	return minimaxTTSTaskContinue{
		Event: "task_continue",
		Text:  text,
	}
}

type minimaxTTSTaskStart struct {
	Event        string                 `json:"event"`
	Model        string                 `json:"model"`
	VoiceSetting minimaxTTSVoiceSetting `json:"voice_setting"`
	AudioSetting minimaxTTSAudioSetting `json:"audio_setting"`
}

type minimaxTTSVoiceSetting struct {
	VoiceID string  `json:"voice_id"`
	Speed   float64 `json:"speed"`
	Volume  float64 `json:"vol"`
	Pitch   float64 `json:"pitch"`
}

type minimaxTTSAudioSetting struct {
	SampleRate int    `json:"sample_rate"`
	Bitrate    int    `json:"bitrate"`
	Format     string `json:"format"`
	Channel    int    `json:"channel"`
}

type minimaxTTSTaskContinue struct {
	Event string `json:"event"`
	Text  string `json:"text"`
}

type minimaxTTSTaskFinish struct {
	Event string `json:"event"`
}

type minimaxTTSServerEvent struct {
	SessionID string `json:"session_id"`
	Event     string `json:"event"`
	TraceID   string `json:"trace_id"`
	IsFinal   bool   `json:"is_final"`
	Data      struct {
		Audio string `json:"audio"`
	} `json:"data"`
	ExtraInfo struct {
		AudioChannel    int    `json:"audio_channel"`
		AudioFormat     string `json:"audio_format"`
		AudioSampleRate int    `json:"audio_sample_rate"`
		Bitrate         int    `json:"bitrate"`
	} `json:"extra_info"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
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
	var payload minimaxErrorPayload
	_ = json.Unmarshal(data, &payload)
	code := minimaxErrorCode(payload)
	message := firstNonEmpty(payload.BaseResp.StatusMsg, payload.Message, strings.TrimSpace(string(data)), "provider websocket handshake failed")
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       firstNonEmpty(code, http.StatusText(resp.StatusCode), "websocket_handshake_failed"),
		Message:    sanitizeProviderMessage(message, apiKey),
	}
}

func copyBytes(input []byte) []byte {
	output := make([]byte, len(input))
	copy(output, input)
	return output
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
