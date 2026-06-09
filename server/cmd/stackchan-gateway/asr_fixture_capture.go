package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/app"
	"stackchan-gateway/internal/audio"
	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/httpapi"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providerprobe"
)

type asrFixtureCaptureOptions struct {
	Config                 *gatewayconfig.Config
	Path                   string
	OutputPath             string
	MaxFrames              int
	Timeout                time.Duration
	Progress               io.Writer
	ProgressEveryFrames    int
	RequireSemanticFixture bool
	AdvertiseURL           string
}

type asrFixtureCaptureResult struct {
	ListenAddr        string
	OutputPath        string
	Frames            int
	Bytes             int
	DurationMS        int
	UniquePayloads    int
	ListenStartCount  int
	ListenStopCount   int
	ListenDetectCount int
}

func runASRFixtureCapture(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("asr-fixture-capture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config")
	listenAddr := flags.String("listen", "", "listen address override")
	websocketPath := flags.String("path", "", "websocket path override")
	advertiseURL := flags.String("advertise-url", "", "client-facing ws:// or wss:// URL to print in the ready line")
	outputPath := flags.String("output", "./var/fixtures/asr/spoken-opus.json", "ASR xiaozhi Opus fixture output path")
	maxFrames := flags.Int("max-frames", 200, "maximum binary Opus frames to capture")
	timeoutMS := flags.Int("timeout-ms", 30000, "capture timeout in milliseconds")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*outputPath) == "" {
		fmt.Fprintln(stderr, "asr-fixture-capture failed: --output is required")
		return 2
	}
	if *maxFrames <= 0 {
		fmt.Fprintln(stderr, "asr-fixture-capture failed: --max-frames must be positive")
		return 2
	}
	if *timeoutMS <= 0 {
		fmt.Fprintln(stderr, "asr-fixture-capture failed: --timeout-ms must be positive")
		return 2
	}
	if err := ensureASRFixtureCaptureOutputIgnored(*outputPath); err != nil {
		fmt.Fprintf(stderr, "asr-fixture-capture failed: %v\n", err)
		return 1
	}

	cfg, err := gatewayconfig.LoadFileWithValidationOptions(*configPath, gatewayconfig.OSLookupEnv, gatewayconfig.ValidationOptions{
		SkipAdminAuth: true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "asr-fixture-capture failed: %v\n", err)
		return 1
	}

	addr := strings.TrimSpace(*listenAddr)
	if addr == "" {
		addr = cfg.Server.ListenAddr
	}
	path := strings.TrimSpace(*websocketPath)
	if path == "" {
		path = cfg.Server.WebsocketPath
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "asr-fixture-capture failed: listen %s: %v\n", addr, err)
		return 1
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := captureASRFixtureFromListener(ctx, listener, asrFixtureCaptureOptions{
		Config:                 cfg,
		Path:                   path,
		OutputPath:             *outputPath,
		MaxFrames:              *maxFrames,
		Timeout:                time.Duration(*timeoutMS) * time.Millisecond,
		Progress:               stderr,
		ProgressEveryFrames:    10,
		RequireSemanticFixture: true,
		AdvertiseURL:           *advertiseURL,
	})
	if err != nil {
		fmt.Fprintf(stderr, "asr-fixture-capture failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "asr_fixture=%s frames=%d bytes=%d duration_ms=%d unique_payloads=%d listen_start=%d listen_stop=%d listen_detect=%d listen=%s\n", result.OutputPath, result.Frames, result.Bytes, result.DurationMS, result.UniquePayloads, result.ListenStartCount, result.ListenStopCount, result.ListenDetectCount, result.ListenAddr)
	return 0
}

func captureASRFixtureFromListener(ctx context.Context, listener net.Listener, options asrFixtureCaptureOptions) (asrFixtureCaptureResult, error) {
	if listener == nil {
		return asrFixtureCaptureResult{}, fmt.Errorf("listener is required")
	}
	if options.Config == nil {
		return asrFixtureCaptureResult{}, fmt.Errorf("config is required")
	}
	if options.MaxFrames <= 0 {
		return asrFixtureCaptureResult{}, fmt.Errorf("max frames must be positive")
	}
	if options.Timeout <= 0 {
		return asrFixtureCaptureResult{}, fmt.Errorf("timeout must be positive")
	}
	progressEveryFrames := options.ProgressEveryFrames
	if progressEveryFrames <= 0 {
		progressEveryFrames = 10
	}

	path := strings.TrimSpace(options.Path)
	if path == "" {
		path = "/xiaozhi/v1/ws"
	}
	connectURL, err := resolveASRFixtureAdvertiseURL(listener.Addr().String(), path, options.AdvertiseURL)
	if err != nil {
		return asrFixtureCaptureResult{}, err
	}
	if connectURL == "" {
		connectURL = "unadvertised"
	}

	captureCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()

	var mu sync.Mutex
	var progressMu sync.Mutex
	var transports []*xiaozhi.Transport
	var frames [][]byte
	var totalBytes int
	var listenStartCount int
	var listenStopCount int
	var listenDetectCount int
	var captureErr error
	done := make(chan struct{})
	var doneOnce sync.Once

	finish := func() {
		doneOnce.Do(func() {
			close(done)
		})
	}
	setCaptureErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if captureErr == nil {
			captureErr = err
		}
	}
	writeProgress := func(format string, args ...any) {
		if options.Progress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		fmt.Fprintf(options.Progress, format, args...)
	}
	closeTransports := func() {
		mu.Lock()
		current := append([]*xiaozhi.Transport(nil), transports...)
		mu.Unlock()
		for _, transport := range current {
			_ = transport.Close(websocket.CloseNormalClosure, "fixture capture finished")
		}
	}

	handler := xiaozhi.NewWebSocketHandler(xiaozhi.WebSocketHandlerOptions{
		Authenticator: app.AuthenticatorForConfig(options.Config),
		OnAuthFailure: func(failure xiaozhi.AuthFailure) {
			writeProgress("asr-fixture-capture auth-failed: status=%d code=%s has_authorization=%t has_protocol_version=%t has_device_id=%t has_client_id=%t\n", failure.Status, failure.Code, failure.HasAuthorization, failure.HasProtocolVersion, failure.HasDeviceID, failure.HasClientID)
		},
		OnConnect: func(transport *xiaozhi.Transport) {
			mu.Lock()
			transports = append(transports, transport)
			mu.Unlock()
			writeProgress("asr-fixture-capture connected: path=%s listen=%s\n", path, listener.Addr().String())
			_ = transport.SendJSON(context.Background(), xiaozhi.NewServerHello("asr-fixture-capture"))
		},
		OnText: func(transport *xiaozhi.Transport, data []byte) {
			textEvent, err := inspectASRFixtureCaptureText(data)
			if err != nil {
				setCaptureErr(err)
				_ = transport.Close(websocket.CloseUnsupportedData, "invalid xiaozhi text message")
				finish()
				return
			}
			if textEvent.listenStart || textEvent.listenStop || textEvent.listenDetect {
				mu.Lock()
				if textEvent.listenStart {
					listenStartCount++
				}
				if textEvent.listenStop {
					listenStopCount++
				}
				if textEvent.listenDetect {
					listenDetectCount++
				}
				starts := listenStartCount
				stops := listenStopCount
				detects := listenDetectCount
				mu.Unlock()
				writeProgress("asr-fixture-capture listen: start=%d stop=%d detect=%d\n", starts, stops, detects)
			}
		},
		OnBinary: func(transport *xiaozhi.Transport, frame audio.Frame) {
			if frame.Format != audio.FormatOpus {
				setCaptureErr(fmt.Errorf("captured audio frame format %q, want opus", frame.Format))
				_ = transport.Close(websocket.CloseUnsupportedData, "invalid audio format")
				finish()
				return
			}
			if frame.SampleRateHz != xiaozhi.XiaozhiUplinkSampleRateHz || frame.FrameDurationMS != xiaozhi.XiaozhiFrameDurationMS {
				setCaptureErr(fmt.Errorf("captured audio params %d Hz/%d ms, want 16000 Hz/60 ms", frame.SampleRateHz, frame.FrameDurationMS))
				_ = transport.Close(websocket.CloseUnsupportedData, "invalid audio params")
				finish()
				return
			}

			payload := append([]byte(nil), frame.Payload...)
			mu.Lock()
			frames = append(frames, payload)
			totalBytes += len(payload)
			reachedLimit := len(frames) >= options.MaxFrames
			frameCount := len(frames)
			byteCount := totalBytes
			mu.Unlock()

			if frameCount == 1 || frameCount%progressEveryFrames == 0 || reachedLimit {
				writeProgress("asr-fixture-capture progress: frames=%d bytes=%d\n", frameCount, byteCount)
			}
			if reachedLimit {
				_ = transport.Close(websocket.CloseNormalClosure, "captured enough frames")
				finish()
			}
		},
		OnError: func(_ *xiaozhi.Transport, err error) {
			mu.Lock()
			frameCount := len(frames)
			mu.Unlock()
			if frameCount == 0 || isCaptureProtocolError(err) {
				setCaptureErr(fmt.Errorf("xiaozhi websocket capture failed: %w", err))
			}
			finish()
		},
	})

	server := &http.Server{
		Handler: httpapi.NewRouter(httpapi.RouterOptions{
			WebSocketPath:    path,
			WebSocketHandler: handler,
		}),
	}
	serverErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()
	deviceID, clientID, authEnv := asrFixtureCaptureDeviceHints(options.Config)
	writeProgress("asr-fixture-capture ready: listen=%s path=%s connect_url=%s device_id=%s client_id=%s auth_env=%s output=%s max_frames=%d timeout_ms=%d\n", listener.Addr().String(), path, connectURL, deviceID, clientID, authEnv, options.OutputPath, options.MaxFrames, options.Timeout.Milliseconds())

	select {
	case <-done:
	case err := <-serverErr:
		if err != nil {
			setCaptureErr(err)
		}
	case <-captureCtx.Done():
	}

	closeTransports()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)

	mu.Lock()
	copiedFrames := make([][]byte, len(frames))
	for index := range frames {
		copiedFrames[index] = append([]byte(nil), frames[index]...)
	}
	err = captureErr
	copiedListenStartCount := listenStartCount
	copiedListenStopCount := listenStopCount
	copiedListenDetectCount := listenDetectCount
	mu.Unlock()
	if err != nil {
		return asrFixtureCaptureResult{}, err
	}
	if len(copiedFrames) == 0 {
		return asrFixtureCaptureResult{}, fmt.Errorf("captured no xiaozhi Opus frames before timeout")
	}
	inspection := providerprobe.InspectASROpusFrames(copiedFrames)
	if options.RequireSemanticFixture {
		validatedInspection, err := providerprobe.ValidateASROpusFramesForSemanticProbe(copiedFrames)
		if err != nil {
			return asrFixtureCaptureResult{}, fmt.Errorf("captured xiaozhi Opus frames are not valid for semantic ASR probes: %w", err)
		}
		inspection = validatedInspection
	}
	if err := providerprobe.WriteASROpusFixture(options.OutputPath, copiedFrames); err != nil {
		return asrFixtureCaptureResult{}, err
	}
	result := asrFixtureCaptureResult{
		ListenAddr:        listener.Addr().String(),
		OutputPath:        options.OutputPath,
		Frames:            inspection.Frames,
		Bytes:             inspection.Bytes,
		DurationMS:        inspection.DurationMS,
		UniquePayloads:    inspection.UniquePayloads,
		ListenStartCount:  copiedListenStartCount,
		ListenStopCount:   copiedListenStopCount,
		ListenDetectCount: copiedListenDetectCount,
	}
	writeProgress("asr-fixture-capture fixture-ready: frames=%d bytes=%d duration_ms=%d unique_payloads=%d listen_start=%d listen_stop=%d listen_detect=%d output=%s\n", result.Frames, result.Bytes, result.DurationMS, result.UniquePayloads, result.ListenStartCount, result.ListenStopCount, result.ListenDetectCount, result.OutputPath)
	return result, nil
}

