package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/providers"
)

func TestTTSMetadataMatchesOfficialDocs(t *testing.T) {
	provider := NewTTS(TTSOptions{
		APIKey:    "mm-key",
		Model:     "speech-2.8-turbo",
		Voice:     "male-qn-qingse",
		Converter: fixedTTSConverter{opusFrames: [][]byte{{0xf8, 0xff}}},
	})

	if provider.ProviderID() != ProviderIDTTS {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDTTS)
	}
	if provider.ModelID() != "speech-2.8-turbo" {
		t.Fatalf("ModelID() = %q", provider.ModelID())
	}
	if provider.VoiceID() != "male-qn-qingse" {
		t.Fatalf("VoiceID() = %q", provider.VoiceID())
	}
	if provider.SourceDocURL() != SourceDocURLTTS {
		t.Fatalf("SourceDocURL() = %q, want %q", provider.SourceDocURL(), SourceDocURLTTS)
	}
	if provider.SourceDocCheckedAt() != SourceDocCheckedAt {
		t.Fatalf("SourceDocCheckedAt() = %q, want %q", provider.SourceDocCheckedAt(), SourceDocCheckedAt)
	}
}

func TestRegisterTTSAddsFactoryToProviderRegistry(t *testing.T) {
	registry := providers.NewRegistry(providers.MockConfig{})
	RegisterTTS(registry, TTSOptions{
		APIKey:    "mm-key",
		Model:     "speech-2.8-turbo",
		Converter: fixedTTSConverter{opusFrames: [][]byte{{0xf8, 0xff}}},
	})

	provider, err := registry.TTSProvider(ProviderIDTTS)
	if err != nil {
		t.Fatalf("TTSProvider(%s) error = %v", ProviderIDTTS, err)
	}
	if _, ok := provider.(*TTS); !ok {
		t.Fatalf("registered provider type = %T, want *TTS", provider)
	}
}

func TestTTSStartsWebSocketTaskAndConvertsHexAudioToXiaozhiOpus(t *testing.T) {
	providerAudio := []byte{0x10, 0x20, 0x30, 0x40}
	opusPayload := []byte{0xf8, 0xff, 0xfe, 0x42}
	done := make(chan struct{})
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/ws/v1/t2a_v2" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mm-key" {
			t.Fatalf("Authorization = %q", got)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_connected_success.json"))); err != nil {
			t.Fatalf("write connected success: %v", err)
		}

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read task_start: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("task_start message type = %d, want text", messageType)
		}
		assertTTSTaskStart(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write task started: %v", err)
		}

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read task_continue: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("task_continue message type = %d, want text", messageType)
		}
		assertTTSTaskContinue(t, payload, "Hello.")

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read task_finish: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("task_finish message type = %d, want text", messageType)
		}
		assertTTSTaskFinish(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_first_audio_delta.json"))); err != nil {
			t.Fatalf("write audio chunk: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_audio_done_or_task_finished.json"))); err != nil {
			t.Fatalf("write task finished: %v", err)
		}
	}))
	defer server.Close()

	converter := &recordingTTSConverter{opusFrames: [][]byte{opusPayload}}
	provider := NewTTS(TTSOptions{
		EndpointURL:     toWebSocketURL(server.URL) + "/ws/v1/t2a_v2",
		APIKey:          "mm-key",
		Model:           "speech-2.8-turbo",
		Voice:           "male-qn-qingse",
		AudioFormat:     "mp3",
		SampleRateHz:    32000,
		Bitrate:         128000,
		Channels:        1,
		FrameDurationMS: 60,
		Converter:       converter,
	})

	frames, err := provider.Stream(context.Background(), providers.TTSRequest{
		SessionID:  "session-fixture",
		DeviceID:   "stackchan-s3-fixture",
		Generation: 7,
		Text:       "Hello.",
		CreatedAt:  time.Date(2026, 6, 6, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	select {
	case frame := <-frames:
		if frame.Generation != 7 {
			t.Fatalf("frame generation = %d, want 7", frame.Generation)
		}
		if !bytes.Equal(frame.Opus, opusPayload) {
			t.Fatalf("frame opus = %v, want converted opus %v", frame.Opus, opusPayload)
		}
		if frame.TextSpan != "Hello." {
			t.Fatalf("frame text span = %q", frame.TextSpan)
		}
		if frame.Duration != 60*time.Millisecond {
			t.Fatalf("frame duration = %s, want 60ms", frame.Duration)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for TTS frame")
	}

	if len(converter.calls) != 1 {
		t.Fatalf("converter calls = %d, want 1", len(converter.calls))
	}
	call := converter.calls[0]
	if !bytes.Equal(call.Audio, providerAudio) || call.Format != "mp3" || call.SampleRateHz != 32000 || call.Channels != 1 {
		t.Fatalf("converter call = %+v, audio=%v", call, call.Audio)
	}

	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("frame channel still open after task_finished")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for TTS stream close")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not finish websocket flow")
	}
}

func TestTTSRejectsMissingConverterBeforeDial(t *testing.T) {
	serverHit := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		serverHit <- struct{}{}
	}))
	defer server.Close()

	provider := NewTTS(TTSOptions{
		EndpointURL: toWebSocketURL(server.URL) + "/ws/v1/t2a_v2",
		APIKey:      "mm-key",
		Model:       "speech-2.8-turbo",
	})

	_, err := provider.Stream(context.Background(), providers.TTSRequest{Text: "Hello."})
	if !errors.Is(err, ErrMissingTTSConverter) {
		t.Fatalf("Stream() error = %v, want ErrMissingTTSConverter", err)
	}

	select {
	case <-serverHit:
		t.Fatal("server was dialed before converter gate")
	default:
	}
}

