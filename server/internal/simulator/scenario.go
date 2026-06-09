package simulator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providerprobe"
	"stackchan-gateway/internal/stackchan"
)

const (
	ScenarioHelloOnly                      = "hello_only"
	ScenarioOfficialStackChanV141ToolsList = "official_stackchan_v1_4_1_tools_list"
	ScenarioHappyPath20Turns               = "happy_path_20_turns"
	ScenarioASRFinalWithoutListenStop      = "asr_final_without_listen_stop"
	ScenarioAbortDuringTTS                 = "abort_during_tts"
	ScenarioProviderSlowFirstAudio         = "provider_slow_first_audio"
	ScenarioWSReconnect                    = "ws_reconnect"
	ScenarioMCPHeadMotion                  = "mcp_head_motion"
	ScenarioMCPDisplayScene                = "mcp_display_scene"
	ScenarioMCPLEDFeedback                 = "mcp_led_feedback"
	ScenarioMCPAgentBridgeSkipFeedback     = "mcp_agent_bridge_skip_feedback"
	ScenarioProviderProfileSwitch          = "provider_profile_switch"
	ScenarioMockGatewaySuite               = "mock_gateway_suite"
)

const (
	FirmwareProfileMockGateway           = "mock-gateway"
	FirmwareProfileOfficialStackChanV141 = "official-v1.4.1"
)

type ScenarioOptions struct {
	Scenario            string
	FirmwareProfile     string
	GatewayURL          string
	DeviceID            string
	ClientID            string
	AuthToken           string
	ProtocolVersion     int
	Turns               int
	FramesPerTurn       int
	ASRFixturePath      string
	Timeout             time.Duration
	MaxFirstAudioMS     int64
	TraceFile           string
	RequireTraceEvents  []string
	MaxBinaryAfterAbort int
}

type ScenarioSummary struct {
	Scenario                   string            `json:"scenario"`
	FirmwareProfile            string            `json:"firmware_profile,omitempty"`
	Turns                      int               `json:"turns"`
	Success                    int               `json:"success"`
	Failures                   int               `json:"failures"`
	Passed                     bool              `json:"passed"`
	P50FirstAudioMS            int64             `json:"p50_first_audio_ms"`
	P95FirstAudioMS            int64             `json:"p95_first_audio_ms"`
	MaxFirstAudioMS            int64             `json:"max_first_audio_ms,omitempty"`
	ASRFinalWithoutListenStop  bool              `json:"asr_final_without_listen_stop,omitempty"`
	AbortOldAudioDropped       bool              `json:"abort_old_audio_dropped,omitempty"`
	ReconnectOldClosed         bool              `json:"reconnect_old_closed,omitempty"`
	MCPHeadMotion              bool              `json:"mcp_head_motion,omitempty"`
	MCPDisplayScene            bool              `json:"mcp_display_scene,omitempty"`
	MCPLEDFeedback             bool              `json:"mcp_led_feedback,omitempty"`
	MCPAgentBridgeSkipFeedback bool              `json:"mcp_agent_bridge_skip_feedback,omitempty"`
	ProviderProfileSwitch      bool              `json:"provider_profile_switch,omitempty"`
	OfficialStackChanV141      bool              `json:"official_stackchan_v1_4_1,omitempty"`
	TraceEvents                []string          `json:"trace_events,omitempty"`
	Scenarios                  []ScenarioSummary `json:"scenarios,omitempty"`
}

type downlinkEvent struct {
	Kind       string
	Type       string
	State      string
	MCPMessage mcp.Message
}

func RunScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	options = normalizeScenarioOptions(options)
	if err := validateScenarioOptions(options); err != nil {
		return ScenarioSummary{}, err
	}
	if options.Scenario == ScenarioHelloOnly {
		return runHelloOnlyScenario(ctx, options)
	}
	if options.Scenario == ScenarioOfficialStackChanV141ToolsList {
		return runOfficialStackChanV141ToolsListScenario(ctx, options)
	}
	if options.Scenario == ScenarioMockGatewaySuite {
		return runMockGatewaySuite(ctx, options)
	}
	if options.Scenario == ScenarioWSReconnect {
		return runReconnectScenario(ctx, options)
	}
	if options.Scenario == ScenarioMCPHeadMotion {
		return runMCPHeadMotionScenario(ctx, options)
	}
	if options.Scenario == ScenarioMCPDisplayScene {
		return runMCPDisplaySceneScenario(ctx, options)
	}
	if options.Scenario == ScenarioMCPLEDFeedback {
		return runMCPLEDFeedbackScenario(ctx, options)
	}
	if options.Scenario == ScenarioMCPAgentBridgeSkipFeedback {
		return runMCPAgentBridgeSkipFeedbackScenario(ctx, options)
	}
	opusFrames, err := scenarioOpusFrames(options)
	if err != nil {
		return ScenarioSummary{}, err
	}

	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect gateway: %w", err)
	}
	defer conn.Close()

	if err := writeDeviceHello(conn, false); err != nil {
		return ScenarioSummary{}, err
	}
	if err := expectServerHello(conn, options.Timeout); err != nil {
		return ScenarioSummary{}, err
	}

	summary := ScenarioSummary{
		Scenario:        options.Scenario,
		Turns:           options.Turns,
		MaxFirstAudioMS: options.MaxFirstAudioMS,
	}
	firstAudioLatencies := make([]int64, 0, options.Turns)
	for index := 0; index < options.Turns; index++ {
		result, err := runScenarioTurn(conn, options, opusFrames, options.Scenario == ScenarioAbortDuringTTS)
		if err != nil {
			summary.Failures++
			return summary, fmt.Errorf("turn %d failed: %w", index+1, err)
		}
		summary.Success++
		firstAudioLatencies = append(firstAudioLatencies, result.FirstAudioMS)
		if options.MaxFirstAudioMS > 0 && result.FirstAudioMS > options.MaxFirstAudioMS {
			summary.Failures++
			return summary, fmt.Errorf("first audio %dms exceeded budget %dms", result.FirstAudioMS, options.MaxFirstAudioMS)
		}
		if options.Scenario == ScenarioAbortDuringTTS {
			summary.AbortOldAudioDropped = result.BinaryAfterAbort <= options.MaxBinaryAfterAbort
			if !summary.AbortOldAudioDropped {
				summary.Failures++
				return summary, fmt.Errorf("abort left %d stale binary frames, max %d", result.BinaryAfterAbort, options.MaxBinaryAfterAbort)
			}
		}
		if options.Scenario == ScenarioASRFinalWithoutListenStop {
			summary.ASRFinalWithoutListenStop = true
		}
		if options.Scenario == ScenarioProviderProfileSwitch {
			summary.ProviderProfileSwitch = true
		}
	}
	summary.P50FirstAudioMS = percentile(firstAudioLatencies, 50)
	summary.P95FirstAudioMS = percentile(firstAudioLatencies, 95)

	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Passed = summary.Failures == 0 && summary.Success == options.Turns
	return summary, nil
}

