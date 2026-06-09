package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"stackchan-gateway/internal/agent"
	"stackchan-gateway/internal/agents"
	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/camera"
	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/feishu"
	"stackchan-gateway/internal/homeassistant"
	"stackchan-gateway/internal/httpapi"
	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/observability"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providerprobe"
	"stackchan-gateway/internal/providerrouter"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/reminder"
	"stackchan-gateway/internal/search"
	"stackchan-gateway/internal/session"
	"stackchan-gateway/internal/stackchan"
	servicetools "stackchan-gateway/internal/tools"
)

type Options struct {
	ConfigPath string
	Logger     *slog.Logger
}

type App struct {
	configPath         string
	config             *gatewayconfig.Config
	listenAddr         string
	logger             *slog.Logger
	server             *http.Server
	metrics            *observability.Metrics
	metricsServer      *http.Server
	adminServer        *http.Server
	sessions           *session.Manager
	voiceRuntime       *gatewayWebSocketRuntime
	traceRecorder      *observability.Recorder
	providerRouter     *providerrouter.Router
	agentMemory        agent.MemoryAdminRepository
	agentModes         *agents.ModeStore
	agentBridges       agents.BridgeCatalogReader
	agentRuntimeStatus agents.RuntimeStatusReader
	serviceTools       *servicetools.Registry
}

func New(options Options) (*App, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	cfg, err := gatewayconfig.LoadFile(options.ConfigPath, gatewayconfig.OSLookupEnv)
	if err != nil {
		return nil, err
	}

	listenAddr := cfg.Server.ListenAddr
	otaHandler, err := newGatewayXiaozhiOTAHandler(cfg, gatewayconfig.OSLookupEnv)
	if err != nil {
		return nil, err
	}
	metrics := observability.NewMetrics()
	tracePath := resolveGatewayRuntimePath(cfg.Observability.TraceJSONLPath)
	traceRecorder, err := observability.NewFileTraceRecorder(observability.FileTraceRecorderOptions{
		Path:          tracePath,
		RedactSecrets: cfg.Observability.RedactSecrets,
	})
	if err != nil {
		return nil, fmt.Errorf("trace recorder: %w", err)
	}
	llmContextBuilder, memoryWriter, conversationRecorder, agentMemory, recentTurnReader, err := newAgentPromptBuilder(cfg)
	if err != nil {
		return nil, err
	}
	agentModes := newAgentModeStore(cfg)
	agentBridges := newAgentBridgeCatalog(cfg)
	serviceTools, err := newAgentServiceTools(cfg, agentMemory, agentModes)
	if err != nil {
		if agentMemory != nil {
			_ = agentMemory.Close()
		}
		return nil, err
	}
	sessionManager := session.NewManager()
	runtimeRegistry := providerprobe.NewRegistryFromEnvWithMockConfig(gatewayconfig.OSLookupEnv, mockProviderConfig(cfg))
	runtimeProviderRouter := providerrouter.New(cfg, runtimeRegistry)
	agentModeCommands := newAgentModeCommandHandler(agentModes)
	providerProfileCommands := newProviderProfileCommandHandler(runtimeProviderRouter)
	agentRuntime := newAgentRuntimeRouter(cfg, agentModes)
	var agentRuntimePolicies agents.RuntimePolicyStatusReader
	if policies, ok := agentRuntime.(agents.RuntimePolicyStatusReader); ok {
		agentRuntimePolicies = policies
	}
	agentRuntimeStatus := newAgentRuntimeStatus(agentModes, agentBridges, agentRuntimePolicies)
	deviceModeHandler := newGatewayDeviceModeHandler(cfg, gatewayconfig.OSLookupEnv, agentModes, agentRuntimeStatus)
	agentModeReader := newAgentModeReader(agentModes)
	voiceRuntime := newGatewayWebSocketHandler(cfg, logger, metrics, traceRecorder, sessionManager, runtimeProviderRouter, llmContextBuilder, agentModeCommands, providerProfileCommands, agentModeReader, agentRuntime, memoryWriter, conversationRecorder, serviceTools)
	displaySceneCatalog := stackchan.NewDisplaySceneCatalog(stackchan.DisplaySceneCatalogOptions{
		Display: displayOptionsFromConfig(cfg.StackChan.Display),
		Devices: displaySceneDevicesFromConfig(cfg.Devices),
	})
	displayCardCatalog := stackchan.NewDisplayCardCatalog(stackchan.DisplayCardCatalogOptions{
		Display: displayOptionsFromConfig(cfg.StackChan.Display),
		Devices: displayCardDevicesFromConfig(cfg.Devices),
	})
	expressionCueCatalog := stackchan.NewExpressionCatalog(stackchan.ExpressionCatalogOptions{
		Policies:      expressionPoliciesFromConfig(cfg.StackChan.Expression.Cues),
		LifecycleCues: lifecycleExpressionCuesFromConfig(cfg.StackChan.Expression.LifecycleCues),
		EventCues:     eventExpressionCuesFromConfig(cfg.StackChan.Expression.EventCues),
		Devices:       expressionDevicesFromConfig(cfg.Devices),
	})
	expressionSequenceCatalog := stackchan.NewExpressionSequenceCatalog(stackchan.ExpressionSequenceCatalogOptions{
		Sequences: expressionSequencesFromConfig(cfg.StackChan.Expression.Sequences),
		Devices:   expressionDevicesFromConfig(cfg.Devices),
	})
	var metricsServer *http.Server
	if cfg.Server.MetricsAddr != "" {
		metricsServer = &http.Server{
			Addr:    cfg.Server.MetricsAddr,
			Handler: httpapi.NewMetricsRouter(metrics.Handler()),
		}
	}

	var adminServer *http.Server
	if cfg.Server.AdminAddr != "" {
		adminToken, _ := gatewayconfig.OSLookupEnv(cfg.Server.AdminTokenEnv)
		adminServer = &http.Server{
			Addr: cfg.Server.AdminAddr,
			Handler: httpapi.NewAdminRouter(httpapi.AdminRouterOptions{
				AdminToken:          adminToken,
				Prober:              providers.NewProbeRunner(runtimeRegistry),
				ProviderProfiles:    runtimeProviderRouter,
				AgentModes:          agentModes,
				AgentBridges:        agentBridges,
				AgentRuntimeStatus:  agentRuntimeStatus,
				Memories:            agentMemory,
				RecentTurns:         recentTurnReader,
				ServiceTools:        serviceTools,
				DisplayScenes:       displaySceneCatalog,
				DisplayCards:        displayCardCatalog,
				ExpressionCues:      expressionCueCatalog,
				ExpressionSequences: expressionSequenceCatalog,
			}),
		}
	}

	return &App{
		configPath:         options.ConfigPath,
		config:             cfg,
		listenAddr:         listenAddr,
		logger:             logger,
		metrics:            metrics,
		metricsServer:      metricsServer,
		adminServer:        adminServer,
		sessions:           sessionManager,
		voiceRuntime:       voiceRuntime,
		traceRecorder:      traceRecorder,
		providerRouter:     runtimeProviderRouter,
		agentMemory:        agentMemory,
		agentModes:         agentModes,
		agentBridges:       agentBridges,
		agentRuntimeStatus: agentRuntimeStatus,
		serviceTools:       serviceTools,
		server: &http.Server{
			Addr: listenAddr,
			Handler: httpapi.NewRouter(httpapi.RouterOptions{
				WebSocketPath:     cfg.Server.WebsocketPath,
				WebSocketHandler:  voiceRuntime,
				OTAPath:           cfg.Server.OTAPath,
				OTAHandler:        otaHandler,
				DeviceModeHandler: deviceModeHandler,
				ReadyCheck:        newGatewayReadinessCheck(cfg, runtimeRegistry),
			}),
		},
	}, nil
}

func mockProviderConfig(cfg *gatewayconfig.Config) providers.MockConfig {
	if cfg == nil {
		return providers.MockConfig{}
	}
	mock := cfg.Providers.Mock
	return providers.MockConfig{
		ASRFinalDelayMS:      mock.ASRFinalDelayMS,
		ASRAutoFinalOnAudio:  mock.ASRAutoFinalOnAudio,
		ASRFinalText:         mock.ASRFinalText,
		LLMFirstTokenDelayMS: mock.LLMFirstTokenDelayMS,
		TTSFirstFrameDelayMS: mock.TTSFirstFrameDelayMS,
		TTSFrameCount:        mock.TTSFrameCount,
	}
}

func (a *App) ListenAddr() string {
	return a.listenAddr
}

func (a *App) Config() *gatewayconfig.Config {
	return a.config
}

