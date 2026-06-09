package dashscope

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/providers"
)

const (
	ProviderIDTTS               = "dashscope-tts"
	DefaultTTSEndpoint          = "wss://dashscope.aliyuncs.com/api-ws/v1/inference"
	DefaultTTSModel             = "cosyvoice-v3-flash"
	DefaultTTSVoice             = "longanyang"
	DefaultTTSAudioFormat       = audio.FormatPCM
	DefaultTTSSampleRateHz      = 24000
	DefaultTTSFrameDurationMS   = 60
	SourceDocURLTTS             = "https://help.aliyun.com/zh/model-studio/cosyvoice-websocket-api"
	SourceDocURLTTSClientEvents = "https://help.aliyun.com/zh/model-studio/cosyvoice-client-events"
	SourceDocURLTTSServerEvents = "https://help.aliyun.com/zh/model-studio/cosyvoice-server-events"
)

var (
	ErrMissingTTSText            = errors.New("dashscope tts request text is required")
	ErrUnsupportedTTSAudioFormat = providers.NewProviderConfigurationError("dashscope tts currently requires pcm provider audio")
	ErrMissingTTSEncoder         = providers.NewProviderConfigurationError("dashscope tts requires an explicit pcm to xiaozhi opus encoder")
)

type TTSOptions struct {
	EndpointURL     string
	APIKey          string
	WorkspaceID     string
	Model           string
	Voice           string
	AudioFormat     string
	SampleRateHz    int
	FrameDurationMS int
	Volume          int
	VolumeSet       bool
	Rate            float64
	Pitch           float64
	OpusEncoder     audio.OpusPCMEncoderFactory
	Dialer          *websocket.Dialer
	TaskIDFactory   func() string
}

