package doubao

import (
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

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/providers"
)

func TestASRMetadataMatchesOfficialDocs(t *testing.T) {
	provider := NewASR(ASROptions{
		APIKey:      "db-key",
		Model:       "bigmodel",
		OpusDecoder: fixedPCMDecoder{pcm: []byte{0x01, 0x02}},
	})

	if provider.ProviderID() != ProviderIDASR {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDASR)
	}
	if provider.ModelID() != "bigmodel" {
		t.Fatalf("ModelID() = %q", provider.ModelID())
	}
	if provider.SourceDocURL() != SourceDocURLASR {
		t.Fatalf("SourceDocURL() = %q, want %q", provider.SourceDocURL(), SourceDocURLASR)
	}
	if provider.SourceDocCheckedAt() != SourceDocCheckedAt {
		t.Fatalf("SourceDocCheckedAt() = %q, want %q", provider.SourceDocCheckedAt(), SourceDocCheckedAt)
	}
}

func TestRegisterASRAddsFactoryToProviderRegistry(t *testing.T) {
	registry := providers.NewRegistry(providers.MockConfig{})
	RegisterASR(registry, ASROptions{
		APIKey:      "db-key",
		Model:       "bigmodel",
		OpusDecoder: fixedPCMDecoder{pcm: []byte{0x01, 0x02}},
	})

	provider, err := registry.ASRProvider(ProviderIDASR)
	if err != nil {
		t.Fatalf("ASRProvider(%s) error = %v", ProviderIDASR, err)
	}
	if _, ok := provider.(*ASR); !ok {
		t.Fatalf("registered provider type = %T, want *ASR", provider)
	}
}

func TestASRStartsRealtimeSessionAndSendsDecodedPCMBase64(t *testing.T) {
	opusPayload := []byte{0x01, 0x02, 0x03, 0x04}
	pcmPayload := []byte{0x10, 0x20, 0x30, 0x40}
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
		if r.URL.Query().Get("model") != "bigmodel" {
			t.Fatalf("model query = %q", r.URL.Query().Get("model"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer db-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Api-Resource-Id"); got != "volc.bigasr.sauc.duration" {
			t.Fatalf("X-Api-Resource-Id = %q", got)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read transcription_session.update: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("session update message type = %d, want text", messageType)
		}
		assertASRSessionUpdate(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readASRFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write session updated: %v", err)
		}

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read audio append: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("audio append message type = %d, want text", messageType)
		}
		assertASRAudioAppend(t, payload, pcmPayload)

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read commit: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("commit message type = %d, want text", messageType)
		}
		assertASRCommit(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readASRFixture(t, "ws_server_finished_or_completed.json"))); err != nil {
			t.Fatalf("write completed: %v", err)
		}
	}))
	defer server.Close()

	provider := NewASR(ASROptions{
		EndpointURL:    toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:         "db-key",
		ResourceID:     "volc.bigasr.sauc.duration",
		Model:          "bigmodel",
		OpusDecoder:    fixedPCMDecoder{pcm: pcmPayload},
		EventIDFactory: sequentialEventIDs("event_fixture_001", "event_fixture_002"),
	})

	stream, err := provider.Start(context.Background(), providers.ASRStartRequest{
		SessionID: "session-fixture",
		DeviceID:  "stackchan-s3-fixture",
		StartedAt: time.Date(2026, 6, 6, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer stream.Close()

	if err := stream.AcceptOpus(audio.NewOpusFrame(opusPayload, 16000, 60, time.Now())); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}
	if err := stream.Finish(); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	select {
	case event := <-stream.Events():
		if event.Type != providers.ASREventFinal || !event.IsFinal || event.Text != "hello" {
			t.Fatalf("ASR event = %+v, want final hello", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ASR final")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not finish websocket flow")
	}
}

func TestASRRejectsMissingOpusDecoderBeforeDial(t *testing.T) {
	serverHit := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		serverHit <- struct{}{}
	}))
	defer server.Close()

	provider := NewASR(ASROptions{
		EndpointURL: toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:      "db-key",
		Model:       "bigmodel",
	})

	_, err := provider.Start(context.Background(), providers.ASRStartRequest{})
	if !errors.Is(err, ErrMissingASRDecoder) {
		t.Fatalf("Start() error = %v, want ErrMissingASRDecoder", err)
	}

	select {
	case <-serverHit:
		t.Fatal("server was dialed before decoder gate")
	default:
	}
}

