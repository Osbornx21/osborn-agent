package xiaozhi

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	MessageTypeHello  = "hello"
	MessageTypeListen = "listen"
	MessageTypeAbort  = "abort"
	MessageTypeMCP    = "mcp"
	MessageTypeSTT    = "stt"
	MessageTypeLLM    = "llm"
	MessageTypeTTS    = "tts"
	MessageTypeSystem = "system"
	MessageTypeAlert  = "alert"
)

const (
	ErrorCodeBadJSON            = "BAD_JSON"
	ErrorCodeMissingMessageType = "MISSING_MESSAGE_TYPE"
	ErrorCodeUnknownMessageType = "UNKNOWN_MESSAGE_TYPE"
	ErrorCodeValidationError    = "VALIDATION_ERROR"
)

const (
	TransportWebSocket = "websocket"
	AudioFormatOpus    = "opus"

	XiaozhiUplinkSampleRateHz = 16000
	XiaozhiDownlinkRateHz     = 24000
	XiaozhiChannels           = 1
	XiaozhiFrameDurationMS    = 60
)

type ProtocolError struct {
	Code    string
	Field   string
	Message string
}

func (e *ProtocolError) Error() string {
	if e.Field == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + ": " + e.Field + ": " + e.Message
}

func HasErrorCode(err error, code string) bool {
	var protocolError *ProtocolError
	return errors.As(err, &protocolError) && protocolError.Code == code
}

type AudioParams struct {
	Format        string `json:"format"`
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
}

type DeviceFeatures struct {
	MCP bool `json:"mcp,omitempty"`
	AEC bool `json:"aec,omitempty"`
}

type DeviceHelloMessage struct {
	Type        string         `json:"type"`
	Version     int            `json:"version"`
	Features    DeviceFeatures `json:"features"`
	Transport   string         `json:"transport"`
	AudioParams *AudioParams   `json:"audio_params"`
}

type ListenMessage struct {
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
	State     string `json:"state"`
	Mode      string `json:"mode,omitempty"`
	Text      string `json:"text,omitempty"`
}

func (m ListenMessage) IsRealtimeUnsupportedForP0() bool {
	return m.State == "start" && m.Mode == "realtime"
}

type AbortMessage struct {
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
}

