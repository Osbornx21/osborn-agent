package dashscope

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

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/providers"
)

func TestTTSMetadataMatchesOfficialDocs(t *testing.T) {
	provider := NewTTS(TTSOptions{
		APIKey: "fake-dashscope-key",
		Model:  "cosyvoice-v3-flash",
		Voice:  "longanyang",
	})

	if provider.ProviderID() != ProviderIDTTS {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDTTS)
	}
	if provider.ModelID() != "cosyvoice-v3-flash" {
		t.Fatalf("ModelID() = %q", provider.ModelID())
	}
	if provider.VoiceID() != "longanyang" {
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
		APIKey: "fake-dashscope-key",
		Model:  "cosyvoice-v3-flash",
	})

	provider, err := registry.TTSProvider(ProviderIDTTS)
	if err != nil {
		t.Fatalf("TTSProvider(%s) error = %v", ProviderIDTTS, err)
	}
	if _, ok := provider.(*TTS); !ok {
		t.Fatalf("registered provider type = %T, want *TTS", provider)
	}
}

func TestTTSBuildsCosyVoiceSessionAndEncodesPCMToXiaozhiOpus(t *testing.T) {
	pcmPayload := []byte{0x01, 0x02, 0x03, 0x04}
	opusPayload := []byte{0xf8, 0xff, 0xfe, 0x42}
	done := make(chan struct{})
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api-ws/v1/inference" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-dashscope-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-DashScope-WorkSpace"); got != "workspace-fixture" {
			t.Fatalf("X-DashScope-WorkSpace = %q", got)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read run-task message: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("run-task message type = %d, want text", messageType)
		}
		assertTTSRunTask(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write task-started: %v", err)
		}

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read continue-task message: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("continue-task message type = %d, want text", messageType)
		}
		assertTTSContinueTask(t, payload, "Hello.")

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read finish-task message: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("finish-task message type = %d, want text", messageType)
		}
		assertTTSFinishTask(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_first_audio_delta.json"))); err != nil {
			t.Fatalf("write sentence-synthesis event: %v", err)
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, pcmPayload); err != nil {
			t.Fatalf("write binary pcm audio: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_audio_done_or_task_finished.json"))); err != nil {
			t.Fatalf("write task-finished: %v", err)
		}
	}))
	defer server.Close()

	provider := NewTTS(TTSOptions{
		EndpointURL:     toWebSocketURL(server.URL) + "/api-ws/v1/inference",
		APIKey:          "fake-dashscope-key",
		WorkspaceID:     "workspace-fixture",
		Model:           "cosyvoice-v3-flash",
		Voice:           "longanyang",
		AudioFormat:     "pcm",
		SampleRateHz:    24000,
		FrameDurationMS: 60,
		OpusEncoder:     fixedOpusEncoderFactory{encoder: &fixedOpusEncoder{wantPCM: cleanedPaddedPCM(t, pcmPayload, 24000, 1, 60, true), opus: opusPayload}},
		TaskIDFactory:   func() string { return "task_fixture_dashscope_tts" },
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
			t.Fatalf("frame opus = %v, want encoded xiaozhi opus %v", frame.Opus, opusPayload)
		}
		if frame.TextSpan != "Hello." {
			t.Fatalf("frame text span = %q", frame.TextSpan)
		}
		if frame.Duration != 60*time.Millisecond {
			t.Fatalf("frame duration = %s, want 60ms", frame.Duration)
		}
		if !frame.AudioQuality.HasSamples() {
			t.Fatalf("frame audio quality missing: %+v", frame.AudioQuality)
		}
		if frame.AudioQuality.SampleRateHz != 24000 || frame.AudioQuality.Channels != 1 || frame.AudioQuality.SampleCount != 1440 {
			t.Fatalf("frame audio quality = %+v, want 1440 pcm16 samples at 24kHz mono", frame.AudioQuality)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for TTS frame")
	}

	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("frame channel still open after task-finished")
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

func TestTTSRejectsMissingEncoderForPCMProfile(t *testing.T) {
	provider := NewTTS(TTSOptions{
		APIKey:      "fake-dashscope-key",
		AudioFormat: "pcm",
	})

	_, err := provider.Stream(context.Background(), providers.TTSRequest{Text: "Hello."})
	if !errors.Is(err, ErrMissingTTSEncoder) {
		t.Fatalf("Stream() error = %v, want ErrMissingTTSEncoder", err)
	}
}

func TestTTSRejectsProviderOpusPassthrough(t *testing.T) {
	provider := NewTTS(TTSOptions{
		APIKey:      "fake-dashscope-key",
		AudioFormat: "opus",
	})

	_, err := provider.Stream(context.Background(), providers.TTSRequest{Text: "Hello."})
	if !errors.Is(err, ErrUnsupportedTTSAudioFormat) {
		t.Fatalf("Stream() error = %v, want ErrUnsupportedTTSAudioFormat", err)
	}
}

func TestTTSParsesServerFixtures(t *testing.T) {
	event, err := DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_first_audio_delta.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(sentence-synthesis) error = %v", err)
	}
	if event.Event != "result-generated" || event.OutputType != "sentence-synthesis" || !event.ExpectsBinaryAudio || event.Terminal {
		t.Fatalf("sentence-synthesis event = %+v", event)
	}

	event, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_server_audio_done_or_task_finished.json")))
	if err != nil {
		t.Fatalf("DecodeTTSServerEvent(task-finished) error = %v", err)
	}
	if event.Event != "task-finished" || !event.Terminal || event.Characters != 6 {
		t.Fatalf("task-finished event = %+v", event)
	}

	_, err = DecodeTTSServerEvent([]byte(readTTSFixture(t, "ws_error_event.json")))
	if err == nil {
		t.Fatal("DecodeTTSServerEvent(task-failed) error = nil, want error")
	}
	if strings.Contains(err.Error(), "fake-dashscope-key") || strings.Contains(err.Error(), "Bearer ") {
		t.Fatalf("task-failed leaked secret material: %v", err)
	}
}

