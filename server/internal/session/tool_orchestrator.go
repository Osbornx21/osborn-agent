package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"stackchan-gateway/internal/homeassistant"
	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/reminder"
	"stackchan-gateway/internal/search"
	"stackchan-gateway/internal/stackchan"
	servicetools "stackchan-gateway/internal/tools"
)

const (
	defaultToolCallTimeout     = 1200 * time.Millisecond
	defaultMaxToolCallsPerTurn = 4

	errorCodeToolCallInvalid               = "tool_call_invalid"
	errorCodeToolCallFailed                = "tool_call_failed"
	errorCodeToolCallLimitExceeded         = "tool_call_limit_exceeded"
	errorCodeToolOrchestratorNotConfigured = "tool_orchestrator_not_configured"
	errorCodeToolSchedulerNotConfigured    = "tool_scheduler_not_configured"
	errorCodeStackChanRateLimited          = "stackchan_rate_limited"
	errorCodeStackChanOldGeneration        = "stackchan_old_generation"
	errorCodeStackChanTurnLimit            = "stackchan_turn_limit"
	v21VoiceQueryToolName                  = "v21.voice_query"
)

var errToolSchedulerNotConfigured = errors.New("tool scheduler is not configured")

type ToolCallRequest struct {
	Turn  Turn
	Calls []providers.ToolCall
}

type ToolCallOutcome struct {
	Index         int
	Name          string
	ArgumentCount int
	ResultBytes   int
	Result        json.RawMessage
	Skipped       bool
	ErrorCode     string
}

type ToolOrchestrator interface {
	ExecuteToolCalls(ctx context.Context, request ToolCallRequest) []ToolCallOutcome
}

func (l *VoiceLoop) executeLLMToolCalls(runtime *TurnRuntime, calls []providers.ToolCall) {
	if len(calls) == 0 || runtime == nil {
		return
	}
	calls = cloneProviderToolCalls(calls)
	go func() {
		_ = l.executeLLMToolCallsSync(runtime.Context, runtime, calls)
	}()
}

func (l *VoiceLoop) executeLLMToolCallsSync(ctx context.Context, runtime *TurnRuntime, calls []providers.ToolCall) []ToolCallOutcome {
	if len(calls) == 0 || runtime == nil {
		return nil
	}
	calls = cloneProviderToolCalls(calls)
	calls = resolveProviderToolAliases(runtime.LLMToolNameAliases, calls)
	l.dispatchEventDisplayScene(runtime.Turn, stackchan.DisplayEventToolRunning)
	if l.toolOrchestrator == nil {
		outcomes := make([]ToolCallOutcome, 0, len(calls))
		for index, call := range calls {
			outcome := ToolCallOutcome{
				Index:         index,
				Name:          strings.TrimSpace(call.Name),
				ArgumentCount: len(call.Arguments),
				Skipped:       true,
				ErrorCode:     errorCodeToolOrchestratorNotConfigured,
			}
			outcomes = append(outcomes, outcome)
			l.recordLLMToolCall(runtime, outcome)
		}
		l.dispatchToolOutcomeDisplayScene(runtime.Turn, outcomes)
		return outcomes
	}

	outcomes := l.toolOrchestrator.ExecuteToolCalls(ctx, ToolCallRequest{
		Turn:  runtime.Turn,
		Calls: calls,
	})
	l.dispatchToolSpecificDisplayScenes(runtime.Turn, outcomes)
	l.dispatchToolOutcomeDisplayScene(runtime.Turn, outcomes)
	for _, outcome := range outcomes {
		l.recordLLMToolCall(runtime, outcome)
	}
	return outcomes
}

func (l *VoiceLoop) dispatchToolOutcomeDisplayScene(turn Turn, outcomes []ToolCallOutcome) {
	var hasSuccess bool
	for _, outcome := range outcomes {
		if outcome.Skipped || outcome.ErrorCode != "" {
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventToolFailed)
			return
		}
		hasSuccess = true
	}
	if hasSuccess {
		l.dispatchEventDisplayScene(turn, stackchan.DisplayEventToolSucceeded)
	}
}

