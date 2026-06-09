package session

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/observability"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/stackchan"
	servicetools "stackchan-gateway/internal/tools"
)

const defaultSegmentMaxRunes = 18
const defaultSegmentMinSoftPunctuationRunes = 4
const defaultMemoryWriteTimeout = 750 * time.Millisecond
const defaultConversationRecordTimeout = 150 * time.Millisecond
const defaultToolFollowUpMaxResults = 3
const defaultToolFollowUpMaxResultBytes = 2048
const defaultToolFollowUpMaxToolCalls = 1
const autoListenTailWindow = 2 * time.Second
const autoListenTailTimeout = 10 * time.Second
const postSpeakingIdleDelay = 500 * time.Millisecond
const displaySceneQueueSize = 16
const lifecycleLEDQueueSize = 16
const lifecycleLEDDispatchTimeout = 250 * time.Millisecond
const traceEventStackChanBodyDispatch = "stackchan_body_dispatch"
const traceEventStackChanExpressionCueDispatch = "stackchan_expression_cue_dispatch"

var (
	ErrVoiceLoopNotConfigured = errors.New("voice loop not configured")
	ErrASRStreamNotStarted    = errors.New("asr stream not started")
	ErrASRStreamClosed        = errors.New("asr stream closed before final event")
	errLifecycleLEDQueueFull  = errors.New("stackchan lifecycle led queue full")
)

type Session struct {
	mu                     sync.Mutex
	id                     string
	deviceID               string
	clientID               string
	state                  State
	generation             int64
	firstGenerationOnHello int64
	generationUsed         bool
}

type Downlink interface {
	SendJSON(ctx context.Context, msg any) error
	SendBinary(ctx context.Context, frame []byte) error
}

type VoiceLoopOptions struct {
	Session                        *Session
	ASR                            providers.ASRProvider
	LLM                            providers.LLMProvider
	TTS                            providers.TTSProvider
	ProviderResolver               VoiceProviderResolver
	ProviderObserver               VoiceProviderObserver
	LLMContextBuilder              LLMContextBuilder
	AgentModeCommands              AgentModeCommandHandler
	ProviderProfileCommands        ProviderProfileCommandHandler
	AgentModeReader                AgentModeReader
	AgentRuntime                   AgentRuntimeRouter
	MemoryWriter                   MemoryWriter
	Downlink                       Downlink
	Pacer                          *audio.Pacer
	SegmentMaxRunes                int
	TraceRecorder                  observability.TraceRecorder
	Metrics                        *observability.Metrics
	MCPBroker                      *mcp.Broker
	ToolOrchestrator               ToolOrchestrator
	ServiceTools                   *servicetools.Registry
	ToolResultFollowUpPolicy       *ToolResultFollowUpPolicy
	ConversationRecorder           ConversationRecorder
	BodyScheduler                  *stackchan.BodyScheduler
	SceneComposer                  *stackchan.SceneComposer
	ExpressionPolicies             map[string]stackchan.ExpressionPolicy
	ExpressionSequences            map[string][]string
	ExpressionProviderToolsEnabled bool
	LifecycleExpressionCues        map[string]string
	EventExpressionCues            map[string]string
	LifecycleLEDs                  map[string]stackchan.LEDCommand
	ListenStartMotionEnabled       bool
	AutoListenTailTimeout          time.Duration
	PostSpeakingIdleDelay          time.Duration
	ASRProviderName                string
	LLMProviderName                string
	TTSProviderName                string
}

type VoiceProviderSet struct {
	Profile string
	ASRName string
	LLMName string
	TTSName string
	ASR     providers.ASRProvider
	LLM     providers.LLMProvider
	TTS     providers.TTSProvider
}

type VoiceProviderResolver interface {
	ResolveVoiceProviders(ctx context.Context, deviceID string) (VoiceProviderSet, error)
}

type VoiceProviderOutcome struct {
	DeviceID              string
	Profile               string
	Generation            int64
	Error                 bool
	ErrorCode             string
	FirstAudibleLatencyMS int64
}

type VoiceProviderObserver interface {
	ObserveVoiceTurn(ctx context.Context, outcome VoiceProviderOutcome)
}

type LLMContextRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Transcript string
	CreatedAt  time.Time
	AgentMode  string
}

type LLMContext struct {
	Text            string
	Messages        []providers.LLMMessage
	MemoryCount     int
	RecentTurnCount int
	PersonaName     string
}

type LLMContextBuilder interface {
	BuildLLMContext(ctx context.Context, request LLMContextRequest) (LLMContext, error)
}

type AgentModeCommandRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Transcript string
	CreatedAt  time.Time
}

type AgentModeCommandResult struct {
	Handled    bool
	Mode       string
	Action     string
	SpokenText string
}

type AgentModeCommandHandler interface {
	HandleAgentModeCommand(ctx context.Context, request AgentModeCommandRequest) (AgentModeCommandResult, error)
}

type ProviderProfileCommandRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Transcript string
	CreatedAt  time.Time
}

type ProviderProfileCommandResult struct {
	Handled    bool
	Profile    string
	Action     string
	SpokenText string
}

type ProviderProfileCommandHandler interface {
	HandleProviderProfileCommand(ctx context.Context, request ProviderProfileCommandRequest) (ProviderProfileCommandResult, error)
}

type AgentModeReadRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
}

type AgentModeReadResult struct {
	Mode string
}

type AgentModeReader interface {
	CurrentAgentMode(ctx context.Context, request AgentModeReadRequest) (AgentModeReadResult, error)
}

type AgentRuntimeRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Transcript string
	CreatedAt  time.Time
}

type AgentRuntimeResult struct {
	Handled     bool
	Mode        string
	Destination string
	SkipReason  string
	Text        string
	ToolCalls   []providers.ToolCall
}

type AgentRuntimeRouter interface {
	RouteAgentTurn(ctx context.Context, request AgentRuntimeRequest) (AgentRuntimeResult, error)
}

type MemoryWriteRequest struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Transcript string
	CreatedAt  time.Time
}

type MemoryWriteResult struct {
	WrittenCount int
}

type MemoryWriter interface {
	WriteMemories(ctx context.Context, request MemoryWriteRequest) (MemoryWriteResult, error)
}

type ConversationTurnRecordRequest struct {
	SessionID     string
	DeviceID      string
	Generation    int64
	UserText      string
	AssistantText string
	CreatedAt     time.Time
}

type ConversationRecorder interface {
	RecordConversationTurn(ctx context.Context, request ConversationTurnRecordRequest) error
}

type staticVoiceProviderResolver struct {
	set VoiceProviderSet
}

func (r staticVoiceProviderResolver) ResolveVoiceProviders(_ context.Context, _ string) (VoiceProviderSet, error) {
	return r.set, nil
}

type VoiceLoop struct {
	session                        *Session
	providerResolver               VoiceProviderResolver
	providerObserver               VoiceProviderObserver
	llmContextBuilder              LLMContextBuilder
	agentModeCommands              AgentModeCommandHandler
	providerProfileCommands        ProviderProfileCommandHandler
	agentModeReader                AgentModeReader
	agentRuntime                   AgentRuntimeRouter
	memoryWriter                   MemoryWriter
	downlink                       Downlink
	pacer                          *audio.Pacer
	segmentMaxRunes                int
	trace                          observability.TraceRecorder
	metrics                        *observability.Metrics
	mcpBroker                      *mcp.Broker
	toolOrchestrator               ToolOrchestrator
	serviceTools                   *servicetools.Registry
	toolResultFollowUpPolicy       ToolResultFollowUpPolicy
	conversationRecorder           ConversationRecorder
	bodyScheduler                  *stackchan.BodyScheduler
	sceneComposer                  *stackchan.SceneComposer
	expressionPolicies             map[string]stackchan.ExpressionPolicy
	expressionSequences            map[string][]string
	expressionProviderToolsEnabled bool
	lifecycleExpressionCues        map[string]string
	eventExpressionCues            map[string]string
	displaySceneCtx                context.Context
	displaySceneCancel             context.CancelFunc
	displaySceneQueue              chan stackchan.Scene
	lifecycleLEDs                  map[string]stackchan.LEDCommand
	lifecycleLEDCtx                context.Context
	lifecycleLEDCancel             context.CancelFunc
	lifecycleLEDQueue              chan lifecycleLEDRequest
	listenStartMotionEnabled       bool
	autoListenTailTimeout          time.Duration
	postSpeakingIdleDelay          time.Duration

	mu                    sync.Mutex
	active                *TurnRuntime
	asrStream             providers.ASRStream
	listenGen             int64
	sessionActiveRecorded bool
	lastSpeakingCompleted time.Time
}

type lifecycleLEDRequest struct {
	turn      Turn
	lifecycle string
	command   stackchan.LEDCommand
}

type ToolResultFollowUpPolicy struct {
	Enabled        bool
	MaxResults     int
	MaxResultBytes int
	AllowedTools   []string
	AllowToolCalls bool
	MaxToolCalls   int
}

func NewVoiceLoop(options VoiceLoopOptions) (*VoiceLoop, error) {
	if options.Session == nil || options.Downlink == nil {
		return nil, ErrVoiceLoopNotConfigured
	}
	resolver := options.ProviderResolver
	if resolver == nil {
		if options.ASR == nil || options.LLM == nil || options.TTS == nil {
			return nil, ErrVoiceLoopNotConfigured
		}
		resolver = staticVoiceProviderResolver{set: normalizeVoiceProviderSet(VoiceProviderSet{
			Profile: providerName("static"),
			ASRName: providerName(options.ASRProviderName),
			LLMName: providerName(options.LLMProviderName),
			TTSName: providerName(options.TTSProviderName),
			ASR:     options.ASR,
			LLM:     options.LLM,
			TTS:     options.TTS,
		})}
	}
	pacer := options.Pacer
	if pacer == nil {
		pacer = audio.NewPacer(audio.PacerOptions{})
	}
	segmentMaxRunes := options.SegmentMaxRunes
	if segmentMaxRunes <= 0 {
		segmentMaxRunes = defaultSegmentMaxRunes
	}
	autoListenTailTimeoutValue := options.AutoListenTailTimeout
	if autoListenTailTimeoutValue <= 0 {
		autoListenTailTimeoutValue = autoListenTailTimeout
	}
	postSpeakingIdleDelayValue := options.PostSpeakingIdleDelay
	if postSpeakingIdleDelayValue <= 0 {
		postSpeakingIdleDelayValue = postSpeakingIdleDelay
	}
	lifecycleLEDs := cloneLifecycleLEDs(options.LifecycleLEDs)
	if lifecycleLEDs == nil {
		lifecycleLEDs = stackchan.DefaultLifecycleLEDs()
	}
	toolOrchestrator := options.ToolOrchestrator
	if toolOrchestrator == nil && (options.MCPBroker != nil || options.ServiceTools != nil) {
		toolOrchestrator = NewMCPToolOrchestrator(MCPToolOrchestratorOptions{
			Broker:              options.MCPBroker,
			ServiceTools:        options.ServiceTools,
			BodyScheduler:       options.BodyScheduler,
			SceneComposer:       options.SceneComposer,
			ExpressionPolicies:  options.ExpressionPolicies,
			ExpressionSequences: options.ExpressionSequences,
		})
	}

	loop := &VoiceLoop{
		session:                        options.Session,
		providerResolver:               resolver,
		providerObserver:               options.ProviderObserver,
		llmContextBuilder:              options.LLMContextBuilder,
		agentModeCommands:              options.AgentModeCommands,
		providerProfileCommands:        options.ProviderProfileCommands,
		agentModeReader:                options.AgentModeReader,
		agentRuntime:                   options.AgentRuntime,
		memoryWriter:                   options.MemoryWriter,
		downlink:                       options.Downlink,
		pacer:                          pacer,
		segmentMaxRunes:                segmentMaxRunes,
		trace:                          options.TraceRecorder,
		metrics:                        options.Metrics,
		mcpBroker:                      options.MCPBroker,
		toolOrchestrator:               toolOrchestrator,
		serviceTools:                   options.ServiceTools,
		toolResultFollowUpPolicy:       toolResultFollowUpPolicyWithDefaults(options.ToolResultFollowUpPolicy),
		conversationRecorder:           options.ConversationRecorder,
		bodyScheduler:                  options.BodyScheduler,
		sceneComposer:                  options.SceneComposer,
		expressionPolicies:             cloneExpressionPolicies(options.ExpressionPolicies),
		expressionSequences:            cloneExpressionSequences(options.ExpressionSequences),
		expressionProviderToolsEnabled: options.ExpressionProviderToolsEnabled,
		lifecycleExpressionCues:        cloneStringMapNormalized(options.LifecycleExpressionCues),
		eventExpressionCues:            cloneStringMapNormalized(options.EventExpressionCues),
		lifecycleLEDs:                  lifecycleLEDs,
		listenStartMotionEnabled:       options.ListenStartMotionEnabled,
		autoListenTailTimeout:          autoListenTailTimeoutValue,
		postSpeakingIdleDelay:          postSpeakingIdleDelayValue,
	}
	loop.startDisplaySceneDispatcher()
	loop.startLifecycleLEDDispatcher()
	return loop, nil
}