func TestTTSHandshakeErrorsDoNotLeakAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key: super-secret-dashscope-key"))
	}))
	defer server.Close()

	provider := NewTTS(TTSOptions{
		EndpointURL: toWebSocketURL(server.URL) + "/api-ws/v1/inference",
		APIKey:      "super-secret-dashscope-key",
		OpusEncoder: fixedOpusEncoderFactory{encoder: &fixedOpusEncoder{}},
	})

	_, err := provider.Stream(context.Background(), providers.TTSRequest{Text: "Hello."})
	if err == nil {
		t.Fatal("Stream() error = nil, want handshake error")
	}
	if strings.Contains(err.Error(), "super-secret-dashscope-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestTTSTaskFailedBeforeStartedDoesNotLeakAPIKey(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read run-task message: %v", err)
		}
		taskFailed := `{"header":{"event":"task-failed","task_id":"task_fixture_dashscope_tts","error_code":"InvalidParameter","error_message":"bad key: super-secret-dashscope-key","attributes":{}},"payload":{}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(taskFailed)); err != nil {
			t.Fatalf("write task-failed: %v", err)
		}
	}))
	defer server.Close()

	provider := NewTTS(TTSOptions{
		EndpointURL:   toWebSocketURL(server.URL) + "/api-ws/v1/inference",
		APIKey:        "super-secret-dashscope-key",
		OpusEncoder:   fixedOpusEncoderFactory{encoder: &fixedOpusEncoder{}},
		TaskIDFactory: func() string { return "task_fixture_dashscope_tts" },
	})

	_, err := provider.Stream(context.Background(), providers.TTSRequest{Text: "Hello."})
	if err == nil {
		t.Fatal("Stream() error = nil, want task-failed error")
	}
	if strings.Contains(err.Error(), "super-secret-dashscope-key") {
		t.Fatalf("error leaked API key: %v", err)
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
			t.Fatalf("read run-task message: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(readTTSFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write task-started: %v", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read continue-task message: %v", err)
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read finish-task message: %v", err)
		}
		close(serverStarted)

		_, _, _ = conn.ReadMessage()
		close(serverClosed)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewTTS(TTSOptions{
		EndpointURL:   toWebSocketURL(server.URL) + "/api-ws/v1/inference",
		APIKey:        "fake-dashscope-key",
		OpusEncoder:   fixedOpusEncoderFactory{encoder: &fixedOpusEncoder{}},
		TaskIDFactory: func() string { return "task_fixture_dashscope_tts" },
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

func assertTTSRunTask(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode run-task JSON: %v", err)
	}

	header := requireObject(t, message, "header")
	if header["action"] != "run-task" || header["task_id"] != "task_fixture_dashscope_tts" || header["streaming"] != "duplex" {
		t.Fatalf("run-task header = %#v", header)
	}
	payloadObject := requireObject(t, message, "payload")
	if payloadObject["task_group"] != "audio" || payloadObject["task"] != "tts" || payloadObject["function"] != "SpeechSynthesizer" {
		t.Fatalf("run-task payload = %#v", payloadObject)
	}
	if payloadObject["model"] != "cosyvoice-v3-flash" {
		t.Fatalf("run-task model = %v", payloadObject["model"])
	}
	parameters := requireObject(t, payloadObject, "parameters")
	if parameters["text_type"] != "PlainText" || parameters["voice"] != "longanyang" {
		t.Fatalf("run-task text_type/voice = %#v", parameters)
	}
	if parameters["format"] != "pcm" || parameters["sample_rate"] != float64(24000) {
		t.Fatalf("run-task audio params = %#v", parameters)
	}
	if parameters["volume"] != float64(50) || parameters["rate"] != float64(1) || parameters["pitch"] != float64(1) {
		t.Fatalf("run-task tuning params = %#v", parameters)
	}
	if parameters["enable_ssml"] != false {
		t.Fatalf("run-task enable_ssml = %v", parameters["enable_ssml"])
	}
	if input := requireObject(t, payloadObject, "input"); len(input) != 0 {
		t.Fatalf("run-task input = %#v, want empty object", input)
	}
}

func paddedPCM(payload []byte, sampleRateHz int, channels int, frameDurationMS int) []byte {
	size := sampleRateHz * channels * frameDurationMS / 1000 * 2
	out := make([]byte, size)
	copy(out, payload)
	return out
}

func cleanedPaddedPCM(t *testing.T, payload []byte, sampleRateHz int, channels int, frameDurationMS int, final bool) []byte {
	t.Helper()
	pcm := paddedPCM(payload, sampleRateHz, channels, frameDurationMS)
	cleaner := audio.NewPCM16StreamCleaner(audio.PCM16CleanerOptions{
		SampleRateHz: sampleRateHz,
		Channels:     channels,
		RemoveDC:     true,
	})
	if err := cleaner.CleanFrame(pcm, final); err != nil {
		t.Fatalf("CleanFrame() error = %v", err)
	}
	return pcm
}

type fixedOpusEncoderFactory struct {
	encoder *fixedOpusEncoder
}

func (f fixedOpusEncoderFactory) NewOpusEncoder(sampleRateHz int, channels int, frameDurationMS int) (audio.OpusPCMEncoder, error) {
	return f.encoder, nil
}

type fixedOpusEncoder struct {
	wantPCM []byte
	opus    []byte
	closed  bool
}

func (e *fixedOpusEncoder) EncodePCM(pcm []byte) ([]byte, error) {
	if len(e.wantPCM) > 0 && !bytes.Equal(pcm, e.wantPCM) {
		return nil, errors.New("unexpected pcm input")
	}
	if len(e.opus) == 0 {
		return []byte{0xf8, 0xff, 0xfe}, nil
	}
	out := make([]byte, len(e.opus))
	copy(out, e.opus)
	return out, nil
}

func (e *fixedOpusEncoder) Close() error {
	e.closed = true
	return nil
}

func assertTTSContinueTask(t *testing.T, payload []byte, text string) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode continue-task JSON: %v", err)
	}
	header := requireObject(t, message, "header")
	if header["action"] != "continue-task" || header["task_id"] != "task_fixture_dashscope_tts" || header["streaming"] != "duplex" {
		t.Fatalf("continue-task header = %#v", header)
	}
	payloadObject := requireObject(t, message, "payload")
	input := requireObject(t, payloadObject, "input")
	if input["text"] != text {
		t.Fatalf("continue-task text = %v, want %q", input["text"], text)
	}
}

func assertTTSFinishTask(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode finish-task JSON: %v", err)
	}
	header := requireObject(t, message, "header")
	if header["action"] != "finish-task" || header["task_id"] != "task_fixture_dashscope_tts" || header["streaming"] != "duplex" {
		t.Fatalf("finish-task header = %#v", header)
	}
	payloadObject := requireObject(t, message, "payload")
	if input := requireObject(t, payloadObject, "input"); len(input) != 0 {
		t.Fatalf("finish-task input = %#v, want empty object", input)
	}
}

func readTTSFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("testdata/tts/" + name)
	if err != nil {
		t.Fatalf("read TTS fixture %s: %v", name, err)
	}
	return string(data)
}