func AuthenticatorForConfig(cfg *gatewayconfig.Config) xiaozhi.AuthenticatorFunc {
	return func(_ context.Context, request xiaozhi.AuthRequest) (xiaozhi.AuthResult, error) {
		device, ok := cfg.DeviceByID(request.DeviceID)
		if !ok {
			return xiaozhi.AuthResult{}, xiaozhi.NewAuthError(http.StatusForbidden, "UNKNOWN_DEVICE", "device is not configured")
		}
		token, ok := gatewayconfig.OSLookupEnv(device.AuthTokenEnv)
		if !ok || !authorizationMatchesToken(request.Authorization, token) {
			return xiaozhi.AuthResult{}, xiaozhi.NewAuthError(http.StatusForbidden, "INVALID_AUTHORIZATION", "authorization does not match configured device token")
		}
		if subtle.ConstantTimeCompare([]byte(request.ClientID), []byte(device.ClientID)) != 1 {
			return xiaozhi.AuthResult{}, xiaozhi.NewAuthError(http.StatusForbidden, "INVALID_CLIENT_ID", "client id does not match configured device")
		}
		configuredProtocolVersion := cfg.Server.WebsocketVersion
		if configuredProtocolVersion == 0 {
			configuredProtocolVersion = xiaozhi.BinaryProtocolV1
		}
		if request.ProtocolVersion != configuredProtocolVersion {
			return xiaozhi.AuthResult{}, xiaozhi.NewAuthError(http.StatusBadRequest, "PROTOCOL_VERSION_MISMATCH", "protocol-version does not match configured websocket version")
		}

		return xiaozhi.AuthResult{
			DeviceID:        request.DeviceID,
			ClientID:        request.ClientID,
			ProtocolVersion: request.ProtocolVersion,
		}, nil
	}
}

type gatewayWebSocketRuntime struct {
	handler http.Handler
	logger  *slog.Logger
	manager *session.Manager
	loops   gatewayVoiceLoops
}

func newGatewayWebSocketHandler(cfg *gatewayconfig.Config, logger *slog.Logger, metrics *observability.Metrics, traceRecorder observability.TraceRecorder, manager *session.Manager, providerResolver session.VoiceProviderResolver, llmContextBuilder session.LLMContextBuilder, agentModeCommands session.AgentModeCommandHandler, providerProfileCommands session.ProviderProfileCommandHandler, agentModeReader session.AgentModeReader, agentRuntime session.AgentRuntimeRouter, memoryWriter session.MemoryWriter, conversationRecorder session.ConversationRecorder, serviceTools *servicetools.Registry) *gatewayWebSocketRuntime {
	runtime := &gatewayWebSocketRuntime{
		logger:  logger,
		manager: manager,
		loops: gatewayVoiceLoops{
			byTransport: map[*xiaozhi.Transport]gatewayVoiceLoop{},
			byDevice:    map[string]*xiaozhi.Transport{},
		},
	}

	runtime.handler = xiaozhi.NewWebSocketHandler(xiaozhi.WebSocketHandlerOptions{
		Authenticator: AuthenticatorForConfig(cfg),
		OnConnect: func(transport *xiaozhi.Transport) {
			if runtime.loops.IsClosed() {
				_ = transport.Close(websocketCloseServerShutdown, "server shutdown")
				return
			}
			binding, err := newVoiceLoopForTransport(cfg, metrics, traceRecorder, manager, providerResolver, llmContextBuilder, agentModeCommands, providerProfileCommands, agentModeReader, agentRuntime, memoryWriter, conversationRecorder, serviceTools, transport)
			if err != nil {
				logger.Warn("xiaozhi voice loop setup failed", "code", gatewayErrorCode(err))
				_ = transport.SendJSON(context.Background(), xiaozhi.NewAlert("", "error", "voice loop unavailable", "neutral"))
				_ = transport.Close(websocketCloseInternalError, "voice loop unavailable")
				return
			}
			previousTransport, previousBinding, replaced, accepted := runtime.loops.Store(transport, binding)
			if !accepted {
				runtime.closeBinding(context.Background(), transport, binding, websocketCloseServerShutdown, "server shutdown")
				return
			}
			if replaced {
				runtime.closeBinding(context.Background(), previousTransport, previousBinding, websocketCloseDeviceReplaced, "device reconnected")
			}
			startXiaozhiIdleKeepalive(binding.ctx, logger, transport, binding.sessionID, xiaozhiIdleKeepaliveInterval, binding.loop.State)
		},
		OnText: func(transport *xiaozhi.Transport, data []byte) {
			binding, ok := runtime.loops.Load(transport)
			if !ok {
				logger.Warn("xiaozhi text without voice loop")
				return
			}
			if err := handleGatewayText(binding.ctx, binding.loop, data, func(err error) {
				if err == nil || errors.Is(err, context.Canceled) {
					return
				}
				logger.Warn("xiaozhi async text handling failed", "code", gatewayErrorCode(err))
			}); err != nil {
				logger.Warn("xiaozhi text handling failed", "code", gatewayErrorCode(err))
				_ = transport.SendJSON(context.Background(), xiaozhi.NewAlert("", "error", "protocol message rejected", "neutral"))
			}
		},
		OnBinary: func(transport *xiaozhi.Transport, frame audio.Frame) {
			binding, ok := runtime.loops.Load(transport)
			if !ok {
				return
			}
			if err := binding.loop.AcceptOpus(frame); err != nil && !errors.Is(err, session.ErrASRStreamNotStarted) {
				logger.Warn("xiaozhi audio handling failed", "code", gatewayErrorCode(err))
			}
		},
		OnError: func(transport *xiaozhi.Transport, err error) {
			logger.Debug("xiaozhi websocket error", "code", gatewayErrorCode(err))
		},
		OnDisconnect: func(transport *xiaozhi.Transport) {
			if binding, ok := runtime.loops.Delete(transport); ok {
				runtime.closeBinding(context.Background(), nil, binding, 0, "")
			}
			logger.Debug("xiaozhi websocket disconnected")
		},
	})
	return runtime
}

func (r *gatewayWebSocketRuntime) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	r.handler.ServeHTTP(w, request)
}

func (r *gatewayWebSocketRuntime) CloseAll(ctx context.Context) {
	for _, entry := range r.loops.DeleteAll() {
		r.closeBinding(ctx, entry.transport, entry.loop, websocketCloseServerShutdown, "server shutdown")
	}
}

func (r *gatewayWebSocketRuntime) closeBinding(ctx context.Context, transport *xiaozhi.Transport, binding gatewayVoiceLoop, closeCode int, closeReason string) {
	binding.cancel()
	binding.loop.Close(ctx)
	_ = r.manager.CloseSession(binding.sessionID)
	if transport != nil {
		_ = transport.Close(closeCode, closeReason)
	}
}

const websocketCloseInternalError = 1011
const websocketCloseDeviceReplaced = 1000
const websocketCloseServerShutdown = 1001
const xiaozhiIdleKeepaliveInterval = 45 * time.Second
const xiaozhiIdleKeepaliveWriteTimeout = 2 * time.Second

type xiaozhiJSONSender interface {
	SendJSON(ctx context.Context, msg any) error
}

func startXiaozhiIdleKeepalive(ctx context.Context, logger *slog.Logger, downlink xiaozhiJSONSender, sessionID string, interval time.Duration, currentState func() session.State) {
	if ctx == nil || downlink == nil || interval <= 0 || currentState == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if currentState() != session.StateIdle {
				continue
			}
			writeCtx, cancel := context.WithTimeout(ctx, xiaozhiIdleKeepaliveWriteTimeout)
			err := downlink.SendJSON(writeCtx, xiaozhi.NewLLMEmotion(sessionID, "neutral"))
			cancel()
			if err != nil && ctx.Err() == nil {
				logger.Debug("xiaozhi idle keepalive failed", "code", gatewayErrorCode(err))
			}
		}
	}()
}

type gatewayVoiceLoop struct {
	loop      *session.VoiceLoop
	sessionID string
	deviceID  string
	ctx       context.Context
	cancel    context.CancelFunc
}

type gatewayVoiceLoops struct {
	mu          sync.Mutex
	byTransport map[*xiaozhi.Transport]gatewayVoiceLoop
	byDevice    map[string]*xiaozhi.Transport
	closed      bool
}

type gatewayVoiceLoopEntry struct {
	transport *xiaozhi.Transport
	loop      gatewayVoiceLoop
}

func (l *gatewayVoiceLoops) Store(transport *xiaozhi.Transport, loop gatewayVoiceLoop) (*xiaozhi.Transport, gatewayVoiceLoop, bool, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, gatewayVoiceLoop{}, false, false
	}

	var previousTransport *xiaozhi.Transport
	var previousLoop gatewayVoiceLoop
	var replaced bool
	if loop.deviceID != "" {
		if candidate := l.byDevice[loop.deviceID]; candidate != nil && candidate != transport {
			if existing, ok := l.byTransport[candidate]; ok {
				previousTransport = candidate
				previousLoop = existing
				replaced = true
			}
			delete(l.byTransport, candidate)
		}
		l.byDevice[loop.deviceID] = transport
	}
	l.byTransport[transport] = loop
	return previousTransport, previousLoop, replaced, true
}

func (l *gatewayVoiceLoops) IsClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func (l *gatewayVoiceLoops) Load(transport *xiaozhi.Transport) (gatewayVoiceLoop, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	loop, ok := l.byTransport[transport]
	return loop, ok
}

func (l *gatewayVoiceLoops) Delete(transport *xiaozhi.Transport) (gatewayVoiceLoop, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	loop, ok := l.byTransport[transport]
	delete(l.byTransport, transport)
	if ok && loop.deviceID != "" && l.byDevice[loop.deviceID] == transport {
		delete(l.byDevice, loop.deviceID)
	}
	return loop, ok
}

func (l *gatewayVoiceLoops) DeleteAll() []gatewayVoiceLoopEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	entries := make([]gatewayVoiceLoopEntry, 0, len(l.byTransport))
	for transport, loop := range l.byTransport {
		entries = append(entries, gatewayVoiceLoopEntry{transport: transport, loop: loop})
	}
	l.byTransport = map[*xiaozhi.Transport]gatewayVoiceLoop{}
	l.byDevice = map[string]*xiaozhi.Transport{}
	l.closed = true
	return entries
}