type TTS struct {
	endpointURL     string
	apiKey          string
	workspaceID     string
	model           string
	voice           string
	audioFormat     string
	sampleRateHz    int
	frameDurationMS int
	volume          int
	rate            float64
	pitch           float64
	encoderFactory  audio.OpusPCMEncoderFactory
	dialer          *websocket.Dialer
	taskIDFactory   func() string
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
	frameDurationMS := options.FrameDurationMS
	if frameDurationMS == 0 {
		frameDurationMS = DefaultTTSFrameDurationMS
	}
	volume := options.Volume
	if !options.VolumeSet && volume == 0 {
		volume = 50
	}
	rate := options.Rate
	if rate == 0 {
		rate = 1
	}
	pitch := options.Pitch
	if pitch == 0 {
		pitch = 1
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	taskIDFactory := options.TaskIDFactory
	if taskIDFactory == nil {
		taskIDFactory = newTaskID
	}

	return &TTS{
		endpointURL:     endpointURL,
		apiKey:          strings.TrimSpace(options.APIKey),
		workspaceID:     strings.TrimSpace(options.WorkspaceID),
		model:           model,
		voice:           voice,
		audioFormat:     audioFormat,
		sampleRateHz:    sampleRateHz,
		frameDurationMS: frameDurationMS,
		volume:          volume,
		rate:            rate,
		pitch:           pitch,
		encoderFactory:  options.OpusEncoder,
		dialer:          dialer,
		taskIDFactory:   taskIDFactory,
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

func (p *TTS) VolumeLevel() int {
	return p.volume
}

func (p *TTS) SpeechRate() float64 {
	return p.rate
}

func (p *TTS) SpeechPitch() float64 {
	return p.pitch
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
	if p.audioFormat != audio.FormatPCM {
		return ErrUnsupportedTTSAudioFormat
	}
	encoder, err := p.newOpusEncoder()
	if err != nil {
		return err
	}
	closeOpusEncoder(encoder)
	return nil
}

func (p *TTS) Stream(ctx context.Context, req providers.TTSRequest) (<-chan providers.TTSFrame, error) {
	if p.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, ErrMissingTTSText
	}
	if p.audioFormat != audio.FormatPCM {
		return nil, ErrUnsupportedTTSAudioFormat
	}
	encoder, err := p.newOpusEncoder()
	if err != nil {
		return nil, err
	}

	conn, err := p.dial(ctx)
	if err != nil {
		closeOpusEncoder(encoder)
		return nil, err
	}

	taskID := strings.TrimSpace(p.taskIDFactory())
	if taskID == "" {
		taskID = newTaskID()
	}
	voice := p.voice
	if strings.TrimSpace(req.Voice) != "" {
		voice = strings.TrimSpace(req.Voice)
	}

	if err := writeTTSJSON(conn, newTTSRunTask(taskID, p.model, voice, p.audioFormat, p.sampleRateHz, p.volume, p.rate, p.pitch)); err != nil {
		closeOpusEncoder(encoder)
		_ = conn.Close()
		return nil, err
	}
	if err := waitForTTSStarted(conn, p.apiKey); err != nil {
		closeOpusEncoder(encoder)
		_ = conn.Close()
		return nil, err
	}
	if err := writeTTSJSON(conn, newTTSContinueTask(taskID, req.Text)); err != nil {
		closeOpusEncoder(encoder)
		_ = conn.Close()
		return nil, err
	}
	if err := writeTTSJSON(conn, newTTSFinishTask(taskID)); err != nil {
		closeOpusEncoder(encoder)
		_ = conn.Close()
		return nil, err
	}

	out := make(chan providers.TTSFrame)
	go readTTSFrames(ctx, conn, out, ttsReadOptions{
		apiKey:  p.apiKey,
		request: req,
		encoder: encoder,
		cleaner: audio.NewPCM16StreamCleaner(audio.PCM16CleanerOptions{
			SampleRateHz: p.sampleRateHz,
			Channels:     audio.DefaultChannels,
			RemoveDC:     true,
		}),
		sampleRateHz:    p.sampleRateHz,
		channels:        audio.DefaultChannels,
		frameDurationMS: p.frameDurationMS,
	})
	return out, nil
}

func (p *TTS) newOpusEncoder() (audio.OpusPCMEncoder, error) {
	if p.encoderFactory == nil {
		return nil, ErrMissingTTSEncoder
	}
	encoder, err := p.encoderFactory.NewOpusEncoder(p.sampleRateHz, audio.DefaultChannels, p.frameDurationMS)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMissingTTSEncoder, err)
	}
	if encoder == nil {
		return nil, ErrMissingTTSEncoder
	}
	return encoder, nil
}

func (p *TTS) dial(ctx context.Context) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+p.apiKey)
	if p.workspaceID != "" {
		header.Set("X-DashScope-WorkSpace", p.workspaceID)
	}
	conn, resp, err := p.dialer.DialContext(ctx, p.endpointURL, header)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			return nil, decodeTTSHandshakeError(resp, p.apiKey)
		}
		return nil, fmt.Errorf("dashscope tts websocket dial: %s", sanitizeProviderMessage(err.Error(), p.apiKey))
	}
	return conn, nil
}

type ttsReadOptions struct {
	apiKey          string
	request         providers.TTSRequest
	encoder         audio.OpusPCMEncoder
	cleaner         *audio.PCM16StreamCleaner
	sampleRateHz    int
	channels        int
	frameDurationMS int
}

func readTTSFrames(ctx context.Context, conn *websocket.Conn, out chan<- providers.TTSFrame, options ttsReadOptions) {
	defer close(out)
	defer conn.Close()
	defer closeOpusEncoder(options.encoder)

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	expectAudio := false
	frameDuration := time.Duration(options.frameDurationMS) * time.Millisecond
	var pcmBuffer []byte
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}

		switch messageType {
		case websocket.TextMessage:
			event, err := decodeTTSServerEvent(payload, options.apiKey)
			if err != nil {
				return
			}
			if event.ExpectsBinaryAudio {
				expectAudio = true
			}
			if event.Terminal {
				flushPCMToOpusFrames(ctx, out, options, frameDuration, &pcmBuffer, true)
				return
			}
		case websocket.BinaryMessage:
			if !expectAudio {
				continue
			}
			expectAudio = false
			pcmBuffer = append(pcmBuffer, payload...)
			flushPCMToOpusFrames(ctx, out, options, frameDuration, &pcmBuffer, false)
		}
	}
}

