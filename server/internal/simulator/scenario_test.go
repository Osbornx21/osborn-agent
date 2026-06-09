package simulator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providerprobe"
)

func TestRunScenarioHelloOnlyCompletesHandshake(t *testing.T) {
	server := newHelloOnlyXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioHelloOnly,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || summary.Success != 1 || summary.Failures != 0 {
		t.Fatalf("summary = %+v, want one successful hello-only handshake", summary)
	}
	if summary.P50FirstAudioMS != 0 || summary.P95FirstAudioMS != 0 {
		t.Fatalf("hello-only first-audio latency = p50 %d p95 %d, want zero", summary.P50FirstAudioMS, summary.P95FirstAudioMS)
	}
}

func TestRunScenarioCompletesHappyPathTurns(t *testing.T) {
	server := newScriptedXiaozhiServer(t, scriptedServerOptions{Turns: 2})
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioHappyPath20Turns,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Turns:      2,
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || summary.Success != 2 || summary.Failures != 0 {
		t.Fatalf("summary = %+v, want 2 successful turns", summary)
	}
	if summary.P50FirstAudioMS <= 0 || summary.P95FirstAudioMS <= 0 {
		t.Fatalf("first audio latency summary = p50 %d p95 %d, want positive values", summary.P50FirstAudioMS, summary.P95FirstAudioMS)
	}
}

func TestRunScenarioCompletesProtocolV3WrappedHappyPath(t *testing.T) {
	server := newProtocolV3WrappedXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:        ScenarioHappyPath20Turns,
		GatewayURL:      wsURLFor(server),
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
		AuthToken:       "test-token",
		ProtocolVersion: xiaozhi.BinaryProtocolV3,
		Turns:           1,
		Timeout:         2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || summary.Success != 1 || summary.Failures != 0 {
		t.Fatalf("summary = %+v, want one protocol-v3 wrapped successful turn", summary)
	}
}

func TestRunScenarioRejectsUnsupportedProtocolVersion(t *testing.T) {
	server := newHelloOnlyXiaozhiServer(t)
	defer server.Close()

	_, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:        ScenarioHelloOnly,
		GatewayURL:      wsURLFor(server),
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
		AuthToken:       "test-token",
		ProtocolVersion: 4,
		Timeout:         2 * time.Second,
	})
	if err == nil {
		t.Fatal("RunScenario() error = nil, want unsupported protocol version rejection")
	}
	if !strings.Contains(err.Error(), "unsupported xiaozhi binary protocol version 4") {
		t.Fatalf("RunScenario() error = %v, want unsupported protocol version", err)
	}
}

func TestRunScenarioCompletesASRFinalWithoutListenStop(t *testing.T) {
	server := newScriptedXiaozhiServer(t, scriptedServerOptions{Turns: 1, ExpectNoListenStop: true})
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioASRFinalWithoutListenStop,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || summary.Success != 1 || !summary.ASRFinalWithoutListenStop {
		t.Fatalf("summary = %+v, want ASR final without listen stop pass", summary)
	}
}

func TestRunScenarioSendsAbortDuringTTS(t *testing.T) {
	server := newScriptedXiaozhiServer(t, scriptedServerOptions{Turns: 1, ExpectAbort: true})
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioAbortDuringTTS,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || !summary.AbortOldAudioDropped {
		t.Fatalf("summary = %+v, want abort scenario to pass and drop stale audio", summary)
	}
}

func TestRunScenarioUsesASROpusFixtureFrames(t *testing.T) {
	fixturePath := filepath.Join(t.TempDir(), "spoken-opus.json")
	wantFrames := variedFixtureFrames(12, 16)
	if err := providerprobe.WriteASROpusFixture(fixturePath, wantFrames); err != nil {
		t.Fatalf("WriteASROpusFixture() error = %v", err)
	}
	recorder := &scriptedFrameRecorder{}
	server := newScriptedXiaozhiServer(t, scriptedServerOptions{Turns: 1, Recorder: recorder})
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:       ScenarioHappyPath20Turns,
		GatewayURL:     wsURLFor(server),
		DeviceID:       "stackchan-s3-main",
		ClientID:       "stackchan-s3-main-client",
		AuthToken:      "test-token",
		Turns:          1,
		Timeout:        2 * time.Second,
		ASRFixturePath: fixturePath,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed {
		t.Fatalf("summary = %+v, want passed", summary)
	}
	got := recorder.Frames()
	if len(got) != len(wantFrames) {
		t.Fatalf("fixture frames sent = %d, want %d", len(got), len(wantFrames))
	}
	if !bytes.Equal(got[0], wantFrames[0]) || !bytes.Equal(got[len(got)-1], wantFrames[len(wantFrames)-1]) {
		t.Fatalf("fixture frame boundaries mismatch")
	}
}