func newVoiceLoopForTransport(cfg *gatewayconfig.Config, metrics *observability.Metrics, traceRecorder observability.TraceRecorder, manager *session.Manager, providerResolver session.VoiceProviderResolver, llmContextBuilder session.LLMContextBuilder, agentModeCommands session.AgentModeCommandHandler, providerProfileCommands session.ProviderProfileCommandHandler, agentModeReader session.AgentModeReader, agentRuntime session.AgentRuntimeRouter, memoryWriter session.MemoryWriter, conversationRecorder session.ConversationRecorder, serviceTools *servicetools.Registry, transport *xiaozhi.Transport) (gatewayVoiceLoop, error) {
	device, ok := cfg.DeviceByID(transport.DeviceID())
	if !ok {
		return gatewayVoiceLoop{}, fmt.Errorf("device config not found: %s", transport.DeviceID())
	}
	voiceSession := manager.CreateSession(transport.DeviceID(), transport.ClientID())
	voiceCtx, cancel := context.WithCancel(context.Background())
	mcpBroker, err := mcp.NewBroker(mcp.BrokerOptions{
		SessionID: voiceSession.ID(),
		Downlink:  transport,
		Allowlist: mcp.NewAllowlist(device.AllowMCPTools),
		Metrics:   metrics,
	})
	if err != nil {
		cancel()
		_ = manager.CloseSession(voiceSession.ID())
		return gatewayVoiceLoop{}, err
	}
	bodyScheduler := stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
		CurrentGeneration:  0,
		Limits:             motionLimitsFromConfig(cfg.StackChan.Body),
		MinCommandGap:      time.Duration(cfg.StackChan.Body.MinCommandGapMS) * time.Millisecond,
		MaxCommandsPerTurn: cfg.StackChan.Body.MaxCommandsPerTurn,
		GenerationIsCurrent: func(generation int64) bool {
			return voiceSession.AcceptsGeneration(generation)
		},
	})
	sceneComposer := stackchan.NewSceneComposer(displayOptionsFromConfig(cfg.StackChan.Display))
	providerObserver, _ := providerResolver.(session.VoiceProviderObserver)
	loop, err := session.NewVoiceLoop(session.VoiceLoopOptions{
		Session:                 voiceSession,
		ProviderResolver:        providerResolver,
		ProviderObserver:        providerObserver,
		LLMContextBuilder:       llmContextBuilder,
		AgentModeCommands:       agentModeCommands,
		ProviderProfileCommands: providerProfileCommands,
		AgentModeReader:         agentModeReader,
		AgentRuntime:            agentRuntime,
		MemoryWriter:            memoryWriter,
		ConversationRecorder:    conversationRecorder,
		Downlink:                transport,
		Pacer:                   audio.NewPacer(audio.PacerOptions{}),
		TraceRecorder:           traceRecorder,
		Metrics:                 metrics,
		MCPBroker:               mcpBroker,
		ServiceTools:            serviceTools,
		BodyScheduler:           bodyScheduler,
		SceneComposer:           sceneComposer,
		ExpressionPolicies:      expressionPoliciesFromConfig(cfg.StackChan.Expression.Cues),
		ExpressionSequences: expressionSequencesFromConfig(
			cfg.StackChan.Expression.Sequences,
		),
		ExpressionProviderToolsEnabled: cfg.StackChan.Expression.ProviderToolsEnabled,
		ToolResultFollowUpPolicy: toolResultFollowUpPolicyFromConfig(
			cfg.Tools.ToolFollowUp,
		),
		LifecycleExpressionCues: lifecycleExpressionCuesFromConfig(
			cfg.StackChan.Expression.LifecycleCues,
		),
		EventExpressionCues: eventExpressionCuesFromConfig(
			cfg.StackChan.Expression.EventCues,
		),
		LifecycleLEDs:            lifecycleLEDsFromConfig(cfg.StackChan.Body.LifecycleLEDs),
		ListenStartMotionEnabled: cfg.StackChan.Body.ListenStartMotionEnabled,
	})
	if err != nil {
		cancel()
		_ = manager.CloseSession(voiceSession.ID())
		return gatewayVoiceLoop{}, err
	}
	return gatewayVoiceLoop{
		loop:      loop,
		sessionID: voiceSession.ID(),
		deviceID:  voiceSession.DeviceID(),
		ctx:       voiceCtx,
		cancel:    cancel,
	}, nil
}

func motionLimitsFromConfig(body gatewayconfig.BodyConfig) stackchan.MotionLimits {
	return stackchan.MotionLimits{
		YawMinDeg:    body.YawMinDeg,
		YawMaxDeg:    body.YawMaxDeg,
		PitchMinDeg:  body.PitchMinDeg,
		PitchMaxDeg:  body.PitchMaxDeg,
		DefaultSpeed: body.DefaultSpeed,
	}
}

func lifecycleLEDsFromConfig(configured map[string]gatewayconfig.LifecycleLEDConfig) map[string]stackchan.LEDCommand {
	if configured == nil {
		return nil
	}
	leds := make(map[string]stackchan.LEDCommand, len(configured))
	for lifecycle, policy := range configured {
		lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
		if lifecycle == "" {
			continue
		}
		if policy.Enabled != nil && !*policy.Enabled {
			continue
		}
		leds[lifecycle] = stackchan.LEDCommand{
			R:      policy.R,
			G:      policy.G,
			B:      policy.B,
			Reason: stackchan.LifecycleLEDReason(lifecycle),
		}
	}
	return leds
}

func displayOptionsFromConfig(display gatewayconfig.DisplayConfig) stackchan.DisplayOptions {
	return stackchan.DisplayOptions{
		SceneTTLMS:      display.SceneTTLMS,
		MaxCaptionChars: display.MaxCaptionChars,
		LifecycleScenes: lifecycleScenePoliciesFromConfig(display.LifecycleScenes),
		EventScenes:     lifecycleScenePoliciesFromConfig(display.EventScenes),
		Cards:           displayCardPoliciesFromConfig(display.Cards),
	}
}

func lifecycleScenePoliciesFromConfig(configured map[string]gatewayconfig.DisplaySceneConfig) map[string]stackchan.ScenePolicy {
	if len(configured) == 0 {
		return nil
	}
	policies := make(map[string]stackchan.ScenePolicy, len(configured))
	for scene, policy := range configured {
		var motion *stackchan.SceneMotion
		if policy.Motion != nil {
			intensity := 0.0
			if policy.Motion.Intensity != nil {
				intensity = *policy.Motion.Intensity
			}
			motion = &stackchan.SceneMotion{
				Preset:    strings.TrimSpace(policy.Motion.Preset),
				Intensity: intensity,
			}
		}
		policies[strings.TrimSpace(scene)] = stackchan.ScenePolicy{
			Scene:   strings.TrimSpace(policy.Scene),
			Emotion: strings.TrimSpace(policy.Emotion),
			Caption: policy.Caption,
			Accent:  strings.TrimSpace(policy.Accent),
			Motion:  motion,
		}
	}
	return policies
}

func displayCardPoliciesFromConfig(configured map[string]gatewayconfig.DisplayCardConfig) map[string]stackchan.DisplayCardPolicy {
	if len(configured) == 0 {
		return nil
	}
	policies := make(map[string]stackchan.DisplayCardPolicy, len(configured))
	for cardID, policy := range configured {
		card := stackchan.DisplayCardPolicy{
			ScenePolicy: stackchan.ScenePolicy{
				Scene:   strings.TrimSpace(policy.Scene),
				Emotion: strings.TrimSpace(policy.Emotion),
				Caption: policy.Caption,
				Accent:  strings.TrimSpace(policy.Accent),
			},
			AllowCaption:    policy.AllowCaption,
			MaxCaptionChars: policy.MaxCaptionChars,
		}
		if policy.Motion != nil {
			intensity := 0.0
			if policy.Motion.Intensity != nil {
				intensity = *policy.Motion.Intensity
			}
			card.Motion = &stackchan.SceneMotion{
				Preset:    strings.TrimSpace(policy.Motion.Preset),
				Intensity: intensity,
			}
		}
		policies[strings.ToLower(strings.TrimSpace(cardID))] = card
	}
	return policies
}

func displayCardDevicesFromConfig(devices []gatewayconfig.DeviceConfig) []stackchan.DisplayCardDevice {
	out := make([]stackchan.DisplayCardDevice, 0, len(devices))
	for _, device := range devices {
		out = append(out, stackchan.DisplayCardDevice{
			DeviceID:      strings.TrimSpace(device.DeviceID),
			AllowMCPTools: append([]string(nil), device.AllowMCPTools...),
		})
	}
	return out
}

func displaySceneDevicesFromConfig(devices []gatewayconfig.DeviceConfig) []stackchan.DisplaySceneDevice {
	out := make([]stackchan.DisplaySceneDevice, 0, len(devices))
	for _, device := range devices {
		out = append(out, stackchan.DisplaySceneDevice{
			DeviceID:      strings.TrimSpace(device.DeviceID),
			AllowMCPTools: append([]string(nil), device.AllowMCPTools...),
		})
	}
	return out
}

func expressionDevicesFromConfig(devices []gatewayconfig.DeviceConfig) []stackchan.ExpressionDevice {
	out := make([]stackchan.ExpressionDevice, 0, len(devices))
	for _, device := range devices {
		out = append(out, stackchan.ExpressionDevice{
			DeviceID:      strings.TrimSpace(device.DeviceID),
			AllowMCPTools: append([]string(nil), device.AllowMCPTools...),
		})
	}
	return out
}