type turnResult struct {
	FirstAudioMS              int64
	BinaryAfterAbort          int
	MCPHeadMotion             bool
	MCPDisplayScene           bool
	MCPLEDFeedback            bool
	MCPAgentBridgeSkipDisplay bool
	MCPAgentBridgeSkipHead    bool
	MCPAgentBridgeSkipLED     bool
}

type mcpHandledResult struct {
	HeadMotion             bool
	DisplayScene           bool
	LEDFeedback            bool
	AgentBridgeSkipDisplay bool
	AgentBridgeSkipHead    bool
	AgentBridgeSkipLED     bool
}

type suiteScenario struct {
	scenario           string
	turns              int
	maxFirstAudioMS    int64
	requireTraceEvents []string
}

func runMockGatewaySuite(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	summary := ScenarioSummary{
		Scenario:        options.Scenario,
		MaxFirstAudioMS: options.MaxFirstAudioMS,
	}
	for _, child := range mockGatewaySuiteScenarios(options) {
		childOptions := options
		childOptions.Scenario = child.scenario
		childOptions.Turns = child.turns
		childOptions.MaxFirstAudioMS = child.maxFirstAudioMS
		childOptions.RequireTraceEvents = suiteTraceEvents(options, child.requireTraceEvents)

		childSummary, err := RunScenario(ctx, childOptions)
		summary.Scenarios = append(summary.Scenarios, childSummary)
		summary.Turns += childSummary.Turns
		summary.Success += childSummary.Success
		summary.Failures += childSummary.Failures
		summary.P50FirstAudioMS = maxInt64(summary.P50FirstAudioMS, childSummary.P50FirstAudioMS)
		summary.P95FirstAudioMS = maxInt64(summary.P95FirstAudioMS, childSummary.P95FirstAudioMS)
		if childSummary.AbortOldAudioDropped {
			summary.AbortOldAudioDropped = true
		}
		if childSummary.ASRFinalWithoutListenStop {
			summary.ASRFinalWithoutListenStop = true
		}
		if childSummary.ReconnectOldClosed {
			summary.ReconnectOldClosed = true
		}
		if childSummary.MCPHeadMotion {
			summary.MCPHeadMotion = true
		}
		if childSummary.MCPDisplayScene {
			summary.MCPDisplayScene = true
		}
		if childSummary.MCPLEDFeedback {
			summary.MCPLEDFeedback = true
		}
		if childSummary.MCPAgentBridgeSkipFeedback {
			summary.MCPAgentBridgeSkipFeedback = true
		}
		if childSummary.ProviderProfileSwitch {
			summary.ProviderProfileSwitch = true
		}
		if err != nil {
			summary.Failures++
			return summary, fmt.Errorf("suite scenario %s failed: %w", child.scenario, err)
		}
		if !childSummary.Passed {
			summary.Failures++
			return summary, fmt.Errorf("suite scenario %s did not pass", child.scenario)
		}
	}
	summary.Passed = summary.Failures == 0 && len(summary.Scenarios) == len(mockGatewaySuiteScenarios(options))
	return summary, nil
}

func mockGatewaySuiteScenarios(options ScenarioOptions) []suiteScenario {
	happyTurns := options.Turns
	if happyTurns < 0 {
		happyTurns = 0
	}
	providerBudget := options.MaxFirstAudioMS
	return []suiteScenario{
		{
			scenario:           ScenarioHappyPath20Turns,
			turns:              happyTurns,
			requireTraceEvents: []string{"hello_received", "listen_start", "first_uplink_audio", "speech_final", "first_downlink_audio_sent", "turn_complete"},
		},
		{
			scenario:           ScenarioASRFinalWithoutListenStop,
			turns:              1,
			requireTraceEvents: []string{"hello_received", "listen_start", "first_uplink_audio", "speech_final", "first_downlink_audio_sent", "turn_complete"},
		},
		{
			scenario:           ScenarioAbortDuringTTS,
			turns:              1,
			requireTraceEvents: []string{"abort_received", "turn_complete"},
		},
		{
			scenario:           ScenarioProviderSlowFirstAudio,
			turns:              1,
			maxFirstAudioMS:    providerBudget,
			requireTraceEvents: []string{"first_downlink_audio_sent", "turn_complete"},
		},
		{
			scenario:           ScenarioWSReconnect,
			turns:              1,
			requireTraceEvents: []string{"hello_received", "turn_complete"},
		},
		{
			scenario:           ScenarioMCPHeadMotion,
			turns:              1,
			requireTraceEvents: []string{"hello_received", "listen_start", "turn_complete"},
		},
		{
			scenario:           ScenarioMCPDisplayScene,
			turns:              1,
			requireTraceEvents: []string{"hello_received", "listen_start", "turn_complete"},
		},
		{
			scenario:           ScenarioMCPLEDFeedback,
			turns:              1,
			requireTraceEvents: []string{"hello_received", "listen_start", "turn_complete"},
		},
		{
			scenario:           ScenarioMCPAgentBridgeSkipFeedback,
			turns:              1,
			requireTraceEvents: []string{"hello_received", "listen_start", "agent_route_skipped", "turn_complete"},
		},
	}
}