func TestRunScenarioRejectsSlowFirstAudio(t *testing.T) {
	server := newScriptedXiaozhiServer(t, scriptedServerOptions{
		Turns:           1,
		FirstAudioDelay: 80 * time.Millisecond,
	})
	defer server.Close()

	_, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:        ScenarioProviderSlowFirstAudio,
		GatewayURL:      wsURLFor(server),
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
		AuthToken:       "test-token",
		Timeout:         500 * time.Millisecond,
		MaxFirstAudioMS: 20,
	})
	if err == nil {
		t.Fatal("RunScenario() error = nil, want slow first-audio failure")
	}
	if !strings.Contains(err.Error(), "first audio") || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("RunScenario() error = %v, want first audio exceeded failure", err)
	}
}

func TestRunScenarioProviderSlowFirstAudioPassesWithinBudget(t *testing.T) {
	server := newScriptedXiaozhiServer(t, scriptedServerOptions{Turns: 1})
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:        ScenarioProviderSlowFirstAudio,
		GatewayURL:      wsURLFor(server),
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
		AuthToken:       "test-token",
		Turns:           1,
		Timeout:         500 * time.Millisecond,
		MaxFirstAudioMS: 500,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || summary.Success != 1 || summary.MaxFirstAudioMS != 500 {
		t.Fatalf("summary = %+v, want passed with max first audio budget", summary)
	}
}

func TestRunScenarioWebSocketReconnectReplacesOldConnection(t *testing.T) {
	server := newReconnectXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioWSReconnect,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || !summary.ReconnectOldClosed || summary.Success != 1 {
		t.Fatalf("summary = %+v, want reconnect pass with old connection closed", summary)
	}
}

func TestRunScenarioWebSocketReconnectFailsWhenOldConnectionStaysOpen(t *testing.T) {
	server, release := newReconnectWithoutOldCloseXiaozhiServer(t)
	defer release()
	defer server.Close()

	_, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioWSReconnect,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("RunScenario() error = nil, want old connection still open failure")
	}
	if !strings.Contains(err.Error(), "old websocket") {
		t.Fatalf("RunScenario() error = %v, want old websocket failure", err)
	}
}

func TestRunScenarioMCPHeadMotionCompletesToolCall(t *testing.T) {
	server := newMCPHeadMotionXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioMCPHeadMotion,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || !summary.MCPHeadMotion {
		t.Fatalf("summary = %+v, want MCP head motion pass", summary)
	}
}

func TestRunScenarioMCPDisplaySceneCompletesToolCall(t *testing.T) {
	server := newMCPDisplaySceneXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioMCPDisplayScene,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed {
		t.Fatalf("summary = %+v, want MCP display scene pass", summary)
	}
}

func TestRunScenarioOfficialStackChanV141ToolsListExcludesCustomScreenScene(t *testing.T) {
	server := newOfficialStackChanV141ToolsListXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:        ScenarioOfficialStackChanV141ToolsList,
		FirmwareProfile: FirmwareProfileOfficialStackChanV141,
		GatewayURL:      wsURLFor(server),
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
		AuthToken:       "test-token",
		Timeout:         2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || !summary.OfficialStackChanV141 || summary.FirmwareProfile != FirmwareProfileOfficialStackChanV141 {
		t.Fatalf("summary = %+v, want official StackChan V1.4.1 tools-list pass", summary)
	}
}

func TestRunScenarioOfficialStackChanV141RejectsCustomDisplaySceneToolCall(t *testing.T) {
	server, done := newUnsupportedDisplaySceneXiaozhiServer(t)
	defer server.Close()

	_, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:        ScenarioMCPDisplayScene,
		FirmwareProfile: FirmwareProfileOfficialStackChanV141,
		GatewayURL:      wsURLFor(server),
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
		AuthToken:       "test-token",
		Timeout:         2 * time.Second,
	})
	if err == nil {
		t.Fatal("RunScenario() error = nil, want official profile to reject self.screen.set_scene")
	}
	if !strings.Contains(err.Error(), "official-v1.4.1") || !strings.Contains(err.Error(), mcp.ToolSetScreenScene) {
		t.Fatalf("RunScenario() error = %v, want official-v1.4.1 screen-scene rejection", err)
	}
	select {
	case serverErr := <-done:
		if serverErr != nil {
			t.Fatalf("server observed unexpected result: %v", serverErr)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not observe simulator disconnect after unsupported screen-scene call")
	}
}