func expressionPoliciesFromConfig(configured map[string]gatewayconfig.ExpressionCueConfig) map[string]stackchan.ExpressionPolicy {
	if len(configured) == 0 {
		return nil
	}
	policies := make(map[string]stackchan.ExpressionPolicy, len(configured))
	for cue, policy := range configured {
		expression := stackchan.ExpressionPolicy{}
		if policy.Motion != nil {
			expression.Motion = &stackchan.MotionCommand{
				Yaw:   policy.Motion.YawDeg,
				Pitch: policy.Motion.PitchDeg,
				Speed: policy.Motion.Speed,
			}
		}
		if policy.LED != nil {
			expression.LED = &stackchan.LEDCommand{
				R: policy.LED.R,
				G: policy.LED.G,
				B: policy.LED.B,
			}
		}
		if policy.Scene != nil {
			expression.Scene = scenePolicyFromConfig(*policy.Scene)
		}
		policies[strings.ToLower(strings.TrimSpace(cue))] = expression
	}
	return policies
}

func lifecycleExpressionCuesFromConfig(configured map[string]string) map[string]string {
	if len(configured) == 0 {
		return nil
	}
	cues := make(map[string]string, len(configured))
	for lifecycle, cue := range configured {
		cues[strings.ToLower(strings.TrimSpace(lifecycle))] = strings.ToLower(strings.TrimSpace(cue))
	}
	return cues
}

func eventExpressionCuesFromConfig(configured map[string]string) map[string]string {
	if len(configured) == 0 {
		return nil
	}
	cues := make(map[string]string, len(configured))
	for event, cue := range configured {
		cues[strings.ToLower(strings.TrimSpace(event))] = strings.ToLower(strings.TrimSpace(cue))
	}
	return cues
}

func expressionSequencesFromConfig(configured map[string]gatewayconfig.ExpressionSequenceConfig) map[string][]string {
	if len(configured) == 0 {
		return nil
	}
	sequences := make(map[string][]string, len(configured))
	for sequenceID, sequence := range configured {
		normalizedID := strings.ToLower(strings.TrimSpace(sequenceID))
		if normalizedID == "" {
			continue
		}
		cues := make([]string, 0, len(sequence.Cues))
		for _, cue := range sequence.Cues {
			cue = strings.ToLower(strings.TrimSpace(cue))
			if cue != "" {
				cues = append(cues, cue)
			}
		}
		sequences[normalizedID] = cues
	}
	return sequences
}

func toolResultFollowUpPolicyFromConfig(configured gatewayconfig.ToolFollowUpConfig) *session.ToolResultFollowUpPolicy {
	if configured.Enabled == nil && configured.MaxResults == nil && configured.MaxResultBytes == nil && len(configured.AllowedTools) == 0 && configured.AllowToolCalls == nil && configured.MaxToolCalls == nil {
		return nil
	}
	enabled := true
	if configured.Enabled != nil {
		enabled = *configured.Enabled
	}
	policy := &session.ToolResultFollowUpPolicy{Enabled: enabled}
	if configured.MaxResults != nil {
		policy.MaxResults = *configured.MaxResults
	}
	if configured.MaxResultBytes != nil {
		policy.MaxResultBytes = *configured.MaxResultBytes
	}
	if len(configured.AllowedTools) > 0 {
		policy.AllowedTools = append([]string(nil), configured.AllowedTools...)
	}
	if configured.AllowToolCalls != nil {
		policy.AllowToolCalls = *configured.AllowToolCalls
	}
	if configured.MaxToolCalls != nil {
		policy.MaxToolCalls = *configured.MaxToolCalls
	}
	return policy
}

func scenePolicyFromConfig(policy gatewayconfig.DisplaySceneConfig) stackchan.ScenePolicy {
	var motion *stackchan.SceneMotion
	if policy.Motion != nil {
		intensity := 0.0
		if policy.Motion.Intensity != nil {
			intensity = *policy.Motion.Intensity
		}
		motion = &stackchan.SceneMotion{
			Preset:    strings.TrimSpace(policy.Motion.Preset),
			Intensity: intensity,
		}
	}
	return stackchan.ScenePolicy{
		Scene:   strings.TrimSpace(policy.Scene),
		Emotion: strings.TrimSpace(policy.Emotion),
		Caption: policy.Caption,
		Accent:  strings.TrimSpace(policy.Accent),
		Motion:  motion,
	}
}

func newAgentPromptBuilder(cfg *gatewayconfig.Config) (session.LLMContextBuilder, session.MemoryWriter, session.ConversationRecorder, agent.MemoryAdminRepository, agent.RecentTurnReader, error) {
	if cfg == nil || strings.TrimSpace(cfg.Agent.PersonaPath) == "" {
		return nil, nil, nil, nil, nil, nil
	}
	personaPath := resolveGatewayRuntimePath(cfg.Agent.PersonaPath)
	persona, err := agent.LoadPersonaFile(personaPath)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	var repository agent.MemoryAdminRepository
	var memoryWriter session.MemoryWriter
	var conversationRecorder session.ConversationRecorder
	var recentTurnStore agent.RecentTurnReader
	recentTurnLimit := cfg.Agent.RecentTurns * 4
	memoryStore := agent.MemoryStore(agent.NewStaticMemoryStore(nil))
	if strings.TrimSpace(cfg.Agent.MemoryDBPath) != "" {
		memoryPath := resolveGatewayRuntimePath(cfg.Agent.MemoryDBPath)
		sqliteStore, err := agent.NewSQLiteMemoryStore(memoryPath)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("agent memory store: %w", err)
		}
		writer, err := agent.NewTranscriptMemoryWriter(agent.TranscriptMemoryWriterOptions{
			Repository:  sqliteStore,
			OwnerUserID: "owner",
		})
		if err != nil {
			_ = sqliteStore.Close()
			return nil, nil, nil, nil, nil, fmt.Errorf("agent memory writer: %w", err)
		}
		repository = sqliteStore
		memoryWriter = writer
		memoryStore = sqliteStore
		if cfg.Agent.RecentTurns > 0 {
			sqliteStore.SetRecentTurnLimit(recentTurnLimit)
			conversationRecorder = sqliteStore
			recentTurnStore = sqliteStore
		}
	} else if cfg.Agent.RecentTurns > 0 {
		recentStore := agent.NewInMemoryRecentTurnStore(recentTurnLimit)
		conversationRecorder = recentStore
		recentTurnStore = recentStore
	}
	return agent.NewPromptBuilder(agent.PromptBuilderOptions{
		Persona:         persona,
		MemoryStore:     memoryStore,
		MemoryMaxItems:  cfg.Agent.MemoryMaxItems,
		RecentTurnStore: recentTurnStore,
		RecentTurns:     cfg.Agent.RecentTurns,
		OwnerUserID:     "owner",
	}), memoryWriter, conversationRecorder, repository, recentTurnStore, nil
}

func newAgentModeStore(cfg *gatewayconfig.Config) *agents.ModeStore {
	if cfg == nil {
		return agents.NewModeStore(agents.ModeCasual, nil)
	}
	return agents.NewModeStore(agents.Mode(cfg.Agent.DefaultMode), configuredDeviceIDs(cfg.Devices))
}

func newAgentBridgeCatalog(cfg *gatewayconfig.Config) agents.BridgeCatalogReader {
	if cfg == nil {
		return agents.NewBridgeCatalogStore(nil)
	}
	return agents.NewBridgeCatalogStore([]agents.BridgeStatus{
		{
			Bridge:                         agents.BridgeHermes,
			Enabled:                        cfg.Agent.Hermes.Enabled,
			RequiredMode:                   agents.ModeRoleplay,
			Invocation:                     agents.BridgeInvocationRuntimeRoute,
			RuntimeRoute:                   cfg.Agent.Hermes.Enabled,
			ToolIntents:                    true,
			AllowedToolIntents:             cfg.Agent.Hermes.AllowedToolIntents,
			MaxToolIntents:                 agents.ResolveBridgeMaxToolIntents(cfg.Agent.Hermes.MaxToolIntents),
			MaxRuntimeRoutesPerMinute:      cfg.Agent.Hermes.MaxRuntimeRoutesPerMinute,
			MaxRuntimeInputChars:           cfg.Agent.Hermes.MaxRuntimeInputChars,
			MaxRuntimeErrorsBeforeCooldown: cfg.Agent.Hermes.MaxRuntimeErrorsBeforeCooldown,
			RuntimeErrorCooldownMS:         cfg.Agent.Hermes.RuntimeErrorCooldownMS,
			FallbackOnError:                true,
			FallbackOnEmpty:                true,
			BoundedSpokenOutput:            true,
		},
		{
			Bridge:                         agents.BridgeOpenClaw,
			Enabled:                        cfg.Agent.OpenClaw.Enabled,
			RequiredMode:                   agents.ModeTool,
			Invocation:                     agents.BridgeInvocationRuntimeRoute,
			RuntimeRoute:                   cfg.Agent.OpenClaw.Enabled,
			ToolIntents:                    true,
			AllowedToolIntents:             cfg.Agent.OpenClaw.AllowedToolIntents,
			MaxToolIntents:                 agents.ResolveBridgeMaxToolIntents(cfg.Agent.OpenClaw.MaxToolIntents),
			MaxRuntimeRoutesPerMinute:      cfg.Agent.OpenClaw.MaxRuntimeRoutesPerMinute,
			MaxRuntimeInputChars:           cfg.Agent.OpenClaw.MaxRuntimeInputChars,
			MaxRuntimeErrorsBeforeCooldown: cfg.Agent.OpenClaw.MaxRuntimeErrorsBeforeCooldown,
			RuntimeErrorCooldownMS:         cfg.Agent.OpenClaw.RuntimeErrorCooldownMS,
			FallbackOnError:                true,
			FallbackOnEmpty:                true,
			BoundedSpokenOutput:            true,
		},
		{
			Bridge:              agents.BridgeV21,
			Enabled:             cfg.Agent.V21.Enabled,
			RequiredMode:        agents.ModeProfessional,
			Invocation:          agents.BridgeInvocationServiceTool,
			ServiceTool:         agents.V21VoiceQueryToolName,
			RuntimeRoute:        false,
			ToolIntents:         false,
			BoundedSpokenOutput: true,
		},
	})
}