func normalizeVoiceProviderSet(set VoiceProviderSet) VoiceProviderSet {
	set.Profile = providerName(set.Profile)
	set.ASRName = providerName(set.ASRName)
	set.LLMName = providerName(set.LLMName)
	set.TTSName = providerName(set.TTSName)
	return set
}

func validateVoiceProviderSet(set VoiceProviderSet) error {
	if set.ASR == nil || set.LLM == nil || set.TTS == nil {
		return ErrVoiceLoopNotConfigured
	}
	return nil
}

func providerName(name string) string {
	if name == "" {
		return "unknown"
	}
	return name
}

func toolResultFollowUpPolicyWithDefaults(policy *ToolResultFollowUpPolicy) ToolResultFollowUpPolicy {
	if policy == nil {
		return ToolResultFollowUpPolicy{
			Enabled:        true,
			MaxResults:     defaultToolFollowUpMaxResults,
			MaxResultBytes: defaultToolFollowUpMaxResultBytes,
			MaxToolCalls:   defaultToolFollowUpMaxToolCalls,
		}
	}
	normalized := *policy
	if normalized.MaxResults <= 0 {
		normalized.MaxResults = defaultToolFollowUpMaxResults
	}
	if normalized.MaxResultBytes <= 0 {
		normalized.MaxResultBytes = defaultToolFollowUpMaxResultBytes
	}
	if normalized.MaxToolCalls <= 0 {
		normalized.MaxToolCalls = defaultToolFollowUpMaxToolCalls
	}
	normalized.AllowedTools = normalizedToolNames(normalized.AllowedTools)
	return normalized
}

func cloneStringMapNormalized(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[strings.ToLower(strings.TrimSpace(key))] = strings.ToLower(strings.TrimSpace(value))
	}
	return clone
}

func cloneLLMMessages(messages []providers.LLMMessage) []providers.LLMMessage {
	if len(messages) == 0 {
		return nil
	}
	clone := make([]providers.LLMMessage, len(messages))
	copy(clone, messages)
	return clone
}

func cloneLifecycleLEDs(values map[string]stackchan.LEDCommand) map[string]stackchan.LEDCommand {
	if values == nil {
		return nil
	}
	clone := make(map[string]stackchan.LEDCommand, len(values))
	for lifecycle, command := range values {
		lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
		if lifecycle == "" {
			continue
		}
		command = stackchan.ClampLED(command)
		command.Reason = strings.ToLower(strings.TrimSpace(command.Reason))
		if command.Reason == "" {
			command.Reason = stackchan.LifecycleLEDReason(lifecycle)
		}
		command.Priority = stackchan.PriorityNormal
		clone[lifecycle] = command
	}
	return clone
}

func traceErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, ErrASRStreamClosed) {
		return "asr_stream_closed"
	}
	return "voice_loop_error"
}

func (l *VoiceLoop) HandleHello(ctx context.Context) (Turn, error) {
	return l.HandleHelloWithFeatures(ctx, xiaozhi.DeviceFeatures{})
}

func (l *VoiceLoop) HandleHelloWithFeatures(ctx context.Context, features xiaozhi.DeviceFeatures) (Turn, error) {
	if err := l.session.ReceiveHello(); err != nil {
		return Turn{}, err
	}
	turn, err := l.session.ServerHelloSent()
	if err != nil {
		return Turn{}, err
	}
	l.recordTurnEvent(ctx, turn, "hello_received", "", "", nil)
	if err := l.downlink.SendJSON(ctx, xiaozhi.NewServerHello(turn.SessionID)); err != nil {
		return Turn{}, err
	}
	l.markSessionActive()
	if features.MCP && l.mcpBroker != nil {
		if err := l.mcpBroker.SendInitialize(ctx); err != nil {
			return Turn{}, err
		}
	}
	return turn, nil
}

func (l *VoiceLoop) Close(ctx context.Context) Turn {
	if ctx == nil {
		ctx = context.Background()
	}
	previous, previousStream := l.cancelActiveTurn("session_close")
	if previousStream != nil {
		_ = previousStream.Close()
	}
	l.recordTurnComplete(ctx, previous, "session_closed", false)
	cleanupTurn := l.session.CurrentTurn()
	l.dispatchLifecycleLEDForSessionClose(cleanupTurn, stackchan.SceneIdle)
	turn := l.session.Close()
	l.stopDisplaySceneDispatcher()
	l.stopLifecycleLEDDispatcher()
	l.markSessionInactive()
	return turn
}

func (l *VoiceLoop) State() State {
	if l == nil || l.session == nil {
		return StateClosed
	}
	return l.session.State()
}

func (l *VoiceLoop) HandleListenStart(ctx context.Context) (Turn, error) {
	isAutoListenTail := l.consumeRecentSpeakingCompletion()
	turn, err := l.session.StartListening()
	if err != nil {
		return Turn{}, err
	}

	runtime, previous, previousStream := l.replaceActiveTurn(ctx, turn, "listen_start")
	runtime.AutoListenTail = isAutoListenTail
	if previousStream != nil {
		_ = previousStream.Close()
	}
	if err := l.sendTTSStopOnce(ctx, previous); err != nil {
		l.failListenStartRuntime(ctx, runtime, "downlink_tts_stop_failed")
		return Turn{}, err
	}
	l.recordTurnComplete(ctx, previous, "barge_in", false)

	providerSet, err := l.providerResolver.ResolveVoiceProviders(ctx, turn.DeviceID)
	if err != nil {
		l.failListenStartRuntime(ctx, runtime, "provider_profile_resolve_failed")
		return Turn{}, err
	}
	providerSet = normalizeVoiceProviderSet(providerSet)
	if err := validateVoiceProviderSet(providerSet); err != nil {
		l.failListenStartRuntime(ctx, runtime, "provider_profile_invalid")
		return Turn{}, err
	}
	runtime.Providers = providerSet
	l.recordTurnEvent(ctx, turn, "listen_start", "", "", map[string]any{"mode": "auto", "provider_profile": providerSet.Profile})
	if l.metrics != nil {
		l.metrics.IncTurns()
	}

	if l.metrics != nil {
		l.metrics.IncProviderRequest("asr", runtime.Providers.ASRName)
	}
	stream, err := runtime.Providers.ASR.Start(runtime.Context, turn.ASRStartRequest(time.Now()))
	if err != nil {
		l.failListenStartRuntime(ctx, runtime, "asr_start_failed")
		return Turn{}, err
	}

	l.mu.Lock()
	l.asrStream = stream
	l.listenGen = turn.Generation
	l.mu.Unlock()
	l.startASRResultPump(runtime, stream)
	l.startAutoListenTailTimeout(runtime, stream)

	l.dispatchListenStartMotion(turn)
	l.dispatchLifecycleLED(turn, stackchan.SceneListening)
	l.dispatchLifecycleDisplayScene(turn, stackchan.SceneListening)
	l.dispatchLifecycleExpressionCue(turn, stackchan.SceneListening)
	return turn, nil
}

func (l *VoiceLoop) failListenStartRuntime(ctx context.Context, runtime *TurnRuntime, errorCode string) {
	if runtime == nil {
		return
	}
	l.clearRuntime(runtime)
	if resetTurn, didReset := l.session.ProviderFatalResetForGeneration(runtime.Turn.Generation); didReset {
		l.dispatchLifecycleLED(resetTurn, stackchan.SceneIdle)
		l.dispatchLifecycleDisplayScene(resetTurn, stackchan.SceneIdle)
		l.dispatchLifecycleExpressionCue(resetTurn, stackchan.SceneIdle)
	}
	l.recordTurnComplete(ctx, runtime, errorCode, true)
}

func (l *VoiceLoop) AcceptOpus(frame audio.Frame) error {
	l.mu.Lock()
	stream := l.asrStream
	l.mu.Unlock()

	if stream == nil {
		return ErrASRStreamNotStarted
	}
	if err := stream.AcceptOpus(frame); err != nil {
		return err
	}

	turn, shouldRecord := l.markFirstUplinkAudio(stream)
	if shouldRecord {
		l.recordTurnEvent(context.Background(), turn, "first_uplink_audio", "", "", map[string]any{
			"frame_duration_ms": frame.FrameDurationMS,
			"sample_rate_hz":    frame.SampleRateHz,
		})
	}
	return nil
}

func (l *VoiceLoop) HandleListenStop(ctx context.Context) error {
	turn, err := l.session.StopListening()
	if err != nil {
		return err
	}
	l.recordTurnEvent(ctx, turn, "listen_stop_received", "", "", nil)

	runtime, stream, err := l.currentRuntimeAndASRStream(turn.Generation)
	if err != nil {
		return err
	}
	if err := stream.Finish(); err != nil {
		l.recordTurnEvent(ctx, turn, "asr_finish_failed", runtime.Providers.ASRName, traceErrorCode(err), nil)
		return err
	}
	l.recordTurnEvent(ctx, turn, "asr_finish_sent", runtime.Providers.ASRName, "", nil)

	return l.waitForASRCompletion(runtime)
}

func (l *VoiceLoop) startASRResultPump(runtime *TurnRuntime, stream providers.ASRStream) {
	if runtime == nil || stream == nil {
		return
	}
	go l.runASRResultPump(runtime, stream)
}

func (l *VoiceLoop) startAutoListenTailTimeout(runtime *TurnRuntime, stream providers.ASRStream) {
	if runtime == nil || stream == nil || !runtime.AutoListenTail || l.autoListenTailTimeout <= 0 {
		return
	}
	go func() {
		timer := time.NewTimer(l.autoListenTailTimeout)
		defer timer.Stop()
		select {
		case <-runtime.Context.Done():
			return
		case <-timer.C:
			l.timeoutAutoListenTail(runtime, stream)
		}
	}()
}

func (l *VoiceLoop) timeoutAutoListenTail(runtime *TurnRuntime, stream providers.ASRStream) {
	l.mu.Lock()
	if l.active != runtime || l.asrStream != stream || l.listenGen != runtime.Turn.Generation || runtime.ASRFinalHandled {
		l.mu.Unlock()
		return
	}
	runtime.ASRFinalHandled = true
	runtime.Cancel("auto_listen_tail_timeout")
	l.active = nil
	l.asrStream = nil
	l.listenGen = 0
	l.mu.Unlock()

	_ = stream.Close()
	turn, err := l.session.CompleteListeningNoSpeech(runtime.Turn.Generation)
	if err == nil {
		l.dispatchLifecycleLED(turn, stackchan.SceneIdle)
		l.dispatchLifecycleDisplayScene(turn, stackchan.SceneIdle)
		l.dispatchLifecycleExpressionCue(turn, stackchan.SceneIdle)
	}
	l.recordTurnEvent(context.Background(), runtime.Turn, "asr_tail_listen_timeout", runtime.Providers.ASRName, "", nil)
	l.recordTurnComplete(context.Background(), runtime, "asr_tail_listen_timeout", false)
}

func (l *VoiceLoop) runASRResultPump(runtime *TurnRuntime, stream providers.ASRStream) {
	err := l.consumeASREvents(runtime, stream)
	l.completeASR(runtime, err)
}