func suiteTraceEvents(options ScenarioOptions, defaults []string) []string {
	if len(options.RequireTraceEvents) > 0 {
		return append([]string(nil), options.RequireTraceEvents...)
	}
	if strings.TrimSpace(options.TraceFile) == "" {
		return nil
	}
	return append([]string(nil), defaults...)
}

func runHelloOnlyScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect gateway: %w", err)
	}
	defer conn.Close()

	summary := ScenarioSummary{
		Scenario: options.Scenario,
		Turns:    1,
	}
	if err := writeDeviceHello(conn, false); err != nil {
		summary.Failures++
		return summary, err
	}
	if err := expectServerHello(conn, options.Timeout); err != nil {
		summary.Failures++
		return summary, err
	}
	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Success = 1
	summary.Passed = summary.Failures == 0
	return summary, nil
}

func runOfficialStackChanV141ToolsListScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect gateway: %w", err)
	}
	defer conn.Close()

	summary := ScenarioSummary{
		Scenario:              options.Scenario,
		FirmwareProfile:       options.FirmwareProfile,
		Turns:                 1,
		OfficialStackChanV141: options.FirmwareProfile == FirmwareProfileOfficialStackChanV141,
	}
	if err := writeDeviceHello(conn, true); err != nil {
		summary.Failures++
		return summary, err
	}
	if err := expectServerHello(conn, options.Timeout); err != nil {
		summary.Failures++
		return summary, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodInitialize); err != nil {
		summary.Failures++
		return summary, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodToolsList); err != nil {
		summary.Failures++
		return summary, err
	}
	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Success = 1
	summary.Passed = summary.Failures == 0 && summary.OfficialStackChanV141
	return summary, nil
}

func runReconnectScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	opusFrames, err := scenarioOpusFrames(options)
	if err != nil {
		return ScenarioSummary{}, err
	}
	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	firstConn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect first gateway: %w", err)
	}
	defer firstConn.Close()
	if err := writeDeviceHello(firstConn, false); err != nil {
		return ScenarioSummary{}, err
	}
	if err := expectServerHello(firstConn, options.Timeout); err != nil {
		return ScenarioSummary{}, fmt.Errorf("first connection: %w", err)
	}

	secondConn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect reconnect gateway: %w", err)
	}
	defer secondConn.Close()
	if err := writeDeviceHello(secondConn, false); err != nil {
		return ScenarioSummary{}, err
	}
	if err := expectServerHello(secondConn, options.Timeout); err != nil {
		return ScenarioSummary{}, fmt.Errorf("reconnect connection: %w", err)
	}

	summary := ScenarioSummary{Scenario: options.Scenario, Turns: 1}
	if err := waitForConnectionClosed(firstConn, options.Timeout); err != nil {
		summary.Failures++
		return summary, err
	}
	summary.ReconnectOldClosed = true

	result, err := runScenarioTurn(secondConn, options, opusFrames, false)
	if err != nil {
		summary.Failures++
		return summary, fmt.Errorf("reconnect turn failed: %w", err)
	}
	summary.Success = 1
	summary.P50FirstAudioMS = result.FirstAudioMS
	summary.P95FirstAudioMS = result.FirstAudioMS

	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Passed = summary.ReconnectOldClosed && summary.Success == 1 && summary.Failures == 0
	return summary, nil
}

func runMCPHeadMotionScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	opusFrames, err := scenarioOpusFrames(options)
	if err != nil {
		return ScenarioSummary{}, err
	}
	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect gateway: %w", err)
	}
	defer conn.Close()

	if err := writeDeviceHello(conn, true); err != nil {
		return ScenarioSummary{}, err
	}
	if err := expectServerHello(conn, options.Timeout); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodInitialize); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodToolsList); err != nil {
		return ScenarioSummary{}, err
	}

	summary := ScenarioSummary{
		Scenario: options.Scenario,
		Turns:    1,
	}
	result, err := runScenarioTurnWithMCP(conn, options, opusFrames, false, true)
	if err != nil {
		summary.Failures++
		return summary, fmt.Errorf("mcp head motion turn failed: %w", err)
	}
	if !result.MCPHeadMotion {
		summary.Failures++
		return summary, errors.New("gateway did not call self.robot.set_head_angles")
	}
	summary.Success = 1
	summary.MCPHeadMotion = true
	summary.P50FirstAudioMS = result.FirstAudioMS
	summary.P95FirstAudioMS = result.FirstAudioMS

	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Passed = summary.Success == 1 && summary.Failures == 0 && summary.MCPHeadMotion
	return summary, nil
}

func runMCPDisplaySceneScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	opusFrames, err := scenarioOpusFrames(options)
	if err != nil {
		return ScenarioSummary{}, err
	}
	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect gateway: %w", err)
	}
	defer conn.Close()

	if err := writeDeviceHello(conn, true); err != nil {
		return ScenarioSummary{}, err
	}
	if err := expectServerHello(conn, options.Timeout); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodInitialize); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodToolsList); err != nil {
		return ScenarioSummary{}, err
	}

	summary := ScenarioSummary{
		Scenario: options.Scenario,
		Turns:    1,
	}
	result, err := runScenarioTurnWithMCP(conn, options, opusFrames, false, true)
	if err != nil {
		summary.Failures++
		return summary, fmt.Errorf("mcp display scene turn failed: %w", err)
	}
	if !result.MCPDisplayScene {
		summary.Failures++
		return summary, errors.New("gateway did not call self.screen.set_scene")
	}
	summary.Success = 1
	summary.MCPDisplayScene = true
	summary.P50FirstAudioMS = result.FirstAudioMS
	summary.P95FirstAudioMS = result.FirstAudioMS

	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Passed = summary.Success == 1 && summary.Failures == 0 && summary.MCPDisplayScene
	return summary, nil
}

func runMCPLEDFeedbackScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	opusFrames, err := scenarioOpusFrames(options)
	if err != nil {
		return ScenarioSummary{}, err
	}
	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect gateway: %w", err)
	}
	defer conn.Close()

	if err := writeDeviceHello(conn, true); err != nil {
		return ScenarioSummary{}, err
	}
	if err := expectServerHello(conn, options.Timeout); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodInitialize); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodToolsList); err != nil {
		return ScenarioSummary{}, err
	}

	summary := ScenarioSummary{
		Scenario: options.Scenario,
		Turns:    1,
	}
	result, err := runScenarioTurnWithMCP(conn, options, opusFrames, false, true)
	if err != nil {
		summary.Failures++
		return summary, fmt.Errorf("mcp led feedback turn failed: %w", err)
	}
	if !result.MCPLEDFeedback {
		summary.Failures++
		return summary, errors.New("gateway did not call self.robot.set_led_color")
	}
	summary.Success = 1
	summary.MCPLEDFeedback = true
	summary.P50FirstAudioMS = result.FirstAudioMS
	summary.P95FirstAudioMS = result.FirstAudioMS

	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Passed = summary.Success == 1 && summary.Failures == 0 && summary.MCPLEDFeedback
	return summary, nil
}

func runMCPAgentBridgeSkipFeedbackScenario(ctx context.Context, options ScenarioOptions) (ScenarioSummary, error) {
	opusFrames, err := scenarioOpusFrames(options)
	if err != nil {
		return ScenarioSummary{}, err
	}
	traceOffset, err := traceStartOffset(options.TraceFile)
	if err != nil {
		return ScenarioSummary{}, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.GatewayURL, simulatorHeaders(options))
	if err != nil {
		return ScenarioSummary{}, fmt.Errorf("connect gateway: %w", err)
	}
	defer conn.Close()

	if err := writeDeviceHello(conn, true); err != nil {
		return ScenarioSummary{}, err
	}
	if err := expectServerHello(conn, options.Timeout); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodInitialize); err != nil {
		return ScenarioSummary{}, err
	}
	if _, err := expectAndHandleMCPRequest(conn, options, mcp.MethodToolsList); err != nil {
		return ScenarioSummary{}, err
	}

	summary := ScenarioSummary{
		Scenario: options.Scenario,
		Turns:    1,
	}
	result, err := runScenarioTurnWithMCP(conn, options, opusFrames, false, true)
	if err != nil {
		summary.Failures++
		return summary, fmt.Errorf("mcp agent bridge skip feedback turn failed: %w", err)
	}
	result, err = waitForAgentBridgeSkipFeedback(conn, options, result)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	missing := missingAgentBridgeSkipFeedback(result)
	if len(missing) > 0 {
		summary.Failures++
		return summary, fmt.Errorf("gateway did not send agent bridge skip MCP feedback: missing %s", strings.Join(missing, ", "))
	}
	summary.Success = 1
	summary.MCPAgentBridgeSkipFeedback = true
	summary.P50FirstAudioMS = result.FirstAudioMS
	summary.P95FirstAudioMS = result.FirstAudioMS

	traceEvents, err := requiredTraceEventsSince(options.TraceFile, traceOffset, options.RequireTraceEvents)
	if err != nil {
		summary.Failures++
		return summary, err
	}
	summary.TraceEvents = traceEvents
	summary.Passed = summary.Success == 1 && summary.Failures == 0 && summary.MCPAgentBridgeSkipFeedback
	return summary, nil
}

func runScenarioTurn(conn *websocket.Conn, options ScenarioOptions, opusFrames [][]byte, abortDuringTTS bool) (turnResult, error) {
	return runScenarioTurnWithMCP(conn, options, opusFrames, abortDuringTTS, false)
}

func runScenarioTurnWithMCP(conn *websocket.Conn, options ScenarioOptions, opusFrames [][]byte, abortDuringTTS bool, handleMCP bool) (turnResult, error) {
	if err := conn.WriteJSON(xiaozhi.ListenMessage{Type: xiaozhi.MessageTypeListen, State: "start", Mode: "auto"}); err != nil {
		return turnResult{}, fmt.Errorf("send listen start: %w", err)
	}
	for _, payload := range opusFrames {
		if err := writeUplinkAudioFrame(conn, options, payload); err != nil {
			return turnResult{}, fmt.Errorf("send opus frame: %w", err)
		}
	}
	speechBoundaryAt := time.Now()
	if options.Scenario != ScenarioASRFinalWithoutListenStop {
		if err := conn.WriteJSON(xiaozhi.ListenMessage{Type: xiaozhi.MessageTypeListen, State: "stop"}); err != nil {
			return turnResult{}, fmt.Errorf("send listen stop: %w", err)
		}
	}

	var result turnResult
	var seenSTT bool
	var seenTTSStart bool
	var seenTTSSentence bool
	var seenBinary bool
	var abortSent bool
	for {
		event, err := readDownlinkEvent(conn, options.Timeout, options.ProtocolVersion)
		if err != nil {
			return result, err
		}
		switch {
		case event.Kind == "binary":
			if result.FirstAudioMS == 0 {
				result.FirstAudioMS = maxInt64(1, time.Since(speechBoundaryAt).Milliseconds())
			}
			seenBinary = true
			if abortDuringTTS {
				if abortSent {
					result.BinaryAfterAbort++
				} else {
					if err := conn.WriteJSON(xiaozhi.AbortMessage{Type: xiaozhi.MessageTypeAbort}); err != nil {
						return result, fmt.Errorf("send abort: %w", err)
					}
					abortSent = true
				}
			}
		case event.Type == xiaozhi.MessageTypeSTT:
			seenSTT = true
		case event.Type == xiaozhi.MessageTypeMCP && handleMCP:
			handled, err := handleMCPDownlink(conn, options, event)
			if err != nil {
				return result, err
			}
			result.MCPHeadMotion = result.MCPHeadMotion || handled.HeadMotion
			result.MCPDisplayScene = result.MCPDisplayScene || handled.DisplayScene
			result.MCPLEDFeedback = result.MCPLEDFeedback || handled.LEDFeedback
			result.MCPAgentBridgeSkipDisplay = result.MCPAgentBridgeSkipDisplay || handled.AgentBridgeSkipDisplay
			result.MCPAgentBridgeSkipHead = result.MCPAgentBridgeSkipHead || handled.AgentBridgeSkipHead
			result.MCPAgentBridgeSkipLED = result.MCPAgentBridgeSkipLED || handled.AgentBridgeSkipLED
		case event.Type == xiaozhi.MessageTypeTTS && event.State == "start":
			seenTTSStart = true
		case event.Type == xiaozhi.MessageTypeTTS && event.State == "sentence_start":
			seenTTSSentence = true
		case event.Type == xiaozhi.MessageTypeTTS && event.State == "stop":
			if !seenSTT || !seenTTSStart || !seenTTSSentence || !seenBinary {
				return result, fmt.Errorf("turn ended with missing downlink events: stt=%t tts_start=%t tts_sentence=%t binary=%t", seenSTT, seenTTSStart, seenTTSSentence, seenBinary)
			}
			if abortDuringTTS && !abortSent {
				return result, errors.New("abort scenario reached tts stop before first audio")
			}
			return result, nil
		}
	}
}