func TestRunScenarioOfficialStackChanV141LEDFeedbackCompletesToolCall(t *testing.T) {
	server := newMCPLEDFeedbackXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:        ScenarioMCPLEDFeedback,
		FirmwareProfile: FirmwareProfileOfficialStackChanV141,
		GatewayURL:      wsURLFor(server),
		DeviceID:        "stackchan-s3-main",
		ClientID:        "stackchan-s3-main-client",
		AuthToken:       "test-token",
		Timeout:         2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || !summary.MCPLEDFeedback {
		t.Fatalf("summary = %+v, want official StackChan V1.4.1 LED feedback pass", summary)
	}
}

func TestRunScenarioMCPLEDFeedbackCompletesToolCall(t *testing.T) {
	server := newMCPLEDFeedbackXiaozhiServer(t)
	defer server.Close()

	summary, err := RunScenario(context.Background(), ScenarioOptions{
		Scenario:   ScenarioMCPLEDFeedback,
		GatewayURL: wsURLFor(server),
		DeviceID:   "stackchan-s3-main",
		ClientID:   "stackchan-s3-main-client",
		AuthToken:  "test-token",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunScenario() error = %v", err)
	}
	if !summary.Passed || !summary.MCPLEDFeedback {
		t.Fatalf("summary = %+v, want MCP LED feedback pass", summary)
	}
}

type scriptedServerOptions struct {
	Turns              int
	ExpectAbort        bool
	ExpectNoListenStop bool
	Recorder           *scriptedFrameRecorder
	FirstAudioDelay    time.Duration
}

func newScriptedXiaozhiServer(t *testing.T, options scriptedServerOptions) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(xiaozhi.HeaderAuthorization); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q, want bearer token", got)
		}
		if got := r.Header.Get(xiaozhi.HeaderDeviceID); got != "stackchan-s3-main" {
			t.Fatalf("device id header = %q, want configured device", got)
		}
		if got := r.Header.Get(xiaozhi.HeaderClientID); got != "stackchan-s3-main-client" {
			t.Fatalf("client id header = %q, want configured client", got)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_sim"))

		turns := options.Turns
		if turns <= 0 {
			turns = 1
		}
		for turn := 0; turn < turns; turn++ {
			readClientListenState(t, conn, "start")
			if options.ExpectNoListenStop {
				readClientAudioWithoutListenStop(t, conn, options.Recorder)
			} else {
				readClientListenStopAfterAudio(t, conn, options.Recorder)
			}

			writeServerJSON(t, conn, xiaozhi.NewSTT("sess_sim", "你好，我是 StackChan。"))
			writeServerJSON(t, conn, xiaozhi.NewTTSStart("sess_sim"))
			writeServerJSON(t, conn, xiaozhi.NewTTSSentenceStart("sess_sim", "你好，"))
			if options.FirstAudioDelay > 0 {
				time.Sleep(options.FirstAudioDelay)
			}
			writeServerBinary(t, conn)
			if options.ExpectAbort {
				readClientJSONType(t, conn, xiaozhi.MessageTypeAbort)
				writeServerJSON(t, conn, xiaozhi.NewTTSStop("sess_sim"))
				return
			}
			writeServerJSON(t, conn, xiaozhi.NewTTSStop("sess_sim"))
		}
	}))
}

func wsURLFor(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func newHelloOnlyXiaozhiServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(xiaozhi.HeaderAuthorization); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q, want bearer token", got)
		}
		if got := r.Header.Get(xiaozhi.HeaderDeviceID); got != "stackchan-s3-main" {
			t.Fatalf("device id header = %q, want configured device", got)
		}
		if got := r.Header.Get(xiaozhi.HeaderClientID); got != "stackchan-s3-main-client" {
			t.Fatalf("client id header = %q, want configured client", got)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_sim"))
	}))
}