func (l *VoiceLoop) consumeASREvents(runtime *TurnRuntime, stream providers.ASRStream) error {
	for {
		select {
		case <-runtime.Context.Done():
			return runtime.Context.Err()
		case event, ok := <-stream.Events():
			if !ok {
				err := ErrASRStreamClosed
				if runtime.Context.Err() != nil {
					return runtime.Context.Err()
				}
				l.recordTurnEvent(context.Background(), runtime.Turn, "asr_final_failed", runtime.Providers.ASRName, traceErrorCode(err), nil)
				l.recordTurnComplete(context.Background(), runtime, traceErrorCode(err), false)
				return err
			}
			if event.IsFinal {
				return l.handleASRFinal(runtime, stream, event)
			}
			if strings.TrimSpace(event.Text) != "" {
				l.recordTurnEvent(runtime.Context, runtime.Turn, "speech_partial", runtime.Providers.ASRName, "", map[string]any{
					"text_length": len([]rune(event.Text)),
				})
			}
		}
	}
}

func (l *VoiceLoop) handleASRFinal(runtime *TurnRuntime, stream providers.ASRStream, event providers.ASREvent) error {
	if !l.claimASRFinal(runtime) {
		return nil
	}
	l.clearASRStream(stream)
	turn, err := l.ensureProcessingForASRFinal(runtime)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, traceErrorCode(err), true)
		return err
	}
	runtime.Turn = turn
	l.markSpeechFinal(runtime, event.FinishedAt)
	l.recordTurnEvent(runtime.Context, turn, "speech_final", runtime.Providers.ASRName, "", map[string]any{
		"text_length": len([]rune(event.Text)),
	})

	if !turn.IsCurrent(l.session) {
		return nil
	}
	if err := l.downlink.SendJSON(runtime.Context, xiaozhi.NewSTT(turn.SessionID, event.Text)); err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "downlink_stt_failed", true)
		return err
	}
	l.dispatchLifecycleLED(turn, stackchan.SceneThinking)
	l.dispatchLifecycleDisplayScene(turn, stackchan.SceneThinking)
	l.dispatchLifecycleExpressionCue(turn, stackchan.SceneThinking)
	handledModeCommand, err := l.handleAgentModeCommand(runtime, event.Text)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "agent_mode_command_failed", true)
		return err
	}
	if handledModeCommand {
		return nil
	}
	handledProviderProfileCommand, err := l.handleProviderProfileCommand(runtime, event.Text)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "provider_profile_command_failed", true)
		return err
	}
	if handledProviderProfileCommand {
		return nil
	}
	handledAgentRoute, err := l.handleAgentRuntimeRoute(runtime, event.Text)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "agent_route_failed", true)
		return err
	}
	if handledAgentRoute {
		return nil
	}
	if err := l.streamLLMAndTTS(runtime, event.Text); err != nil {
		l.recordTurnComplete(runtime.Context, runtime, traceErrorCode(err), true)
		return err
	}
	return nil
}

func (l *VoiceLoop) claimASRFinal(runtime *TurnRuntime) bool {
	if runtime == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if runtime.ASRFinalHandled {
		return false
	}
	runtime.ASRFinalHandled = true
	return true
}

func (l *VoiceLoop) ensureProcessingForASRFinal(runtime *TurnRuntime) (Turn, error) {
	turn := runtime.Turn
	if !turn.IsCurrent(l.session) {
		return turn, nil
	}
	switch l.session.State() {
	case StateListening:
		return l.session.StopListening()
	case StateProcessing, StateSpeaking:
		return l.session.CurrentTurn(), nil
	default:
		return turn, nil
	}
}

func (l *VoiceLoop) completeASR(runtime *TurnRuntime, err error) {
	if runtime == nil || runtime.ASRCompletion == nil {
		return
	}
	select {
	case runtime.ASRCompletion <- err:
	default:
	}
}

func (l *VoiceLoop) waitForASRCompletion(runtime *TurnRuntime) error {
	if runtime == nil || runtime.ASRCompletion == nil {
		return nil
	}
	select {
	case err := <-runtime.ASRCompletion:
		return err
	case <-runtime.Context.Done():
		return runtime.Context.Err()
	}
}

func (l *VoiceLoop) HandleAbort(ctx context.Context) (Turn, error) {
	turn := l.session.Abort()
	previous, previousStream := l.cancelActiveTurn("abort")
	if previousStream != nil {
		_ = previousStream.Close()
	}
	if previous != nil {
		l.recordTurnEvent(ctx, previous.Turn, "abort_received", "", "", map[string]any{
			"old_generation": previous.Turn.Generation,
			"new_generation": turn.Generation,
		})
	} else {
		l.recordTurnEvent(ctx, turn, "abort_received", "", "", map[string]any{
			"new_generation": turn.Generation,
		})
	}
	if err := l.sendTTSStopOnce(ctx, previous); err != nil {
		return turn, err
	}
	l.dispatchLifecycleLED(turn, stackchan.SceneIdle)
	l.recordTurnComplete(ctx, previous, "aborted", false)
	return turn, nil
}

func (l *VoiceLoop) HandleMCPPayload(payload json.RawMessage) error {
	if l.mcpBroker == nil {
		return nil
	}
	return l.mcpBroker.HandleDevicePayload(payload)
}

func (l *VoiceLoop) dispatchListenStartMotion(turn Turn) {
	if !l.listenStartMotionEnabled || l.mcpBroker == nil || l.bodyScheduler == nil {
		return
	}
	l.bodyScheduler.SetGeneration(turn.Generation)
	command := turn.StackChanMotionCommand(0, 8, 0, stackchan.PriorityNormal, "listen_start")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := l.bodyScheduler.DispatchMotion(ctx, l.mcpBroker, command)
		l.recordStackChanBodyDispatch(ctx, turn, "motion", command.Reason, err)
	}()
}

func (l *VoiceLoop) dispatchLifecycleLED(turn Turn, lifecycle string) {
	if l.lifecycleLEDQueue == nil || len(l.lifecycleLEDs) == 0 {
		return
	}
	lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
	command, ok := l.lifecycleLEDs[lifecycle]
	if !ok {
		return
	}
	command.Generation = turn.Generation
	command.Priority = stackchan.PriorityNormal
	if strings.TrimSpace(command.Reason) == "" {
		command.Reason = stackchan.LifecycleLEDReason(lifecycle)
	}
	request := lifecycleLEDRequest{
		turn:      turn,
		lifecycle: lifecycle,
		command:   command,
	}
	select {
	case l.lifecycleLEDQueue <- request:
	case <-l.lifecycleLEDCtx.Done():
	default:
		l.recordStackChanBodyDispatch(context.Background(), turn, "led", command.Reason, errLifecycleLEDQueueFull)
	}
}

func (l *VoiceLoop) dispatchLifecycleDisplayScene(turn Turn, sceneName string) {
	if l.mcpBroker == nil || l.sceneComposer == nil || l.displaySceneQueue == nil || l.displaySceneCtx == nil {
		return
	}
	scene := l.sceneComposer.ComposeLifecycle(stackchan.SceneRequest{
		SessionID:  turn.SessionID,
		Generation: turn.Generation,
		Scene:      sceneName,
	})
	select {
	case l.displaySceneQueue <- scene:
	case <-l.displaySceneCtx.Done():
	default:
	}
}

func (l *VoiceLoop) dispatchEventDisplayScene(turn Turn, event string) {
	l.dispatchEventExpressionCue(turn, event)
	if l.mcpBroker == nil || l.sceneComposer == nil || l.displaySceneQueue == nil || l.displaySceneCtx == nil {
		return
	}
	scene, ok := l.sceneComposer.ComposeEvent(event, stackchan.SceneRequest{
		SessionID:  turn.SessionID,
		Generation: turn.Generation,
	})
	if !ok {
		return
	}
	select {
	case l.displaySceneQueue <- scene:
	case <-l.displaySceneCtx.Done():
	default:
	}
}

func (l *VoiceLoop) dispatchEventExpressionCue(turn Turn, event string) {
	if l.mcpBroker == nil || len(l.eventExpressionCues) == 0 {
		return
	}
	cue := l.eventExpressionCues[strings.ToLower(strings.TrimSpace(event))]
	if strings.TrimSpace(cue) == "" {
		return
	}
	plan, err := stackchan.ExpressionForCueWithPolicies(cue, l.expressionPolicies)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		dispatched, err := dispatchExpressionPlan(ctx, l.mcpBroker, l.bodyScheduler, l.sceneComposer, turn, plan, false)
		l.recordStackChanExpressionCueDispatch(ctx, turn, "event", event, plan, dispatched, err)
	}()
}

func (l *VoiceLoop) dispatchLifecycleExpressionCue(turn Turn, lifecycle string) {
	if l.mcpBroker == nil || len(l.lifecycleExpressionCues) == 0 {
		return
	}
	cue := l.lifecycleExpressionCues[strings.ToLower(strings.TrimSpace(lifecycle))]
	if strings.TrimSpace(cue) == "" {
		return
	}
	plan, err := stackchan.ExpressionForCueWithPolicies(cue, l.expressionPolicies)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		dispatched, err := dispatchExpressionPlan(ctx, l.mcpBroker, l.bodyScheduler, l.sceneComposer, turn, plan, false)
		l.recordStackChanExpressionCueDispatch(ctx, turn, "lifecycle", lifecycle, plan, dispatched, err)
	}()
}

func (l *VoiceLoop) recordStackChanBodyDispatch(ctx context.Context, turn Turn, channel string, reason string, err error) {
	result, errorCode := stackchanDispatchResult(err)
	l.recordTurnEvent(ctx, turn, traceEventStackChanBodyDispatch, "", errorCode, map[string]any{
		"channel": strings.ToLower(strings.TrimSpace(channel)),
		"reason":  strings.ToLower(strings.TrimSpace(reason)),
		"result":  result,
	})
}

func (l *VoiceLoop) recordStackChanExpressionCueDispatch(ctx context.Context, turn Turn, scope string, trigger string, plan stackchan.ExpressionPlan, dispatched int, err error) {
	result, errorCode := stackchanDispatchResult(err)
	l.recordTurnEvent(ctx, turn, traceEventStackChanExpressionCueDispatch, "", errorCode, map[string]any{
		"scope":            strings.ToLower(strings.TrimSpace(scope)),
		"trigger":          strings.ToLower(strings.TrimSpace(trigger)),
		"cue":              strings.ToLower(strings.TrimSpace(plan.Cue)),
		"result":           result,
		"dispatched_count": dispatched,
		"has_motion":       plan.Motion != nil,
		"has_led":          plan.LED != nil,
		"has_scene":        strings.TrimSpace(plan.Scene.Scene) != "",
	})
}

func stackchanDispatchResult(err error) (string, string) {
	if err == nil {
		return "sent", ""
	}
	if isSkippedToolCallError(err) {
		return "skipped", safeToolCallErrorCode(err)
	}
	return "failed", safeToolCallErrorCode(err)
}

func (l *VoiceLoop) startDisplaySceneDispatcher() {
	if l.mcpBroker == nil || l.sceneComposer == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.displaySceneCtx = ctx
	l.displaySceneCancel = cancel
	l.displaySceneQueue = make(chan stackchan.Scene, displaySceneQueueSize)
	go l.runDisplaySceneDispatcher(ctx)
}

func (l *VoiceLoop) stopDisplaySceneDispatcher() {
	if l.displaySceneCancel != nil {
		l.displaySceneCancel()
	}
}

func (l *VoiceLoop) runDisplaySceneDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case scene := <-l.displaySceneQueue:
			l.sendDisplayScene(ctx, scene)
		}
	}
}