func expectServerHello(conn *websocket.Conn, timeout time.Duration) error {
	event, err := readDownlinkEvent(conn, timeout, xiaozhi.BinaryProtocolV1)
	if err != nil {
		return fmt.Errorf("read server hello: %w", err)
	}
	if event.Kind != "json" || event.Type != xiaozhi.MessageTypeHello {
		return fmt.Errorf("first downlink event is %s/%s, want server hello", event.Kind, event.Type)
	}
	return nil
}

func writeUplinkAudioFrame(conn *websocket.Conn, options ScenarioOptions, payload []byte) error {
	encoded, err := xiaozhi.EncodeBinaryAudioFrame(payload, xiaozhi.BinaryFrameOptions{
		ProtocolVersion: options.ProtocolVersion,
		SampleRateHz:    xiaozhi.XiaozhiUplinkSampleRateHz,
		FrameDurationMS: xiaozhi.XiaozhiFrameDurationMS,
	})
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, encoded)
}

func scenarioOpusFrames(options ScenarioOptions) ([][]byte, error) {
	fixturePath := strings.TrimSpace(options.ASRFixturePath)
	if fixturePath != "" {
		frames, err := providerprobe.LoadASROpusFixture(fixturePath)
		if err != nil {
			return nil, err
		}
		if _, err := providerprobe.ValidateASROpusFramesForSemanticProbe(frames); err != nil {
			return nil, err
		}
		return frames, nil
	}

	frames := make([][]byte, 0, options.FramesPerTurn)
	for frame := 0; frame < options.FramesPerTurn; frame++ {
		frames = append(frames, simulatedOpusFrame(frame))
	}
	return frames, nil
}

