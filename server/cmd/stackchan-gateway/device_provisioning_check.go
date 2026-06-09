package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/audio"
	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/httpapi"
	"stackchan-gateway/internal/protocol/xiaozhi"
)

type deviceProvisioningCheckOptions struct {
	Config             *gatewayconfig.Config
	Path               string
	Timeout            time.Duration
	Progress           io.Writer
	AdvertiseURL       string
	ShowDeviceIdentity bool
}

type deviceProvisioningCheckResult struct {
	ListenAddr       string
	ConnectURL       string
	Connected        bool
	AuthFailures     int
	LastAuthStatus   int
	LastAuthCode     string
	ProtocolVersion  int
	HelloSeen        bool
	BinaryFrames     int
	DeviceIDMatch    bool
	ClientIDMatch    bool
	ReadyForCapture  bool
	DeviceIDHash     string
	ClientIDHash     string
	RawDeviceID      string
	RawClientID      string
	ExpectedDeviceID string
	ExpectedClientID string
}

func runDeviceProvisioningCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("device-provisioning-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config")
	listenAddr := flags.String("listen", "", "listen address override")
	websocketPath := flags.String("path", "", "websocket path override")
	advertiseURL := flags.String("advertise-url", "", "client-facing ws:// or wss:// URL to print in the ready line")
	timeoutMS := flags.Int("timeout-ms", 30000, "provisioning check timeout in milliseconds")
	showDeviceIdentity := flags.Bool("show-device-identity", false, "print raw Device-Id and Client-Id after token-valid connection")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *timeoutMS <= 0 {
		fmt.Fprintln(stderr, "device-provisioning-check failed: --timeout-ms must be positive")
		return 2
	}

	cfg, err := gatewayconfig.LoadFileWithValidationOptions(*configPath, gatewayconfig.OSLookupEnv, gatewayconfig.ValidationOptions{
		SkipAdminAuth: true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "device-provisioning-check failed: %v\n", err)
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
		fmt.Fprintf(stderr, "device-provisioning-check failed: listen %s: %v\n", addr, err)
		return 1
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := checkDeviceProvisioningFromListener(ctx, listener, deviceProvisioningCheckOptions{
		Config:             cfg,
		Path:               path,
		Timeout:            time.Duration(*timeoutMS) * time.Millisecond,
		Progress:           stderr,
		AdvertiseURL:       *advertiseURL,
		ShowDeviceIdentity: *showDeviceIdentity,
	})
	if err != nil {
		fmt.Fprintf(stderr, "device-provisioning-check failed: %v\n", err)
	}
	fmt.Fprintf(stdout, "device_provisioning ready_for_capture=%t connected=%t hello=%t device_id_match=%t client_id_match=%t auth_failures=%d binary_frames=%d listen=%s connect_url=%s\n", result.ReadyForCapture, result.Connected, result.HelloSeen, result.DeviceIDMatch, result.ClientIDMatch, result.AuthFailures, result.BinaryFrames, result.ListenAddr, result.ConnectURL)
	if err != nil {
		return 1
	}
	return 0
}