type ClientMCPMessage struct {
	SessionID string          `json:"session_id,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type ParsedClientMessage struct {
	Type              string
	Hello             *DeviceHelloMessage
	Listen            *ListenMessage
	Abort             *AbortMessage
	MCP               *ClientMCPMessage
	UnsupportedForP0  bool
	OriginalRawLength int
}

func ParseClientMessage(data []byte) (*ParsedClientMessage, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, &ProtocolError{Code: ErrorCodeBadJSON, Message: err.Error()}
	}
	if envelope.Type == "" {
		return nil, &ProtocolError{Code: ErrorCodeMissingMessageType, Field: "type", Message: "message type is required"}
	}

	parsed := &ParsedClientMessage{
		Type:              envelope.Type,
		OriginalRawLength: len(data),
	}

	switch envelope.Type {
	case MessageTypeHello:
		var message DeviceHelloMessage
		if err := decodeAndValidate(data, &message, validateDeviceHello); err != nil {
			return nil, err
		}
		parsed.Hello = &message
	case MessageTypeListen:
		var message ListenMessage
		if err := decodeAndValidate(data, &message, validateListen); err != nil {
			return nil, err
		}
		parsed.Listen = &message
		parsed.UnsupportedForP0 = message.IsRealtimeUnsupportedForP0()
	case MessageTypeAbort:
		var message AbortMessage
		if err := decodeAndValidate(data, &message, validateAbort); err != nil {
			return nil, err
		}
		parsed.Abort = &message
	case MessageTypeMCP:
		var message ClientMCPMessage
		if err := decodeAndValidate(data, &message, validateClientMCP); err != nil {
			return nil, err
		}
		parsed.MCP = &message
	default:
		return nil, &ProtocolError{
			Code:    ErrorCodeUnknownMessageType,
			Field:   "type",
			Message: fmt.Sprintf("unsupported client message type %q", envelope.Type),
		}
	}

	return parsed, nil
}

func decodeAndValidate[T any](data []byte, target *T, validate func(*T) error) error {
	if err := json.Unmarshal(data, target); err != nil {
		return &ProtocolError{Code: ErrorCodeBadJSON, Message: err.Error()}
	}
	return validate(target)
}

func validateDeviceHello(message *DeviceHelloMessage) error {
	if message.Type != MessageTypeHello {
		return validation("type", "must be hello")
	}
	if message.Transport != TransportWebSocket {
		return validation("transport", "must be websocket")
	}
	if message.AudioParams == nil {
		return validation("audio_params", "is required")
	}
	params := message.AudioParams
	if params.Format != AudioFormatOpus {
		return validation("audio_params.format", "must be opus")
	}
	if params.SampleRate != XiaozhiUplinkSampleRateHz {
		return validation("audio_params.sample_rate", "must be 16000")
	}
	if params.Channels != XiaozhiChannels {
		return validation("audio_params.channels", "must be 1")
	}
	if params.FrameDuration != XiaozhiFrameDurationMS {
		return validation("audio_params.frame_duration", "must be 60")
	}
	return nil
}

func validateListen(message *ListenMessage) error {
	if message.Type != MessageTypeListen {
		return validation("type", "must be listen")
	}
	switch message.State {
	case "start":
		switch message.Mode {
		case "", "auto", "manual", "realtime":
			return nil
		default:
			return validation("mode", "must be auto, manual, or realtime")
		}
	case "stop", "detect":
		return nil
	default:
		return validation("state", "must be start, stop, or detect")
	}
}

func validateAbort(message *AbortMessage) error {
	if message.Type != MessageTypeAbort {
		return validation("type", "must be abort")
	}
	return nil
}

func validateClientMCP(message *ClientMCPMessage) error {
	if message.Type != MessageTypeMCP {
		return validation("type", "must be mcp")
	}
	if len(message.Payload) == 0 {
		return validation("payload", "is required")
	}
	return nil
}

func validation(field, message string) error {
	return &ProtocolError{Code: ErrorCodeValidationError, Field: field, Message: message}
}

type ServerHelloMessage struct {
	Type        string      `json:"type"`
	Transport   string      `json:"transport"`
	SessionID   string      `json:"session_id"`
	AudioParams AudioParams `json:"audio_params"`
}

func NewServerHello(sessionID string) ServerHelloMessage {
	return ServerHelloMessage{
		Type:      MessageTypeHello,
		Transport: TransportWebSocket,
		SessionID: sessionID,
		AudioParams: AudioParams{
			Format:        AudioFormatOpus,
			SampleRate:    XiaozhiDownlinkRateHz,
			Channels:      XiaozhiChannels,
			FrameDuration: XiaozhiFrameDurationMS,
		},
	}
}

type STTMessage struct {
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
	Text      string `json:"text"`
}

func NewSTT(sessionID, text string) STTMessage {
	return STTMessage{SessionID: sessionID, Type: MessageTypeSTT, Text: text}
}

type LLMMessage struct {
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
	Emotion   string `json:"emotion"`
}

func NewLLMEmotion(sessionID, emotion string) LLMMessage {
	return LLMMessage{SessionID: sessionID, Type: MessageTypeLLM, Emotion: emotion}
}

type TTSMessage struct {
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
	State     string `json:"state"`
	Text      string `json:"text,omitempty"`
}

func NewTTSStart(sessionID string) TTSMessage {
	return TTSMessage{SessionID: sessionID, Type: MessageTypeTTS, State: "start"}
}

func NewTTSStop(sessionID string) TTSMessage {
	return TTSMessage{SessionID: sessionID, Type: MessageTypeTTS, State: "stop"}
}

func NewTTSSentenceStart(sessionID, text string) TTSMessage {
	return TTSMessage{SessionID: sessionID, Type: MessageTypeTTS, State: "sentence_start", Text: text}
}

type ServerMCPMessage struct {
	SessionID string          `json:"session_id,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func NewServerMCP(sessionID string, payload json.RawMessage) ServerMCPMessage {
	return ServerMCPMessage{SessionID: sessionID, Type: MessageTypeMCP, Payload: payload}
}

type SystemMessage struct {
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
	Command   string `json:"command"`
}

func NewSystemCommand(sessionID, command string) SystemMessage {
	return SystemMessage{SessionID: sessionID, Type: MessageTypeSystem, Command: command}
}

type AlertMessage struct {
	SessionID string `json:"session_id,omitempty"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Emotion   string `json:"emotion"`
}

func NewAlert(sessionID, status, message, emotion string) AlertMessage {
	return AlertMessage{
		SessionID: sessionID,
		Type:      MessageTypeAlert,
		Status:    status,
		Message:   message,
		Emotion:   emotion,
	}
}