func (l *VoiceLoop) sendDisplayScene(ctx context.Context, scene stackchan.Scene) {
	if l.mcpBroker == nil {
		return
	}
	if !l.mcpBroker.AllowsTool(mcp.ToolSetScreenScene) || !l.mcpBroker.HasDiscoveredTool(mcp.ToolSetScreenScene) {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, _ = l.mcpBroker.CallTool(callCtx, mcp.ToolSetScreenScene, scene.MCPArguments())
}

func (l *VoiceLoop) startLifecycleLEDDispatcher() {
	if l.mcpBroker == nil || l.bodyScheduler == nil || len(l.lifecycleLEDs) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.lifecycleLEDCtx = ctx
	l.lifecycleLEDCancel = cancel
	l.lifecycleLEDQueue = make(chan lifecycleLEDRequest, lifecycleLEDQueueSize)
	go l.runLifecycleLEDDispatcher(ctx)
}

func (l *VoiceLoop) stopLifecycleLEDDispatcher() {
	if l.lifecycleLEDCancel != nil {
		l.lifecycleLEDCancel()
	}
}

func (l *VoiceLoop) runLifecycleLEDDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-l.lifecycleLEDQueue:
			l.sendLifecycleLED(ctx, request)
		}
	}
}

func (l *VoiceLoop) sendLifecycleLED(ctx context.Context, request lifecycleLEDRequest) {
	if l.mcpBroker == nil || l.bodyScheduler == nil {
		return
	}
	if !l.mcpBroker.AllowsTool(mcp.ToolSetLEDColor) || !l.mcpBroker.HasDiscoveredTool(mcp.ToolSetLEDColor) {
		return
	}
	if !l.lifecycleLEDRequestIsCurrent(request) {
		return
	}
	command := stackchan.ClampLED(request.command)
	command.Generation = request.turn.Generation
	command.Priority = stackchan.PriorityNormal
	if strings.TrimSpace(command.Reason) == "" {
		command.Reason = stackchan.LifecycleLEDReason(request.lifecycle)
	}
	l.bodyScheduler.SetGeneration(request.turn.Generation)
	callCtx, cancel := context.WithTimeout(ctx, lifecycleLEDDispatchTimeout)
	defer cancel()
	_, err := l.bodyScheduler.DispatchLED(callCtx, l.mcpBroker, command)
	l.recordStackChanBodyDispatch(context.Background(), request.turn, "led", command.Reason, err)
}

func (l *VoiceLoop) lifecycleLEDRequestIsCurrent(request lifecycleLEDRequest) bool {
	if l == nil || l.session == nil {
		return false
	}
	if !request.turn.IsCurrent(l.session) {
		return false
	}
	lifecycle := strings.ToLower(strings.TrimSpace(request.lifecycle))
	if lifecycle == stackchan.SceneIdle {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active != nil &&
		l.active.Turn.SessionID == request.turn.SessionID &&
		l.active.Turn.Generation == request.turn.Generation
}

func (l *VoiceLoop) dispatchLifecycleLEDForSessionClose(turn Turn, lifecycle string) {
	if l.mcpBroker == nil || l.bodyScheduler == nil || len(l.lifecycleLEDs) == 0 {
		return
	}
	if !l.mcpBroker.AllowsTool(mcp.ToolSetLEDColor) || !l.mcpBroker.HasDiscoveredTool(mcp.ToolSetLEDColor) {
		return
	}
	lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
	command, ok := l.lifecycleLEDs[lifecycle]
	if !ok {
		return
	}
	command.Generation = turn.Generation
	command.Priority = stackchan.PriorityNormal
	if strings.TrimSpace(command.Reason) == "" {
		command.Reason = stackchan.LifecycleLEDReason(lifecycle)
	}
	command = stackchan.ClampLED(command)
	callCtx, cancel := context.WithTimeout(context.Background(), lifecycleLEDDispatchTimeout)
	defer cancel()
	_, err := l.mcpBroker.CallTool(callCtx, mcp.ToolSetLEDColor, command.MCPArguments())
	l.recordStackChanBodyDispatch(context.Background(), turn, "led", command.Reason, err)
}

func (l *VoiceLoop) replaceActiveTurn(ctx context.Context, turn Turn, reason string) (*TurnRuntime, *TurnRuntime, providers.ASRStream) {
	runtime := NewTurnRuntime(ctx, turn)
	runtime.ProviderRequestIDs.ASR = turn.ProviderRequestID("asr")

	l.mu.Lock()
	previous := l.active
	previousStream := l.asrStream
	if previous != nil {
		previous.Cancel(reason)
	}
	l.active = runtime
	l.asrStream = nil
	l.listenGen = 0
	l.mu.Unlock()

	return runtime, previous, previousStream
}

func (l *VoiceLoop) setProviderRequestID(runtime *TurnRuntime, provider string) {
	if runtime == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	switch provider {
	case "llm":
		runtime.ProviderRequestIDs.LLM = runtime.Turn.ProviderRequestID("llm")
	case "tts":
		runtime.ProviderRequestIDs.TTS = runtime.Turn.ProviderRequestID("tts")
	}
}

func (l *VoiceLoop) cancelActiveTurn(reason string) (*TurnRuntime, providers.ASRStream) {
	l.mu.Lock()
	previous := l.active
	previousStream := l.asrStream
	if previous != nil {
		previous.Cancel(reason)
	}
	l.active = nil
	l.asrStream = nil
	l.listenGen = 0
	l.mu.Unlock()

	return previous, previousStream
}

func (l *VoiceLoop) clearRuntime(runtime *TurnRuntime) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.active == runtime {
		l.active = nil
	}
}

func (l *VoiceLoop) currentRuntimeAndASRStream(generation int64) (*TurnRuntime, providers.ASRStream, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.active == nil || l.active.Turn.Generation != generation || l.asrStream == nil || l.listenGen != generation {
		return nil, nil, ErrASRStreamNotStarted
	}
	return l.active, l.asrStream, nil
}

func (l *VoiceLoop) clearASRStream(stream providers.ASRStream) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.asrStream == stream {
		l.asrStream = nil
		l.listenGen = 0
	}
}

func (l *VoiceLoop) markSessionActive() {
	l.mu.Lock()
	shouldIncrement := !l.sessionActiveRecorded
	if shouldIncrement {
		l.sessionActiveRecorded = true
	}
	l.mu.Unlock()

	if shouldIncrement && l.metrics != nil {
		l.metrics.IncSessionsActive()
	}
}

func (l *VoiceLoop) markSessionInactive() {
	l.mu.Lock()
	shouldDecrement := l.sessionActiveRecorded
	if shouldDecrement {
		l.sessionActiveRecorded = false
	}
	l.mu.Unlock()

	if shouldDecrement && l.metrics != nil {
		l.metrics.DecSessionsActive()
	}
}

func (l *VoiceLoop) markSpeakingCompleted() {
	l.mu.Lock()
	l.lastSpeakingCompleted = time.Now()
	l.mu.Unlock()
}

func (l *VoiceLoop) consumeRecentSpeakingCompletion() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastSpeakingCompleted.IsZero() {
		return false
	}
	if time.Since(l.lastSpeakingCompleted) > autoListenTailWindow {
		return false
	}
	l.lastSpeakingCompleted = time.Time{}
	return true
}

func (l *VoiceLoop) markTTSStarted(runtime *TurnRuntime) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if runtime != nil {
		runtime.TTSStarted = true
	}
}

func (l *VoiceLoop) sendTTSStopOnce(ctx context.Context, runtime *TurnRuntime) error {
	if runtime == nil {
		return nil
	}

	l.mu.Lock()
	shouldSend := runtime.TTSStarted && !runtime.TTSStopSent
	canceledAt := runtime.CanceledAt
	reason := runtime.DownlinkDrainMarker.Reason
	if shouldSend {
		runtime.TTSStopSent = true
	}
	l.mu.Unlock()

	if !shouldSend {
		return nil
	}
	if err := l.downlink.SendJSON(ctx, xiaozhi.NewTTSStop(runtime.Turn.SessionID)); err != nil {
		return err
	}
	fields := map[string]any{
		"reason": reason,
	}
	if !canceledAt.IsZero() {
		stopLatency := time.Since(canceledAt)
		stopLatencyMS := int(stopLatency.Milliseconds())
		if stopLatency > 0 && stopLatencyMS == 0 {
			stopLatencyMS = 1
		}
		fields["stop_latency_ms"] = stopLatencyMS
		if l.metrics != nil {
			l.metrics.ObserveBargeInStop(reason, stopLatency)
		}
	}
	l.recordTurnEvent(ctx, runtime.Turn, "tts_stop_sent", runtime.Providers.TTSName, "", fields)
	return nil
}

func (l *VoiceLoop) markFirstUplinkAudio(stream providers.ASRStream) (Turn, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.active == nil || l.asrStream != stream || l.active.FirstUplinkAudio {
		return Turn{}, false
	}
	l.active.FirstUplinkAudio = true
	return l.active.Turn, true
}

func (l *VoiceLoop) markSpeechFinal(runtime *TurnRuntime, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if runtime != nil {
		runtime.SpeechFinalAt = at
	}
}

func (l *VoiceLoop) markFirstLLMToken(runtime *TurnRuntime) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if runtime == nil || runtime.FirstLLMToken {
		return false
	}
	runtime.FirstLLMToken = true
	return true
}

func (l *VoiceLoop) markFirstTTSAudio(runtime *TurnRuntime) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if runtime == nil || runtime.FirstTTSAudio {
		return false
	}
	runtime.FirstTTSAudio = true
	return true
}

func (l *VoiceLoop) markFirstDownlinkAudio(runtime *TurnRuntime) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if runtime == nil || runtime.FirstDownlinkAudio {
		return time.Time{}, false
	}
	runtime.FirstDownlinkAudio = true
	return runtime.SpeechFinalAt, true
}

func (l *VoiceLoop) markFirstAudibleLatency(runtime *TurnRuntime, latency time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if runtime != nil && runtime.FirstAudibleLatencyMS == 0 {
		runtime.FirstAudibleLatencyMS = latency.Milliseconds()
	}
}

func (l *VoiceLoop) recordTurnComplete(ctx context.Context, runtime *TurnRuntime, errorCode string, countError bool) {
	if runtime == nil {
		return
	}

	l.mu.Lock()
	if runtime.CompletionRecorded {
		l.mu.Unlock()
		return
	}
	runtime.CompletionRecorded = true
	turn := runtime.Turn
	reason := runtime.DownlinkDrainMarker.Reason
	outcome := VoiceProviderOutcome{
		DeviceID:              turn.DeviceID,
		Profile:               runtime.Providers.Profile,
		Generation:            turn.Generation,
		Error:                 countError && errorCode != "",
		ErrorCode:             errorCode,
		FirstAudibleLatencyMS: runtime.FirstAudibleLatencyMS,
	}
	l.mu.Unlock()

	if countError && l.metrics != nil && errorCode != "" {
		l.metrics.IncTurnErrors(errorCode)
	}
	l.recordTurnEvent(ctx, turn, "turn_complete", "", errorCode, map[string]any{
		"reason": reason,
	})
	if l.providerObserver != nil && outcome.Profile != "" && outcome.Profile != "unknown" {
		l.providerObserver.ObserveVoiceTurn(ctx, outcome)
	}
}

func (l *VoiceLoop) recordTurnEvent(ctx context.Context, turn Turn, event string, provider string, errorCode string, fields map[string]any) {
	if l.trace == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = l.trace.Record(ctx, observability.TraceEvent{
		TraceID:    observability.TraceID(turn.SessionID, turn.Generation),
		SessionID:  turn.SessionID,
		DeviceID:   turn.DeviceID,
		Generation: turn.Generation,
		Event:      event,
		Provider:   provider,
		ErrorCode:  errorCode,
		Fields:     fields,
	})
}