func newProtocolV3WrappedXiaozhiServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(xiaozhi.HeaderAuthorization); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q, want bearer token", got)
		}
		if got := r.Header.Get(xiaozhi.HeaderProtocolVersion); got != "3" {
			t.Fatalf("protocol version header = %q, want 3", got)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_sim"))
		readClientListenState(t, conn, "start")
		readClientProtocolV3AudioBeforeListenStop(t, conn)
		writeServerJSON(t, conn, xiaozhi.NewSTT("sess_sim", "你好，我是 StackChan。"))
		writeServerJSON(t, conn, xiaozhi.NewTTSStart("sess_sim"))
		writeServerJSON(t, conn, xiaozhi.NewTTSSentenceStart("sess_sim", "你好，"))
		writeProtocolV3ServerBinary(t, conn)
		writeServerJSON(t, conn, xiaozhi.NewTTSStop("sess_sim"))
	}))
}

func newReconnectXiaozhiServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	firstConnReady := make(chan *websocket.Conn, 1)
	reconnectAllowed := make(chan struct{})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(xiaozhi.HeaderAuthorization); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q, want bearer token", got)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}

		readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_sim"))

		select {
		case firstConnReady <- conn:
			<-reconnectAllowed
			_ = conn.Close()
			return
		default:
			defer conn.Close()
			first := <-firstConnReady
			_ = first.Close()
			close(reconnectAllowed)

			readClientListenState(t, conn, "start")
			readClientListenStopAfterAudio(t, conn, nil)
			writeServerJSON(t, conn, xiaozhi.NewSTT("sess_sim_2", "重连后继续。"))
			writeServerJSON(t, conn, xiaozhi.NewTTSStart("sess_sim_2"))
			writeServerJSON(t, conn, xiaozhi.NewTTSSentenceStart("sess_sim_2", "继续。"))
			writeServerBinary(t, conn)
			writeServerJSON(t, conn, xiaozhi.NewTTSStop("sess_sim_2"))
		}
	}))
}

func newReconnectWithoutOldCloseXiaozhiServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	firstConnReady := make(chan *websocket.Conn, 1)
	release := make(chan struct{})
	releaseOnce := sync.Once{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}

		readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_sim"))

		select {
		case firstConnReady <- conn:
			<-release
			_ = conn.Close()
			return
		default:
			defer conn.Close()
			<-firstConnReady
			<-release
		}
	}))
	return server, func() {
		releaseOnce.Do(func() { close(release) })
	}
}

func newMCPHeadMotionXiaozhiServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		hello := readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		features, ok := hello["features"].(map[string]any)
		if !ok || features["mcp"] != true {
			t.Fatalf("hello features = %#v, want mcp true", hello["features"])
		}
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_mcp_sim"))

		initialize := sendServerMCPRequest(t, conn, 1, mcp.MethodInitialize, mcp.DefaultInitializeParams())
		readClientMCPResponse(t, conn, initialize)
		toolsList := sendServerMCPRequest(t, conn, 2, mcp.MethodToolsList, mcp.ToolsListParams{WithUserTools: false})
		readClientMCPResponse(t, conn, toolsList)

		readClientListenState(t, conn, "start")
		call := sendServerMCPRequest(t, conn, 3, mcp.MethodToolsCall, mcp.ToolCallParams{
			Name: mcp.ToolSetHeadAngles,
			Arguments: map[string]any{
				"yaw":   0,
				"pitch": 8,
				"speed": 150,
			},
		})
		if !readClientMCPResponseDuringUplink(t, conn, call) {
			readClientListenStopAfterAudio(t, conn, nil)
		}

		writeServerJSON(t, conn, xiaozhi.NewSTT("sess_mcp_sim", "你好，我在听。"))
		writeServerJSON(t, conn, xiaozhi.NewTTSStart("sess_mcp_sim"))
		writeServerJSON(t, conn, xiaozhi.NewTTSSentenceStart("sess_mcp_sim", "我在听。"))
		writeServerBinary(t, conn)
		writeServerJSON(t, conn, xiaozhi.NewTTSStop("sess_mcp_sim"))
	}))
}

func newMCPDisplaySceneXiaozhiServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		hello := readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		features, ok := hello["features"].(map[string]any)
		if !ok || features["mcp"] != true {
			t.Fatalf("hello features = %#v, want mcp true", hello["features"])
		}
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_mcp_display_sim"))

		initialize := sendServerMCPRequest(t, conn, 1, mcp.MethodInitialize, mcp.DefaultInitializeParams())
		readClientMCPResponse(t, conn, initialize)
		toolsList := sendServerMCPRequest(t, conn, 2, mcp.MethodToolsList, mcp.ToolsListParams{WithUserTools: false})
		readClientMCPResponse(t, conn, toolsList)

		readClientListenState(t, conn, "start")
		call := sendServerMCPRequest(t, conn, 3, mcp.MethodToolsCall, mcp.ToolCallParams{
			Name: mcp.ToolSetScreenScene,
			Arguments: map[string]any{
				"type":       "stackchan.scene",
				"scene":      "listening",
				"emotion":    "curious",
				"caption":    "我在听。",
				"accent":     "cyan",
				"ttl_ms":     1800,
				"generation": 1,
				"session_id": "sess_mcp_display_sim",
			},
		})
		if !readClientMCPResponseDuringUplink(t, conn, call) {
			readClientListenStopAfterAudio(t, conn, nil)
		}

		writeServerJSON(t, conn, xiaozhi.NewSTT("sess_mcp_display_sim", "你好，我在听。"))
		writeServerJSON(t, conn, xiaozhi.NewTTSStart("sess_mcp_display_sim"))
		writeServerJSON(t, conn, xiaozhi.NewTTSSentenceStart("sess_mcp_display_sim", "我在听。"))
		writeServerBinary(t, conn)
		writeServerJSON(t, conn, xiaozhi.NewTTSStop("sess_mcp_display_sim"))
	}))
}

func newUnsupportedDisplaySceneXiaozhiServer(t *testing.T) (*httptest.Server, <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		if _, err := readClientJSONTypeErr(conn, xiaozhi.MessageTypeHello); err != nil {
			done <- err
			return
		}
		if err := conn.WriteJSON(xiaozhi.NewServerHello("sess_mcp_display_sim")); err != nil {
			done <- err
			return
		}
		initialize, err := mcp.NewRequest(1, mcp.MethodInitialize, mcp.DefaultInitializeParams())
		if err != nil {
			done <- err
			return
		}
		if err := writeServerMCP(conn, initialize); err != nil {
			done <- err
			return
		}
		if _, err := readClientMCPResponseErr(conn, initialize); err != nil {
			done <- err
			return
		}
		toolsList, err := mcp.NewRequest(2, mcp.MethodToolsList, mcp.ToolsListParams{WithUserTools: false})
		if err != nil {
			done <- err
			return
		}
		if err := writeServerMCP(conn, toolsList); err != nil {
			done <- err
			return
		}
		if _, err := readClientMCPResponseErr(conn, toolsList); err != nil {
			done <- err
			return
		}
		if _, err := readClientJSONTypeErr(conn, xiaozhi.MessageTypeListen); err != nil {
			done <- err
			return
		}
		call, err := mcp.NewRequest(3, mcp.MethodToolsCall, mcp.ToolCallParams{
			Name: mcp.ToolSetScreenScene,
			Arguments: map[string]any{
				"type":       "stackchan.scene",
				"scene":      "listening",
				"emotion":    "curious",
				"caption":    "我在听。",
				"accent":     "cyan",
				"ttl_ms":     1800,
				"generation": 1,
				"session_id": "sess_mcp_display_sim",
			},
		})
		if err != nil {
			done <- err
			return
		}
		if err := writeServerMCP(conn, call); err != nil {
			done <- err
			return
		}
		for {
			if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				done <- err
				return
			}
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				done <- nil
				return
			}
			if messageType != websocket.TextMessage {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				done <- err
				return
			}
			if payload["type"] != xiaozhi.MessageTypeMCP {
				continue
			}
			done <- errors.New("unsupported self.screen.set_scene received an MCP response")
			return
		}
	}))
	return server, done
}

func newMCPLEDFeedbackXiaozhiServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		hello := readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		features, ok := hello["features"].(map[string]any)
		if !ok || features["mcp"] != true {
			t.Fatalf("hello features = %#v, want mcp true", hello["features"])
		}
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_mcp_led_sim"))

		initialize := sendServerMCPRequest(t, conn, 1, mcp.MethodInitialize, mcp.DefaultInitializeParams())
		readClientMCPResponse(t, conn, initialize)
		toolsList := sendServerMCPRequest(t, conn, 2, mcp.MethodToolsList, mcp.ToolsListParams{WithUserTools: false})
		readClientMCPResponse(t, conn, toolsList)

		readClientListenState(t, conn, "start")
		call := sendServerMCPRequest(t, conn, 3, mcp.MethodToolsCall, mcp.ToolCallParams{
			Name: mcp.ToolSetLEDColor,
			Arguments: map[string]any{
				"red":   0,
				"green": 168,
				"blue":  0,
			},
		})
		if !readClientMCPResponseDuringUplink(t, conn, call) {
			readClientListenStopAfterAudio(t, conn, nil)
		}

		writeServerJSON(t, conn, xiaozhi.NewSTT("sess_mcp_led_sim", "你好，我在听。"))
		writeServerJSON(t, conn, xiaozhi.NewTTSStart("sess_mcp_led_sim"))
		writeServerJSON(t, conn, xiaozhi.NewTTSSentenceStart("sess_mcp_led_sim", "我在听。"))
		writeServerBinary(t, conn)
		writeServerJSON(t, conn, xiaozhi.NewTTSStop("sess_mcp_led_sim"))
	}))
}

func TestClassifyLEDArgumentsAcceptsLifecycleColors(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "listening", args: map[string]any{"red": 0, "green": 168, "blue": 0}, want: "listening"},
		{name: "thinking", args: map[string]any{"red": 168, "green": 112, "blue": 0}, want: "thinking"},
		{name: "speaking", args: map[string]any{"red": 0, "green": 0, "blue": 168}, want: "speaking"},
		{name: "idle", args: map[string]any{"red": 0, "green": 0, "blue": 0}, want: "idle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyLEDArguments(tc.args)
			if err != nil {
				t.Fatalf("classifyLEDArguments() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("classifyLEDArguments() = %q, want %q", got, tc.want)
			}
		})
	}
}

func newOfficialStackChanV141ToolsListXiaozhiServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		hello := readClientJSONType(t, conn, xiaozhi.MessageTypeHello)
		features, ok := hello["features"].(map[string]any)
		if !ok || features["mcp"] != true {
			t.Fatalf("hello features = %#v, want mcp true", hello["features"])
		}
		writeServerJSON(t, conn, xiaozhi.NewServerHello("sess_official_v141_sim"))

		initialize := sendServerMCPRequest(t, conn, 1, mcp.MethodInitialize, mcp.DefaultInitializeParams())
		readClientMCPResponse(t, conn, initialize)
		toolsList := sendServerMCPRequest(t, conn, 2, mcp.MethodToolsList, mcp.ToolsListParams{WithUserTools: false})
		response := readClientMCPResponse(t, conn, toolsList)
		result := decodeToolsListResult(t, response)
		names := map[string]bool{}
		for _, tool := range result.Tools {
			names[tool.Name] = true
		}
		for _, want := range []string{mcp.ToolSetHeadAngles, mcp.ToolSetLEDColor, mcp.ToolScreenBrightness, mcp.ToolScreenTheme} {
			if !names[want] {
				t.Fatalf("official tools/list missing %s; tools=%v", want, result.Tools)
			}
		}
		if names[mcp.ToolSetScreenScene] {
			t.Fatalf("official tools/list exposed custom %s; tools=%v", mcp.ToolSetScreenScene, result.Tools)
		}
	}))
}

func readClientJSONType(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read client json: %v", err)
	}
	if messageType != websocket.TextMessage {
		t.Fatalf("client message type = %d, want text", messageType)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode client json: %v", err)
	}
	if got, _ := payload["type"].(string); got != want {
		t.Fatalf("client json type = %q, want %q; payload=%s", got, want, string(data))
	}
	return payload
}

func readClientJSONTypeErr(conn *websocket.Conn, want string) (map[string]any, error) {
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read client json: %w", err)
	}
	if messageType != websocket.TextMessage {
		return nil, fmt.Errorf("client message type = %d, want text", messageType)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode client json: %w", err)
	}
	if got, _ := payload["type"].(string); got != want {
		return nil, fmt.Errorf("client json type = %q, want %q; payload=%s", got, want, string(data))
	}
	return payload, nil
}

func sendServerMCPRequest(t *testing.T, conn *websocket.Conn, id uint64, method string, params any) mcp.Message {
	t.Helper()
	message, err := mcp.NewRequest(id, method, params)
	if err != nil {
		t.Fatalf("NewRequest(%s) error = %v", method, err)
	}
	raw, err := message.Raw()
	if err != nil {
		t.Fatalf("MCP request Raw() error = %v", err)
	}
	writeServerJSON(t, conn, xiaozhi.NewServerMCP("sess_mcp_sim", raw))
	return message
}