type asrFixtureCaptureTextEvent struct {
	listenStart  bool
	listenStop   bool
	listenDetect bool
}

func inspectASRFixtureCaptureText(data []byte) (asrFixtureCaptureTextEvent, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return asrFixtureCaptureTextEvent{}, fmt.Errorf("parse xiaozhi text frame: %w", err)
	}
	if envelope.Type == "" {
		return asrFixtureCaptureTextEvent{}, nil
	}
	if envelope.Type != xiaozhi.MessageTypeHello && envelope.Type != xiaozhi.MessageTypeListen {
		return asrFixtureCaptureTextEvent{}, nil
	}
	parsed, err := xiaozhi.ParseClientMessage(data)
	if err != nil {
		return asrFixtureCaptureTextEvent{}, err
	}
	if parsed.Type != xiaozhi.MessageTypeListen || parsed.Listen == nil {
		return asrFixtureCaptureTextEvent{}, nil
	}
	switch parsed.Listen.State {
	case "start":
		return asrFixtureCaptureTextEvent{listenStart: true}, nil
	case "stop":
		return asrFixtureCaptureTextEvent{listenStop: true}, nil
	case "detect":
		return asrFixtureCaptureTextEvent{listenDetect: true}, nil
	default:
		return asrFixtureCaptureTextEvent{}, nil
	}
}