func newAgentRuntimeStatus(modes agents.ModeController, bridges agents.BridgeCatalogReader, policies agents.RuntimePolicyStatusReader) agents.RuntimeStatusReader {
	if modes == nil || bridges == nil {
		return nil
	}
	return agents.NewRuntimeStatusStoreWithPolicies(modes, bridges, policies)
}

type agentModeCommandHandler struct {
	modes agents.ModeController
}

type providerProfileCommandHandler struct {
	profiles providerrouter.Controller
}

func newAgentModeCommandHandler(modes agents.ModeController) session.AgentModeCommandHandler {
	if modes == nil {
		return nil
	}
	return agentModeCommandHandler{modes: modes}
}

func newProviderProfileCommandHandler(profiles providerrouter.Controller) session.ProviderProfileCommandHandler {
	if profiles == nil {
		return nil
	}
	return providerProfileCommandHandler{profiles: profiles}
}

type agentModeReader struct {
	modes agents.ModeReader
}

func newAgentModeReader(modes agents.ModeReader) session.AgentModeReader {
	if modes == nil {
		return nil
	}
	return agentModeReader{modes: modes}
}

func (r agentModeReader) CurrentAgentMode(ctx context.Context, request session.AgentModeReadRequest) (session.AgentModeReadResult, error) {
	status, err := r.modes.GetDeviceMode(ctx, request.DeviceID)
	if err != nil {
		return session.AgentModeReadResult{}, err
	}
	return session.AgentModeReadResult{Mode: string(status.ActiveMode)}, nil
}

func (h agentModeCommandHandler) HandleAgentModeCommand(ctx context.Context, request session.AgentModeCommandRequest) (session.AgentModeCommandResult, error) {
	command := agents.ParseModeCommand(request.Transcript)
	if !command.Handled {
		return session.AgentModeCommandResult{}, nil
	}
	var (
		status agents.ModeStatus
		err    error
	)
	if command.ClearOverride {
		status, err = h.modes.ClearDeviceMode(ctx, request.DeviceID)
	} else {
		status, err = h.modes.SetDeviceMode(ctx, request.DeviceID, command.Mode)
	}
	if err != nil {
		return session.AgentModeCommandResult{}, err
	}
	return session.AgentModeCommandResult{
		Handled:    true,
		Mode:       string(status.ActiveMode),
		Action:     command.Action,
		SpokenText: command.SpokenText,
	}, nil
}

func (h providerProfileCommandHandler) HandleProviderProfileCommand(ctx context.Context, request session.ProviderProfileCommandRequest) (session.ProviderProfileCommandResult, error) {
	command := parseProviderProfileCommand(request.Transcript)
	if !command.handled {
		return session.ProviderProfileCommandResult{}, nil
	}
	if command.status {
		status, err := h.profiles.GetDeviceProfile(ctx, request.DeviceID)
		if err != nil {
			return session.ProviderProfileCommandResult{
				Handled:    true,
				Action:     "status_failed",
				SpokenText: "我现在还查不到语音链路。",
			}, nil
		}
		return session.ProviderProfileCommandResult{
			Handled:    true,
			Profile:    status.ActiveProfile,
			Action:     "status",
			SpokenText: "当前语音链路是" + friendlyProviderProfileName(status.ActiveProfile) + "。",
		}, nil
	}
	if command.clear {
		status, err := h.profiles.ClearDeviceProfile(ctx, request.DeviceID)
		if err != nil {
			return session.ProviderProfileCommandResult{}, err
		}
		return session.ProviderProfileCommandResult{
			Handled:    true,
			Profile:    status.ActiveProfile,
			Action:     "clear",
			SpokenText: "已切回默认语音链路。",
		}, nil
	}
	status, err := h.profiles.SetDeviceProfile(ctx, request.DeviceID, command.profile)
	if err != nil {
		return session.ProviderProfileCommandResult{
			Handled:    true,
			Profile:    command.profile,
			Action:     "unavailable",
			SpokenText: friendlyProviderProfileName(command.profile) + "还没在云端配置好。",
		}, nil
	}
	return session.ProviderProfileCommandResult{
		Handled:    true,
		Profile:    status.ActiveProfile,
		Action:     "set",
		SpokenText: "已切到" + friendlyProviderProfileName(status.ActiveProfile) + "语音链路。",
	}, nil
}

type providerProfileCommand struct {
	handled bool
	clear   bool
	status  bool
	profile string
}

func parseProviderProfileCommand(transcript string) providerProfileCommand {
	command := normalizeProviderProfileCommandTranscript(transcript)
	command = trimProviderProfileCommandPrefixes(command)
	if isProviderProfileStatusCommand(command) {
		return providerProfileCommand{handled: true, status: true}
	}
	if isProviderProfileClearCommand(command) {
		return providerProfileCommand{handled: true, clear: true}
	}
	for _, candidate := range []struct {
		profile string
		aliases []string
	}{
		{profile: "dashscope-cosyvoice", aliases: []string{"阿里", "百炼", "通义", "qwen", "dashscope"}},
		{profile: "doubao-dashscope-voice", aliases: []string{"字节", "豆包", "火山", "doubao", "volcengine", "ark"}},
		{profile: "deepseek-dashscope-voice", aliases: []string{"deepseek", "深度求索"}},
		{profile: "moonshot-dashscope-voice", aliases: []string{"kimi", "moonshot", "月之暗面"}},
		{profile: "minimax-dashscope-voice", aliases: []string{"minimax", "海螺"}},
	} {
		if isProviderProfileSwitchCommand(command, candidate.aliases) {
			return providerProfileCommand{handled: true, profile: candidate.profile}
		}
	}
	return providerProfileCommand{}
}

func isProviderProfileStatusCommand(command string) bool {
	switch command {
	case "当前模型", "当前语音模型", "当前语音链路", "当前provider", "当前llm",
		"现在用什么模型", "现在用哪个模型", "你现在用什么模型", "你现在用哪个模型",
		"当前模型是什么", "当前provider是什么", "当前语音链路是什么":
		return true
	default:
		return false
	}
}

func isProviderProfileClearCommand(command string) bool {
	switch command {
	case "切回默认模型", "切换回默认模型", "恢复默认模型", "回到默认模型", "使用默认模型",
		"切回默认语音模型", "切换回默认语音模型", "恢复默认语音模型",
		"切回默认语音链路", "切换回默认语音链路", "恢复默认语音链路",
		"切回主模型", "切换回主模型", "恢复主模型",
		"切回主语音链路", "恢复主语音链路",
		"clearproviderprofile", "resetproviderprofile", "usedefaultmodel":
		return true
	default:
		return false
	}
}

func isProviderProfileSwitchCommand(command string, aliases []string) bool {
	for _, prefix := range []string{"切到", "切换到", "换到", "改用", "使用", "启用", "打开"} {
		for _, alias := range aliases {
			for _, suffix := range []string{"模型", "语音模型", "语音链路", "provider", "llm"} {
				if command == prefix+alias+suffix {
					return true
				}
			}
			if command == prefix+alias {
				return true
			}
		}
	}
	return false
}

func friendlyProviderProfileName(profile string) string {
	switch strings.TrimSpace(profile) {
	case "siliconflow-dashscope-voice":
		return "默认"
	case "dashscope-cosyvoice":
		return "阿里百炼"
	case "doubao-dashscope-voice":
		return "字节豆包"
	case "deepseek-dashscope-voice":
		return "DeepSeek"
	case "moonshot-dashscope-voice":
		return "Kimi"
	case "minimax-dashscope-voice":
		return "MiniMax"
	default:
		return "当前"
	}
}

func trimProviderProfileCommandPrefixes(command string) string {
	for {
		trimmed := command
		for _, prefix := range []string{"大头", "stackchan", "小智同学", "小智", "请", "麻烦", "帮我"} {
			trimmed = strings.TrimPrefix(trimmed, prefix)
		}
		if trimmed == command {
			return command
		}
		command = trimmed
	}
}

func normalizeProviderProfileCommandTranscript(transcript string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r',
			'，', '。', '！', '？', '、', '：', '；',
			',', '.', '!', '?', ':', ';', '-',
			'“', '”', '"', '\'', '‘', '’',
			'（', '）', '(', ')', '【', '】', '[', ']':
			return -1
		default:
			return r
		}
	}, strings.ToLower(strings.TrimSpace(transcript)))
}

