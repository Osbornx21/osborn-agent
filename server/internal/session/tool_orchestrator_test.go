package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/stackchan"
	servicetools "stackchan-gateway/internal/tools"
)

func TestMCPToolOrchestratorSkipsRawStackChanHardwareToolsFromVoiceHotPath(t *testing.T) {
	turn := Turn{SessionID: "sess_raw_stackchan_tools", DeviceID: "stackchan-s3-main", Generation: 7}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker: broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return generation == turn.Generation
			},
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{}),
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{
			{
				Name:      mcp.ToolSetHeadAngles,
				Arguments: map[string]any{"yaw": 40, "pitch": 40, "speed": 500},
			},
			{
				Name:      mcp.ToolSetLEDColor,
				Arguments: map[string]any{"red": 999, "green": -8, "blue": 64},
			},
			{
				Name: mcp.ToolSetScreenScene,
				Arguments: map[string]any{
					"scene":   stackchan.SceneTool,
					"emotion": stackchan.EmotionHappy,
					"caption": "raw",
				},
			},
		},
	})

	if len(outcomes) != 3 {
		t.Fatalf("outcomes = %+v, want one skipped outcome per raw StackChan tool", outcomes)
	}
	for _, outcome := range outcomes {
		if !outcome.Skipped || outcome.ErrorCode != mcp.ErrorCodeToolNotAllowed {
			t.Fatalf("outcome = %+v, want skipped raw StackChan tool with %s", outcome, mcp.ErrorCodeToolNotAllowed)
		}
	}
	for _, rawTool := range []string{mcp.ToolSetHeadAngles, mcp.ToolSetLEDColor, mcp.ToolSetScreenScene} {
		if calls := downlink.MCPToolCalls(t, rawTool); len(calls) != 0 {
			t.Fatalf("%s calls = %+v, want no raw StackChan hardware dispatch from voice hot path", rawTool, calls)
		}
	}
}

func TestMCPToolOrchestratorRoutesStackChanExpressionThroughSchedulers(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_expression", DeviceID: "stackchan-s3-main", Generation: 12}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker: broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return generation == turn.Generation
			},
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			MaxCaptionChars: 24,
			SceneTTLMS:      900,
		}),
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      stackchan.ToolExpress,
			Arguments: map[string]any{"cue": "nod"},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != "" || outcomes[0].ResultBytes == 0 {
		t.Fatalf("outcomes = %+v, want successful expression call", outcomes)
	}
	if !downlink.HasMCPToolCall(t, mcp.ToolSetHeadAngles, map[string]float64{"yaw": 0, "pitch": 16, "speed": 220}) {
		t.Fatalf("missing nod head motion; sequence=%v", downlink.Sequence())
	}
	if !downlink.HasMCPToolCall(t, mcp.ToolSetLEDColor, map[string]float64{"red": 0, "green": 168, "blue": 96}) {
		t.Fatalf("missing nod LED; sequence=%v", downlink.Sequence())
	}
	scene := waitForScreenSceneCaption(t, downlink, "我点头。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneSpeaking || scene.Arguments["emotion"] != stackchan.EmotionReady || scene.Arguments["accent"] != stackchan.AccentGreen {
		t.Fatalf("expression scene = %#v, want nod scene", scene.Arguments)
	}
}