func isCaptureProtocolError(err error) bool {
	return xiaozhi.HasErrorCode(err, xiaozhi.ErrorCodeEmptyAudioFrame) ||
		xiaozhi.HasErrorCode(err, xiaozhi.ErrorCodeUnsupportedBinaryProtocol)
}

func asrFixtureCaptureDeviceHints(cfg *gatewayconfig.Config) (string, string, string) {
	deviceID := "unconfigured"
	clientID := "unconfigured"
	authEnv := "unconfigured"
	if cfg == nil || len(cfg.Devices) == 0 {
		return deviceID, clientID, authEnv
	}

	device := cfg.Devices[0]
	if value := strings.TrimSpace(device.DeviceID); value != "" {
		deviceID = value
	}
	if value := strings.TrimSpace(device.ClientID); value != "" {
		clientID = value
	}
	if value := strings.TrimSpace(device.AuthTokenEnv); value != "" {
		authEnv = value
	}
	return deviceID, clientID, authEnv
}

func resolveASRFixtureAdvertiseURL(listenerAddr string, path string, advertiseURL string) (string, error) {
	advertised := strings.TrimSpace(advertiseURL)
	expectedPath := normalizeASRFixturePath(path)
	if advertised != "" {
		parsed, err := url.Parse(advertised)
		if err != nil {
			return "", fmt.Errorf("parse advertise URL: %w", err)
		}
		if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
			return "", fmt.Errorf("advertise URL scheme must be ws or wss")
		}
		if parsed.Host == "" {
			return "", fmt.Errorf("advertise URL host is required")
		}
		if parsed.Path == "" {
			return "", fmt.Errorf("advertise URL path is required")
		}
		if parsed.Path != expectedPath {
			return "", fmt.Errorf("advertise URL path %q must match capture path %q", parsed.Path, expectedPath)
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("advertise URL must not include user info, query parameters or fragments")
		}
		return parsed.String(), nil
	}

	host, port, err := net.SplitHostPort(listenerAddr)
	if err != nil || port == "" {
		return "", nil
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "", fmt.Errorf("advertise URL is required when listener address is %s; pass --advertise-url ws://<reachable-host>:%s%s", listenerAddr, port, expectedPath)
	}
	return "ws://" + net.JoinHostPort(host, port) + expectedPath, nil
}