func (l *VoiceLoop) dispatchToolSpecificDisplayScenes(turn Turn, outcomes []ToolCallOutcome) {
	for _, outcome := range outcomes {
		if outcome.Skipped || outcome.ErrorCode != "" {
			continue
		}
		switch strings.TrimSpace(outcome.Name) {
		case homeassistant.GetStateToolName:
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventHomeAssistantState)
		case homeassistant.CallActionToolName:
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventHomeAssistantAction)
		case search.WebSearchToolName:
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventSearchWeb)
		case mcp.ToolTakePhoto:
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventCameraCapturing)
		case reminder.AnnounceToolName:
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventReminderDue)
		case v21VoiceQueryToolName:
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventAgentRouteV21)
		}
	}
}

func resolveProviderToolAliases(aliases map[string]string, calls []providers.ToolCall) []providers.ToolCall {
	if len(aliases) == 0 || len(calls) == 0 {
		return calls
	}
	for index := range calls {
		alias := strings.TrimSpace(calls[index].Name)
		if internalName, ok := aliases[alias]; ok {
			calls[index].Name = internalName
		}
	}
	return calls
}

func (l *VoiceLoop) recordLLMToolCall(runtime *TurnRuntime, outcome ToolCallOutcome) {
	fields := map[string]any{
		"call_index":     outcome.Index,
		"tool_name":      outcome.Name,
		"argument_count": outcome.ArgumentCount,
		"result_bytes":   outcome.ResultBytes,
		"skipped":        outcome.Skipped,
	}
	l.recordTurnEvent(runtime.Context, runtime.Turn, "llm_tool_call", "", outcome.ErrorCode, fields)
}

type MCPToolOrchestratorOptions struct {
	Broker              *mcp.Broker
	ServiceTools        *servicetools.Registry
	BodyScheduler       *stackchan.BodyScheduler
	SceneComposer       *stackchan.SceneComposer
	ExpressionPolicies  map[string]stackchan.ExpressionPolicy
	ExpressionSequences map[string][]string
	Timeout             time.Duration
	MaxCallsPerTurn     int
}

type MCPToolOrchestrator struct {
	broker              *mcp.Broker
	serviceTools        *servicetools.Registry
	bodyScheduler       *stackchan.BodyScheduler
	sceneComposer       *stackchan.SceneComposer
	expressionPolicies  map[string]stackchan.ExpressionPolicy
	expressionSequences map[string][]string
	timeout             time.Duration
	maxCallsPerTurn     int
}

func NewMCPToolOrchestrator(options MCPToolOrchestratorOptions) *MCPToolOrchestrator {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultToolCallTimeout
	}
	maxCalls := options.MaxCallsPerTurn
	if maxCalls <= 0 {
		maxCalls = defaultMaxToolCallsPerTurn
	}
	return &MCPToolOrchestrator{
		broker:              options.Broker,
		serviceTools:        options.ServiceTools,
		bodyScheduler:       options.BodyScheduler,
		sceneComposer:       options.SceneComposer,
		expressionPolicies:  cloneExpressionPolicies(options.ExpressionPolicies),
		expressionSequences: cloneExpressionSequences(options.ExpressionSequences),
		timeout:             timeout,
		maxCallsPerTurn:     maxCalls,
	}
}