func normalizeScenarioOptions(options ScenarioOptions) ScenarioOptions {
	if strings.TrimSpace(options.DeviceID) == "" {
		options.DeviceID = "stackchan-s3-main"
	}
	if strings.TrimSpace(options.ClientID) == "" {
		options.ClientID = "stackchan-s3-main-client"
	}
	if options.ProtocolVersion <= 0 {
		options.ProtocolVersion = xiaozhi.BinaryProtocolV1
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.FramesPerTurn <= 0 {
		options.FramesPerTurn = 3
	}
	if options.MaxBinaryAfterAbort < 0 {
		options.MaxBinaryAfterAbort = 0
	}
	if options.MaxBinaryAfterAbort == 0 {
		options.MaxBinaryAfterAbort = 2
	}
	if strings.TrimSpace(options.FirmwareProfile) == "" {
		options.FirmwareProfile = FirmwareProfileMockGateway
	}
	switch options.Scenario {
	case ScenarioHelloOnly:
		options.Turns = 1
	case ScenarioOfficialStackChanV141ToolsList:
		options.Turns = 1
		options.FirmwareProfile = FirmwareProfileOfficialStackChanV141
	case ScenarioHappyPath20Turns:
		if options.Turns <= 0 {
			options.Turns = 20
		}
	case ScenarioASRFinalWithoutListenStop:
		options.Turns = 1
	case ScenarioAbortDuringTTS:
		options.Turns = 1
	case ScenarioProviderSlowFirstAudio:
		if options.Turns <= 0 {
			options.Turns = 3
		}
		if options.MaxFirstAudioMS <= 0 {
			options.MaxFirstAudioMS = 1500
		}
	case ScenarioWSReconnect:
		options.Turns = 1
	case ScenarioMCPHeadMotion:
		options.Turns = 1
	case ScenarioMCPDisplayScene:
		options.Turns = 1
	case ScenarioMCPLEDFeedback:
		options.Turns = 1
	case ScenarioMCPAgentBridgeSkipFeedback:
		options.Turns = 1
	case ScenarioProviderProfileSwitch:
		options.Turns = 1
		if strings.TrimSpace(options.TraceFile) != "" && len(options.RequireTraceEvents) == 0 {
			options.RequireTraceEvents = []string{"speech_final", "provider_profile_command", "turn_complete"}
		}
	case ScenarioMockGatewaySuite:
		if options.Turns < 0 {
			options.Turns = 0
		}
	}
	return options
}

func validateScenarioOptions(options ScenarioOptions) error {
	switch options.Scenario {
	case ScenarioHelloOnly, ScenarioOfficialStackChanV141ToolsList, ScenarioHappyPath20Turns, ScenarioASRFinalWithoutListenStop, ScenarioAbortDuringTTS, ScenarioProviderSlowFirstAudio, ScenarioWSReconnect, ScenarioMCPHeadMotion, ScenarioMCPDisplayScene, ScenarioMCPLEDFeedback, ScenarioMCPAgentBridgeSkipFeedback, ScenarioProviderProfileSwitch, ScenarioMockGatewaySuite:
	default:
		return fmt.Errorf("unsupported scenario %q", options.Scenario)
	}
	switch options.FirmwareProfile {
	case FirmwareProfileMockGateway, FirmwareProfileOfficialStackChanV141:
	default:
		return fmt.Errorf("unsupported firmware profile %q", options.FirmwareProfile)
	}
	if strings.TrimSpace(options.GatewayURL) == "" {
		return errors.New("gateway URL is required")
	}
	if strings.TrimSpace(options.AuthToken) == "" {
		return errors.New("auth token is required")
	}
	if options.Scenario == ScenarioProviderProfileSwitch && strings.TrimSpace(options.TraceFile) == "" {
		return errors.New("trace file is required for provider_profile_switch")
	}
	if options.ProtocolVersion < xiaozhi.BinaryProtocolV1 || options.ProtocolVersion > xiaozhi.BinaryProtocolV3 {
		return fmt.Errorf("unsupported xiaozhi binary protocol version %d", options.ProtocolVersion)
	}
	return nil
}

func simulatorHeaders(options ScenarioOptions) http.Header {
	headers := http.Header{}
	headers.Set(xiaozhi.HeaderAuthorization, "Bearer "+strings.TrimSpace(options.AuthToken))
	headers.Set(xiaozhi.HeaderProtocolVersion, strconv.Itoa(options.ProtocolVersion))
	headers.Set(xiaozhi.HeaderDeviceID, options.DeviceID)
	headers.Set(xiaozhi.HeaderClientID, options.ClientID)
	return headers
}

func writeDeviceHello(conn *websocket.Conn, mcpEnabled bool) error {
	message := xiaozhi.DeviceHelloMessage{
		Type:      xiaozhi.MessageTypeHello,
		Version:   1,
		Features:  xiaozhi.DeviceFeatures{MCP: mcpEnabled},
		Transport: xiaozhi.TransportWebSocket,
		AudioParams: &xiaozhi.AudioParams{
			Format:        xiaozhi.AudioFormatOpus,
			SampleRate:    xiaozhi.XiaozhiUplinkSampleRateHz,
			Channels:      xiaozhi.XiaozhiChannels,
			FrameDuration: xiaozhi.XiaozhiFrameDurationMS,
		},
	}
	if err := conn.WriteJSON(message); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}
	return nil
}