func TestASRRejectsNonOpusFrameBeforeDecode(t *testing.T) {
	stream := &ASRStream{
		decoder:      fixedPCMDecoder{pcm: []byte{0x01, 0x02}},
		sampleRateHz: 16000,
		events:       make(chan providers.ASREvent),
	}

	err := stream.AcceptOpus(audio.Frame{
		Format:          "pcm",
		SampleRateHz:    16000,
		Channels:        1,
		FrameDurationMS: 60,
		Payload:         []byte{0x00},
	})
	if !errors.Is(err, ErrUnsupportedASRAudioFrame) {
		t.Fatalf("AcceptOpus() error = %v, want ErrUnsupportedASRAudioFrame", err)
	}
}

func TestASRParsesServerFixtures(t *testing.T) {
	_, ok, terminal, err := DecodeASRServerEvent([]byte(readASRFixture(t, "ws_server_started_or_session_updated.json")))
	if err != nil {
		t.Fatalf("DecodeASRServerEvent(updated) error = %v", err)
	}
	if ok || terminal {
		t.Fatalf("updated ok=%v terminal=%v, want no event nonterminal", ok, terminal)
	}

	partial, ok, terminal, err := DecodeASRServerEvent([]byte(readASRFixture(t, "ws_server_first_result.json")))
	if err != nil {
		t.Fatalf("DecodeASRServerEvent(result) error = %v", err)
	}
	if !ok || terminal || partial.Type != providers.ASREventPartial || partial.IsFinal || partial.Text != "hello" {
		t.Fatalf("partial event = %+v ok=%v terminal=%v", partial, ok, terminal)
	}

	final, ok, terminal, err := DecodeASRServerEvent([]byte(readASRFixture(t, "ws_server_finished_or_completed.json")))
	if err != nil {
		t.Fatalf("DecodeASRServerEvent(completed) error = %v", err)
	}
	if !ok || !terminal || final.Type != providers.ASREventFinal || !final.IsFinal || final.Text != "hello" {
		t.Fatalf("final event = %+v ok=%v terminal=%v", final, ok, terminal)
	}

	_, ok, terminal, err = DecodeASRServerEvent([]byte(readASRFixture(t, "ws_error_event.json")))
	if err == nil || ok || !terminal {
		t.Fatalf("error event ok=%v terminal=%v err=%v, want terminal error", ok, terminal, err)
	}

	_, ok, terminal, err = DecodeASRServerEvent([]byte(readASRFixture(t, "ws_error_resource_mismatch_55000000.json")))
	if err == nil || ok || !terminal {
		t.Fatalf("resource mismatch event ok=%v terminal=%v err=%v, want terminal error", ok, terminal, err)
	}
	if !strings.Contains(err.Error(), "55000000") {
		t.Fatalf("resource mismatch error = %v, want code 55000000", err)
	}
}

func TestASRHandshakeErrorsDoNotLeakAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key: db-secret"))
	}))
	defer server.Close()

	provider := NewASR(ASROptions{
		EndpointURL: toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:      "db-secret",
		Model:       "bigmodel",
		OpusDecoder: fixedPCMDecoder{pcm: []byte{0x01, 0x02}},
	})

	_, err := provider.Start(context.Background(), providers.ASRStartRequest{})
	if err == nil {
		t.Fatal("Start() error = nil, want handshake error")
	}
	if strings.Contains(err.Error(), "db-secret") || strings.Contains(err.Error(), "Bearer ") {
		t.Fatalf("error leaked secret material: %v", err)
	}
}

