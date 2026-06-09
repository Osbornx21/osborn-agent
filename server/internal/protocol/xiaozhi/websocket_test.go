package xiaozhi

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"stackchan-gateway/internal/audio"
)

func TestWebSocketMissingAuthorizationReturns401(t *testing.T) {
	server := httptest.NewServer(NewWebSocketHandler(WebSocketHandlerOptions{Authenticator: allowKnownDeviceAuth()}))
	defer server.Close()

	_, response, err := websocket.DefaultDialer.Dial(toWebSocketURL(server.URL), requiredHeaders(""))

	if err == nil {
		t.Fatal("Dial() error = nil, want unauthorized")
	}
	if response == nil {
		t.Fatal("response = nil, want HTTP response")
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestWebSocketUnknownDeviceReturns403(t *testing.T) {
	server := httptest.NewServer(NewWebSocketHandler(WebSocketHandlerOptions{Authenticator: allowKnownDeviceAuth()}))
	defer server.Close()

	headers := requiredHeaders("test-token")
	headers.Set(HeaderDeviceID, "unknown-device")

	_, response, err := websocket.DefaultDialer.Dial(toWebSocketURL(server.URL), headers)

	if err == nil {
		t.Fatal("Dial() error = nil, want forbidden")
	}
	if response == nil {
		t.Fatal("response = nil, want HTTP response")
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
}

func TestWebSocketAuthFailureCallbackReportsSafeMetadata(t *testing.T) {
	failures := make(chan AuthFailure, 1)
	server := httptest.NewServer(NewWebSocketHandler(WebSocketHandlerOptions{
		Authenticator: AuthenticatorFunc(func(context.Context, AuthRequest) (AuthResult, error) {
			return AuthResult{}, NewAuthError(http.StatusForbidden, "INVALID_AUTHORIZATION", "authorization does not match configured device token")
		}),
		OnAuthFailure: func(failure AuthFailure) {
			failures <- failure
		},
	}))
	defer server.Close()

	headers := requiredHeaders("bad-secret")
	headers.Set(HeaderClientID, "wrong-client")
	_, response, err := websocket.DefaultDialer.Dial(toWebSocketURL(server.URL), headers)

	if err == nil {
		t.Fatal("Dial() error = nil, want auth failure")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("response = %v, want forbidden", response)
	}
	failure := receive(t, failures)
	if failure.Status != http.StatusForbidden || failure.Code != "INVALID_AUTHORIZATION" {
		t.Fatalf("auth failure = %#v, want forbidden INVALID_AUTHORIZATION", failure)
	}
	if !failure.HasAuthorization || !failure.HasProtocolVersion || !failure.HasDeviceID || !failure.HasClientID {
		t.Fatalf("auth failure header presence = %#v, want all headers present", failure)
	}
	for _, forbidden := range []string{"bad-secret", "wrong-client"} {
		if strings.Contains(fmt.Sprintf("%+v", failure), forbidden) {
			t.Fatalf("auth failure leaked %q: %+v", forbidden, failure)
		}
	}
}

func TestWebSocketValidConnectionUpgrades(t *testing.T) {
	connected := make(chan AuthResult, 1)
	server := httptest.NewServer(NewWebSocketHandler(WebSocketHandlerOptions{
		Authenticator: allowKnownDeviceAuth(),
		OnConnect: func(transport *Transport) {
			connected <- AuthResult{
				DeviceID:        transport.DeviceID(),
				ClientID:        transport.ClientID(),
				ProtocolVersion: transport.ProtocolVersion(),
			}
		},
	}))
	defer server.Close()

	conn, response, err := websocket.DefaultDialer.Dial(toWebSocketURL(server.URL), requiredHeaders("test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	result := receive(t, connected)
	if result.DeviceID != "stackchan-s3-main" {
		t.Fatalf("device id = %q, want stackchan-s3-main", result.DeviceID)
	}
	if result.ClientID != "stackchan-s3-main-client" {
		t.Fatalf("client id = %q, want stackchan-s3-main-client", result.ClientID)
	}
	if result.ProtocolVersion != BinaryProtocolV1 {
		t.Fatalf("protocol version = %d, want %d", result.ProtocolVersion, BinaryProtocolV1)
	}
}

func TestWebSocketDisconnectCallbackReportsNormalClose(t *testing.T) {
	disconnected := make(chan string, 1)
	server := httptest.NewServer(NewWebSocketHandler(WebSocketHandlerOptions{
		Authenticator: allowKnownDeviceAuth(),
		OnDisconnect: func(transport *Transport) {
			disconnected <- transport.DeviceID()
		},
	}))
	defer server.Close()

	conn, response, err := websocket.DefaultDialer.Dial(toWebSocketURL(server.URL), requiredHeaders("test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		t.Fatalf("write close: %v", err)
	}

	if got := receive(t, disconnected); got != "stackchan-s3-main" {
		t.Fatalf("disconnect device id = %q, want stackchan-s3-main", got)
	}
}

func TestWebSocketTextAndBinaryCallbacksAreSeparate(t *testing.T) {
	textMessages := make(chan []byte, 1)
	binaryFrames := make(chan []byte, 1)

	server := httptest.NewServer(NewWebSocketHandler(WebSocketHandlerOptions{
		Authenticator: allowKnownDeviceAuth(),
		OnText: func(_ *Transport, data []byte) {
			textMessages <- append([]byte(nil), data...)
		},
		OnBinary: func(_ *Transport, frame audio.Frame) {
			binaryFrames <- append([]byte(nil), frame.Payload...)
		},
	}))
	defer server.Close()

	conn, response, err := websocket.DefaultDialer.Dial(toWebSocketURL(server.URL), requiredHeaders("test-token"))
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"abort"}`)); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x11, 0x22}); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	if got := string(receive(t, textMessages)); got != `{"type":"abort"}` {
		t.Fatalf("text callback = %q, want abort json", got)
	}
	if got := receive(t, binaryFrames); len(got) != 2 || got[0] != 0x11 || got[1] != 0x22 {
		t.Fatalf("binary callback = %v, want raw opus payload", got)
	}
}

func TestWebSocketParsesAndSendsWrappedBinaryFrames(t *testing.T) {
	binaryFrames := make(chan audio.Frame, 1)
	server := httptest.NewServer(NewWebSocketHandler(WebSocketHandlerOptions{
		Authenticator: allowKnownDeviceAuth(),
		OnConnect: func(transport *Transport) {
			if err := transport.SendBinary(context.Background(), []byte{0xaa, 0xbb}); err != nil {
				t.Errorf("SendBinary() error = %v", err)
			}
		},
		OnBinary: func(_ *Transport, frame audio.Frame) {
			binaryFrames <- frame
		},
	}))
	defer server.Close()

	headers := requiredHeaders("test-token")
	headers.Set(HeaderProtocolVersion, "3")
	conn, response, err := websocket.DefaultDialer.Dial(toWebSocketURL(server.URL), headers)
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read downlink: %v", err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("downlink message type = %d, want binary", messageType)
	}
	if len(data) != 6 || data[0] != 0 || data[1] != 0 || binary.BigEndian.Uint16(data[2:4]) != 2 || data[4] != 0xaa || data[5] != 0xbb {
		t.Fatalf("downlink binary = %v, want official v3 wrapped frame", data)
	}

	payload := []byte{0x11, 0x22, 0x33}
	uplink := make([]byte, 4+len(payload))
	uplink[0] = 0
	binary.BigEndian.PutUint16(uplink[2:4], uint16(len(payload)))
	copy(uplink[4:], payload)
	if err := conn.WriteMessage(websocket.BinaryMessage, uplink); err != nil {
		t.Fatalf("write v3 binary: %v", err)
	}
	frame := receive(t, binaryFrames)
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("binary callback payload = %v, want %v", frame.Payload, payload)
	}
}

func allowKnownDeviceAuth() AuthenticatorFunc {
	return func(_ context.Context, request AuthRequest) (AuthResult, error) {
		if request.DeviceID != "stackchan-s3-main" {
			return AuthResult{}, NewAuthError(http.StatusForbidden, "UNKNOWN_DEVICE", "device is not configured")
		}
		if request.Authorization != "test-token" {
			return AuthResult{}, NewAuthError(http.StatusForbidden, "INVALID_AUTHORIZATION", "authorization does not match configured device token")
		}
		return AuthResult{
			DeviceID:        request.DeviceID,
			ClientID:        request.ClientID,
			ProtocolVersion: request.ProtocolVersion,
		}, nil
	}
}

func requiredHeaders(authorization string) http.Header {
	headers := http.Header{}
	if authorization != "" {
		headers.Set(HeaderAuthorization, authorization)
	}
	headers.Set(HeaderProtocolVersion, "1")
	headers.Set(HeaderDeviceID, "stackchan-s3-main")
	headers.Set(HeaderClientID, "stackchan-s3-main-client")
	return headers
}

func toWebSocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatal("timed out waiting for channel value")
		var zero T
		return zero
	}
}