func readDownlinkEvent(conn *websocket.Conn, timeout time.Duration, protocolVersion int) (downlinkEvent, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return downlinkEvent{}, fmt.Errorf("set read deadline: %w", err)
	}
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		return downlinkEvent{}, fmt.Errorf("read downlink: %w", err)
	}
	if messageType == websocket.BinaryMessage {
		frame, err := xiaozhi.ParseBinaryAudioFrame(data, xiaozhi.BinaryFrameOptions{
			ProtocolVersion: protocolVersion,
			SampleRateHz:    xiaozhi.XiaozhiDownlinkRateHz,
			FrameDurationMS: xiaozhi.XiaozhiFrameDurationMS,
		})
		if err != nil {
			return downlinkEvent{}, fmt.Errorf("decode downlink binary frame: %w", err)
		}
		if len(frame.Payload) == 0 {
			return downlinkEvent{}, errors.New("empty downlink binary payload")
		}
		return downlinkEvent{Kind: "binary"}, nil
	}
	if messageType != websocket.TextMessage {
		return downlinkEvent{}, fmt.Errorf("unsupported downlink websocket message type %d", messageType)
	}
	var envelope struct {
		Type    string          `json:"type"`
		State   string          `json:"state"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return downlinkEvent{}, fmt.Errorf("decode downlink json: %w", err)
	}
	event := downlinkEvent{Kind: "json", Type: envelope.Type, State: envelope.State}
	if envelope.Type == xiaozhi.MessageTypeMCP {
		message, err := mcp.ParseMessage(envelope.Payload)
		if err != nil {
			return downlinkEvent{}, fmt.Errorf("parse downlink mcp: %w", err)
		}
		event.MCPMessage = message
	}
	return event, nil
}

func expectAndHandleMCPRequest(conn *websocket.Conn, options ScenarioOptions, method string) (mcpHandledResult, error) {
	event, err := readDownlinkEvent(conn, options.Timeout, options.ProtocolVersion)
	if err != nil {
		return mcpHandledResult{}, err
	}
	if event.Type != xiaozhi.MessageTypeMCP {
		return mcpHandledResult{}, fmt.Errorf("downlink type = %q, want mcp %s", event.Type, method)
	}
	if event.MCPMessage.Method != method {
		return mcpHandledResult{}, fmt.Errorf("mcp method = %q, want %q", event.MCPMessage.Method, method)
	}
	return handleMCPDownlink(conn, options, event)
}

func handleMCPDownlink(conn *websocket.Conn, options ScenarioOptions, event downlinkEvent) (mcpHandledResult, error) {
	if event.Type != xiaozhi.MessageTypeMCP {
		return mcpHandledResult{}, nil
	}
	message := event.MCPMessage
	if !message.IsRequest() {
		return mcpHandledResult{}, nil
	}

	switch message.Method {
	case mcp.MethodInitialize:
		return mcpHandledResult{}, writeMCPResult(conn, message, json.RawMessage(`{}`))
	case mcp.MethodToolsList:
		result := mcp.ToolsListResult{
			Tools: toolsForFirmwareProfile(options.FirmwareProfile),
		}
		return mcpHandledResult{}, writeMCPResult(conn, message, result)
	case mcp.MethodToolsCall:
		var params mcp.ToolCallParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return mcpHandledResult{}, fmt.Errorf("decode mcp tools/call params: %w", err)
		}
		if !firmwareProfileSupportsTool(options.FirmwareProfile, params.Name) {
			return mcpHandledResult{}, fmt.Errorf("%s simulator does not support mcp tools/call %s", options.FirmwareProfile, params.Name)
		}
		switch params.Name {
		case mcp.ToolSetHeadAngles:
			kind, err := classifyHeadMotionArguments(params.Arguments)
			if err != nil {
				return mcpHandledResult{}, err
			}
			return mcpHandledResult{
				HeadMotion:          kind == "listening",
				AgentBridgeSkipHead: kind == "agent_route.skipped",
			}, writeMCPResult(conn, message, json.RawMessage(`{"ok":true}`))
		case mcp.ToolSetScreenScene:
			kind, err := classifyDisplaySceneArguments(params.Arguments)
			if err != nil {
				return mcpHandledResult{}, err
			}
			return mcpHandledResult{
				DisplayScene:           kind == "listening",
				AgentBridgeSkipDisplay: kind == "agent_route.skipped",
			}, writeMCPResult(conn, message, json.RawMessage(`{"ok":true}`))
		case mcp.ToolSetLEDColor:
			kind, err := classifyLEDArguments(params.Arguments)
			if err != nil {
				return mcpHandledResult{}, err
			}
			return mcpHandledResult{
				LEDFeedback:        kind == "listening",
				AgentBridgeSkipLED: kind == "agent_route.skipped",
			}, writeMCPResult(conn, message, json.RawMessage(`{"ok":true}`))
		default:
			return mcpHandledResult{}, fmt.Errorf("unsupported mcp tools/call name %q", params.Name)
		}
	default:
		return mcpHandledResult{}, fmt.Errorf("unsupported mcp method %q", message.Method)
	}
}

func toolsForFirmwareProfile(profile string) []mcp.Tool {
	if profile == FirmwareProfileOfficialStackChanV141 {
		return []mcp.Tool{
			{Name: mcp.ToolSetHeadAngles, Description: "Set StackChan head angles"},
			{Name: mcp.ToolSetLEDColor, Description: "Set StackChan LED color"},
			{Name: mcp.ToolScreenBrightness, Description: "Set screen brightness"},
			{Name: mcp.ToolScreenTheme, Description: "Set screen theme"},
			{Name: mcp.ToolTakePhoto, Description: "Take a photo"},
		}
	}
	return []mcp.Tool{
		{Name: mcp.ToolSetHeadAngles, Description: "Set StackChan head angles"},
		{Name: mcp.ToolSetScreenScene, Description: "Set StackChan semantic screen scene"},
		{Name: mcp.ToolSetLEDColor, Description: "Set StackChan LED color"},
	}
}

func firmwareProfileSupportsTool(profile string, tool string) bool {
	for _, supported := range toolsForFirmwareProfile(profile) {
		if supported.Name == tool {
			return true
		}
	}
	return false
}

func writeMCPResult(conn *websocket.Conn, request mcp.Message, result any) error {
	if request.ID == nil {
		return errors.New("mcp request is missing id")
	}
	response, err := mcp.NewResultResponse(*request.ID, result)
	if err != nil {
		return err
	}
	raw, err := response.Raw()
	if err != nil {
		return err
	}
	return conn.WriteJSON(xiaozhi.ClientMCPMessage{
		Type:    xiaozhi.MessageTypeMCP,
		Payload: raw,
	})
}

func waitForAgentBridgeSkipFeedback(conn *websocket.Conn, options ScenarioOptions, result turnResult) (turnResult, error) {
	if hasAgentBridgeSkipFeedback(result) {
		return result, nil
	}
	wait := options.Timeout
	if wait <= 0 || wait > time.Second {
		wait = time.Second
	}
	deadline := time.Now().Add(wait)
	for !hasAgentBridgeSkipFeedback(result) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return result, nil
		}
		event, err := readDownlinkEvent(conn, remaining, options.ProtocolVersion)
		if err != nil {
			var netError net.Error
			if errors.As(err, &netError) && netError.Timeout() {
				return result, nil
			}
			return result, err
		}
		if event.Type != xiaozhi.MessageTypeMCP {
			continue
		}
		handled, err := handleMCPDownlink(conn, options, event)
		if err != nil {
			return result, err
		}
		result.MCPAgentBridgeSkipDisplay = result.MCPAgentBridgeSkipDisplay || handled.AgentBridgeSkipDisplay
		result.MCPAgentBridgeSkipHead = result.MCPAgentBridgeSkipHead || handled.AgentBridgeSkipHead
		result.MCPAgentBridgeSkipLED = result.MCPAgentBridgeSkipLED || handled.AgentBridgeSkipLED
	}
	return result, nil
}

func hasAgentBridgeSkipFeedback(result turnResult) bool {
	return result.MCPAgentBridgeSkipDisplay && result.MCPAgentBridgeSkipHead && result.MCPAgentBridgeSkipLED
}

func missingAgentBridgeSkipFeedback(result turnResult) []string {
	missing := make([]string, 0, 3)
	if !result.MCPAgentBridgeSkipDisplay {
		missing = append(missing, "agent_route.skipped display scene")
	}
	if !result.MCPAgentBridgeSkipHead {
		missing = append(missing, "settle head motion")
	}
	if !result.MCPAgentBridgeSkipLED {
		missing = append(missing, "settle LED")
	}
	return missing
}

func classifyHeadMotionArguments(arguments map[string]any) (string, error) {
	if intArgument(arguments["yaw"]) != 0 {
		return "", fmt.Errorf("head yaw = %v, want 0", arguments["yaw"])
	}
	speed := intArgument(arguments["speed"])
	switch pitch := intArgument(arguments["pitch"]); {
	case pitch == 8 && speed == 150:
		return "listening", nil
	case pitch == 0 && speed == 150:
		return "agent_route.skipped", nil
	default:
		return "", fmt.Errorf("head motion = pitch %v speed %v, want listening or settle feedback", arguments["pitch"], arguments["speed"])
	}
}

func classifyDisplaySceneArguments(arguments map[string]any) (string, error) {
	if arguments["type"] != stackchan.SceneType {
		return "", fmt.Errorf("display scene type = %v, want stackchan.scene", arguments["type"])
	}
	scene, ok := arguments["scene"].(string)
	if !ok {
		return "", fmt.Errorf("display scene = %v, want string", arguments["scene"])
	}
	expectations := map[string]struct {
		emotion string
		caption string
		accent  string
	}{
		stackchan.SceneListening: {
			emotion: stackchan.EmotionCurious,
			caption: "我在听。",
			accent:  stackchan.AccentCyan,
		},
		stackchan.SceneThinking: {
			emotion: stackchan.EmotionCurious,
			caption: "我在想。",
			accent:  stackchan.AccentAmber,
		},
		stackchan.SceneSpeaking: {
			emotion: stackchan.EmotionWarm,
			caption: "我在说。",
			accent:  stackchan.AccentGreen,
		},
		stackchan.SceneIdle: {
			emotion: stackchan.EmotionNeutral,
			accent:  stackchan.AccentDefault,
		},
		stackchan.DisplayEventAgentRouteSkipped: {
			emotion: stackchan.EmotionCurious,
			caption: "我先用普通对话。",
			accent:  stackchan.AccentAmber,
		},
	}
	kind := scene
	if scene == stackchan.SceneTool && arguments["caption"] == "我先用普通对话。" {
		kind = stackchan.DisplayEventAgentRouteSkipped
	}
	want, ok := expectations[kind]
	if !ok {
		return "", fmt.Errorf("display scene = %v, want supported lifecycle or agent bridge skip scene", arguments["scene"])
	}
	if arguments["emotion"] != want.emotion {
		return "", fmt.Errorf("display emotion = %v, want %s for %s", arguments["emotion"], want.emotion, kind)
	}
	if caption, _ := arguments["caption"].(string); caption != want.caption {
		return "", fmt.Errorf("display caption = %v, want %q for %s", arguments["caption"], want.caption, kind)
	}
	if arguments["accent"] != want.accent {
		return "", fmt.Errorf("display accent = %v, want %s for %s", arguments["accent"], want.accent, kind)
	}
	if intArgument(arguments["ttl_ms"]) != 1800 {
		return "", fmt.Errorf("display ttl_ms = %v, want 1800", arguments["ttl_ms"])
	}
	if kind == stackchan.DisplayEventAgentRouteSkipped {
		return "agent_route.skipped", nil
	}
	return scene, nil
}

func classifyLEDArguments(arguments map[string]any) (string, error) {
	red := intArgument(arguments["red"])
	green := intArgument(arguments["green"])
	blue := intArgument(arguments["blue"])
	switch {
	case red == 0 && green == 168 && blue == 0:
		return "listening", nil
	case red == 168 && green == 112 && blue == 0:
		return "thinking", nil
	case red == 0 && green == 0 && blue == 168:
		return "speaking", nil
	case red == 0 && green == 0 && blue == 0:
		return "idle", nil
	case red == 0 && green == 24 && blue == 32:
		return "agent_route.skipped", nil
	default:
		return "", fmt.Errorf("led color = %#v, want lifecycle or settle feedback", arguments)
	}
}

func intArgument(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
}

func waitForConnectionClosed(conn *websocket.Conn, timeout time.Duration) error {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set reconnect close deadline: %w", err)
	}
	_, _, err := conn.ReadMessage()
	if err == nil {
		return errors.New("old websocket remained readable after reconnect")
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return fmt.Errorf("old websocket did not close within %s", timeout)
	}
	return nil
}

func simulatedOpusFrame(index int) []byte {
	return []byte{0xf8, 0xff, 0xfe, byte(index)}
}

func percentile(values []int64, pct int) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := (len(sorted)*pct + 99) / 100
	if index <= 0 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func traceStartOffset(path string) (int64, error) {
	if strings.TrimSpace(path) == "" {
		return 0, nil
	}
	stat, err := os.Stat(path)
	if err == nil {
		return stat.Size(), nil
	}
	if os.IsNotExist(err) {
		return 0, nil
	}
	return 0, fmt.Errorf("stat trace file: %w", err)
}

func requiredTraceEventsSince(path string, offset int64, required []string) ([]string, error) {
	if len(required) == 0 {
		return nil, nil
	}
	deadline := time.Now().Add(time.Second)
	for {
		data, err := readTraceFileSince(path, offset)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var event struct {
				Event string `json:"event"`
			}
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				return nil, fmt.Errorf("decode trace jsonl: %w", err)
			}
			if event.Event != "" {
				seen[event.Event] = true
			}
		}
		missingEvent := ""
		for _, event := range required {
			if !seen[event] {
				missingEvent = event
				break
			}
		}
		if missingEvent != "" {
			if time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return sortedTraceEvents(seen), fmt.Errorf("trace missing required event %q", missingEvent)
		}
		return sortedTraceEvents(seen), nil
	}
}

func readTraceFileSince(path string, offset int64) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("trace file is required when trace events are required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read trace file: %w", err)
	}
	if offset < 0 || offset > int64(len(data)) {
		offset = 0
	}
	return data[offset:], nil
}

func sortedTraceEvents(seen map[string]bool) []string {
	events := make([]string, 0, len(seen))
	for event := range seen {
		events = append(events, event)
	}
	sort.Strings(events)
	return events
}