func (l *VoiceLoop) streamLLMAndTTS(runtime *TurnRuntime, transcript string) error {
	turn := runtime.Turn
	l.setProviderRequestID(runtime, "llm")
	requestedAt := time.Now()
	agentMode := l.currentAgentMode(runtime)
	llmContext, err := l.buildLLMContext(runtime, transcript, requestedAt, agentMode)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "llm_context_failed", true)
		return err
	}
	llmRequest := turn.LLMRequest(llmContext.Text, requestedAt)
	llmRequest.Messages = cloneLLMMessages(llmContext.Messages)
	llmRequest.Tools, runtime.LLMToolNameAliases = l.llmToolDefinitions(agentMode)
	l.recordTurnEvent(runtime.Context, turn, "llm_request", runtime.Providers.LLMName, "", map[string]any{
		"request_id":         runtime.ProviderRequestIDs.LLM,
		"text_length":        len([]rune(transcript)),
		"prompt_text_length": len([]rune(llmContext.Text)),
		"memory_count":       llmContext.MemoryCount,
		"recent_turn_count":  llmContext.RecentTurnCount,
		"message_count":      len(llmRequest.Messages),
		"agent_mode":         agentMode,
		"tool_count":         len(llmRequest.Tools),
	})
	if l.metrics != nil {
		l.metrics.IncProviderRequest("llm", runtime.Providers.LLMName)
	}
	chunks, err := runtime.Providers.LLM.Stream(runtime.Context, llmRequest)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "llm_stream_failed", true)
		return err
	}

	segmenter := newTextSegmenter(l.segmentMaxRunes)
	ttsStarted := false
	spoke := false
	textSeen := false
	var followUpToolCalls []providers.ToolCall

	for {
		select {
		case <-runtime.Context.Done():
			l.recordTurnComplete(context.Background(), runtime, traceErrorCode(runtime.Context.Err()), false)
			return nil
		case chunk, ok := <-chunks:
			if !ok {
				if !textSeen && len(followUpToolCalls) > 0 {
					if err := l.runToolResultFollowUp(runtime, llmContext.Text, llmRequest.Tools, followUpToolCalls, segmenter, &ttsStarted, &spoke); err != nil {
						return err
					}
				} else if len(followUpToolCalls) > 0 {
					l.executeLLMToolCalls(runtime, followUpToolCalls)
				}
				return l.flushRemainingTTS(runtime, segmenter, ttsStarted, spoke, transcript)
			}
			if !turn.IsCurrent(l.session) {
				l.recordTurnComplete(context.Background(), runtime, "stale_generation", false)
				return nil
			}
			if l.markFirstLLMToken(runtime) {
				latency := time.Since(requestedAt)
				l.recordTurnEvent(runtime.Context, turn, "first_llm_token", runtime.Providers.LLMName, "", map[string]any{
					"elapsed_from_request_ms": latency.Milliseconds(),
				})
				if l.metrics != nil {
					l.metrics.ObserveProviderFirstToken(runtime.Providers.LLMName, latency)
				}
			}
			if strings.TrimSpace(chunk.Text) == "" {
				followUpToolCalls = append(followUpToolCalls, cloneProviderToolCalls(chunk.ToolCalls)...)
			} else {
				textSeen = true
				if len(followUpToolCalls) > 0 {
					l.executeLLMToolCalls(runtime, followUpToolCalls)
					followUpToolCalls = nil
				}
				l.executeLLMToolCalls(runtime, chunk.ToolCalls)
			}
			if err := l.speakLLMChunkText(runtime, segmenter, &ttsStarted, &spoke, chunk); err != nil {
				return err
			}
		}
	}
}

func (l *VoiceLoop) handleAgentModeCommand(runtime *TurnRuntime, transcript string) (bool, error) {
	if l.agentModeCommands == nil || runtime == nil {
		return false, nil
	}
	turn := runtime.Turn
	result, err := l.agentModeCommands.HandleAgentModeCommand(runtime.Context, AgentModeCommandRequest{
		SessionID:  turn.SessionID,
		DeviceID:   turn.DeviceID,
		Generation: turn.Generation,
		Transcript: transcript,
		CreatedAt:  time.Now(),
	})
	if err != nil || !result.Handled {
		return false, err
	}
	l.recordTurnEvent(runtime.Context, turn, "agent_mode_command", "", "", map[string]any{
		"action": strings.TrimSpace(result.Action),
		"mode":   strings.TrimSpace(result.Mode),
	})
	l.dispatchEventDisplayScene(turn, stackchan.DisplayEventAgentMode(result.Mode))
	return true, l.speakSystemText(runtime, result.SpokenText, "")
}

func (l *VoiceLoop) handleProviderProfileCommand(runtime *TurnRuntime, transcript string) (bool, error) {
	if l.providerProfileCommands == nil || runtime == nil {
		return false, nil
	}
	turn := runtime.Turn
	result, err := l.providerProfileCommands.HandleProviderProfileCommand(runtime.Context, ProviderProfileCommandRequest{
		SessionID:  turn.SessionID,
		DeviceID:   turn.DeviceID,
		Generation: turn.Generation,
		Transcript: transcript,
		CreatedAt:  time.Now(),
	})
	if err != nil || !result.Handled {
		return false, err
	}
	l.recordTurnEvent(runtime.Context, turn, "provider_profile_command", "", "", map[string]any{
		"action":  strings.TrimSpace(result.Action),
		"profile": strings.TrimSpace(result.Profile),
	})
	return true, l.speakSystemText(runtime, result.SpokenText, "")
}

func (l *VoiceLoop) handleAgentRuntimeRoute(runtime *TurnRuntime, transcript string) (bool, error) {
	if l.agentRuntime == nil || runtime == nil {
		return false, nil
	}
	turn := runtime.Turn
	result, err := l.agentRuntime.RouteAgentTurn(runtime.Context, AgentRuntimeRequest{
		SessionID:  turn.SessionID,
		DeviceID:   turn.DeviceID,
		Generation: turn.Generation,
		Transcript: transcript,
		CreatedAt:  time.Now(),
	})
	if err != nil {
		if runtime.Context.Err() != nil {
			return false, err
		}
		l.recordTurnEvent(runtime.Context, turn, "agent_route_error", "", "agent_route_failed", nil)
		return false, nil
	}
	if !result.Handled {
		result.SkipReason = strings.TrimSpace(result.SkipReason)
		if result.SkipReason != "" {
			l.recordTurnEvent(runtime.Context, turn, "agent_route_skipped", "", "", map[string]any{
				"reason":      result.SkipReason,
				"mode":        strings.TrimSpace(result.Mode),
				"destination": strings.TrimSpace(result.Destination),
			})
			l.dispatchEventDisplayScene(turn, stackchan.DisplayEventAgentRouteSkipped)
		}
		return false, nil
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" && len(result.ToolCalls) == 0 {
		return false, nil
	}
	l.recordTurnEvent(runtime.Context, turn, "agent_route", "", "", map[string]any{
		"mode":            strings.TrimSpace(result.Mode),
		"destination":     strings.TrimSpace(result.Destination),
		"text_length":     len([]rune(result.Text)),
		"tool_call_count": len(result.ToolCalls),
	})
	l.dispatchEventDisplayScene(turn, stackchan.DisplayEventAgentRoute(result.Destination))
	l.executeLLMToolCalls(runtime, result.ToolCalls)
	return true, l.speakSystemText(runtime, result.Text, transcript)
}

func (l *VoiceLoop) speakSystemText(runtime *TurnRuntime, text string, memoryTranscript string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "好的。"
	}
	segmenter := newTextSegmenter(l.segmentMaxRunes)
	ttsStarted := false
	spoke := false
	if err := l.speakLLMChunkText(runtime, segmenter, &ttsStarted, &spoke, providers.LLMChunk{
		Text:      text,
		IsFinal:   true,
		CreatedAt: time.Now(),
	}); err != nil {
		return err
	}
	return l.flushRemainingTTS(runtime, segmenter, ttsStarted, spoke, memoryTranscript)
}

func (l *VoiceLoop) speakLLMChunkText(runtime *TurnRuntime, segmenter *textSegmenter, ttsStarted *bool, spoke *bool, chunk providers.LLMChunk) error {
	if strings.TrimSpace(chunk.Text) == "" && !chunk.IsFinal {
		return nil
	}
	l.appendAssistantText(runtime, chunk.Text)
	segments := segmenter.Append(chunk.Text, chunk.IsFinal)
	for _, segment := range segments {
		if err := l.beginTTS(runtime, ttsStarted); err != nil {
			return err
		}
		sentAudio, err := l.streamTTSSegment(runtime, segment, !*spoke)
		if err != nil {
			return err
		}
		*spoke = *spoke || sentAudio
	}
	return nil
}

func (l *VoiceLoop) beginTTS(runtime *TurnRuntime, ttsStarted *bool) error {
	if runtime == nil || ttsStarted == nil || *ttsStarted {
		return nil
	}
	turn := runtime.Turn
	l.dispatchLifecycleLED(turn, stackchan.SceneSpeaking)
	l.dispatchLifecycleDisplayScene(turn, stackchan.SceneSpeaking)
	l.dispatchLifecycleExpressionCue(turn, stackchan.SceneSpeaking)
	if err := l.downlink.SendJSON(runtime.Context, xiaozhi.NewTTSStart(turn.SessionID)); err != nil {
		return err
	}
	l.markTTSStarted(runtime)
	l.pacer.Reset()
	*ttsStarted = true
	return nil
}

func (l *VoiceLoop) runToolResultFollowUp(runtime *TurnRuntime, basePrompt string, initialTools []providers.LLMTool, calls []providers.ToolCall, segmenter *textSegmenter, ttsStarted *bool, spoke *bool) error {
	outcomes := l.executeLLMToolCallsSync(runtime.Context, runtime, calls)
	if !l.toolResultFollowUpPolicy.Enabled {
		return nil
	}
	followUpText, resultCount, resultBytes := buildToolFollowUpPromptWithPolicy(basePrompt, outcomes, l.toolResultFollowUpPolicy)
	if strings.TrimSpace(followUpText) == "" {
		followUpText = buildToolOnlyRecoveryPrompt(basePrompt, outcomes)
		if strings.TrimSpace(followUpText) == "" {
			return nil
		}
		return l.runToolResultRecoveryAnswer(runtime, followUpText, len(outcomes), segmenter, ttsStarted, spoke)
	}

	turn := runtime.Turn
	requestedAt := time.Now()
	l.recordTurnEvent(runtime.Context, turn, "llm_tool_followup_request", runtime.Providers.LLMName, "", map[string]any{
		"tool_result_count":  resultCount,
		"tool_result_bytes":  resultBytes,
		"prompt_text_length": len([]rune(followUpText)),
		"allow_tool_calls":   l.toolResultFollowUpPolicy.AllowToolCalls,
	})
	if l.metrics != nil {
		l.metrics.IncProviderRequest("llm", runtime.Providers.LLMName)
	}
	followUpRequest := turn.LLMRequest(followUpText, requestedAt)
	followUpRequest.Tools = followUpToolDefinitions(initialTools, runtime.LLMToolNameAliases, l.toolResultFollowUpPolicy)
	chunks, err := runtime.Providers.LLM.Stream(runtime.Context, followUpRequest)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "llm_tool_followup_stream_failed", true)
		return err
	}

	var followUpToolCalls []providers.ToolCall
	textSeen := false
	for {
		select {
		case <-runtime.Context.Done():
			l.recordTurnComplete(context.Background(), runtime, traceErrorCode(runtime.Context.Err()), false)
			return nil
		case chunk, ok := <-chunks:
			if !ok {
				if !textSeen && len(followUpToolCalls) > 0 {
					nextOutcomes := l.executeToolFollowUpToolCallsSync(runtime.Context, runtime, followUpToolCalls)
					outcomes = append(outcomes, nextOutcomes...)
					return l.runToolResultFinalAnswer(runtime, basePrompt, outcomes, segmenter, ttsStarted, spoke)
				}
				return nil
			}
			if !turn.IsCurrent(l.session) {
				l.recordTurnComplete(context.Background(), runtime, "stale_generation", false)
				return nil
			}
			if len(chunk.ToolCalls) > 0 {
				if l.toolResultFollowUpPolicy.AllowToolCalls && !textSeen && strings.TrimSpace(chunk.Text) == "" {
					followUpToolCalls = append(followUpToolCalls, cloneProviderToolCalls(chunk.ToolCalls)...)
				} else {
					l.recordSuppressedFollowUpToolCalls(runtime, chunk.ToolCalls, "tool_followup_loop_suppressed")
				}
			}
			if strings.TrimSpace(chunk.Text) != "" {
				textSeen = true
			}
			if err := l.speakLLMChunkText(runtime, segmenter, ttsStarted, spoke, chunk); err != nil {
				return err
			}
		}
	}
}