func TestMCPToolOrchestratorUsesConfiguredStackChanExpressionPolicy(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_expression_configured", DeviceID: "stackchan-s3-main", Generation: 14}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker: broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return generation == turn.Generation
			},
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			MaxCaptionChars: 24,
			SceneTTLMS:      900,
		}),
		ExpressionPolicies: map[string]stackchan.ExpressionPolicy{
			stackchan.CueNod: {
				Motion: &stackchan.MotionCommand{Yaw: -6, Pitch: 12, Speed: 280},
				LED:    &stackchan.LEDCommand{R: 16, G: 32, B: 48},
				Scene: stackchan.ScenePolicy{
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionHappy,
					Caption: "收到。",
					Accent:  stackchan.AccentGreen,
				},
			},
		},
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      stackchan.ToolExpress,
			Arguments: map[string]any{"cue": "nod"},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != "" {
		t.Fatalf("outcomes = %+v, want successful configured expression call", outcomes)
	}
	if !downlink.HasMCPToolCall(t, mcp.ToolSetHeadAngles, map[string]float64{"yaw": -6, "pitch": 12, "speed": 280}) {
		t.Fatalf("missing configured head motion; sequence=%v", downlink.Sequence())
	}
	if !downlink.HasMCPToolCall(t, mcp.ToolSetLEDColor, map[string]float64{"red": 16, "green": 32, "blue": 48}) {
		t.Fatalf("missing configured LED; sequence=%v", downlink.Sequence())
	}
	scene := waitForScreenSceneCaption(t, downlink, "收到。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionHappy {
		t.Fatalf("configured expression scene = %#v, want configured scene", scene.Arguments)
	}
}

func TestMCPToolOrchestratorRejectsUnknownStackChanExpressionCue(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_expression_bad", DeviceID: "stackchan-s3-main", Generation: 13}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker:        broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{CurrentGeneration: turn.Generation}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{}),
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      stackchan.ToolExpress,
			Arguments: map[string]any{"cue": "spin_forever"},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != errorCodeToolCallInvalid || !outcomes[0].Skipped {
		t.Fatalf("outcomes = %+v, want skipped invalid expression cue", outcomes)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetHeadAngles); len(calls) != 0 {
		t.Fatalf("unexpected head calls for rejected expression: %+v", calls)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetLEDColor); len(calls) != 0 {
		t.Fatalf("unexpected LED calls for rejected expression: %+v", calls)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetScreenScene); len(calls) != 0 {
		t.Fatalf("unexpected scene calls for rejected expression: %+v", calls)
	}
}

func TestMCPToolOrchestratorRoutesStackChanExpressionSequenceThroughSchedulers(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_expression_sequence", DeviceID: "stackchan-s3-main", Generation: 17}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker: broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			MinCommandGap: time.Millisecond,
			GenerationIsCurrent: func(generation int64) bool {
				return generation == turn.Generation
			},
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			MaxCaptionChars: 24,
			SceneTTLMS:      900,
		}),
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name: stackchan.ToolExpressionSequence,
			Arguments: map[string]any{
				"cues": []any{"thinking", "nod", "settle"},
			},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != "" || outcomes[0].ResultBytes == 0 {
		t.Fatalf("outcomes = %+v, want successful expression sequence call", outcomes)
	}
	for _, want := range []map[string]float64{
		{"yaw": 0, "pitch": 8, "speed": 150},
		{"yaw": 0, "pitch": 16, "speed": 220},
		{"yaw": 0, "pitch": 0, "speed": 150},
	} {
		if !downlink.HasMCPToolCall(t, mcp.ToolSetHeadAngles, want) {
			t.Fatalf("missing sequence head motion %+v; sequence=%v", want, downlink.Sequence())
		}
	}
	for _, caption := range []string{"我在想。", "我点头。"} {
		_ = waitForScreenSceneCaption(t, downlink, caption, time.Second)
	}
}

func TestMCPToolOrchestratorRejectsInvalidStackChanExpressionSequenceBeforeDispatch(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_expression_sequence_bad", DeviceID: "stackchan-s3-main", Generation: 18}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker:        broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{CurrentGeneration: turn.Generation}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{}),
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name: stackchan.ToolExpressionSequence,
			Arguments: map[string]any{
				"cues": []any{"nod", "spin_forever"},
			},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != errorCodeToolCallInvalid || !outcomes[0].Skipped {
		t.Fatalf("outcomes = %+v, want skipped invalid expression sequence", outcomes)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetHeadAngles); len(calls) != 0 {
		t.Fatalf("unexpected head calls for rejected expression sequence: %+v", calls)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetLEDColor); len(calls) != 0 {
		t.Fatalf("unexpected LED calls for rejected expression sequence: %+v", calls)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetScreenScene); len(calls) != 0 {
		t.Fatalf("unexpected scene calls for rejected expression sequence: %+v", calls)
	}
}