func closeOpusEncoder(encoder audio.OpusPCMEncoder) {
	if closer, ok := encoder.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func flushPCMToOpusFrames(ctx context.Context, out chan<- providers.TTSFrame, options ttsReadOptions, frameDuration time.Duration, pcmBuffer *[]byte, final bool) {
	if options.encoder == nil || pcmBuffer == nil {
		return
	}
	pcmBytesPerFrame := options.sampleRateHz * options.frameDurationMS / 1000 * options.channels * 2
	if pcmBytesPerFrame <= 0 {
		return
	}
	for len(*pcmBuffer) >= pcmBytesPerFrame || final && len(*pcmBuffer) > 0 {
		pcm := make([]byte, pcmBytesPerFrame)
		if len(*pcmBuffer) >= pcmBytesPerFrame {
			copy(pcm, (*pcmBuffer)[:pcmBytesPerFrame])
			*pcmBuffer = (*pcmBuffer)[pcmBytesPerFrame:]
		} else {
			copy(pcm, *pcmBuffer)
			*pcmBuffer = nil
		}
		isFinalFrame := final && len(*pcmBuffer) == 0
		if err := options.cleaner.CleanFrame(pcm, isFinalFrame); err != nil {
			return
		}
		opus, err := options.encoder.EncodePCM(pcm)
		if err != nil {
			return
		}
		audioQuality, _ := audio.AnalyzePCM16LE(pcm, audio.PCM16AnalysisOptions{
			SampleRateHz:         options.sampleRateHz,
			Channels:             options.channels,
			SilenceThresholdDBFS: audio.DefaultPCM16SilenceThresholdDBFS,
		})
		frame := providers.TTSFrame{
			Generation:   options.request.Generation,
			Opus:         opus,
			TextSpan:     options.request.Text,
			Duration:     frameDuration,
			CreatedAt:    time.Now(),
			AudioQuality: audioQuality,
		}
		select {
		case <-ctx.Done():
			return
		case out <- frame:
		}
	}
}

func waitForTTSStarted(conn *websocket.Conn, apiKey string) error {
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read dashscope tts task-started: %s", sanitizeProviderMessage(err.Error(), apiKey))
	}
	if messageType != websocket.TextMessage {
		return fmt.Errorf("dashscope tts unexpected start message type: %d", messageType)
	}
	event, err := decodeTTSServerEvent(payload, apiKey)
	if err != nil {
		return err
	}
	if event.Terminal {
		return errors.New("dashscope tts task ended before task-started")
	}
	if event.Event != "task-started" {
		return fmt.Errorf("dashscope tts unexpected start event: %s", event.Event)
	}
	return nil
}

type TTSServerEvent struct {
	Event              string
	OutputType         string
	ExpectsBinaryAudio bool
	Terminal           bool
	Characters         int
}

func DecodeTTSServerEvent(data []byte) (TTSServerEvent, error) {
	return decodeTTSServerEvent(data, "")
}

func decodeTTSServerEvent(data []byte, apiKey string) (TTSServerEvent, error) {
	var event dashscopeTTSServerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return TTSServerEvent{}, fmt.Errorf("decode dashscope tts server event: %w", err)
	}

	switch event.Header.Event {
	case "task-started":
		return TTSServerEvent{Event: event.Header.Event}, nil
	case "result-generated":
		outputType := event.Payload.Output.Type
		return TTSServerEvent{
			Event:              event.Header.Event,
			OutputType:         outputType,
			ExpectsBinaryAudio: outputType == "sentence-synthesis",
		}, nil
	case "task-finished":
		return TTSServerEvent{
			Event:      event.Header.Event,
			Terminal:   true,
			Characters: event.Payload.Usage.Characters,
		}, nil
	case "task-failed":
		return TTSServerEvent{}, &ProviderError{
			Code:    firstNonEmpty(event.Header.ErrorCode, "task-failed"),
			Message: sanitizeProviderMessage(firstNonEmpty(event.Header.ErrorMessage, "provider task failed"), apiKey),
		}
	default:
		return TTSServerEvent{Event: event.Header.Event}, nil
	}
}

