package xiaozhi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/audio"
)

const (
	HeaderAuthorization   = "Authorization"
	HeaderProtocolVersion = "Protocol-Version"
	HeaderDeviceID        = "Device-Id"
	HeaderClientID        = "Client-Id"
)

type AuthRequest struct {
	Authorization   string
	ProtocolVersion int
	DeviceID        string
	ClientID        string
}

type AuthResult struct {
	DeviceID        string
	ClientID        string
	ProtocolVersion int
}

type AuthFailure struct {
	Status             int
	Code               string
	HasAuthorization   bool
	HasProtocolVersion bool
	HasDeviceID        bool
	HasClientID        bool
}

type Authenticator interface {
	AuthenticateDevice(ctx context.Context, request AuthRequest) (AuthResult, error)
}

type AuthenticatorFunc func(ctx context.Context, request AuthRequest) (AuthResult, error)

func (fn AuthenticatorFunc) AuthenticateDevice(ctx context.Context, request AuthRequest) (AuthResult, error) {
	return fn(ctx, request)
}

type AuthError struct {
	Status  int
	Code    string
	Message string
}

func NewAuthError(status int, code string, message string) *AuthError {
	return &AuthError{Status: status, Code: code, Message: message}
}

func (e *AuthError) Error() string {
	return e.Code + ": " + e.Message
}

type WebSocketHandlerOptions struct {
	Authenticator Authenticator
	OnConnect     func(*Transport)
	OnAuthFailure func(AuthFailure)
	OnText        func(*Transport, []byte)
	OnBinary      func(*Transport, audio.Frame)
	OnError       func(*Transport, error)
	OnDisconnect  func(*Transport)
}

type Transport struct {
	conn            *websocket.Conn
	writeMu         sync.Mutex
	auth            AuthResult
	protocolVersion int
}

func (t *Transport) DeviceID() string {
	return t.auth.DeviceID
}

func (t *Transport) ClientID() string {
	return t.auth.ClientID
}

func (t *Transport) ProtocolVersion() int {
	return t.protocolVersion
}

func (t *Transport) SendJSON(_ context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal websocket json: %w", err)
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.conn.WriteMessage(websocket.TextMessage, data)
}

func (t *Transport) SendBinary(_ context.Context, frame []byte) error {
	encodedFrame, err := EncodeBinaryAudioFrame(frame, BinaryFrameOptions{
		ProtocolVersion: t.protocolVersion,
	})
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.conn.WriteMessage(websocket.BinaryMessage, encodedFrame)
}

func (t *Transport) Close(code int, reason string) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	deadline := time.Now().Add(time.Second)
	if err := t.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), deadline); err != nil {
		_ = t.conn.Close()
		return err
	}
	return t.conn.Close()
}

func NewWebSocketHandler(options WebSocketHandlerOptions) http.Handler {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool {
			return true
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, err := authenticateWebSocketRequest(r.Context(), r, options.Authenticator)
		if err != nil {
			if options.OnAuthFailure != nil {
				options.OnAuthFailure(authFailureFromRequest(r, err))
			}
			writeAuthFailure(w, err)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		transport := &Transport{
			conn:            conn,
			auth:            auth,
			protocolVersion: auth.ProtocolVersion,
		}
		if options.OnConnect != nil {
			options.OnConnect(transport)
		}
		readWebSocketLoop(transport, options)
	})
}

func authenticateWebSocketRequest(ctx context.Context, r *http.Request, authenticator Authenticator) (AuthResult, error) {
	if authenticator == nil {
		return AuthResult{}, NewAuthError(http.StatusForbidden, "AUTHENTICATOR_NOT_CONFIGURED", "websocket authenticator is not configured")
	}

	authorization := r.Header.Get(HeaderAuthorization)
	if authorization == "" {
		return AuthResult{}, NewAuthError(http.StatusUnauthorized, "MISSING_AUTHORIZATION", "authorization header is required")
	}

	protocolVersionRaw := r.Header.Get(HeaderProtocolVersion)
	if protocolVersionRaw == "" {
		return AuthResult{}, NewAuthError(http.StatusBadRequest, "MISSING_PROTOCOL_VERSION", "protocol-version header is required")
	}
	protocolVersion, err := strconv.Atoi(protocolVersionRaw)
	if err != nil {
		return AuthResult{}, NewAuthError(http.StatusBadRequest, "INVALID_PROTOCOL_VERSION", "protocol-version header must be an integer")
	}

	deviceID := r.Header.Get(HeaderDeviceID)
	if deviceID == "" {
		return AuthResult{}, NewAuthError(http.StatusBadRequest, "MISSING_DEVICE_ID", "device-id header is required")
	}

	clientID := r.Header.Get(HeaderClientID)
	if clientID == "" {
		return AuthResult{}, NewAuthError(http.StatusBadRequest, "MISSING_CLIENT_ID", "client-id header is required")
	}

	return authenticator.AuthenticateDevice(ctx, AuthRequest{
		Authorization:   authorization,
		ProtocolVersion: protocolVersion,
		DeviceID:        deviceID,
		ClientID:        clientID,
	})
}

func writeAuthFailure(w http.ResponseWriter, err error) {
	var authError *AuthError
	if errors.As(err, &authError) {
		http.Error(w, authError.Code, authError.Status)
		return
	}
	http.Error(w, "WEBSOCKET_AUTH_ERROR", http.StatusInternalServerError)
}

func authFailureFromRequest(r *http.Request, err error) AuthFailure {
	failure := AuthFailure{
		Status:             http.StatusInternalServerError,
		Code:               "WEBSOCKET_AUTH_ERROR",
		HasAuthorization:   r.Header.Get(HeaderAuthorization) != "",
		HasProtocolVersion: r.Header.Get(HeaderProtocolVersion) != "",
		HasDeviceID:        r.Header.Get(HeaderDeviceID) != "",
		HasClientID:        r.Header.Get(HeaderClientID) != "",
	}
	var authError *AuthError
	if errors.As(err, &authError) {
		failure.Status = authError.Status
		failure.Code = authError.Code
	}
	return failure
}

func readWebSocketLoop(transport *Transport, options WebSocketHandlerOptions) {
	defer transport.conn.Close()
	defer func() {
		if options.OnDisconnect != nil {
			options.OnDisconnect(transport)
		}
	}()

	for {
		messageType, data, err := transport.conn.ReadMessage()
		if err != nil {
			if options.OnError != nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				options.OnError(transport, err)
			}
			return
		}

		switch messageType {
		case websocket.TextMessage:
			if options.OnText != nil {
				options.OnText(transport, data)
			}
		case websocket.BinaryMessage:
			frame, err := ParseBinaryAudioFrame(data, BinaryFrameOptions{
				ProtocolVersion: transport.ProtocolVersion(),
				SampleRateHz:    XiaozhiUplinkSampleRateHz,
				FrameDurationMS: XiaozhiFrameDurationMS,
			})
			if err != nil {
				if options.OnError != nil {
					options.OnError(transport, err)
				}
				continue
			}
			if options.OnBinary != nil {
				options.OnBinary(transport, frame)
			}
		}
	}
}