func writeServerMCP(conn *websocket.Conn, message mcp.Message) error {
	raw, err := message.Raw()
	if err != nil {
		return err
	}
	return conn.WriteJSON(xiaozhi.NewServerMCP("sess_mcp_sim", raw))
}

func readClientMCPResponse(t *testing.T, conn *websocket.Conn, request mcp.Message) mcp.Message {
	t.Helper()
	payload := readClientJSONType(t, conn, xiaozhi.MessageTypeMCP)
	raw, err := json.Marshal(payload["payload"])
	if err != nil {
		t.Fatalf("marshal client mcp payload: %v", err)
	}
	message, err := mcp.ParseMessage(raw)
	if err != nil {
		t.Fatalf("parse client mcp response: %v", err)
	}
	if !message.IsResponse() {
		t.Fatalf("client mcp message is not response: %s", string(raw))
	}
	if request.ID == nil || message.IDKey() != string(*request.ID) {
		t.Fatalf("client mcp response id = %q, want %q", message.IDKey(), request.IDKey())
	}
	return message
}

func readClientMCPResponseErr(conn *websocket.Conn, request mcp.Message) (mcp.Message, error) {
	payload, err := readClientJSONTypeErr(conn, xiaozhi.MessageTypeMCP)
	if err != nil {
		return mcp.Message{}, err
	}
	raw, err := json.Marshal(payload["payload"])
	if err != nil {
		return mcp.Message{}, fmt.Errorf("marshal client mcp payload: %w", err)
	}
	message, err := mcp.ParseMessage(raw)
	if err != nil {
		return mcp.Message{}, fmt.Errorf("parse client mcp response: %w", err)
	}
	if !message.IsResponse() {
		return mcp.Message{}, fmt.Errorf("client mcp message is not response: %s", string(raw))
	}
	if request.ID == nil || message.IDKey() != string(*request.ID) {
		return mcp.Message{}, fmt.Errorf("client mcp response id = %q, want %q", message.IDKey(), request.IDKey())
	}
	return message, nil
}

func decodeToolsListResult(t *testing.T, response mcp.Message) mcp.ToolsListResult {
	t.Helper()
	if len(response.Result) == 0 {
		t.Fatalf("tools/list response missing result")
	}
	var result mcp.ToolsListResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode tools/list result: %v", err)
	}
	return result
}

func readClientMCPResponseDuringUplink(t *testing.T, conn *websocket.Conn, request mcp.Message) bool {
	t.Helper()

	seenBinary := false
	seenListenStop := false
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read client message while waiting for mcp response: %v", err)
		}
		if messageType == websocket.BinaryMessage {
			if len(data) == 0 {
				t.Fatal("client sent empty binary frame")
			}
			seenBinary = true
			continue
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("client message type = %d, want text or binary", messageType)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode client json: %v", err)
		}
		switch payload["type"] {
		case xiaozhi.MessageTypeListen:
			if payload["state"] == "stop" {
				if !seenBinary {
					t.Fatal("listen stop arrived before any binary audio")
				}
				seenListenStop = true
			}
		case xiaozhi.MessageTypeMCP:
			raw, err := json.Marshal(payload["payload"])
			if err != nil {
				t.Fatalf("marshal client mcp payload: %v", err)
			}
			message, err := mcp.ParseMessage(raw)
			if err != nil {
				t.Fatalf("parse client mcp response: %v", err)
			}
			if !message.IsResponse() {
				t.Fatalf("client mcp message is not response: %s", string(raw))
			}
			if request.ID == nil || message.IDKey() != string(*request.ID) {
				t.Fatalf("client mcp response id = %q, want %q", message.IDKey(), request.IDKey())
			}
			return seenListenStop
		default:
			t.Fatalf("client json type = %q, want listen or mcp; payload=%s", payload["type"], string(data))
		}
	}
}

func readClientListenState(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	payload := readClientJSONType(t, conn, xiaozhi.MessageTypeListen)
	if got, _ := payload["state"].(string); got != want {
		t.Fatalf("listen state = %q, want %q", got, want)
	}
}

func readClientBinary(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read client binary: %v", err)
	}
	if messageType != websocket.BinaryMessage || len(data) == 0 {
		t.Fatalf("client binary type/len = %d/%d, want non-empty binary", messageType, len(data))
	}
}