type agentRuntimeRouter struct {
	modes                 agents.ModeReader
	router                *agents.Router
	runtimeRouteLimits    *agentRuntimeRouteLimiter
	runtimeInputLimits    map[agents.Destination]int
	runtimeErrorCooldowns *agentRuntimeErrorCooldowns
	hermesEnabled         bool
	openClawEnabled       bool
}

func newAgentRuntimeRouter(cfg *gatewayconfig.Config, modes agents.ModeReader) session.AgentRuntimeRouter {
	if cfg == nil || modes == nil || (!cfg.Agent.Hermes.Enabled && !cfg.Agent.OpenClaw.Enabled) {
		return nil
	}
	var hermes agents.Bridge
	if cfg.Agent.Hermes.Enabled {
		hermesCfg := cfg.Agent.Hermes
		hermes = agents.NewHermesClient(agents.HermesClientOptions{
			BaseURL:            os.Getenv(hermesCfg.BaseURLEnv),
			Token:              os.Getenv(hermesCfg.TokenEnv),
			MaxSpokenChars:     hermesCfg.MaxSpokenChars,
			AllowedToolIntents: hermesCfg.AllowedToolIntents,
			MaxToolIntents:     hermesCfg.MaxToolIntents,
			Client:             &http.Client{Timeout: agentHermesTimeout(cfg)},
		})
	}
	var openClaw agents.Bridge
	openClawCfg := cfg.Agent.OpenClaw
	if cfg.Agent.OpenClaw.Enabled {
		openClaw = agents.NewOpenClawClient(agents.OpenClawClientOptions{
			BaseURL:            os.Getenv(openClawCfg.BaseURLEnv),
			Token:              os.Getenv(openClawCfg.TokenEnv),
			MaxSpokenChars:     openClawCfg.MaxSpokenChars,
			AllowedToolIntents: openClawCfg.AllowedToolIntents,
			MaxToolIntents:     openClawCfg.MaxToolIntents,
			Client:             &http.Client{Timeout: agentOpenClawTimeout(cfg)},
		})
	}
	router := agents.NewRouter(agents.RouterOptions{
		Hermes:   hermes,
		OpenClaw: openClaw,
	})
	return agentRuntimeRouter{
		modes:  modes,
		router: router,
		runtimeRouteLimits: newAgentRuntimeRouteLimiter(map[agents.Destination]int{
			agents.DestinationHermes:   cfg.Agent.Hermes.MaxRuntimeRoutesPerMinute,
			agents.DestinationOpenClaw: cfg.Agent.OpenClaw.MaxRuntimeRoutesPerMinute,
		}),
		runtimeInputLimits: map[agents.Destination]int{
			agents.DestinationHermes:   cfg.Agent.Hermes.MaxRuntimeInputChars,
			agents.DestinationOpenClaw: cfg.Agent.OpenClaw.MaxRuntimeInputChars,
		},
		runtimeErrorCooldowns: newAgentRuntimeErrorCooldowns(map[agents.Destination]agentRuntimeErrorCooldownPolicy{
			agents.DestinationHermes: {
				MaxErrors: cfg.Agent.Hermes.MaxRuntimeErrorsBeforeCooldown,
				Cooldown:  time.Duration(cfg.Agent.Hermes.RuntimeErrorCooldownMS) * time.Millisecond,
			},
			agents.DestinationOpenClaw: {
				MaxErrors: cfg.Agent.OpenClaw.MaxRuntimeErrorsBeforeCooldown,
				Cooldown:  time.Duration(cfg.Agent.OpenClaw.RuntimeErrorCooldownMS) * time.Millisecond,
			},
		}),
		hermesEnabled:   cfg.Agent.Hermes.Enabled,
		openClawEnabled: cfg.Agent.OpenClaw.Enabled,
	}
}

func (r agentRuntimeRouter) RouteAgentTurn(ctx context.Context, request session.AgentRuntimeRequest) (session.AgentRuntimeResult, error) {
	status, err := r.modes.GetDeviceMode(ctx, request.DeviceID)
	if err != nil {
		return session.AgentRuntimeResult{}, err
	}
	destination, ok := r.destinationForMode(status.ActiveMode)
	if !ok {
		return session.AgentRuntimeResult{}, nil
	}
	if r.runtimeRouteLimits != nil && !r.runtimeRouteLimits.Allow(destination, request.DeviceID, time.Now()) {
		return agentRuntimeSkippedResult(status.ActiveMode, destination, agents.RuntimeStatusReasonRuntimeRateLimited), nil
	}
	if r.runtimeErrorCooldowns != nil && !r.runtimeErrorCooldowns.Allow(destination, request.DeviceID, time.Now()) {
		return agentRuntimeSkippedResult(status.ActiveMode, destination, agents.RuntimeStatusReasonRuntimeErrorCooldown), nil
	}
	if !agentRuntimeInputAllowed(r.runtimeInputLimits[destination], request.Transcript) {
		return agentRuntimeSkippedResult(status.ActiveMode, destination, agents.RuntimeStatusReasonRuntimeInputTooLong), nil
	}
	response, err := r.router.Route(ctx, agents.RouteRequest{
		Mode:        status.ActiveMode,
		Destination: destination,
		Text:        request.Transcript,
		DeviceID:    request.DeviceID,
		SessionID:   request.SessionID,
		TurnID:      fmt.Sprintf("%d", request.Generation),
	})
	if err != nil {
		if r.runtimeErrorCooldowns != nil {
			r.runtimeErrorCooldowns.RecordFailure(destination, request.DeviceID, time.Now())
		}
		return session.AgentRuntimeResult{}, err
	}
	if r.runtimeErrorCooldowns != nil {
		r.runtimeErrorCooldowns.RecordSuccess(destination, request.DeviceID)
	}
	response.Text = strings.TrimSpace(response.Text)
	if response.Text == "" && len(response.ToolCalls) == 0 {
		return session.AgentRuntimeResult{}, nil
	}
	return session.AgentRuntimeResult{
		Handled:     true,
		Mode:        string(status.ActiveMode),
		Destination: string(destination),
		Text:        response.Text,
		ToolCalls:   response.ToolCalls,
	}, nil
}

func agentRuntimeSkippedResult(mode agents.Mode, destination agents.Destination, reason string) session.AgentRuntimeResult {
	return session.AgentRuntimeResult{
		Handled:     false,
		Mode:        string(mode),
		Destination: string(destination),
		SkipReason:  strings.TrimSpace(reason),
	}
}

func agentRuntimeInputAllowed(limit int, transcript string) bool {
	if limit <= 0 {
		return true
	}
	return len([]rune(strings.TrimSpace(transcript))) <= limit
}

func (r agentRuntimeRouter) RuntimePolicyStatus(_ context.Context, deviceID string, bridge string) agents.RuntimePolicyStatus {
	destination, ok := agentRuntimeDestinationForBridge(bridge)
	if !ok {
		return agents.RuntimePolicyStatus{
			Available: true,
			Reason:    agents.RuntimeStatusReasonAvailable,
		}
	}
	now := time.Now()
	if r.runtimeRouteLimits != nil && !r.runtimeRouteLimits.Available(destination, deviceID, now) {
		return agents.RuntimePolicyStatus{
			Available: false,
			Reason:    agents.RuntimeStatusReasonRuntimeRateLimited,
		}
	}
	if r.runtimeErrorCooldowns != nil && !r.runtimeErrorCooldowns.Available(destination, deviceID, now) {
		return agents.RuntimePolicyStatus{
			Available: false,
			Reason:    agents.RuntimeStatusReasonRuntimeErrorCooldown,
		}
	}
	return agents.RuntimePolicyStatus{
		Available: true,
		Reason:    agents.RuntimeStatusReasonAvailable,
	}
}

func agentRuntimeDestinationForBridge(bridge string) (agents.Destination, bool) {
	switch strings.ToLower(strings.TrimSpace(bridge)) {
	case agents.BridgeHermes:
		return agents.DestinationHermes, true
	case agents.BridgeOpenClaw:
		return agents.DestinationOpenClaw, true
	default:
		return "", false
	}
}

func (r agentRuntimeRouter) destinationForMode(mode agents.Mode) (agents.Destination, bool) {
	switch mode {
	case agents.ModeRoleplay:
		if r.hermesEnabled {
			return agents.DestinationHermes, true
		}
	case agents.ModeTool:
		if r.openClawEnabled {
			return agents.DestinationOpenClaw, true
		}
	}
	return "", false
}

type agentRuntimeErrorCooldownPolicy struct {
	MaxErrors int
	Cooldown  time.Duration
}

type agentRuntimeErrorCooldownState struct {
	consecutiveErrors int
	cooldownUntil     time.Time
}

type agentRuntimeErrorCooldowns struct {
	mu       sync.Mutex
	policies map[agents.Destination]agentRuntimeErrorCooldownPolicy
	states   map[string]agentRuntimeErrorCooldownState
}