func (l *VoiceLoop) runToolResultRecoveryAnswer(runtime *TurnRuntime, followUpText string, toolCallCount int, segmenter *textSegmenter, ttsStarted *bool, spoke *bool) error {
	turn := runtime.Turn
	requestedAt := time.Now()
	l.recordTurnEvent(runtime.Context, turn, "llm_tool_followup_no_result_request", runtime.Providers.LLMName, "", map[string]any{
		"tool_call_count":    toolCallCount,
		"prompt_text_length": len([]rune(followUpText)),
	})
	if l.metrics != nil {
		l.metrics.IncProviderRequest("llm", runtime.Providers.LLMName)
	}
	followUpRequest := turn.LLMRequest(followUpText, requestedAt)
	chunks, err := runtime.Providers.LLM.Stream(runtime.Context, followUpRequest)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "llm_tool_followup_no_result_stream_failed", true)
		return err
	}
	for {
		select {
		case <-runtime.Context.Done():
			l.recordTurnComplete(context.Background(), runtime, traceErrorCode(runtime.Context.Err()), false)
			return nil
		case chunk, ok := <-chunks:
			if !ok {
				return nil
			}
			if !turn.IsCurrent(l.session) {
				l.recordTurnComplete(context.Background(), runtime, "stale_generation", false)
				return nil
			}
			if len(chunk.ToolCalls) > 0 {
				l.recordSuppressedFollowUpToolCalls(runtime, chunk.ToolCalls, "tool_followup_no_result_loop_suppressed")
			}
			if err := l.speakLLMChunkText(runtime, segmenter, ttsStarted, spoke, providers.LLMChunk{
				Text:      chunk.Text,
				IsFinal:   chunk.IsFinal,
				CreatedAt: chunk.CreatedAt,
			}); err != nil {
				return err
			}
		}
	}
}

func (l *VoiceLoop) runToolResultFinalAnswer(runtime *TurnRuntime, basePrompt string, outcomes []ToolCallOutcome, segmenter *textSegmenter, ttsStarted *bool, spoke *bool) error {
	finalText, resultCount, resultBytes := buildToolFollowUpPromptWithPolicy(basePrompt, outcomes, l.toolResultFollowUpPolicy)
	if strings.TrimSpace(finalText) == "" {
		return nil
	}
	turn := runtime.Turn
	requestedAt := time.Now()
	l.recordTurnEvent(runtime.Context, turn, "llm_tool_followup_final_request", runtime.Providers.LLMName, "", map[string]any{
		"tool_result_count":  resultCount,
		"tool_result_bytes":  resultBytes,
		"prompt_text_length": len([]rune(finalText)),
	})
	if l.metrics != nil {
		l.metrics.IncProviderRequest("llm", runtime.Providers.LLMName)
	}
	finalRequest := turn.LLMRequest(finalText, requestedAt)
	chunks, err := runtime.Providers.LLM.Stream(runtime.Context, finalRequest)
	if err != nil {
		l.recordTurnComplete(runtime.Context, runtime, "llm_tool_followup_final_stream_failed", true)
		return err
	}
	for {
		select {
		case <-runtime.Context.Done():
			l.recordTurnComplete(context.Background(), runtime, traceErrorCode(runtime.Context.Err()), false)
			return nil
		case chunk, ok := <-chunks:
			if !ok {
				return nil
			}
			if !turn.IsCurrent(l.session) {
				l.recordTurnComplete(context.Background(), runtime, "stale_generation", false)
				return nil
			}
			if len(chunk.ToolCalls) > 0 {
				l.recordSuppressedFollowUpToolCalls(runtime, chunk.ToolCalls, "tool_followup_final_loop_suppressed")
			}
			if err := l.speakLLMChunkText(runtime, segmenter, ttsStarted, spoke, chunk); err != nil {
				return err
			}
		}
	}
}

func (l *VoiceLoop) executeToolFollowUpToolCallsSync(ctx context.Context, runtime *TurnRuntime, calls []providers.ToolCall) []ToolCallOutcome {
	if len(calls) == 0 || runtime == nil {
		return nil
	}
	policy := toolResultFollowUpPolicyWithDefaults(&l.toolResultFollowUpPolicy)
	allowedTools := followUpAllowedToolSet(policy.AllowedTools)
	resolvedCalls := resolveProviderToolAliases(runtime.LLMToolNameAliases, cloneProviderToolCalls(calls))
	executableCalls := make([]providers.ToolCall, 0, len(resolvedCalls))
	outcomes := make([]ToolCallOutcome, 0, len(resolvedCalls))
	for index, call := range resolvedCalls {
		name := strings.ToLower(strings.TrimSpace(call.Name))
		if len(allowedTools) == 0 {
			outcome := skippedToolFollowUpOutcome(index, call, "tool_followup_tool_not_allowed")
			outcomes = append(outcomes, outcome)
			l.recordLLMToolCall(runtime, outcome)
			continue
		}
		if _, ok := allowedTools[name]; !ok {
			outcome := skippedToolFollowUpOutcome(index, call, "tool_followup_tool_not_allowed")
			outcomes = append(outcomes, outcome)
			l.recordLLMToolCall(runtime, outcome)
			continue
		}
		if len(executableCalls) >= policy.MaxToolCalls {
			outcome := skippedToolFollowUpOutcome(index, call, "tool_followup_tool_limit_exceeded")
			outcomes = append(outcomes, outcome)
			l.recordLLMToolCall(runtime, outcome)
			continue
		}
		executableCalls = append(executableCalls, call)
	}
	outcomes = append(outcomes, l.executeLLMToolCallsSync(ctx, runtime, executableCalls)...)
	return outcomes
}

func (l *VoiceLoop) recordSuppressedFollowUpToolCalls(runtime *TurnRuntime, calls []providers.ToolCall, errorCode string) {
	if len(calls) == 0 || runtime == nil {
		return
	}
	suppressedCalls := resolveProviderToolAliases(runtime.LLMToolNameAliases, cloneProviderToolCalls(calls))
	for index, call := range suppressedCalls {
		outcome := skippedToolFollowUpOutcome(index, call, errorCode)
		l.recordLLMToolCall(runtime, outcome)
	}
}

func skippedToolFollowUpOutcome(index int, call providers.ToolCall, errorCode string) ToolCallOutcome {
	return ToolCallOutcome{
		Index:         index,
		Name:          strings.TrimSpace(call.Name),
		ArgumentCount: len(call.Arguments),
		Skipped:       true,
		ErrorCode:     errorCode,
	}
}

func followUpToolDefinitions(initialTools []providers.LLMTool, aliases map[string]string, policy ToolResultFollowUpPolicy) []providers.LLMTool {
	policy = toolResultFollowUpPolicyWithDefaults(&policy)
	allowedTools := followUpAllowedToolSet(policy.AllowedTools)
	if !policy.AllowToolCalls || policy.MaxToolCalls <= 0 || len(allowedTools) == 0 {
		return nil
	}
	definitions := make([]providers.LLMTool, 0, len(initialTools))
	seen := map[string]struct{}{}
	for _, tool := range initialTools {
		internalName := strings.ToLower(strings.TrimSpace(aliases[tool.Name]))
		if internalName == "" {
			internalName = strings.ToLower(strings.TrimSpace(tool.Name))
		}
		if _, ok := allowedTools[internalName]; !ok {
			continue
		}
		if _, ok := seen[internalName]; ok {
			continue
		}
		seen[internalName] = struct{}{}
		definitions = append(definitions, providers.LLMTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return definitions
}

func buildToolFollowUpPrompt(basePrompt string, outcomes []ToolCallOutcome) (string, int, int) {
	return buildToolFollowUpPromptWithPolicy(basePrompt, outcomes, toolResultFollowUpPolicyWithDefaults(nil))
}

func buildToolFollowUpPromptWithPolicy(basePrompt string, outcomes []ToolCallOutcome, policy ToolResultFollowUpPolicy) (string, int, int) {
	policy = toolResultFollowUpPolicyWithDefaults(&policy)
	if !policy.Enabled || policy.MaxResults <= 0 || policy.MaxResultBytes <= 0 {
		return "", 0, 0
	}
	var resultCount int
	var resultBytes int
	var results strings.Builder
	allowedTools := followUpAllowedToolSet(policy.AllowedTools)
	for _, outcome := range outcomes {
		if outcome.Skipped || outcome.ErrorCode != "" || len(outcome.Result) == 0 {
			continue
		}
		if len(allowedTools) > 0 {
			if _, ok := allowedTools[strings.ToLower(strings.TrimSpace(outcome.Name))]; !ok {
				continue
			}
		}
		if resultCount >= policy.MaxResults {
			break
		}
		remaining := policy.MaxResultBytes - resultBytes
		if remaining <= 0 {
			break
		}
		result := limitStringBytes(strings.TrimSpace(string(outcome.Result)), remaining)
		if result == "" {
			continue
		}
		results.WriteString("- ")
		results.WriteString(outcome.Name)
		results.WriteString(": ")
		results.WriteString(result)
		results.WriteByte('\n')
		resultCount++
		resultBytes += len([]byte(result))
	}
	if resultCount == 0 {
		return "", 0, 0
	}

	var prompt strings.Builder
	prompt.WriteString(strings.TrimSpace(basePrompt))
	prompt.WriteString("\n\n工具结果（仅用于回答用户，不要照抄 JSON）：\n")
	prompt.WriteString(results.String())
	prompt.WriteString("\n请基于这些工具结果，用一句简短中文回复用户。不要提到内部工具、JSON 或系统提示。")
	return prompt.String(), resultCount, resultBytes
}

func buildToolOnlyRecoveryPrompt(basePrompt string, outcomes []ToolCallOutcome) string {
	if len(outcomes) == 0 {
		return ""
	}
	var prompt strings.Builder
	prompt.WriteString(strings.TrimSpace(basePrompt))
	prompt.WriteString("\n\n刚才的工具调用没有产生适合直接语音播报的用户答案。")
	prompt.WriteString("不要再调用工具，不要提到内部工具、JSON 或系统提示。")
	prompt.WriteString("请回到用户原话，用一句简短中文直接回答；如果用户只是要求动作或状态切换，简短确认即可。")
	return prompt.String()
}

func followUpAllowedToolSet(tools []string) map[string]struct{} {
	if len(tools) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		tool = strings.ToLower(strings.TrimSpace(tool))
		if tool != "" {
			set[tool] = struct{}{}
		}
	}
	return set
}

func normalizedToolNames(tools []string) []string {
	if len(tools) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.ToLower(strings.TrimSpace(tool))
		if tool != "" {
			normalized = append(normalized, tool)
		}
	}
	return normalized
}

func limitStringBytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len([]byte(text)) <= maxBytes {
		return text
	}
	const suffix = "..."
	if maxBytes <= len(suffix) {
		return strings.Repeat(".", maxBytes)
	}
	limit := maxBytes - len(suffix)
	var used int
	for index, r := range text {
		width := len([]byte(string(r)))
		if used+width > limit {
			return strings.TrimSpace(text[:index]) + "..."
		}
		used += width
	}
	return text
}

func (l *VoiceLoop) buildLLMContext(runtime *TurnRuntime, transcript string, createdAt time.Time, agentMode string) (LLMContext, error) {
	if l.llmContextBuilder == nil {
		return LLMContext{Text: transcript}, nil
	}
	context, err := l.llmContextBuilder.BuildLLMContext(runtime.Context, LLMContextRequest{
		SessionID:  runtime.Turn.SessionID,
		DeviceID:   runtime.Turn.DeviceID,
		Generation: runtime.Turn.Generation,
		Transcript: transcript,
		CreatedAt:  createdAt,
		AgentMode:  agentMode,
	})
	if err != nil {
		return LLMContext{}, err
	}
	if strings.TrimSpace(context.Text) == "" {
		context.Text = transcript
	}
	return context, nil
}

func (l *VoiceLoop) currentAgentMode(runtime *TurnRuntime) string {
	if l == nil || l.agentModeReader == nil || runtime == nil {
		return ""
	}
	result, err := l.agentModeReader.CurrentAgentMode(runtime.Context, AgentModeReadRequest{
		SessionID:  runtime.Turn.SessionID,
		DeviceID:   runtime.Turn.DeviceID,
		Generation: runtime.Turn.Generation,
	})
	if err != nil {
		l.recordTurnEvent(runtime.Context, runtime.Turn, "agent_mode_read_failed", "", "agent_mode_read_failed", nil)
		return ""
	}
	return normalizeAgentMode(result.Mode)
}

