package dashscope

import (
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

func TestASRMetadataMatchesOfficialDocs(t *testing.T) {
	provider := NewASR(ASROptions{
		APIKey: "fake-dashscope-key",
		Model:  "fun-asr-realtime",
	})

	if provider.ProviderID() != ProviderIDASR {
		t.Fatalf("ProviderID() = %q, want %q", provider.ProviderID(), ProviderIDASR)
	}
	if provider.ModelID() != "fun-asr-realtime" {
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
		APIKey: "fake-dashscope-key",
		Model:  "fun-asr-realtime",
	})

	provider, err := registry.ASRProvider(ProviderIDASR)
	if err != nil {
		t.Fatalf("ASRProvider(%s) error = %v", ProviderIDASR, err)
	}
	if _, ok := provider.(*ASR); !ok {
		t.Fatalf("registered provider type = %T, want *ASR", provider)
	}
}

func TestASRStartsFunASRSessionAndStreamsDecodedPCM(t *testing.T) {
	opusPayload := []byte{0x01, 0x02, 0x03, 0x04}
	pcmPayload := []byte{0x10, 0x20, 0x30, 0x40}
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
		assertASRRunTask(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readASRFixture(t, "ws_server_started_or_session_updated.json"))); err != nil {
			t.Fatalf("write task-started: %v", err)
		}

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read audio frame: %v", err)
		}
		if messageType != websocket.BinaryMessage {
			t.Fatalf("audio message type = %d, want binary", messageType)
		}
		if string(payload) != string(pcmPayload) {
			t.Fatalf("audio payload = %v, want decoded pcm %v", payload, pcmPayload)
		}

		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read finish-task message: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("finish-task message type = %d, want text", messageType)
		}
		assertASRFinishTask(t, payload)

		if err := conn.WriteMessage(websocket.TextMessage, []byte(readASRFixture(t, "ws_server_finished_or_completed.json"))); err != nil {
			t.Fatalf("write final result: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"header":{"event":"task-finished","task_id":"task_fixture_dashscope_asr","attributes":{}},"payload":{"output":{},"usage":null}}`)); err != nil {
			t.Fatalf("write task-finished: %v", err)
		}
	}))
	defer server.Close()

	provider := NewASR(ASROptions{
		EndpointURL:  toWebSocketURL(server.URL) + "/api-ws/v1/inference",
		APIKey:       "fake-dashscope-key",
		WorkspaceID:  "workspace-fixture",
		Model:        "fun-asr-realtime",
		AudioFormat:  "pcm",
		SampleRateHz: 16000,
		OpusDecoderFactory: fixedPCMDecoderFactory{
			decoder: fixedPCMDecoder{pcm: pcmPayload},
		},
		TaskIDFactory: func() string { return "task_fixture_dashscope_asr" },
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
		if event.Type != providers.ASREventFinal || !event.IsFinal || event.Text != "hello." {
			t.Fatalf("ASR event = %+v, want final hello.", event)
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
	provider := NewASR(ASROptions{
		EndpointURL:  "ws://127.0.0.1:1/api-ws/v1/inference",
		APIKey:       "fake-dashscope-key",
		AudioFormat:  "pcm",
		SampleRateHz: 16000,
	})

	err := provider.ValidateProviderConfig()
	if !errors.Is(err, ErrMissingASRDecoder) {
		t.Fatalf("ValidateProviderConfig() error = %v, want ErrMissingASRDecoder", err)
	}
	_, err = provider.Start(context.Background(), providers.ASRStartRequest{})
	if !errors.Is(err, ErrMissingASRDecoder) {
		t.Fatalf("Start() error = %v, want ErrMissingASRDecoder", err)
	}
}

func TestASRRejectsNonOpusFrameForXiaozhiDecoder(t *testing.T) {
	stream := &ASRStream{
		audioFormat:  "pcm",
		sampleRateHz: 16000,
		decoder:      fixedPCMDecoder{pcm: []byte{0x01}},
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
	partial, ok, terminal, err := DecodeASRServerEvent([]byte(readASRFixture(t, "ws_server_first_result.json")))
	if err != nil {
		t.Fatalf("DecodeASRServerEvent(partial) error = %v", err)
	}
	if !ok || terminal || partial.Type != providers.ASREventPartial || partial.IsFinal || partial.Text != "hello" {
		t.Fatalf("partial event = %+v ok=%v terminal=%v", partial, ok, terminal)
	}

	final, ok, terminal, err := DecodeASRServerEvent([]byte(readASRFixture(t, "ws_server_finished_or_completed.json")))
	if err != nil {
		t.Fatalf("DecodeASRServerEvent(final) error = %v", err)
	}
	if !ok || terminal || final.Type != providers.ASREventFinal || !final.IsFinal || final.Text != "hello." {
		t.Fatalf("final event = %+v ok=%v terminal=%v", final, ok, terminal)
	}

	_, ok, terminal, err = DecodeASRServerEvent([]byte(`{"header":{"event":"task-finished","task_id":"task_fixture_dashscope_asr","attributes":{}},"payload":{"output":{},"usage":null}}`))
	if err != nil {
		t.Fatalf("DecodeASRServerEvent(task-finished) error = %v", err)
	}
	if ok || !terminal {
		t.Fatalf("task-finished ok=%v terminal=%v, want no event terminal", ok, terminal)
	}

	_, ok, terminal, err = DecodeASRServerEvent([]byte(readASRFixture(t, "ws_error_event.json")))
	if err == nil || ok || !terminal {
		t.Fatalf("task-failed event ok=%v terminal=%v err=%v, want terminal error", ok, terminal, err)
	}
	if strings.Contains(err.Error(), "fake-dashscope-key") || strings.Contains(err.Error(), "Bearer ") {
		t.Fatalf("task-failed leaked secret material: %v", err)
	}
}

func TestASRHandshakeErrorsDoNotLeakAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key: super-secret-dashscope-key"))
	}))
	defer server.Close()

	provider := NewASR(ASROptions{
		EndpointURL:        toWebSocketURL(server.URL) + "/api-ws/v1/inference",
		APIKey:             "super-secret-dashscope-key",
		OpusDecoderFactory: fixedPCMDecoderFactory{decoder: fixedPCMDecoder{pcm: []byte{0x01}}},
	})

	_, err := provider.Start(context.Background(), providers.ASRStartRequest{})
	if err == nil {
		t.Fatal("Start() error = nil, want handshake error")
	}
	if strings.Contains(err.Error(), "super-secret-dashscope-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestASRTaskFailedBeforeStartedDoesNotLeakAPIKey(t *testing.T) {
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
		taskFailed := `{"header":{"event":"task-failed","task_id":"task_fixture_dashscope_asr","error_code":"CLIENT_ERROR","error_message":"bad key: super-secret-dashscope-key","attributes":{}},"payload":{}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(taskFailed)); err != nil {
			t.Fatalf("write task-failed: %v", err)
		}
	}))
	defer server.Close()

	provider := NewASR(ASROptions{
		EndpointURL:        toWebSocketURL(server.URL) + "/api-ws/v1/inference",
		APIKey:             "super-secret-dashscope-key",
		OpusDecoderFactory: fixedPCMDecoderFactory{decoder: fixedPCMDecoder{pcm: []byte{0x01}}},
		TaskIDFactory:      func() string { return "task_fixture_dashscope_asr" },
	})

	_, err := provider.Start(context.Background(), providers.ASRStartRequest{})
	if err == nil {
		t.Fatal("Start() error = nil, want task-failed error")
	}
	if strings.Contains(err.Error(), "super-secret-dashscope-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func assertASRRunTask(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode run-task JSON: %v", err)
	}

	header := requireObject(t, message, "header")
	if header["action"] != "run-task" {
		t.Fatalf("run-task action = %v", header["action"])
	}
	if header["task_id"] != "task_fixture_dashscope_asr" {
		t.Fatalf("run-task task_id = %v", header["task_id"])
	}
	if header["streaming"] != "duplex" {
		t.Fatalf("run-task streaming = %v", header["streaming"])
	}

	payloadObject := requireObject(t, message, "payload")
	if payloadObject["task_group"] != "audio" || payloadObject["task"] != "asr" || payloadObject["function"] != "recognition" {
		t.Fatalf("run-task payload = %#v", payloadObject)
	}
	if payloadObject["model"] != "fun-asr-realtime" {
		t.Fatalf("run-task model = %v", payloadObject["model"])
	}
	parameters := requireObject(t, payloadObject, "parameters")
	if parameters["format"] != "pcm" {
		t.Fatalf("run-task format = %v", parameters["format"])
	}
	if parameters["sample_rate"] != float64(16000) {
		t.Fatalf("run-task sample_rate = %v", parameters["sample_rate"])
	}
	if parameters["semantic_punctuation_enabled"] != false {
		t.Fatalf("run-task semantic_punctuation_enabled = %v", parameters["semantic_punctuation_enabled"])
	}
	if input := requireObject(t, payloadObject, "input"); len(input) != 0 {
		t.Fatalf("run-task input = %#v, want empty object", input)
	}
}

func assertASRFinishTask(t *testing.T, payload []byte) {
	t.Helper()

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode finish-task JSON: %v", err)
	}
	header := requireObject(t, message, "header")
	if header["action"] != "finish-task" {
		t.Fatalf("finish-task action = %v", header["action"])
	}
	if header["task_id"] != "task_fixture_dashscope_asr" {
		t.Fatalf("finish-task task_id = %v", header["task_id"])
	}
	if header["streaming"] != "duplex" {
		t.Fatalf("finish-task streaming = %v", header["streaming"])
	}
	payloadObject := requireObject(t, message, "payload")
	if input := requireObject(t, payloadObject, "input"); len(input) != 0 {
		t.Fatalf("finish-task input = %#v, want empty object", input)
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

type fixedPCMDecoderFactory struct {
	decoder fixedPCMDecoder
}

func (f fixedPCMDecoderFactory) NewOpusDecoder() (OpusDecoder, error) {
	return f.decoder, nil
}

type fixedPCMDecoder struct {
	pcm []byte
}

func (d fixedPCMDecoder) DecodeOpus(audio.Frame) ([]byte, error) {
	out := make([]byte, len(d.pcm))
	copy(out, d.pcm)
	return out, nil
}