func normalizeASRFixturePath(path string) string {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "/xiaozhi/v1/ws"
	}
	if !strings.HasPrefix(cleanPath, "/") {
		return "/" + cleanPath
	}
	return cleanPath
}

func ensureASRFixtureCaptureOutputIgnored(outputPath string) error {
	path := strings.TrimSpace(outputPath)
	if path == "" {
		return fmt.Errorf("output path is required")
	}
	cmd := exec.Command("git", "check-ignore", "--no-index", "-q", "--", path)
	if err := cmd.Run(); err == nil {
		return nil
	}
	if isAllowedPersistentASRFixturePath(path) {
		return nil
	}
	return fmt.Errorf("output path is not ignored by git and is not an allowed runtime fixture path: %s; keep spoken ASR fixtures under server/var/fixtures/asr/ or /var/lib/a21-air/fixtures/asr/ before capture", path)
}

func isAllowedPersistentASRFixturePath(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if normalized == "." || normalized == "" || !strings.HasSuffix(normalized, ".json") {
		return false
	}
	if strings.Contains(normalized, "/../") || strings.HasPrefix(normalized, "../") || strings.HasSuffix(normalized, "/..") {
		return false
	}
	allowedPrefixes := []string{
		"/var/lib/a21-air/fixtures/asr/",
		"var/fixtures/asr/",
		"server/var/fixtures/asr/",
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(normalized, prefix) && len(normalized) > len(prefix) {
			return true
		}
	}
	return false
}