func TestMCPToolOrchestratorRoutesStackChanExpressionSequencePreset(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_expression_sequence_preset", DeviceID: "stackchan-s3-main", Generation: 19}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker: broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			MinCommandGap: time.Millisecond,
			GenerationIsCurrent: func(generation int64) bool {
				return generation == turn.Generation
			},
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			MaxCaptionChars: 24,
			SceneTTLMS:      900,
		}),
		ExpressionSequences: map[string][]string{
			"agree.quick": {"attentive", "nod"},
		},
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name: stackchan.ToolPlayExpressionSequence,
			Arguments: map[string]any{
				"sequence": "agree.quick",
			},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != "" || outcomes[0].ResultBytes == 0 {
		t.Fatalf("outcomes = %+v, want successful expression preset call", outcomes)
	}
	for _, want := range []map[string]float64{
		{"yaw": 0, "pitch": 12, "speed": 180},
		{"yaw": 0, "pitch": 16, "speed": 220},
	} {
		if !downlink.HasMCPToolCall(t, mcp.ToolSetHeadAngles, want) {
			t.Fatalf("missing preset head motion %+v; sequence=%v", want, downlink.Sequence())
		}
	}
	for _, caption := range []string{"我在。", "我点头。"} {
		_ = waitForScreenSceneCaption(t, downlink, caption, time.Second)
	}
}

func TestMCPToolOrchestratorRejectsUnknownStackChanExpressionSequencePreset(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_expression_sequence_preset_bad", DeviceID: "stackchan-s3-main", Generation: 20}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker:        broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{CurrentGeneration: turn.Generation}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{}),
		ExpressionSequences: map[string][]string{
			"agree.quick": {"attentive", "nod"},
		},
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      stackchan.ToolPlayExpressionSequence,
			Arguments: map[string]any{"sequence": "raw_pixels"},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != errorCodeToolCallInvalid || !outcomes[0].Skipped {
		t.Fatalf("outcomes = %+v, want skipped invalid expression preset", outcomes)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetHeadAngles); len(calls) != 0 {
		t.Fatalf("unexpected head calls for rejected expression preset: %+v", calls)
	}
}

func TestMCPToolOrchestratorRoutesStackChanDisplayCardThroughSceneComposer(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_display_card", DeviceID: "stackchan-s3-main", Generation: 15}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetScreenScene},
	})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker: broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      900,
			MaxCaptionChars: 20,
			Cards: map[string]stackchan.DisplayCardPolicy{
				"status.note": {
					ScenePolicy: stackchan.ScenePolicy{
						Scene:   stackchan.SceneTool,
						Emotion: stackchan.EmotionWarm,
						Accent:  stackchan.AccentGreen,
					},
					AllowCaption:    true,
					MaxCaptionChars: 10,
				},
			},
		}),
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      stackchan.ToolShowCard,
			Arguments: map[string]any{"card": "status.note", "caption": "这是一段很长的状态提示"},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != "" || outcomes[0].ResultBytes == 0 {
		t.Fatalf("outcomes = %+v, want successful display-card call", outcomes)
	}
	scene := waitForScreenSceneCaption(t, downlink, "这是一段很长的状态提", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionWarm || scene.Arguments["accent"] != stackchan.AccentGreen {
		t.Fatalf("display card scene = %#v, want configured scene", scene.Arguments)
	}
}