func checkDeviceProvisioningFromListener(ctx context.Context, listener net.Listener, options deviceProvisioningCheckOptions) (deviceProvisioningCheckResult, error) {
	if listener == nil {
		return deviceProvisioningCheckResult{}, fmt.Errorf("listener is required")
	}
	if options.Config == nil {
		return deviceProvisioningCheckResult{}, fmt.Errorf("config is required")
	}
	if len(options.Config.Devices) == 0 {
		return deviceProvisioningCheckResult{}, fmt.Errorf("at least one configured device is required")
	}
	if options.Timeout <= 0 {
		return deviceProvisioningCheckResult{}, fmt.Errorf("timeout must be positive")
	}

	expected := options.Config.Devices[0]
	token, ok := gatewayconfig.OSLookupEnv(expected.AuthTokenEnv)
	if !ok || strings.TrimSpace(token) == "" {
		return deviceProvisioningCheckResult{}, fmt.Errorf("missing required secret env %s", expected.AuthTokenEnv)
	}
	path := strings.TrimSpace(options.Path)
	if path == "" {
		path = "/xiaozhi/v1/ws"
	}
	connectURL, err := resolveASRFixtureAdvertiseURL(listener.Addr().String(), path, options.AdvertiseURL)
	if err != nil {
		return deviceProvisioningCheckResult{}, err
	}
	if connectURL == "" {
		connectURL = "unadvertised"
	}

	checkCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()

	result := deviceProvisioningCheckResult{
		ListenAddr:       listener.Addr().String(),
		ConnectURL:       connectURL,
		ExpectedDeviceID: strings.TrimSpace(expected.DeviceID),
		ExpectedClientID: strings.TrimSpace(expected.ClientID),
	}
	var mu sync.Mutex
	var progressMu sync.Mutex
	var transport *xiaozhi.Transport
	var checkErr error
	done := make(chan struct{})
	var doneOnce sync.Once

	finish := func() {
		doneOnce.Do(func() {
			close(done)
		})
	}
	setCheckErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if checkErr == nil {
			checkErr = err
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

	authenticator := xiaozhi.AuthenticatorFunc(func(_ context.Context, request xiaozhi.AuthRequest) (xiaozhi.AuthResult, error) {
		if !provisioningAuthorizationMatchesToken(request.Authorization, token) {
			return xiaozhi.AuthResult{}, xiaozhi.NewAuthError(http.StatusForbidden, "INVALID_AUTHORIZATION", "authorization does not match configured device token")
		}
		return xiaozhi.AuthResult{
			DeviceID:        request.DeviceID,
			ClientID:        request.ClientID,
			ProtocolVersion: request.ProtocolVersion,
		}, nil
	})

	handler := xiaozhi.NewWebSocketHandler(xiaozhi.WebSocketHandlerOptions{
		Authenticator: authenticator,
		OnAuthFailure: func(failure xiaozhi.AuthFailure) {
			mu.Lock()
			result.AuthFailures++
			result.LastAuthStatus = failure.Status
			result.LastAuthCode = failure.Code
			mu.Unlock()
			writeProgress("device-provisioning-check auth-failed: status=%d code=%s has_authorization=%t has_protocol_version=%t has_device_id=%t has_client_id=%t\n", failure.Status, failure.Code, failure.HasAuthorization, failure.HasProtocolVersion, failure.HasDeviceID, failure.HasClientID)
			finish()
		},
		OnConnect: func(connected *xiaozhi.Transport) {
			mu.Lock()
			transport = connected
			result.Connected = true
			result.ProtocolVersion = connected.ProtocolVersion()
			result.RawDeviceID = connected.DeviceID()
			result.RawClientID = connected.ClientID()
			result.DeviceIDHash = provisioningFingerprint(connected.DeviceID())
			result.ClientIDHash = provisioningFingerprint(connected.ClientID())
			result.DeviceIDMatch = connected.DeviceID() == result.ExpectedDeviceID
			result.ClientIDMatch = connected.ClientID() == result.ExpectedClientID
			deviceIDMatch := result.DeviceIDMatch
			clientIDMatch := result.ClientIDMatch
			deviceIDHash := result.DeviceIDHash
			clientIDHash := result.ClientIDHash
			protocolVersion := result.ProtocolVersion
			rawDeviceID := result.RawDeviceID
			rawClientID := result.RawClientID
			mu.Unlock()

			writeProgress("device-provisioning-check connected: path=%s listen=%s protocol_version=%d device_id_match=%t client_id_match=%t device_id_sha256=%s client_id_sha256=%s\n", path, listener.Addr().String(), protocolVersion, deviceIDMatch, clientIDMatch, deviceIDHash, clientIDHash)
			if options.ShowDeviceIdentity {
				writeProgress("device-provisioning-check identity: device_id=%s client_id=%s\n", rawDeviceID, rawClientID)
			}
			_ = connected.SendJSON(context.Background(), xiaozhi.NewServerHello("device-provisioning-check"))
		},
		OnText: func(_ *xiaozhi.Transport, data []byte) {
			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				setCheckErr(fmt.Errorf("parse xiaozhi text frame: %w", err))
				finish()
				return
			}
			if envelope.Type != xiaozhi.MessageTypeHello {
				return
			}
			if _, err := xiaozhi.ParseClientMessage(data); err != nil {
				setCheckErr(err)
				finish()
				return
			}
			mu.Lock()
			result.HelloSeen = true
			result.ReadyForCapture = result.Connected && result.DeviceIDMatch && result.ClientIDMatch
			ready := result.ReadyForCapture
			mu.Unlock()
			writeProgress("device-provisioning-check hello: ready_for_capture=%t\n", ready)
			finish()
		},
		OnBinary: func(_ *xiaozhi.Transport, frame audio.Frame) {
			if frame.Format != audio.FormatOpus {
				setCheckErr(fmt.Errorf("provisioning audio frame format %q, want opus", frame.Format))
				finish()
				return
			}
			mu.Lock()
			result.BinaryFrames++
			mu.Unlock()
		},
		OnError: func(_ *xiaozhi.Transport, err error) {
			if isCaptureProtocolError(err) {
				setCheckErr(fmt.Errorf("xiaozhi websocket provisioning failed: %w", err))
				finish()
			}
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
	writeProgress("device-provisioning-check ready: listen=%s path=%s connect_url=%s expected_device_id=%s expected_client_id=%s auth_env=%s timeout_ms=%d\n", listener.Addr().String(), path, connectURL, result.ExpectedDeviceID, result.ExpectedClientID, expected.AuthTokenEnv, options.Timeout.Milliseconds())

	select {
	case <-done:
	case err := <-serverErr:
		if err != nil {
			setCheckErr(err)
		}
	case <-checkCtx.Done():
	}

	mu.Lock()
	if transport != nil {
		_ = transport.Close(websocket.CloseNormalClosure, "device provisioning check finished")
	}
	err = checkErr
	final := result
	mu.Unlock()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)

	if err != nil {
		return final, err
	}
	if final.AuthFailures > 0 {
		return final, fmt.Errorf("device auth failed: code=%s status=%d", final.LastAuthCode, final.LastAuthStatus)
	}
	if !final.Connected {
		return final, fmt.Errorf("observed no xiaozhi websocket connection before timeout")
	}
	if !final.HelloSeen {
		return final, fmt.Errorf("device connected but did not send xiaozhi hello before timeout")
	}
	if !final.ReadyForCapture {
		return final, fmt.Errorf("device provisioning incomplete: device_id_match=%t client_id_match=%t", final.DeviceIDMatch, final.ClientIDMatch)
	}
	return final, nil
}

func provisioningAuthorizationMatchesToken(authorization, token string) bool {
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

func provisioningFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}