func (l *VoiceLoop) llmToolDefinitions(agentMode string) ([]providers.LLMTool, map[string]string) {
	var internalDefinitions []providers.LLMTool
	agentMode = normalizeAgentMode(agentMode)
	if l.serviceTools != nil {
		for _, definition := range l.serviceTools.Definitions() {
			if strings.TrimSpace(definition.Name) == "" {
				continue
			}
			if !serviceToolVisibleForAgentMode(definition.Name, agentMode) {
				continue
			}
			internalDefinitions = append(internalDefinitions, providers.LLMTool{
				Name:        definition.Name,
				Description: definition.Description,
				InputSchema: cloneToolSchema(definition.InputSchema),
			})
		}
	}
	if l.expressionProviderToolsEnabled && l.mcpBroker != nil && hasStackChanExpressionTarget(l.mcpBroker.AllowedTools(), l.bodyScheduler != nil, l.sceneComposer != nil) {
		internalDefinitions = append(internalDefinitions, providers.LLMTool{
			Name:        stackchan.ToolExpress,
			Description: stackchan.ExpressionToolDescription(),
			InputSchema: stackchan.ExpressionToolInputSchema(),
		})
		internalDefinitions = append(internalDefinitions, providers.LLMTool{
			Name:        stackchan.ToolExpressionSequence,
			Description: stackchan.ExpressionSequenceToolDescription(),
			InputSchema: stackchan.ExpressionSequenceToolInputSchema(),
		})
		if sequenceIDs := expressionSequenceIDs(l.expressionSequences); len(sequenceIDs) > 0 {
			internalDefinitions = append(internalDefinitions, providers.LLMTool{
				Name:        stackchan.ToolPlayExpressionSequence,
				Description: stackchan.ExpressionSequencePresetToolDescription(),
				InputSchema: stackchan.ExpressionSequencePresetToolInputSchema(sequenceIDs),
			})
		}
	}
	if l.mcpBroker != nil && hasStackChanDisplayCardTarget(l.mcpBroker.AllowedTools(), l.sceneComposer) {
		internalDefinitions = append(internalDefinitions, providers.LLMTool{
			Name:        stackchan.ToolShowCard,
			Description: stackchan.DisplayCardToolDescription(),
			InputSchema: stackchan.DisplayCardToolInputSchema(l.sceneComposer.CardIDs()),
		})
	}
	if l.mcpBroker != nil {
		for _, tool := range l.mcpBroker.AllowedTools() {
			if strings.TrimSpace(tool.Name) == "" {
				continue
			}
			if !llmExposedMCPTool(tool.Name) {
				continue
			}
			internalDefinitions = append(internalDefinitions, providers.LLMTool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: cloneToolSchema(tool.InputSchema),
			})
		}
	}
	sort.SliceStable(internalDefinitions, func(i, j int) bool {
		return internalDefinitions[i].Name < internalDefinitions[j].Name
	})
	definitions := make([]providers.LLMTool, 0, len(internalDefinitions))
	aliases := make(map[string]string, len(internalDefinitions))
	for _, definition := range internalDefinitions {
		internalName := strings.TrimSpace(definition.Name)
		alias := llmProviderToolAlias(internalName, aliases)
		aliases[alias] = internalName
		definition.Name = alias
		definitions = append(definitions, definition)
	}
	return definitions, aliases
}

func llmExposedMCPTool(name string) bool {
	switch strings.TrimSpace(name) {
	case mcp.ToolTakePhoto, mcp.ToolDeviceStatus, mcp.ToolSetHeadAngles, mcp.ToolSetLEDColor, mcp.ToolSetScreenScene:
		return false
	default:
		return true
	}
}

func serviceToolVisibleForAgentMode(name string, agentMode string) bool {
	name = strings.TrimSpace(name)
	agentMode = normalizeAgentMode(agentMode)
	switch name {
	case v21VoiceQueryToolName:
		return agentMode == "professional"
	default:
		return true
	}
}

func normalizeAgentMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func hasStackChanExpressionTarget(tools []mcp.Tool, canUseBody bool, canUseScene bool) bool {
	for _, tool := range tools {
		switch strings.TrimSpace(tool.Name) {
		case mcp.ToolSetHeadAngles, mcp.ToolSetLEDColor:
			if canUseBody {
				return true
			}
		case mcp.ToolSetScreenScene:
			if canUseScene {
				return true
			}
		}
	}
	return false
}

func hasStackChanDisplayCardTarget(tools []mcp.Tool, sceneComposer *stackchan.SceneComposer) bool {
	if sceneComposer == nil || len(sceneComposer.CardIDs()) == 0 {
		return false
	}
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == mcp.ToolSetScreenScene {
			return true
		}
	}
	return false
}

func expressionSequenceIDs(sequences map[string][]string) []string {
	if len(sequences) == 0 {
		return nil
	}
	ids := make([]string, 0, len(sequences))
	for sequenceID := range sequences {
		sequenceID = normalizeExpressionSequenceID(sequenceID)
		if sequenceID != "" {
			ids = append(ids, sequenceID)
		}
	}
	sort.Strings(ids)
	return ids
}

func llmProviderToolAlias(internalName string, used map[string]string) string {
	alias := providerSafeToolName(internalName)
	if existing, ok := used[alias]; !ok || existing == internalName {
		return alias
	}

	hash := shortToolNameHash(internalName)
	base := sanitizeProviderToolName(internalName)
	suffix := "_" + hash
	maxBase := 64 - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "_-")
	}
	if base == "" {
		base = "tool"
	}
	alias = base + suffix
	for index := 2; ; index++ {
		if existing, ok := used[alias]; !ok || existing == internalName {
			return alias
		}
		extra := "_" + strconv.Itoa(index)
		maxBase = 64 - len(suffix) - len(extra)
		if maxBase < 1 {
			maxBase = 1
		}
		trimmedBase := base
		if len(trimmedBase) > maxBase {
			trimmedBase = strings.TrimRight(trimmedBase[:maxBase], "_-")
		}
		if trimmedBase == "" {
			trimmedBase = "tool"
		}
		alias = trimmedBase + suffix + extra
	}
}

func providerSafeToolName(internalName string) string {
	internalName = strings.TrimSpace(internalName)
	base := sanitizeProviderToolName(internalName)
	if base == "" {
		base = "tool"
	}
	if base == internalName && len(base) <= 64 {
		return base
	}
	suffix := "_" + shortToolNameHash(internalName)
	maxBase := 64 - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "_-")
	}
	if base == "" {
		base = "tool"
	}
	return base + suffix
}

func sanitizeProviderToolName(name string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if isProviderToolNameChar(r) {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	return strings.Trim(builder.String(), "_-")
}

func isProviderToolNameChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
}

func shortToolNameHash(name string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(name)))
	return hex.EncodeToString(sum[:])[:8]
}

func cloneToolSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	clone := make(map[string]any, len(schema))
	for key, value := range schema {
		clone[key] = cloneToolSchemaValue(value)
	}
	return clone
}

func cloneToolSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneToolSchema(typed)
	case []any:
		clone := make([]any, len(typed))
		for index, value := range typed {
			clone[index] = cloneToolSchemaValue(value)
		}
		return clone
	default:
		return typed
	}
}

func (l *VoiceLoop) flushRemainingTTS(runtime *TurnRuntime, segmenter *textSegmenter, ttsStarted bool, spoke bool, transcript string) error {
	if runtime.Context.Err() != nil || !runtime.Turn.IsCurrent(l.session) {
		l.recordTurnComplete(context.Background(), runtime, nonCurrentErrorCode(runtime), false)
		return nil
	}
	turn := runtime.Turn

	for _, segment := range segmenter.Flush() {
		if err := l.beginTTS(runtime, &ttsStarted); err != nil {
			return err
		}
		sentAudio, err := l.streamTTSSegment(runtime, segment, !spoke)
		if err != nil {
			return err
		}
		spoke = spoke || sentAudio
	}

	if runtime.Context.Err() != nil || !turn.IsCurrent(l.session) {
		l.recordTurnComplete(context.Background(), runtime, nonCurrentErrorCode(runtime), false)
		return nil
	}
	if ttsStarted {
		if err := l.sendTTSStopOnce(runtime.Context, runtime); err != nil {
			l.recordTurnComplete(runtime.Context, runtime, "downlink_tts_stop_failed", true)
			return err
		}
	}
	if spoke {
		completedTurn, err := l.session.CompleteSpeaking(turn.Generation)
		if err != nil {
			l.recordTurnComplete(runtime.Context, runtime, traceErrorCode(err), true)
			return err
		}
		l.markSpeakingCompleted()
		l.schedulePostSpeakingIdleLifecycle(completedTurn)
		l.clearRuntime(runtime)
		l.writeMemories(runtime, transcript)
		l.recordConversationTurn(runtime, transcript)
		l.recordTurnComplete(runtime.Context, runtime, "", false)
		return nil
	}
	l.dispatchLifecycleLED(turn, stackchan.SceneIdle)
	l.dispatchLifecycleDisplayScene(turn, stackchan.SceneIdle)
	l.dispatchLifecycleExpressionCue(turn, stackchan.SceneIdle)
	l.clearRuntime(runtime)
	l.writeMemories(runtime, transcript)
	l.recordConversationTurn(runtime, transcript)
	l.recordTurnComplete(runtime.Context, runtime, "", false)
	return nil
}

func (l *VoiceLoop) schedulePostSpeakingIdleLifecycle(turn Turn) {
	delay := l.postSpeakingIdleDelay
	if delay <= 0 {
		l.dispatchPostSpeakingIdleLifecycle(turn)
		return
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
		l.dispatchPostSpeakingIdleLifecycle(turn)
	}()
}

func (l *VoiceLoop) dispatchPostSpeakingIdleLifecycle(turn Turn) {
	if l == nil || l.session == nil || !turn.IsCurrent(l.session) || l.session.State() != StateIdle {
		return
	}
	l.dispatchLifecycleLED(turn, stackchan.SceneIdle)
	l.dispatchLifecycleDisplayScene(turn, stackchan.SceneIdle)
	l.dispatchLifecycleExpressionCue(turn, stackchan.SceneIdle)
}

func (l *VoiceLoop) writeMemories(runtime *TurnRuntime, transcript string) {
	if l.memoryWriter == nil || runtime == nil || strings.TrimSpace(transcript) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(runtime.Context, defaultMemoryWriteTimeout)
	defer cancel()
	result, err := l.memoryWriter.WriteMemories(ctx, MemoryWriteRequest{
		SessionID:  runtime.Turn.SessionID,
		DeviceID:   runtime.Turn.DeviceID,
		Generation: runtime.Turn.Generation,
		Transcript: transcript,
		CreatedAt:  time.Now(),
	})
	if err != nil {
		l.recordTurnEvent(context.Background(), runtime.Turn, "memory_write_failed", "", "memory_write_failed", nil)
		return
	}
	if result.WrittenCount > 0 {
		l.recordTurnEvent(runtime.Context, runtime.Turn, "memory_written", "", "", map[string]any{
			"written_count": result.WrittenCount,
		})
		l.dispatchEventDisplayScene(runtime.Turn, stackchan.DisplayEventMemoryUpdated)
	}
}

func (l *VoiceLoop) appendAssistantText(runtime *TurnRuntime, text string) {
	if runtime == nil || strings.TrimSpace(text) == "" {
		return
	}
	l.mu.Lock()
	runtime.AssistantText += text
	l.mu.Unlock()
}

func (l *VoiceLoop) assistantText(runtime *TurnRuntime) string {
	if runtime == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(runtime.AssistantText)
}