func newTTSRunTask(taskID string, model string, voice string, audioFormat string, sampleRateHz int, volume int, rate float64, pitch float64) dashscopeTTSClientEvent {
	return dashscopeTTSClientEvent{
		Header: dashscopeTTSClientHeader{
			Action:    "run-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: dashscopeTTSClientPayload{
			TaskGroup: "audio",
			Task:      "tts",
			Function:  "SpeechSynthesizer",
			Model:     model,
			Parameters: dashscopeTTSParameters{
				TextType:   "PlainText",
				Voice:      voice,
				Format:     audioFormat,
				SampleRate: sampleRateHz,
				Volume:     volume,
				Rate:       rate,
				Pitch:      pitch,
				EnableSSML: false,
			},
			Input: map[string]any{},
		},
	}
}

func newTTSContinueTask(taskID string, text string) dashscopeTTSClientEvent {
	return dashscopeTTSClientEvent{
		Header: dashscopeTTSClientHeader{
			Action:    "continue-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: dashscopeTTSClientPayload{
			Input: map[string]any{"text": text},
		},
	}
}

func newTTSFinishTask(taskID string) dashscopeTTSClientEvent {
	return dashscopeTTSClientEvent{
		Header: dashscopeTTSClientHeader{
			Action:    "finish-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: dashscopeTTSClientPayload{
			Input: map[string]any{},
		},
	}
}

type dashscopeTTSClientEvent struct {
	Header  dashscopeTTSClientHeader  `json:"header"`
	Payload dashscopeTTSClientPayload `json:"payload"`
}

type dashscopeTTSClientHeader struct {
	Action    string `json:"action"`
	TaskID    string `json:"task_id"`
	Streaming string `json:"streaming"`
}

type dashscopeTTSClientPayload struct {
	TaskGroup  string                 `json:"task_group,omitempty"`
	Task       string                 `json:"task,omitempty"`
	Function   string                 `json:"function,omitempty"`
	Model      string                 `json:"model,omitempty"`
	Parameters dashscopeTTSParameters `json:"parameters,omitempty"`
	Input      map[string]any         `json:"input"`
}

type dashscopeTTSParameters struct {
	TextType   string  `json:"text_type"`
	Voice      string  `json:"voice"`
	Format     string  `json:"format"`
	SampleRate int     `json:"sample_rate"`
	Volume     int     `json:"volume"`
	Rate       float64 `json:"rate"`
	Pitch      float64 `json:"pitch"`
	EnableSSML bool    `json:"enable_ssml"`
}

type dashscopeTTSServerEvent struct {
	Header struct {
		Event        string `json:"event"`
		TaskID       string `json:"task_id"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"header"`
	Payload struct {
		Output struct {
			Type string `json:"type"`
		} `json:"output"`
		Usage struct {
			Characters int `json:"characters"`
		} `json:"usage"`
	} `json:"payload"`
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
	var payload dashscopeErrorPayload
	_ = json.Unmarshal(data, &payload)
	message := firstNonEmpty(payload.Message, strings.TrimSpace(string(data)), "provider websocket handshake failed")
	return &ProviderError{
		StatusCode: resp.StatusCode,
		Code:       firstNonEmpty(payload.Code, http.StatusText(resp.StatusCode), "websocket_handshake_failed"),
		Message:    sanitizeProviderMessage(message, apiKey),
	}
}