func TestTTSParsesServerFixtures(t *testing.T) {
	event, err := DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_connected_success.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(connected) error = %v", err)
	}
	if event.Event != "connected_success" || event.Terminal || event.AudioHex != "" {
		t.Fatalf("connected event = %+v", event)
	}

	event, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_started_or_session_updated.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(started) error = %v", err)
	}
	if event.Event != "task_started" || event.Terminal || event.AudioHex != "" {
		t.Fatalf("started event = %+v", event)
	}

	event, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_first_audio_delta.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(audio) error = %v", err)
	}
	if event.Event != "task_continued" || event.Terminal || event.AudioHex == "" || event.AudioFormat != "mp3" || event.SampleRateHz != 32000 || event.Channels != 1 {
		t.Fatalf("audio event = %+v", event)
	}

	event, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_audio_done_or_task_finished.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(finished) error = %v", err)
	}
	if event.Event != "task_finished" || !event.Terminal {
		t.Fatalf("finished event = %+v", event)
	}

	_, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_error_event.json")))
	if err == nil {
		t.Fatal("DecodeTTSServerEvent(error) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "1004") {
		t.Fatalf("error event = %v, want code 1004", err)
	}
}

func TestTTSHandshakeErrorsDoNotLeakAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":1004,"status_msg":"bad key: mm-secret"}}`))
	}))
	defer server.Close()

	provider := NewTTS(TTSOptions{
		EndpointURL: toWebSocketURL(server.URL) + "/ws/v1/t2a_v2",
		APIKey:      "mm-secret",
		Model:       "speech-2.8-turbo",
		Converter:   fixedTTSConverter{opusFrames: [][]byte{{0xf8, 0xff}}},
	})

	_, err := provider.Stream(context.Background(), providers.TTSRequest{Text: "Hello."})
	if err == nil {
		t.Fatal("Stream() error = nil, want handshake error")
	}
	if strings.Contains(err.Error(), "mm-secret") || strings.Contains(err.Error(), "Bearer ") {
		t.Fatalf("error leaked secret material: %v", err)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if providerErr.StatusCode != http.StatusUnauthorized || providerErr.Code != "1004" {
		t.Fatalf("providerErr = %+v", providerErr)
	}
}