func TestASRContextCancelClosesWebSocketAndEvents(t *testing.T) {
	upgrader := websocket.Upgrader{}
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read session update: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(readASRFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write session updated: %v", err)
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewASR(ASROptions{
		EndpointURL: toWebSocketURL(server.URL) + "/v1/realtime",
		APIKey:      "db-key",
		Model:       "bigmodel",
		OpusDecoder: fixedPCMDecoder{pcm: []byte{0x01, 0x02}},
	})

	stream, err := provider.Start(ctx, providers.ASRStartRequest{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not start streaming")
	}

	cancel()

	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("event channel still open after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ASR stream close after cancel")
	}
}

func assertASRSessionUpdate(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode session update JSON: %v", err)
	}
	if message["type"] != "transcription_session.update" {
		t.Fatalf("session update type = %v", message["type"])
	}
	session := requireObject(t, message, "session")
	if session["input_audio_format"] != "pcm" || session["input_audio_codec"] != "raw" {
		t.Fatalf("session audio format = %#v", session)
	}
	if session["input_audio_sample_rate"] != float64(16000) || session["input_audio_bits"] != float64(16) || session["input_audio_channel"] != float64(1) {
		t.Fatalf("session audio params = %#v", session)
	}
	transcription := requireObject(t, session, "input_audio_transcription")
	if transcription["model"] != "bigmodel" {
		t.Fatalf("transcription model = %v", transcription["model"])
	}
}

func assertASRAudioAppend(t *testing.T, payload []byte, wantPCM []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode audio append JSON: %v", err)
	}
	if message["type"] != "input_audio_buffer.append" {
		t.Fatalf("audio append type = %v", message["type"])
	}
	if message["event_id"] != "event_fixture_001" {
		t.Fatalf("audio append event_id = %v", message["event_id"])
	}
	audioData, ok := message["audio"].(string)
	if !ok || audioData == "" {
		t.Fatalf("audio append audio = %#v", message["audio"])
	}
	gotPCM, err := base64.StdEncoding.DecodeString(audioData)
	if err != nil {
		t.Fatalf("decode audio base64: %v", err)
	}
	if string(gotPCM) != string(wantPCM) {
		t.Fatalf("pcm payload = %v, want %v", gotPCM, wantPCM)
	}
}

func assertASRCommit(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode commit JSON: %v", err)
	}
	if message["type"] != "input_audio_buffer.commit" {
		t.Fatalf("commit type = %v", message["type"])
	}
	if message["event_id"] != "event_fixture_002" {
		t.Fatalf("commit event_id = %v", message["event_id"])
	}
}

func requireObject(t *testing.T, parent map[string]any, name string) map[string]any {
	t.Helper()

	value, ok := parent[name].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", name, parent[name])
	}
	return value
}

type fixedPCMDecoder struct {
	pcm []byte
	err error
}

func (d fixedPCMDecoder) DecodeOpus(frame audio.Frame) ([]byte, error) {
	if d.err != nil {
		return nil, d.err
	}
	if frame.Format != audio.FormatOpus {
		return nil, ErrUnsupportedASRAudioFrame
	}
	out := make([]byte, len(d.pcm))
	copy(out, d.pcm)
	return out, nil
}

func sequentialEventIDs(ids ...string) func() string {
	index := 0
	return func() string {
		if index >= len(ids) {
			return "event_extra"
		}
		id := ids[index]
		index++
		return id
	}
}

func readASRFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("testdata/asr/" + name)
	if err != nil {
		t.Fatalf("read ASR fixture %s: %v", name, err)
	}
	return string(data)
}

func toWebSocketURL(url string) string {
	return "ws" + strings.TrimPrefix(url, "http")
}