func TestMCPToolOrchestratorRejectsUnknownStackChanDisplayCard(t *testing.T) {
	turn := Turn{SessionID: "sess_tool_display_card_bad", DeviceID: "stackchan-s3-main", Generation: 16}
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, turn.SessionID, downlink, []string{
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		Broker: broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			Cards: map[string]stackchan.DisplayCardPolicy{
				"status.note": {ScenePolicy: stackchan.ScenePolicy{Scene: stackchan.SceneTool}},
			},
		}),
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      stackchan.ToolShowCard,
			Arguments: map[string]any{"card": "raw_pixels", "caption": "bad"},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != errorCodeToolCallInvalid || !outcomes[0].Skipped {
		t.Fatalf("outcomes = %+v, want skipped invalid display card", outcomes)
	}
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetScreenScene); len(calls) != 0 {
		t.Fatalf("unexpected display scene for rejected card: %+v", calls)
	}
}

func TestMCPToolOrchestratorRoutesServiceToolThroughRegistry(t *testing.T) {
	turn := Turn{SessionID: "sess_service_tool", DeviceID: "stackchan-s3-main", Generation: 9}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	var received servicetools.Call
	if err := registry.Register(servicetools.Definition{
		Name:       "memory.lookup",
		Permission: servicetools.PermissionRead,
	}, func(_ context.Context, call servicetools.Call) (servicetools.Result, error) {
		received = call
		return servicetools.Result{Payload: json.RawMessage(`{"count":1}`)}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		ServiceTools: registry,
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      "memory.lookup",
			Arguments: map[string]any{"query": "低延迟"},
		}},
	})

	if len(outcomes) != 1 || outcomes[0].ErrorCode != "" || outcomes[0].ResultBytes == 0 {
		t.Fatalf("outcomes = %+v, want successful service tool call with result bytes", outcomes)
	}
	if received.SessionID != turn.SessionID || received.DeviceID != turn.DeviceID || received.Generation != turn.Generation {
		t.Fatalf("received service call = %+v, want turn identity", received)
	}
}

func TestMCPToolOrchestratorRejectsServiceToolPermission(t *testing.T) {
	turn := Turn{SessionID: "sess_service_permission", DeviceID: "stackchan-s3-main", Generation: 10}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	if err := registry.Register(servicetools.Definition{
		Name:       "calendar.create",
		Permission: servicetools.PermissionWrite,
	}, func(context.Context, servicetools.Call) (servicetools.Result, error) {
		t.Fatal("permission-denied service tool should not execute")
		return servicetools.Result{}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	orchestrator := NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
		ServiceTools: registry,
	})

	outcomes := orchestrator.ExecuteToolCalls(context.Background(), ToolCallRequest{
		Turn: turn,
		Calls: []providers.ToolCall{{
			Name:      "calendar.create",
			Arguments: map[string]any{"title": "must-not-leak"},
		}},
	})

	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %+v, want one outcome", outcomes)
	}
	if outcomes[0].ErrorCode != servicetools.ErrorCodePermissionDenied || !outcomes[0].Skipped {
		t.Fatalf("outcome = %+v, want skipped permission-denied outcome", outcomes[0])
	}
}

func newRespondingTestBroker(t *testing.T, sessionID string, downlink *recordingDownlink, allowed []string) *mcp.Broker {
	t.Helper()
	broker, err := mcp.NewBroker(mcp.BrokerOptions{
		SessionID: sessionID,
		Downlink:  downlink,
		Allowlist: mcp.NewAllowlist(allowed),
	})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	downlink.onJSON = func(event downlinkEvent) {
		if event.Type != xiaozhi.MessageTypeMCP {
			return
		}
		message, err := mcp.ParseMessage(event.Payload)
		if err != nil || message.Method != mcp.MethodToolsCall || message.ID == nil {
			return
		}
		response, err := mcp.NewResultResponse(*message.ID, json.RawMessage(`{"ok":true}`))
		if err != nil {
			t.Errorf("NewResultResponse() error = %v", err)
			return
		}
		responseRaw, err := response.Raw()
		if err != nil {
			t.Errorf("response Raw() error = %v", err)
			return
		}
		if err := broker.HandleDevicePayload(responseRaw); err != nil {
			t.Errorf("HandleDevicePayload() error = %v", err)
		}
	}
	return broker
}