func (l *VoiceLoop) recordConversationTurn(runtime *TurnRuntime, transcript string) {
	if l.conversationRecorder == nil || runtime == nil {
		return
	}
	userText := strings.TrimSpace(transcript)
	assistantText := l.assistantText(runtime)
	if userText == "" || assistantText == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultConversationRecordTimeout)
	defer cancel()
	err := l.conversationRecorder.RecordConversationTurn(ctx, ConversationTurnRecordRequest{
		SessionID:     runtime.Turn.SessionID,
		DeviceID:      runtime.Turn.DeviceID,
		Generation:    runtime.Turn.Generation,
		UserText:      userText,
		AssistantText: assistantText,
		CreatedAt:     time.Now(),
	})
	if err != nil {
		l.recordTurnEvent(context.Background(), runtime.Turn, "recent_turn_record_failed", "", "recent_turn_record_failed", nil)
		return
	}
	l.recordTurnEvent(context.Background(), runtime.Turn, "recent_turn_recorded", "", "", map[string]any{
		"user_text_length":      len([]rune(userText)),
		"assistant_text_length": len([]rune(assistantText)),
	})
}

func nonCurrentErrorCode(runtime *TurnRuntime) string {
	if runtime == nil {
		return ""
	}
	if code := traceErrorCode(runtime.Context.Err()); code != "" {
		return code
	}
	return "stale_generation"
}

func (l *VoiceLoop) streamTTSSegment(runtime *TurnRuntime, text string, firstSegment bool) (bool, error) {
	turn := runtime.Turn
	if text == "" {
		return false, nil
	}
	if runtime.Context.Err() != nil || !turn.IsCurrent(l.session) {
		return false, nil
	}
	if err := l.downlink.SendJSON(runtime.Context, xiaozhi.NewTTSSentenceStart(turn.SessionID, text)); err != nil {
		return false, err
	}

	l.setProviderRequestID(runtime, "tts")
	requestedAt := time.Now()
	l.recordTurnEvent(runtime.Context, turn, "tts_request", runtime.Providers.TTSName, "", map[string]any{
		"request_id":  runtime.ProviderRequestIDs.TTS,
		"text_length": len([]rune(text)),
	})
	if l.metrics != nil {
		l.metrics.IncProviderRequest("tts", runtime.Providers.TTSName)
	}
	frames, err := runtime.Providers.TTS.Stream(runtime.Context, turn.TTSRequest(text, requestedAt))
	if err != nil {
		l.stopStartedTTSOnError(runtime)
		l.recordTurnComplete(runtime.Context, runtime, "tts_stream_failed", true)
		return false, err
	}

	sentAudio := false
	qualityAccumulator := audio.NewPCM16StatsAccumulator(audio.PCM16AnalysisOptions{})
	for {
		select {
		case <-runtime.Context.Done():
			return sentAudio, nil
		case frame, ok := <-frames:
			if !ok {
				l.recordTTSAudioQuality(runtime, qualityAccumulator.Snapshot())
				return sentAudio, nil
			}
			if frame.AudioQuality.HasSamples() {
				qualityAccumulator.Add(frame.AudioQuality)
			}
			if frame.Generation != turn.Generation || !turn.IsCurrent(l.session) {
				l.recordTurnComplete(context.Background(), runtime, "stale_generation", false)
				return sentAudio, nil
			}
			if l.markFirstTTSAudio(runtime) {
				latency := time.Since(requestedAt)
				l.recordTurnEvent(runtime.Context, turn, "first_tts_audio", runtime.Providers.TTSName, "", map[string]any{
					"elapsed_from_request_ms": latency.Milliseconds(),
				})
				if l.metrics != nil {
					l.metrics.ObserveProviderFirstAudio(runtime.Providers.TTSName, latency)
				}
			}
			if err := l.pacer.Wait(runtime.Context, frame.Duration); err != nil {
				return sentAudio, err
			}
			if frame.Generation != l.session.CurrentGeneration() || !turn.IsCurrent(l.session) {
				l.recordTurnComplete(context.Background(), runtime, "stale_generation", false)
				return sentAudio, nil
			}
			if !sentAudio && firstSegment {
				if _, err := l.session.BeginSpeaking(turn.Generation); err != nil {
					l.recordTurnComplete(runtime.Context, runtime, traceErrorCode(err), true)
					return sentAudio, err
				}
			}
			if frame.Generation != l.session.CurrentGeneration() || !turn.IsCurrent(l.session) {
				l.recordTurnComplete(context.Background(), runtime, "stale_generation", false)
				return sentAudio, nil
			}
			if err := l.downlink.SendBinary(runtime.Context, frame.Opus); err != nil {
				l.recordTurnComplete(runtime.Context, runtime, "downlink_audio_failed", true)
				return sentAudio, err
			}
			if speechFinalAt, first := l.markFirstDownlinkAudio(runtime); first {
				var firstAudibleLatency time.Duration
				if !speechFinalAt.IsZero() {
					firstAudibleLatency = time.Since(speechFinalAt)
					l.markFirstAudibleLatency(runtime, firstAudibleLatency)
				}
				l.recordTurnEvent(runtime.Context, turn, "first_downlink_audio_sent", runtime.Providers.TTSName, "", map[string]any{
					"frame_duration_ms": frame.Duration.Milliseconds(),
				})
				if l.metrics != nil && firstAudibleLatency > 0 {
					l.metrics.ObserveSpeechEndToFirstAudible(runtime.Providers.TTSName, firstAudibleLatency)
				}
			}
			sentAudio = true
		}
	}
}

func (l *VoiceLoop) recordTTSAudioQuality(runtime *TurnRuntime, stats audio.PCM16Stats) {
	if runtime == nil || !stats.HasSamples() {
		return
	}
	fields := stats.TraceFields()
	fields["request_id"] = runtime.ProviderRequestIDs.TTS
	l.recordTurnEvent(runtime.Context, runtime.Turn, "tts_audio_quality", runtime.Providers.TTSName, "", fields)
}

func (l *VoiceLoop) stopStartedTTSOnError(runtime *TurnRuntime) {
	if runtime == nil || !runtime.TTSStarted {
		return
	}
	_ = l.sendTTSStopOnce(runtime.Context, runtime)
	turn := runtime.Turn
	l.dispatchLifecycleLED(turn, stackchan.SceneIdle)
	l.dispatchLifecycleDisplayScene(turn, stackchan.SceneIdle)
	l.dispatchLifecycleExpressionCue(turn, stackchan.SceneIdle)
}

type textSegmenter struct {
	maxRunes int
	buffer   []rune
}

func newTextSegmenter(maxRunes int) *textSegmenter {
	if maxRunes <= 0 {
		maxRunes = defaultSegmentMaxRunes
	}
	return &textSegmenter{maxRunes: maxRunes}
}

func (s *textSegmenter) Append(text string, final bool) []string {
	var segments []string
	for _, r := range text {
		s.buffer = append(s.buffer, r)
		if shouldFlushSegment(s.buffer, s.maxRunes, r) {
			segments = append(segments, s.flushOne())
		}
	}
	if final {
		segments = append(segments, s.Flush()...)
	}
	return segments
}

func (s *textSegmenter) Flush() []string {
	if len(s.buffer) == 0 {
		return nil
	}
	return []string{s.flushOne()}
}

func (s *textSegmenter) flushOne() string {
	text := string(s.buffer)
	s.buffer = s.buffer[:0]
	return text
}

func isChinesePunctuation(r rune) bool {
	switch r {
	case '。', '，', '、', '！', '？', '；', '：', '.', ',', '!', '?', ';', ':':
		return true
	default:
		return false
	}
}

func shouldFlushSegment(buffer []rune, maxRunes int, last rune) bool {
	if len(buffer) >= maxRunes {
		return true
	}
	if !isChinesePunctuation(last) {
		return false
	}
	if isSoftChinesePunctuation(last) && len(buffer) < defaultSegmentMinSoftPunctuationRunes {
		return false
	}
	return true
}

func isSoftChinesePunctuation(r rune) bool {
	switch r {
	case '，', '、', ',':
		return true
	default:
		return false
	}
}

func New(id string, deviceID string, clientID string) *Session {
	return newWithFirstGeneration(id, deviceID, clientID, 1)
}

func newWithFirstGeneration(id string, deviceID string, clientID string, firstGeneration int64) *Session {
	if firstGeneration < 1 {
		firstGeneration = 1
	}
	return &Session{
		id:                     id,
		deviceID:               deviceID,
		clientID:               clientID,
		state:                  StateConnected,
		firstGenerationOnHello: firstGeneration,
	}
}

func (s *Session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

func (s *Session) DeviceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deviceID
}

func (s *Session) ClientID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientID
}

func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) CurrentGeneration() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation
}

func (s *Session) CurrentTurn() Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnLocked()
}

func (s *Session) ReceiveHello() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateConnected {
		return invalidTransition(s.state, "hello_received", "client hello is only valid after connection")
	}
	s.state = StateHelloReceived
	return nil
}

func (s *Session) ServerHelloSent() (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateHelloReceived {
		return Turn{}, invalidTransition(s.state, "server_hello_sent", "server hello requires client hello first")
	}
	s.generation = s.firstGenerationOnHello
	s.generationUsed = false
	s.state = StateIdle
	return s.turnLocked(), nil
}

func (s *Session) StartListening() (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.state {
	case StateIdle:
		if s.generationUsed {
			s.incrementGenerationLocked()
		}
		s.generationUsed = true
		s.state = StateListening
	case StateInterrupted:
		s.generationUsed = true
		s.state = StateListening
	case StateListening:
		return s.turnLocked(), nil
	case StateProcessing, StateSpeaking:
		s.incrementGenerationLocked()
		s.generationUsed = true
		s.state = StateListening
	default:
		return Turn{}, invalidTransition(s.state, "listen_start", "listen/start requires idle, interrupted, listening, processing, or speaking")
	}
	return s.turnLocked(), nil
}

func (s *Session) StopListening() (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateListening {
		return Turn{}, invalidTransition(s.state, "listen_stop", "listen/stop requires listening state")
	}
	s.state = StateProcessing
	return s.turnLocked(), nil
}

func (s *Session) BeginSpeaking(generation int64) (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateProcessing {
		return Turn{}, invalidTransition(s.state, "first_tts_frame", "speaking starts only while processing")
	}
	if generation != s.generation {
		return Turn{}, invalidTransition(s.state, "first_tts_frame", "generation is stale")
	}
	s.state = StateSpeaking
	return s.turnLocked(), nil
}

func (s *Session) CompleteSpeaking(generation int64) (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateSpeaking {
		return Turn{}, invalidTransition(s.state, "tts_complete", "tts complete requires speaking state")
	}
	if generation != s.generation {
		return Turn{}, invalidTransition(s.state, "tts_complete", "generation is stale")
	}
	s.state = StateIdle
	return s.turnLocked(), nil
}

func (s *Session) CompleteListeningNoSpeech(generation int64) (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateListening {
		return Turn{}, invalidTransition(s.state, "listen_timeout", "listen timeout requires listening state")
	}
	if generation != s.generation {
		return Turn{}, invalidTransition(s.state, "listen_timeout", "generation is stale")
	}
	s.state = StateIdle
	return s.turnLocked(), nil
}

func (s *Session) Abort() Turn {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateClosed {
		s.incrementGenerationLocked()
		s.state = StateInterrupted
	}
	return s.turnLocked()
}

func (s *Session) ProviderFatalReset() Turn {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateClosed {
		s.incrementGenerationLocked()
		s.state = StateInterrupted
	}
	return s.turnLocked()
}

func (s *Session) ProviderFatalResetForGeneration(generation int64) (Turn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StateClosed && s.generation == generation {
		s.incrementGenerationLocked()
		s.state = StateInterrupted
		return s.turnLocked(), true
	}
	return s.turnLocked(), false
}

func (s *Session) Close() Turn {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = StateClosed
	return s.turnLocked()
}

func (s *Session) AcceptsGeneration(generation int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state != StateClosed && generation == s.generation
}

func (s *Session) incrementGenerationLocked() {
	if s.generation < 1 {
		s.generation = s.firstGenerationOnHello
		s.generationUsed = false
		return
	}
	s.generation++
	s.generationUsed = false
}

func (s *Session) turnLocked() Turn {
	return Turn{
		SessionID:  s.id,
		DeviceID:   s.deviceID,
		Generation: s.generation,
		State:      s.state,
	}
}
