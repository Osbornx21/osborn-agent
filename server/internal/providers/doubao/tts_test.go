package doubao

import (
	"bytes"
	"context"
	"encoding/base64"
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
		APIKey:    "db-key",
		Model:     "doubao-tts",
		Voice:     "zh_female_kailangjiejie_moon_bigtts",
		Converter: fixedTTSConverter{opusFrames: [][]byte{{0xf8, 0xff}}},
	})

	if provider.ProviderID() != ProviderIDTTS {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDTTS)
	}
	if provider.ModelID() != "doubao-tts" {
		t.Fatalf("ModelID() = %q", provider.ModelID())
	}
	if provider.VoiceID() != "zh_female_kailangjiejie_moon_bigtts" {
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
		APIKey:    "db-key",
		Model:     "doubao-tts",
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

func TestTTSStartsRealtimeSessionAndConvertsBase64AudioToXiaozhiOpus(t *testing.T) {
	providerAudio := []byte{0x10, 0x20, 0x30, 0x40}
	opusPayload := []byte{0xf8, 0xff, 0xfe, 0x42}
	done := make(chan struct{})
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/realtime" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("model") != "doubao-tts" {
			t.Fatalf("model query = %q", r.URL.Query().Get("model"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer db-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Api-Resource-Id"); got != "volc.service_type.10029" {
			t.Fatalf("X-Api-Resource-Id = %q", got)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read tts_session.update: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("session update message type = %d, want text", messageType)
		}
		assertTTSSessionUpdate(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write session updated: %v", err)
		}

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read input_text.append: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("input_text.append message type = %d, want text", messageType)
		}
		assertTTSTextAppend(t, payload, "Hello.")

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read input_text.done: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("input_text.done message type = %d, want text", messageType)
		}
		assertTTSTextDone(t, payload)

		audioDelta := map[string]any{
			"type":     "response.audio.delta",
			"event_id": "event_fixture_004",
			"item_id":  "item_fixture_001",
			"delta":    base64.StdEncoding.EncodeToString(providerAudio),
		}
		audioPayload, err := json.Marshal(audioDelta)
		if err != nil {
			t.Fatalf("marshal audio delta: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, audioPayload); err != nil {
			t.Fatalf("write audio delta: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_audio_done_or_task_finished.json"))); err != nil {
			t.Fatalf("write audio done: %v", err)
		}
	}))
	defer server.Close()

	converter := &recordingTTSConverter{opusFrames: [][]byte{opusPayload}}
	provider := NewTTS(TTSOptions{
		EndpointURL:     toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:          "db-key",
		ResourceID:      "volc.service_type.10029",
		Model:           "doubao-tts",
		Voice:           "zh_female_kailangjiejie_moon_bigtts",
		AudioFormat:     "pcm",
		SampleRateHz:    16000,
		FrameDurationMS: 60,
		Converter:       converter,
		EventIDFactory:  sequentialEventIDs("event_fixture_001", "event_fixture_002"),
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
	if !bytes.Equal(call.Audio, providerAudio) || call.Format != "pcm" || call.SampleRateHz != 16000 || call.Channels != 1 {
		t.Fatalf("converter call = %+v, audio=%v", call, call.Audio)
	}

	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("frame channel still open after response.audio.done")
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
		EndpointURL: toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:      "db-key",
		Model:       "doubao-tts",
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
	event, err := DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_started_or_session_updated.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(updated) error = %v", err)
	}
	if event.Event != "tts_session.updated" || event.Terminal || event.AudioDelta != "" {
		t.Fatalf("session updated event = %+v", event)
	}

	event, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_first_audio_delta.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(audio delta) error = %v", err)
	}
	if event.Event != "response.audio.delta" || event.Terminal || event.AudioDelta == "" {
		t.Fatalf("audio delta event = %+v", event)
	}

	event, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_audio_done_or_task_finished.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(audio done) error = %v", err)
	}
	if event.Event != "response.audio.done" || !event.Terminal {
		t.Fatalf("audio done event = %+v", event)
	}

	_, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_error_event.json")))
	if err == nil {
		t.Fatal("DecodeTTSServerEvent(error) error = nil, want error")
	}

	_, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_error_resource_mismatch_55000000.json")))
	if err == nil {
		t.Fatal("DecodeTTSServerEvent(resource mismatch) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "55000000") {
		t.Fatalf("resource mismatch error = %v, want code 55000000", err)
	}
}

func TestTTSHandshakeErrorsDoNotLeakAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key: db-secret"))
	}))
	defer server.Close()

	provider := NewTTS(TTSOptions{
		EndpointURL: toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:      "db-secret",
		Model:       "doubao-tts",
		Converter:   fixedTTSConverter{opusFrames: [][]byte{{0xf8, 0xff}}},
	})

	_, err := provider.Stream(context.Background(), providers.TTSRequest{Text: "Hello."})
	if err == nil {
		t.Fatal("Stream() error = nil, want handshake error")
	}
	if strings.Contains(err.Error(), "db-secret") || strings.Contains(err.Error(), "Bearer ") {
		t.Fatalf("error leaked secret material: %v", err)
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

		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read session update: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write session updated: %v", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read text append: %v", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read text done: %v", err)
		}
		close(serverStarted)

		_, _, _ = conn.ReadMessage()
		close(serverClosed)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewTTS(TTSOptions{
		EndpointURL:    toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:         "db-key",
		Model:          "doubao-tts",
		Converter:      fixedTTSConverter{opusFrames: [][]byte{{0xf8, 0xff}}},
		EventIDFactory: sequentialEventIDs("event_fixture_001", "event_fixture_002"),
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

func assertTTSSessionUpdate(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode session update JSON: %v", err)
	}
	if message["type"] != "tts_session.update" {
		t.Fatalf("session update type = %v", message["type"])
	}
	session := requireObject(t, message, "session")
	if session["voice"] != "zh_female_kailangjiejie_moon_bigtts" {
		t.Fatalf("session voice = %v", session["voice"])
	}
	if session["output_audio_format"] != "pcm" || session["output_audio_sample_rate"] != float64(16000) {
		t.Fatalf("session audio params = %#v", session)
	}
	textToSpeech := requireObject(t, session, "text_to_speech")
	if textToSpeech["model"] != "doubao-tts" {
		t.Fatalf("text_to_speech model = %v", textToSpeech["model"])
	}
}

func assertTTSTextAppend(t *testing.T, payload []byte, text string) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode input_text.append JSON: %v", err)
	}
	if message["type"] != "input_text.append" {
		t.Fatalf("text append type = %v", message["type"])
	}
	if message["event_id"] != "event_fixture_001" {
		t.Fatalf("text append event_id = %v", message["event_id"])
	}
	if message["delta"] != text {
		t.Fatalf("text append delta = %v, want %q", message["delta"], text)
	}
}

func assertTTSTextDone(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode input_text.done JSON: %v", err)
	}
	if message["type"] != "input_text.done" {
		t.Fatalf("text done type = %v", message["type"])
	}
	if message["event_id"] != "event_fixture_002" {
		t.Fatalf("text done event_id = %v", message["event_id"])
	}
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

	data, err := os.ReadFile("testdata/tts/" + name)
	if err != nil {
		t.Fatalf("read TTS fixture %s: %v", name, err)
	}
	return string(data)
}