func newAgentRuntimeErrorCooldowns(policies map[agents.Destination]agentRuntimeErrorCooldownPolicy) *agentRuntimeErrorCooldowns {
	normalized := make(map[agents.Destination]agentRuntimeErrorCooldownPolicy, len(policies))
	for destination, policy := range policies {
		if policy.MaxErrors > 0 && policy.Cooldown > 0 {
			normalized[destination] = policy
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return &agentRuntimeErrorCooldowns{
		policies: normalized,
		states:   make(map[string]agentRuntimeErrorCooldownState),
	}
}

func (c *agentRuntimeErrorCooldowns) Allow(destination agents.Destination, deviceID string, now time.Time) bool {
	return c.Available(destination, deviceID, now)
}

func (c *agentRuntimeErrorCooldowns) Available(destination agents.Destination, deviceID string, now time.Time) bool {
	if c == nil {
		return true
	}
	if c.policies[destination].MaxErrors <= 0 {
		return true
	}
	key := agentRuntimePolicyKey(destination, deviceID)
	if key == "" {
		return true
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.states[key]
	if state.cooldownUntil.After(now) {
		return false
	}
	return true
}

func (c *agentRuntimeErrorCooldowns) RecordFailure(destination agents.Destination, deviceID string, now time.Time) {
	if c == nil {
		return
	}
	policy := c.policies[destination]
	if policy.MaxErrors <= 0 || policy.Cooldown <= 0 {
		return
	}
	key := agentRuntimePolicyKey(destination, deviceID)
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.states[key]
	if state.cooldownUntil.After(now) {
		c.states[key] = state
		return
	}
	state.consecutiveErrors++
	if state.consecutiveErrors >= policy.MaxErrors {
		state.consecutiveErrors = 0
		state.cooldownUntil = now.Add(policy.Cooldown)
	}
	c.states[key] = state
}

func (c *agentRuntimeErrorCooldowns) RecordSuccess(destination agents.Destination, deviceID string) {
	if c == nil {
		return
	}
	if c.policies[destination].MaxErrors <= 0 {
		return
	}
	key := agentRuntimePolicyKey(destination, deviceID)
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.states, key)
}

type agentRuntimeRouteLimiter struct {
	mu     sync.Mutex
	limits map[agents.Destination]int
	seen   map[string][]time.Time
}

func newAgentRuntimeRouteLimiter(limits map[agents.Destination]int) *agentRuntimeRouteLimiter {
	normalized := make(map[agents.Destination]int, len(limits))
	for destination, limit := range limits {
		if limit > 0 {
			normalized[destination] = limit
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return &agentRuntimeRouteLimiter{
		limits: normalized,
		seen:   make(map[string][]time.Time),
	}
}

func (l *agentRuntimeRouteLimiter) Allow(destination agents.Destination, deviceID string, now time.Time) bool {
	if l == nil {
		return true
	}
	limit := l.limits[destination]
	if limit <= 0 {
		return true
	}
	key := agentRuntimePolicyKey(destination, deviceID)
	if key == "" {
		return true
	}
	cutoff := now.Add(-time.Minute)

	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.pruneLocked(key, cutoff)
	if len(kept) >= limit {
		l.seen[key] = kept
		return false
	}
	l.seen[key] = append(kept, now)
	return true
}

func (l *agentRuntimeRouteLimiter) Available(destination agents.Destination, deviceID string, now time.Time) bool {
	if l == nil {
		return true
	}
	limit := l.limits[destination]
	if limit <= 0 {
		return true
	}
	key := agentRuntimePolicyKey(destination, deviceID)
	if key == "" {
		return true
	}
	cutoff := now.Add(-time.Minute)

	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.pruneLocked(key, cutoff)
	return len(kept) < limit
}

func (l *agentRuntimeRouteLimiter) pruneLocked(key string, cutoff time.Time) []time.Time {
	previous := l.seen[key]
	kept := previous[:0]
	for _, at := range previous {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	l.seen[key] = kept
	return kept
}

func agentRuntimePolicyKey(destination agents.Destination, deviceID string) string {
	destinationKey := strings.TrimSpace(string(destination))
	deviceKey := strings.TrimSpace(deviceID)
	if destinationKey == "" && deviceKey == "" {
		return ""
	}
	return destinationKey + "\x00" + deviceKey
}

func configuredDeviceIDs(devices []gatewayconfig.DeviceConfig) []string {
	out := make([]string, 0, len(devices))
	for _, device := range devices {
		out = append(out, device.DeviceID)
	}
	return out
}

func newAgentServiceTools(cfg *gatewayconfig.Config, memoryStore agent.MemoryStore, modeStore agents.ModeReader) (*servicetools.Registry, error) {
	if cfg == nil {
		return nil, nil
	}
	allowedTools := []string{agent.MemoryLookupToolName}
	allowedPermissions := []string{servicetools.PermissionRead}
	if cfg.Agent.V21.Enabled {
		allowedTools = append(allowedTools, agents.V21VoiceQueryToolName)
		allowedPermissions = append(allowedPermissions, servicetools.PermissionExternal)
	}
	if cfg.Tools.HomeAssistant.Enabled {
		allowedTools = append(allowedTools, homeassistant.GetStateToolName)
		allowedPermissions = append(allowedPermissions, servicetools.PermissionExternal)
		if len(cfg.Tools.HomeAssistant.AllowedActions) > 0 {
			allowedTools = append(allowedTools, homeassistant.CallActionToolName)
			allowedPermissions = append(allowedPermissions, servicetools.PermissionWrite)
		}
	}
	if cfg.Tools.Search.Enabled {
		allowedTools = append(allowedTools, search.WebSearchToolName)
		allowedPermissions = append(allowedPermissions, servicetools.PermissionExternal)
	}
	if cfg.Tools.Feishu.Enabled {
		allowedTools = append(allowedTools, feishu.ListTargetsToolName, feishu.SendTextToolName)
		allowedPermissions = append(allowedPermissions, servicetools.PermissionRead, servicetools.PermissionWrite)
	}
	if cfg.Tools.Camera.Enabled {
		allowedTools = append(allowedTools, camera.RequestCaptureToolName)
		allowedPermissions = append(allowedPermissions, servicetools.PermissionDeviceControl)
	}
	if cfg.Tools.Reminder.Enabled {
		allowedTools = append(allowedTools, reminder.AnnounceToolName)
		allowedPermissions = append(allowedPermissions, servicetools.PermissionDeviceControl)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       allowedTools,
		AllowedPermissions: allowedPermissions,
		DefaultTimeout:     serviceToolTimeout(cfg),
	})
	if memoryStore != nil {
		if err := agent.RegisterMemoryLookupTool(registry, agent.MemoryLookupToolOptions{
			Store:        memoryStore,
			OwnerUserID:  "owner",
			DefaultLimit: cfg.Agent.MemoryMaxItems,
		}); err != nil {
			return nil, err
		}
	}
	if cfg.Tools.HomeAssistant.Enabled {
		haClient, err := homeassistant.NewClient(homeassistant.ClientOptions{
			BaseURL: cfg.Tools.HomeAssistant.BaseURL,
			Token:   os.Getenv(cfg.Tools.HomeAssistant.TokenEnv),
		})
		if err != nil {
			return nil, err
		}
		if err := homeassistant.RegisterGetStateTool(registry, homeassistant.GetStateToolOptions{
			Client:          haClient,
			AllowedEntities: cfg.Tools.HomeAssistant.AllowedEntities,
		}); err != nil {
			return nil, err
		}
		if len(cfg.Tools.HomeAssistant.AllowedActions) > 0 {
			if err := homeassistant.RegisterCallActionTool(registry, homeassistant.CallActionToolOptions{
				Client:  haClient,
				Actions: homeAssistantActionsFromConfig(cfg.Tools.HomeAssistant.AllowedActions),
			}); err != nil {
				return nil, err
			}
		}
	}
	if cfg.Tools.Search.Enabled {
		searchClient, err := search.NewClient(search.ClientOptions{
			BaseURL: os.Getenv(cfg.Tools.Search.BaseURLEnv),
			Token:   os.Getenv(cfg.Tools.Search.TokenEnv),
			Client:  &http.Client{Timeout: searchToolTimeout(cfg)},
		})
		if err != nil {
			return nil, err
		}
		if err := search.RegisterWebSearchTool(registry, search.WebSearchToolOptions{
			Client:         searchClient,
			MaxResults:     cfg.Tools.Search.MaxResults,
			MaxQueryRunes:  cfg.Tools.Search.MaxQueryChars,
			AllowedDomains: cfg.Tools.Search.AllowedDomains,
		}); err != nil {
			return nil, err
		}
	}
	if cfg.Tools.Feishu.Enabled {
		feishuClient, err := feishu.NewClient(feishu.ClientOptions{
			BaseURL:   cfg.Tools.Feishu.BaseURL,
			AppID:     os.Getenv(cfg.Tools.Feishu.AppIDEnv),
			AppSecret: os.Getenv(cfg.Tools.Feishu.AppSecretEnv),
			Client:    &http.Client{Timeout: feishuToolTimeout(cfg)},
		})
		if err != nil {
			return nil, err
		}
		if err := feishu.RegisterServiceTools(registry, feishu.ServiceToolOptions{
			Client:              feishuClient,
			Targets:             feishuTargetsFromConfig(cfg.Tools.Feishu.AllowedTargets),
			MaxTextRunes:        cfg.Tools.Feishu.MaxTextChars,
			RequireConfirmation: true,
		}); err != nil {
			return nil, err
		}
	}
	if cfg.Tools.Camera.Enabled {
		if err := camera.RegisterRequestCaptureTool(registry, camera.RequestCaptureToolOptions{
			MaxReasonRunes: cfg.Tools.Camera.MaxReasonChars,
		}); err != nil {
			return nil, err
		}
	}
	if cfg.Tools.Reminder.Enabled {
		if err := reminder.RegisterAnnounceTool(registry, reminder.AnnounceToolOptions{
			MaxTitleRunes:   cfg.Tools.Reminder.MaxTitleChars,
			MaxMessageRunes: cfg.Tools.Reminder.MaxMessageChars,
		}); err != nil {
			return nil, err
		}
	}
	if cfg.Agent.V21.Enabled {
		if modeStore == nil {
			return nil, fmt.Errorf("agent mode store is required when V21 bridge is enabled")
		}
		v21BaseURL := os.Getenv(cfg.Agent.V21.BaseURLEnv)
		v21Router := agents.NewRouter(agents.RouterOptions{
			V21: agents.NewV21Client(agents.V21ClientOptions{
				BaseURL:        v21BaseURL,
				Token:          os.Getenv(cfg.Agent.V21.TokenEnv),
				MaxSpokenChars: cfg.Agent.V21.MaxSpokenChars,
				Client:         &http.Client{Timeout: agentV21Timeout(cfg)},
			}),
		})
		if err := agents.RegisterV21VoiceQueryTool(registry, agents.V21VoiceQueryToolOptions{
			Router:               v21Router,
			Modes:                modeStore,
			AllowedCollectionIDs: cfg.Agent.V21.AllowedCollectionIDs,
		}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func homeAssistantActionsFromConfig(actions []gatewayconfig.HomeAssistantActionConfig) []homeassistant.ActionConfig {
	out := make([]homeassistant.ActionConfig, 0, len(actions))
	for _, action := range actions {
		out = append(out, homeassistant.ActionConfig{
			ActionID:    action.ActionID,
			Description: action.Description,
			Domain:      action.Domain,
			Service:     action.Service,
			EntityIDs:   action.EntityIDs,
			Data:        action.Data,
			Slots:       homeAssistantActionSlotsFromConfig(action.Slots),
		})
	}
	return out
}

func homeAssistantActionSlotsFromConfig(slots []gatewayconfig.HomeAssistantActionSlotConfig) []homeassistant.ActionSlotConfig {
	out := make([]homeassistant.ActionSlotConfig, 0, len(slots))
	for _, slot := range slots {
		out = append(out, homeassistant.ActionSlotConfig{
			Name:        slot.Name,
			Description: slot.Description,
			Type:        slot.Type,
			Enum:        slot.Enum,
			Min:         slot.Min,
			Max:         slot.Max,
			MaxChars:    slot.MaxChars,
		})
	}
	return out
}

func feishuTargetsFromConfig(targets []gatewayconfig.FeishuTargetConfig) []feishu.TargetConfig {
	out := make([]feishu.TargetConfig, 0, len(targets))
	for _, target := range targets {
		out = append(out, feishu.TargetConfig{
			TargetID:      target.TargetID,
			Description:   target.Description,
			ReceiveIDType: target.ReceiveIDType,
			ReceiveID:     os.Getenv(target.ReceiveIDEnv),
		})
	}
	return out
}

func serviceToolTimeout(cfg *gatewayconfig.Config) time.Duration {
	if cfg == nil {
		return 0
	}
	timeoutMS := cfg.Tools.HomeAssistant.TimeoutMS
	if cfg.Agent.V21.Enabled && cfg.Agent.V21.TimeoutMS > timeoutMS {
		timeoutMS = cfg.Agent.V21.TimeoutMS
	}
	if cfg.Tools.Search.Enabled && cfg.Tools.Search.TimeoutMS > timeoutMS {
		timeoutMS = cfg.Tools.Search.TimeoutMS
	}
	if cfg.Tools.Feishu.Enabled && cfg.Tools.Feishu.TimeoutMS > timeoutMS {
		timeoutMS = cfg.Tools.Feishu.TimeoutMS
	}
	if timeoutMS <= 0 {
		return 0
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func searchToolTimeout(cfg *gatewayconfig.Config) time.Duration {
	if cfg == nil || cfg.Tools.Search.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(cfg.Tools.Search.TimeoutMS) * time.Millisecond
}

func feishuToolTimeout(cfg *gatewayconfig.Config) time.Duration {
	if cfg == nil || cfg.Tools.Feishu.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(cfg.Tools.Feishu.TimeoutMS) * time.Millisecond
}

func agentV21Timeout(cfg *gatewayconfig.Config) time.Duration {
	if cfg == nil || cfg.Agent.V21.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(cfg.Agent.V21.TimeoutMS) * time.Millisecond
}

func agentHermesTimeout(cfg *gatewayconfig.Config) time.Duration {
	if cfg == nil || cfg.Agent.Hermes.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(cfg.Agent.Hermes.TimeoutMS) * time.Millisecond
}

func agentOpenClawTimeout(cfg *gatewayconfig.Config) time.Duration {
	if cfg == nil || cfg.Agent.OpenClaw.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(cfg.Agent.OpenClaw.TimeoutMS) * time.Millisecond
}

func resolveGatewayRuntimePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	return filepath.Join(findGoModuleRoot(cwd), path)
}

func findGoModuleRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

func newGatewayReadinessCheck(cfg *gatewayconfig.Config, registry *providers.Registry) httpapi.ReadyCheck {
	return func(context.Context) httpapi.ReadyStatus {
		checks := map[string]string{
			"config": "ok",
		}
		if err := validateDefaultVoiceProfile(cfg, registry); err != nil {
			checks["providers"] = readinessErrorCode(err)
			return httpapi.ReadyStatus{
				Ready:  false,
				Checks: checks,
			}
		}
		checks["providers"] = "ok"
		return httpapi.ReadyStatus{
			Ready:  true,
			Checks: checks,
		}
	}
}

func validateDefaultVoiceProfile(cfg *gatewayconfig.Config, registry *providers.Registry) error {
	if cfg == nil {
		return fmt.Errorf("gateway config is not loaded")
	}
	if registry == nil {
		return fmt.Errorf("provider registry is not configured")
	}
	profile, ok := cfg.Providers.Profiles[cfg.Providers.DefaultProfile]
	if !ok {
		return fmt.Errorf("%w: default provider profile %s", providers.ErrProviderNotFound, cfg.Providers.DefaultProfile)
	}
	if err := validateASRProvider(registry, profile.ASR); err != nil {
		return fmt.Errorf("default asr provider: %w", err)
	}
	if err := validateLLMProvider(registry, profile.LLM); err != nil {
		return fmt.Errorf("default llm provider: %w", err)
	}
	if err := validateTTSProvider(registry, profile.TTS); err != nil {
		return fmt.Errorf("default tts provider: %w", err)
	}
	return nil
}

func validateASRProvider(registry *providers.Registry, name string) error {
	provider, err := registry.ASRProvider(name)
	if err != nil {
		return err
	}
	return providers.ValidateProviderConfig(provider)
}

func validateLLMProvider(registry *providers.Registry, name string) error {
	provider, err := registry.LLMProvider(name)
	if err != nil {
		return err
	}
	return providers.ValidateProviderConfig(provider)
}

func validateTTSProvider(registry *providers.Registry, name string) error {
	provider, err := registry.TTSProvider(name)
	if err != nil {
		return err
	}
	return providers.ValidateProviderConfig(provider)
}

func readinessErrorCode(err error) string {
	switch {
	case errors.Is(err, providers.ErrProviderConfiguration):
		return "provider_config_error"
	case errors.Is(err, providers.ErrProviderNotFound):
		return "provider_not_found"
	default:
		return "gateway_error"
	}
}

func handleGatewayText(ctx context.Context, loop *session.VoiceLoop, data []byte, onAsyncError func(error)) error {
	message, err := xiaozhi.ParseClientMessage(data)
	if err != nil {
		return err
	}
	switch message.Type {
	case xiaozhi.MessageTypeHello:
		_, err := loop.HandleHelloWithFeatures(ctx, message.Hello.Features)
		return err
	case xiaozhi.MessageTypeListen:
		if message.UnsupportedForP0 {
			return fmt.Errorf("realtime listen mode is not supported in P0")
		}
		switch message.Listen.State {
		case "start":
			_, err := loop.HandleListenStart(ctx)
			return err
		case "stop":
			go func() {
				if err := loop.HandleListenStop(ctx); err != nil && onAsyncError != nil {
					onAsyncError(err)
				}
			}()
			return nil
		case "detect":
			return nil
		default:
			return fmt.Errorf("unsupported listen state")
		}
	case xiaozhi.MessageTypeAbort:
		_, err := loop.HandleAbort(ctx)
		return err
	case xiaozhi.MessageTypeMCP:
		return loop.HandleMCPPayload(message.MCP.Payload)
	default:
		return fmt.Errorf("unsupported xiaozhi message")
	}
}

func gatewayErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var protocolError *xiaozhi.ProtocolError
	if errors.As(err, &protocolError) {
		return protocolError.Code
	}
	if errors.Is(err, providers.ErrProviderNotFound) {
		return "provider_not_found"
	}
	if errors.Is(err, providers.ErrProviderConfiguration) {
		return "provider_config_error"
	}
	return "gateway_error"
}

func authorizationMatchesToken(authorization, token string) bool {
	if token == "" {
		return false
	}

	authorization = strings.TrimSpace(authorization)
	if subtle.ConstantTimeCompare([]byte(authorization), []byte(token)) == 1 {
		return true
	}

	const bearerPrefix = "Bearer "
	if len(authorization) <= len(bearerPrefix) || !strings.EqualFold(authorization[:len(bearerPrefix)], bearerPrefix) {
		return false
	}

	bearerToken := strings.TrimSpace(authorization[len(bearerPrefix):])
	return subtle.ConstantTimeCompare([]byte(bearerToken), []byte(token)) == 1
}