func TestTTSContextCancelClosesWebSocketAndFrames(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverStarted := make(chan struct{})
	serverClosed := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_connected_success.json"))); err != nil {
			t.Fatalf("write connected success: %v", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read task start: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write task started: %v", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read task continue: %v", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read task finish: %v", err)
		}
		close(serverStarted)

		_, _, _ = conn.ReadMessage()
		close(serverClosed)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewTTS(TTSOptions{
		EndpointURL: toWebSocketURL(server.URL) + "/ws/v1/t2a_v2",
		APIKey:      "mm-key",
		Model:       "speech-2.8-turbo",
		Converter:   fixedTTSConverter{opusFrames: [][]byte{{0xf8, 0xff}}},
	})

	frames, err := provider.Stream(ctx, providers.TTSRequest{Text: "Hello."})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	select {
	case <-serverStarted:
	case <-time.After(time.Second):
		t.Fatal("server did not observe TTS request flow")
	}

	cancel()

	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("frame channel still open after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame channel close after cancel")
	}
	select {
	case <-serverClosed:
	case <-time.After(time.Second):
		t.Fatal("server did not observe websocket close after cancel")
	}
}

func assertTTSTaskStart(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode task_start JSON: %v", err)
	}
	if message["event"] != "task_start" {
		t.Fatalf("task_start event = %v", message["event"])
	}
	if message["model"] != "speech-2.8-turbo" {
		t.Fatalf("task_start model = %v", message["model"])
	}
	voiceSetting := requireObject(t, message, "voice_setting")
	if voiceSetting["voice_id"] != "male-qn-qingse" || voiceSetting["speed"] != float64(1) || voiceSetting["vol"] != float64(1) || voiceSetting["pitch"] != float64(0) {
		t.Fatalf("voice_setting = %#v", voiceSetting)
	}
	audioSetting := requireObject(t, message, "audio_setting")
	if audioSetting["format"] != "mp3" || audioSetting["sample_rate"] != float64(32000) || audioSetting["bitrate"] != float64(128000) || audioSetting["channel"] != float64(1) {
		t.Fatalf("audio_setting = %#v", audioSetting)
	}
}

func assertTTSTaskContinue(t *testing.T, payload []byte, text string) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode task_continue JSON: %v", err)
	}
	if message["event"] != "task_continue" {
		t.Fatalf("task_continue event = %v", message["event"])
	}
	if message["text"] != text {
		t.Fatalf("task_continue text = %v, want %q", message["text"], text)
	}
}

func assertTTSTaskFinish(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode task_finish JSON: %v", err)
	}
	if message["event"] != "task_finish" {
		t.Fatalf("task_finish event = %v", message["event"])
	}
}

func requireObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, parent[key])
	}
	return value
}

type recordingTTSConverter struct {
	opusFrames [][]byte
	err        error
	calls      []TTSConversionInput
}

func (c *recordingTTSConverter) ConvertToOpusFrames(ctx context.Context, input TTSConversionInput) ([][]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.calls = append(c.calls, input)
	out := make([][]byte, len(c.opusFrames))
	for i, frame := range c.opusFrames {
		out[i] = append([]byte(nil), frame...)
	}
	return out, nil
}

type fixedTTSConverter struct {
	opusFrames [][]byte
	err        error
}

func (c fixedTTSConverter) ConvertToOpusFrames(ctx context.Context, input TTSConversionInput) ([][]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	out := make([][]byte, len(c.opusFrames))
	for i, frame := range c.opusFrames {
		out[i] = append([]byte(nil), frame...)
	}
	return out, nil
}

func readTTSFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("testdata/tts_ws/" + name)
	if err != nil {
		t.Fatalf("read TTS fixture %s: %v", name, err)
	}
	return string(data)
}

func toWebSocketURL(input string) string {
	return "ws" + strings.TrimPrefix(input, "http")
}