func readClientProtocolV3AudioBeforeListenStop(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	seenBinary := false
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read protocol-v3 client message before listen stop: %v", err)
		}
		if messageType == websocket.BinaryMessage {
			frame, err := xiaozhi.ParseBinaryAudioFrame(data, xiaozhi.BinaryFrameOptions{
				ProtocolVersion: xiaozhi.BinaryProtocolV3,
			})
			if err != nil {
				t.Fatalf("decode protocol-v3 client audio: %v", err)
			}
			if len(frame.Payload) == 0 {
				t.Fatal("client sent empty protocol-v3 audio payload")
			}
			seenBinary = true
			continue
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("client message type = %d, want text or binary", messageType)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode client json: %v", err)
		}
		if payload["type"] == xiaozhi.MessageTypeListen && payload["state"] == "stop" {
			if !seenBinary {
				t.Fatal("listen stop arrived before protocol-v3 binary audio")
			}
			return
		}
		t.Fatalf("client json type = %q, want listen stop after audio; payload=%s", payload["type"], string(data))
	}
}

func readClientListenStopAfterAudio(t *testing.T, conn *websocket.Conn, recorder *scriptedFrameRecorder) {
	t.Helper()
	seenBinary := false
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read client message before listen stop: %v", err)
		}
		if messageType == websocket.BinaryMessage {
			if len(data) == 0 {
				t.Fatal("client sent empty binary frame")
			}
			if recorder != nil {
				recorder.Add(data)
			}
			seenBinary = true
			continue
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("client message type = %d, want text or binary", messageType)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode client json: %v", err)
		}
		if got, _ := payload["type"].(string); got != xiaozhi.MessageTypeListen {
			t.Fatalf("client json type = %q, want listen; payload=%s", got, string(data))
		}
		if got, _ := payload["state"].(string); got != "stop" {
			t.Fatalf("listen state = %q, want stop", got)
		}
		if !seenBinary {
			t.Fatal("listen stop arrived before any binary audio")
		}
		return
	}
}

func readClientAudioWithoutListenStop(t *testing.T, conn *websocket.Conn, recorder *scriptedFrameRecorder) {
	t.Helper()
	seenBinary := false
	for {
		if seenBinary {
			if err := conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
				t.Fatalf("set no-stop read deadline: %v", err)
			}
		}
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if seenBinary {
				var netError net.Error
				if errors.As(err, &netError) && netError.Timeout() {
					_ = conn.SetReadDeadline(time.Time{})
					return
				}
			}
			t.Fatalf("read client audio without listen stop: %v", err)
		}
		if messageType == websocket.BinaryMessage {
			if len(data) == 0 {
				t.Fatal("client sent empty binary frame")
			}
			if recorder != nil {
				recorder.Add(data)
			}
			seenBinary = true
			continue
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("client message type = %d, want text or binary", messageType)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode client json: %v", err)
		}
		if got, _ := payload["type"].(string); got == xiaozhi.MessageTypeListen {
			t.Fatalf("unexpected listen message while waiting for provider ASR final: payload=%s", string(data))
		}
		t.Fatalf("client json type = %q, want only audio before provider ASR final; payload=%s", payload["type"], string(data))
	}
}

type scriptedFrameRecorder struct {
	mu     sync.Mutex
	frames [][]byte
}

func (r *scriptedFrameRecorder) Add(frame []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, append([]byte(nil), frame...))
}

func (r *scriptedFrameRecorder) Frames() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.frames...)
}

func variedFixtureFrames(count int, size int) [][]byte {
	frames := make([][]byte, count)
	for index := range frames {
		frame := make([]byte, size)
		for offset := range frame {
			frame[offset] = byte(0x30 + index + offset)
		}
		frames[index] = frame
	}
	return frames
}

func writeServerJSON(t *testing.T, conn *websocket.Conn, payload any) {
	t.Helper()
	if err := conn.WriteJSON(payload); err != nil {
		t.Fatalf("write server json: %v", err)
	}
}

func writeServerBinary(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0xf8, 0xff, 0xfe}); err != nil {
		t.Fatalf("write server binary: %v", err)
	}
}

func writeProtocolV3ServerBinary(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	frame, err := xiaozhi.EncodeBinaryAudioFrame([]byte{0xf8, 0xff, 0xfe}, xiaozhi.BinaryFrameOptions{
		ProtocolVersion: xiaozhi.BinaryProtocolV3,
	})
	if err != nil {
		t.Fatalf("encode protocol-v3 server binary: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("write protocol-v3 server binary: %v", err)
	}
}
