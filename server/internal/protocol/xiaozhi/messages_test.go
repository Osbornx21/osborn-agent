package xiaozhi

import (
	"encoding/json"
	"testing"
)

func TestMessageParseValidHello(t *testing.T) {
	raw := []byte(`{
		"type":"hello",
		"version":1,
		"features":{"mcp":true},
		"transport":"websocket",
		"audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60}
	}`)

	parsed, err := ParseClientMessage(raw)

	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}
	if parsed.Type != MessageTypeHello {
		t.Fatalf("type = %q, want hello", parsed.Type)
	}
	if parsed.Hello == nil {
		t.Fatal("hello = nil, want parsed hello")
	}
	if !parsed.Hello.Features.MCP {
		t.Fatal("features.mcp = false, want true")
	}
	if parsed.Hello.AudioParams.FrameDuration != XiaozhiFrameDurationMS {
		t.Fatalf("frame duration = %d, want %d", parsed.Hello.AudioParams.FrameDuration, XiaozhiFrameDurationMS)
	}
}

func TestMessageParseHelloFailsWithoutAudioParams(t *testing.T) {
	raw := []byte(`{
		"type":"hello",
		"version":1,
		"features":{"mcp":true},
		"transport":"websocket"
	}`)

	_, err := ParseClientMessage(raw)

	requireProtocolError(t, err, ErrorCodeValidationError)
}

func TestMessageParseHelloFailsForPCMFormat(t *testing.T) {
	raw := []byte(`{
		"type":"hello",
		"version":1,
		"features":{"mcp":true},
		"transport":"websocket",
		"audio_params":{"format":"pcm","sample_rate":16000,"channels":1,"frame_duration":60}
	}`)

	_, err := ParseClientMessage(raw)

	requireProtocolError(t, err, ErrorCodeValidationError)
}

func TestMessageParseListenStartAuto(t *testing.T) {
	raw := []byte(`{"session_id":"sess_1","type":"listen","state":"start","mode":"auto"}`)

	parsed, err := ParseClientMessage(raw)

	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}
	if parsed.Listen == nil {
		t.Fatal("listen = nil, want parsed listen")
	}
	if parsed.Listen.Mode != "auto" {
		t.Fatalf("mode = %q, want auto", parsed.Listen.Mode)
	}
	if parsed.UnsupportedForP0 {
		t.Fatal("UnsupportedForP0 = true, want false")
	}
}

func TestMessageParseListenStartRealtimeIsParsedButUnsupportedForP0(t *testing.T) {
	raw := []byte(`{"session_id":"sess_1","type":"listen","state":"start","mode":"realtime"}`)

	parsed, err := ParseClientMessage(raw)

	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}
	if parsed.Listen == nil {
		t.Fatal("listen = nil, want parsed listen")
	}
	if parsed.Listen.Mode != "realtime" {
		t.Fatalf("mode = %q, want realtime", parsed.Listen.Mode)
	}
	if !parsed.UnsupportedForP0 {
		t.Fatal("UnsupportedForP0 = false, want true")
	}
}

func TestMessageParseUnknownTypeReturnsProtocolError(t *testing.T) {
	_, err := ParseClientMessage([]byte(`{"type":"custom_audio","payload":{}}`))

	requireProtocolError(t, err, ErrorCodeUnknownMessageType)
}

func TestMessageParseMCPPreservesPayload(t *testing.T) {
	raw := []byte(`{"session_id":"sess_1","type":"mcp","payload":{"jsonrpc":"2.0","id":1,"method":"initialize"}}`)

	parsed, err := ParseClientMessage(raw)

	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}
	if parsed.MCP == nil {
		t.Fatal("mcp = nil, want parsed mcp")
	}
	var payload map[string]any
	if err := json.Unmarshal(parsed.MCP.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["method"] != "initialize" {
		t.Fatalf("method = %v, want initialize", payload["method"])
	}
}

func TestServerMessageBuildersExposeXiaozhiTargets(t *testing.T) {
	payload := json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{}}`)

	messages := []struct {
		name string
		msg  any
		want string
	}{
		{name: "hello", msg: NewServerHello("sess_1"), want: MessageTypeHello},
		{name: "stt", msg: NewSTT("sess_1", "你好"), want: MessageTypeSTT},
		{name: "llm", msg: NewLLMEmotion("sess_1", "happy"), want: MessageTypeLLM},
		{name: "tts start", msg: NewTTSStart("sess_1"), want: MessageTypeTTS},
		{name: "tts stop", msg: NewTTSStop("sess_1"), want: MessageTypeTTS},
		{name: "tts sentence", msg: NewTTSSentenceStart("sess_1", "我在。"), want: MessageTypeTTS},
		{name: "mcp", msg: NewServerMCP("sess_1", payload), want: MessageTypeMCP},
		{name: "system", msg: NewSystemCommand("sess_1", "reboot"), want: MessageTypeSystem},
		{name: "alert", msg: NewAlert("sess_1", "info", "提醒", "neutral"), want: MessageTypeAlert},
	}

	for _, tt := range messages {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if envelope.Type != tt.want {
				t.Fatalf("type = %q, want %q; json=%s", envelope.Type, tt.want, string(data))
			}
		})
	}
}

func requireProtocolError(t *testing.T, err error, code string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	if !HasErrorCode(err, code) {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}