func (o *MCPToolOrchestrator) ExecuteToolCalls(ctx context.Context, request ToolCallRequest) []ToolCallOutcome {
	outcomes := make([]ToolCallOutcome, 0, len(request.Calls))
	if o == nil {
		for index, call := range request.Calls {
			outcomes = append(outcomes, ToolCallOutcome{
				Index:         index,
				Name:          strings.TrimSpace(call.Name),
				ArgumentCount: len(call.Arguments),
				Skipped:       true,
				ErrorCode:     errorCodeToolOrchestratorNotConfigured,
			})
		}
		return outcomes
	}
	for index, call := range request.Calls {
		outcome := ToolCallOutcome{
			Index:         index,
			Name:          strings.TrimSpace(call.Name),
			ArgumentCount: len(call.Arguments),
		}
		if index >= o.maxCallsPerTurn {
			outcome.Skipped = true
			outcome.ErrorCode = errorCodeToolCallLimitExceeded
			outcomes = append(outcomes, outcome)
			continue
		}
		if outcome.Name == "" {
			outcome.Skipped = true
			outcome.ErrorCode = errorCodeToolCallInvalid
			outcomes = append(outcomes, outcome)
			continue
		}
		result, err := o.executeOne(ctx, request.Turn, providers.ToolCall{
			ID:        call.ID,
			Name:      outcome.Name,
			Arguments: call.Arguments,
		})
		if err != nil {
			outcome.ErrorCode = safeToolCallErrorCode(err)
			outcome.Skipped = isSkippedToolCallError(err)
			outcomes = append(outcomes, outcome)
			continue
		}
		outcome.ResultBytes = len(result)
		outcome.Result = cloneRawJSON(result)
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func (o *MCPToolOrchestrator) executeOne(ctx context.Context, turn Turn, call providers.ToolCall) (json.RawMessage, error) {
	if o != nil && o.serviceTools != nil && o.serviceTools.HasTool(call.Name) {
		result, err := o.serviceTools.ExecuteTool(ctx, servicetools.Call{
			SessionID:  turn.SessionID,
			DeviceID:   turn.DeviceID,
			Generation: turn.Generation,
			Name:       call.Name,
			Arguments:  cloneToolArguments(call.Arguments),
			CreatedAt:  time.Now(),
		})
		if err != nil {
			return nil, err
		}
		return result.Payload, nil
	}
	if o == nil || o.broker == nil {
		return nil, mcp.ErrBrokerNotConfigured
	}
	callCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	switch call.Name {
	case stackchan.ToolExpress:
		return o.dispatchExpression(callCtx, turn, call.Arguments)
	case stackchan.ToolExpressionSequence:
		return o.dispatchExpressionSequence(callCtx, turn, call.Arguments)
	case stackchan.ToolPlayExpressionSequence:
		return o.dispatchExpressionSequencePreset(callCtx, turn, call.Arguments)
	case stackchan.ToolShowCard:
		return o.dispatchDisplayCard(callCtx, turn, call.Arguments)
	case mcp.ToolTakePhoto:
		return nil, &mcp.ToolError{Code: mcp.ErrorCodeToolNotAllowed, Message: "camera capture is not exposed to voice hot path"}
	case mcp.ToolSetHeadAngles, mcp.ToolSetLEDColor, mcp.ToolSetScreenScene:
		return nil, &mcp.ToolError{Code: mcp.ErrorCodeToolNotAllowed, Message: "raw StackChan body and screen tools are not exposed to voice hot path"}
	default:
		return o.broker.CallTool(callCtx, call.Name, cloneToolArguments(call.Arguments))
	}
}

func (o *MCPToolOrchestrator) dispatchHeadAngles(ctx context.Context, turn Turn, arguments map[string]any) (json.RawMessage, error) {
	if o.bodyScheduler == nil {
		return nil, errToolSchedulerNotConfigured
	}
	o.bodyScheduler.SetGeneration(turn.Generation)
	return o.bodyScheduler.DispatchMotion(ctx, o.broker, stackchan.MotionCommand{
		Generation: turn.Generation,
		Yaw:        intToolArgument(arguments, "yaw"),
		Pitch:      intToolArgument(arguments, "pitch"),
		Speed:      intToolArgument(arguments, "speed"),
		Priority:   stackchan.PriorityNormal,
		Reason:     "llm_tool_call",
	})
}

func (o *MCPToolOrchestrator) dispatchLED(ctx context.Context, turn Turn, arguments map[string]any) (json.RawMessage, error) {
	if o.bodyScheduler == nil {
		return nil, errToolSchedulerNotConfigured
	}
	o.bodyScheduler.SetGeneration(turn.Generation)
	return o.bodyScheduler.DispatchLED(ctx, o.broker, stackchan.LEDCommand{
		Generation: turn.Generation,
		R:          intToolArgument(arguments, "red"),
		G:          intToolArgument(arguments, "green"),
		B:          intToolArgument(arguments, "blue"),
		Priority:   stackchan.PriorityNormal,
		Reason:     "llm_tool_call",
	})
}

func (o *MCPToolOrchestrator) dispatchScene(ctx context.Context, turn Turn, arguments map[string]any) (json.RawMessage, error) {
	if o.sceneComposer == nil {
		return nil, errToolSchedulerNotConfigured
	}
	scene := o.sceneComposer.Compose(stackchan.SceneRequest{
		SessionID:  turn.SessionID,
		Generation: turn.Generation,
		Scene:      stringToolArgument(arguments, "scene"),
		Emotion:    stringToolArgument(arguments, "emotion"),
		Caption:    stringToolArgument(arguments, "caption"),
		Accent:     stringToolArgument(arguments, "accent"),
		Motion:     sceneMotionToolArgument(arguments, "motion"),
	})
	return o.broker.CallTool(ctx, mcp.ToolSetScreenScene, scene.MCPArguments())
}

func (o *MCPToolOrchestrator) dispatchExpression(ctx context.Context, turn Turn, arguments map[string]any) (json.RawMessage, error) {
	plan, err := stackchan.ExpressionForCueWithPolicies(stringToolArgument(arguments, "cue"), o.expressionPolicies)
	if err != nil {
		return nil, err
	}
	if o.broker == nil {
		return nil, mcp.ErrBrokerNotConfigured
	}
	dispatched, err := dispatchExpressionPlan(ctx, o.broker, o.bodyScheduler, o.sceneComposer, turn, plan, true)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"ok":         true,
		"cue":        plan.Cue,
		"dispatched": dispatched,
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (o *MCPToolOrchestrator) dispatchExpressionSequence(ctx context.Context, turn Turn, arguments map[string]any) (json.RawMessage, error) {
	cues := stringListToolArgument(arguments, "cues", stackchan.MaxExpressionSequenceCues)
	return o.dispatchExpressionCueSequence(ctx, turn, cues, "")
}

func (o *MCPToolOrchestrator) dispatchExpressionSequencePreset(ctx context.Context, turn Turn, arguments map[string]any) (json.RawMessage, error) {
	sequenceID := normalizeExpressionSequenceID(stringToolArgument(arguments, "sequence"))
	cues, ok := o.expressionSequences[sequenceID]
	if !ok {
		return nil, stackchan.ErrExpressionCueInvalid
	}
	return o.dispatchExpressionCueSequence(ctx, turn, cues, sequenceID)
}

func (o *MCPToolOrchestrator) dispatchExpressionCueSequence(ctx context.Context, turn Turn, cues []string, sequenceID string) (json.RawMessage, error) {
	if len(cues) == 0 || len(cues) > stackchan.MaxExpressionSequenceCues {
		return nil, stackchan.ErrExpressionCueInvalid
	}
	plans := make([]stackchan.ExpressionPlan, 0, len(cues))
	for _, cue := range cues {
		plan, err := stackchan.ExpressionForCueWithPolicies(cue, o.expressionPolicies)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	if o.broker == nil {
		return nil, mcp.ErrBrokerNotConfigured
	}
	dispatched := 0
	for index, plan := range plans {
		if index > 0 {
			if err := waitForExpressionSequenceCueGap(ctx, o.bodyScheduler); err != nil {
				return nil, err
			}
		}
		count, err := dispatchExpressionPlan(ctx, o.broker, o.bodyScheduler, o.sceneComposer, turn, plan, true)
		if err != nil {
			return nil, err
		}
		dispatched += count
	}
	payloadFields := map[string]any{
		"ok":         true,
		"cues":       cues,
		"dispatched": dispatched,
	}
	if sequenceID != "" {
		payloadFields["sequence"] = sequenceID
	}
	payload, err := json.Marshal(payloadFields)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func waitForExpressionSequenceCueGap(ctx context.Context, bodyScheduler *stackchan.BodyScheduler) error {
	gap := bodyScheduler.MinCommandGap()
	if gap <= 0 {
		return nil
	}
	timer := time.NewTimer(gap)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (o *MCPToolOrchestrator) dispatchDisplayCard(ctx context.Context, turn Turn, arguments map[string]any) (json.RawMessage, error) {
	if o == nil || o.broker == nil {
		return nil, mcp.ErrBrokerNotConfigured
	}
	if o.sceneComposer == nil {
		return nil, errToolSchedulerNotConfigured
	}
	scene, ok := o.sceneComposer.ComposeCard(stringToolArgument(arguments, "card"), stackchan.SceneRequest{
		SessionID:  turn.SessionID,
		Generation: turn.Generation,
		Caption:    stringToolArgument(arguments, "caption"),
	})
	if !ok {
		return nil, stackchan.ErrDisplayCardInvalid
	}
	if _, err := o.broker.CallTool(ctx, mcp.ToolSetScreenScene, scene.MCPArguments()); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"ok":   true,
		"card": stringToolArgument(arguments, "card"),
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func dispatchExpressionPlan(
	ctx context.Context,
	broker *mcp.Broker,
	bodyScheduler *stackchan.BodyScheduler,
	sceneComposer *stackchan.SceneComposer,
	turn Turn,
	plan stackchan.ExpressionPlan,
	includeScene bool,
) (int, error) {
	if broker == nil {
		return 0, mcp.ErrBrokerNotConfigured
	}
	var firstErr error
	dispatched := 0
	if bodyScheduler != nil {
		bodyScheduler.SetGeneration(turn.Generation)
		if plan.Motion != nil {
			motion := *plan.Motion
			motion.Generation = turn.Generation
			if _, err := bodyScheduler.DispatchMotion(ctx, broker, motion); err != nil {
				firstErr = rememberFirstErr(firstErr, err)
			} else {
				dispatched++
			}
		}
		if plan.LED != nil {
			led := *plan.LED
			led.Generation = turn.Generation
			if _, err := bodyScheduler.DispatchLED(ctx, broker, led); err != nil {
				firstErr = rememberFirstErr(firstErr, err)
			} else {
				dispatched++
			}
		}
	}
	if includeScene && sceneComposer != nil && strings.TrimSpace(plan.Scene.Scene) != "" {
		scene := sceneComposer.Compose(stackchan.SceneRequest{
			SessionID:  turn.SessionID,
			Generation: turn.Generation,
			Scene:      plan.Scene.Scene,
			Emotion:    plan.Scene.Emotion,
			Caption:    plan.Scene.Caption,
			Accent:     plan.Scene.Accent,
			Motion:     plan.Scene.Motion,
		})
		if _, err := broker.CallTool(ctx, mcp.ToolSetScreenScene, scene.MCPArguments()); err != nil {
			firstErr = rememberFirstErr(firstErr, err)
		} else {
			dispatched++
		}
	}
	if dispatched == 0 {
		if firstErr != nil {
			return 0, firstErr
		}
		return 0, errToolSchedulerNotConfigured
	}
	return dispatched, nil
}

func rememberFirstErr(firstErr, err error) error {
	if firstErr != nil {
		return firstErr
	}
	return err
}

func cloneExpressionPolicies(policies map[string]stackchan.ExpressionPolicy) map[string]stackchan.ExpressionPolicy {
	if len(policies) == 0 {
		return nil
	}
	clone := make(map[string]stackchan.ExpressionPolicy, len(policies))
	for cue, policy := range policies {
		if policy.Motion != nil {
			motion := *policy.Motion
			policy.Motion = &motion
		}
		if policy.LED != nil {
			led := *policy.LED
			policy.LED = &led
		}
		if policy.Scene.Motion != nil {
			motion := *policy.Scene.Motion
			policy.Scene.Motion = &motion
		}
		clone[cue] = policy
	}
	return clone
}

func cloneExpressionSequences(sequences map[string][]string) map[string][]string {
	if len(sequences) == 0 {
		return nil
	}
	clone := make(map[string][]string, len(sequences))
	for sequenceID, cues := range sequences {
		sequenceID = normalizeExpressionSequenceID(sequenceID)
		if sequenceID == "" {
			continue
		}
		clone[sequenceID] = append([]string(nil), cues...)
	}
	return clone
}

func normalizeExpressionSequenceID(sequenceID string) string {
	return strings.ToLower(strings.TrimSpace(sequenceID))
}

func cloneToolArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	clone := make(map[string]any, len(arguments))
	for key, value := range arguments {
		clone[key] = value
	}
	return clone
}

func cloneProviderToolCalls(calls []providers.ToolCall) []providers.ToolCall {
	clone := make([]providers.ToolCall, len(calls))
	for index, call := range calls {
		clone[index] = providers.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: cloneToolArguments(call.Arguments),
		}
	}
	return clone
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	clone := make([]byte, len(raw))
	copy(clone, raw)
	return clone
}

func intToolArgument(arguments map[string]any, key string) int {
	value, ok := arguments[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	default:
		return 0
	}
}

func stringToolArgument(arguments map[string]any, key string) string {
	value, ok := arguments[key]
	if !ok {
		return ""
	}
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func stringListToolArgument(arguments map[string]any, key string, maxItems int) []string {
	raw, ok := arguments[key]
	if !ok {
		return nil
	}
	var values []string
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			} else {
				values = append(values, "")
			}
		}
	default:
		return nil
	}
	if maxItems > 0 && len(values) > maxItems {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		out = append(out, value)
	}
	return out
}

func sceneMotionToolArgument(arguments map[string]any, key string) *stackchan.SceneMotion {
	raw, ok := arguments[key]
	if !ok {
		return nil
	}
	motionMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return &stackchan.SceneMotion{
		Preset:    stringToolArgument(motionMap, "preset"),
		Intensity: floatToolArgument(motionMap, "intensity"),
	}
}

func floatToolArgument(arguments map[string]any, key string) float64 {
	value, ok := arguments[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		n, _ := typed.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n
	default:
		return 0
	}
}

func safeToolCallErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var toolError *mcp.ToolError
	if errors.As(err, &toolError) && toolError.Code != "" {
		return toolError.Code
	}
	if code := servicetools.ErrorCode(err); code != "" {
		return code
	}
	switch {
	case errors.Is(err, stackchan.ErrExpressionCueInvalid), errors.Is(err, stackchan.ErrDisplayCardInvalid):
		return errorCodeToolCallInvalid
	case errors.Is(err, errToolSchedulerNotConfigured):
		return errorCodeToolSchedulerNotConfigured
	case errors.Is(err, mcp.ErrBrokerNotConfigured):
		return errorCodeToolOrchestratorNotConfigured
	case errors.Is(err, stackchan.ErrRateLimited):
		return errorCodeStackChanRateLimited
	case errors.Is(err, stackchan.ErrOldGeneration), errors.Is(err, stackchan.ErrSchedulerNotActive):
		return errorCodeStackChanOldGeneration
	case errors.Is(err, stackchan.ErrTurnCommandLimit):
		return errorCodeStackChanTurnLimit
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return mcp.ErrorCodeToolTimeout
	default:
		return errorCodeToolCallFailed
	}
}

func isSkippedToolCallError(err error) bool {
	if errors.Is(err, stackchan.ErrExpressionCueInvalid) || errors.Is(err, stackchan.ErrDisplayCardInvalid) {
		return true
	}
	if errors.Is(err, errToolSchedulerNotConfigured) || errors.Is(err, mcp.ErrBrokerNotConfigured) {
		return true
	}
	var toolError *mcp.ToolError
	if errors.As(err, &toolError) {
		switch toolError.Code {
		case mcp.ErrorCodeToolNotAllowed, mcp.ErrorCodeToolUnavailable:
			return true
		}
	}
	switch servicetools.ErrorCode(err) {
	case servicetools.ErrorCodeToolNotFound, servicetools.ErrorCodeToolNotAllowed, servicetools.ErrorCodePermissionDenied, servicetools.ErrorCodeInvalidToolDefinition:
		return true
	default:
		return false
	}
}
