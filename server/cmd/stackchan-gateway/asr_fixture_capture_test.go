package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providerprobe"
)

func TestASRFixtureCaptureFromListenerWritesXiaozhiOpusFixture(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg, err := gatewayconfig.LoadFile("../../configs/stackchan-gateway.example.yaml", gatewayconfig.OSLookupEnv)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "spoken-opus.json")
	resultCh := make(chan asrFixtureCaptureResult, 1)
	errCh := make(chan error, 1)

	go func() {
		result, err := captureASRFixtureFromListener(context.Background(), listener, asrFixtureCaptureOptions{
			Config:     cfg,
			Path:       cfg.Server.WebsocketPath,
			OutputPath: outputPath,
			MaxFrames:  2,
			Timeout:    2 * time.Second,
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	conn, response, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+cfg.Server.WebsocketPath, captureHeaders())
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{
		"type":"hello",
		"version":1,
		"features":{"mcp":true},
		"transport":"websocket",
		"audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60}
	}`)); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"listen","state":"start","mode":"auto"}`)); err != nil {
		t.Fatalf("write listen start: %v", err)
	}
	first := []byte{0xf8, 0xff, 0xfe}
	second := []byte{0xf8, 0xff, 0xfd}
	if err := conn.WriteMessage(websocket.BinaryMessage, first); err != nil {
		t.Fatalf("write first binary: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"listen","state":"stop"}`)); err != nil {
		t.Fatalf("write listen stop: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, second); err != nil {
		t.Fatalf("write second binary: %v", err)
	}

	result := receiveCaptureResult(t, resultCh, errCh)
	if result.OutputPath != outputPath {
		t.Fatalf("output path = %q, want %q", result.OutputPath, outputPath)
	}
	if result.Frames != 2 || result.Bytes != 6 {
		t.Fatalf("result frames/bytes = %d/%d, want 2/6", result.Frames, result.Bytes)
	}
	if result.ListenStartCount != 1 || result.ListenStopCount != 1 || result.ListenDetectCount != 0 {
		t.Fatalf("listen counts = start:%d stop:%d detect:%d, want 1/1/0", result.ListenStartCount, result.ListenStopCount, result.ListenDetectCount)
	}

	frames, err := providerprobe.LoadASROpusFixture(outputPath)
	if err != nil {
		t.Fatalf("LoadASROpusFixture() error = %v", err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[0], first) || !bytes.Equal(frames[1], second) {
		t.Fatalf("captured frames = %#v, want original payloads", frames)
	}
}

func TestASRFixtureCaptureFromListenerRequiresSemanticFixture(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg, err := gatewayconfig.LoadFile("../../configs/stackchan-gateway.example.yaml", gatewayconfig.OSLookupEnv)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "spoken-opus.json")
	errCh := make(chan error, 1)

	go func() {
		_, err := captureASRFixtureFromListener(context.Background(), listener, asrFixtureCaptureOptions{
			Config:                 cfg,
			Path:                   cfg.Server.WebsocketPath,
			OutputPath:             outputPath,
			MaxFrames:              10,
			Timeout:                2 * time.Second,
			RequireSemanticFixture: true,
		})
		errCh <- err
	}()

	sendCaptureFrames(t, listener.Addr().String(), cfg.Server.WebsocketPath, repeatedASROpusFrames(10, []byte{0xf8, 0xff, 0xfe}))

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("capture error = nil, want semantic fixture rejection")
		}
		if !strings.Contains(err.Error(), "semantic ASR probes") {
			t.Fatalf("capture error = %v, want semantic rejection", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for capture error")
	}
	if _, err := providerprobe.LoadASROpusFixture(outputPath); err == nil {
		t.Fatal("LoadASROpusFixture() error = nil, want invalid capture to avoid writing output")
	}
}

func TestASRFixtureCaptureFromListenerReportsSafeProgress(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg, err := gatewayconfig.LoadFile("../../configs/stackchan-gateway.example.yaml", gatewayconfig.OSLookupEnv)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "spoken-opus.json")
	var progress bytes.Buffer
	resultCh := make(chan asrFixtureCaptureResult, 1)
	errCh := make(chan error, 1)

	go func() {
		result, err := captureASRFixtureFromListener(context.Background(), listener, asrFixtureCaptureOptions{
			Config:                 cfg,
			Path:                   cfg.Server.WebsocketPath,
			OutputPath:             outputPath,
			MaxFrames:              12,
			Timeout:                2 * time.Second,
			Progress:               &progress,
			ProgressEveryFrames:    4,
			RequireSemanticFixture: true,
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	sendCaptureFrames(t, listener.Addr().String(), cfg.Server.WebsocketPath, variedCommandASROpusFrames(12, 16))

	result := receiveCaptureResult(t, resultCh, errCh)
	if result.Frames != 12 || result.DurationMS != 720 || result.UniquePayloads != 12 {
		t.Fatalf("result = %+v, want semantic inspection counts", result)
	}
	progressText := progress.String()
	for _, want := range []string{
		"asr-fixture-capture ready:",
		"connect_url=ws://127.0.0.1:",
		"device_id=stackchan-s3-main",
		"client_id=stackchan-s3-main-client",
		"auth_env=STACKCHAN_MAIN_AUTH_TOKEN",
		"max_frames=12",
		"timeout_ms=2000",
		"asr-fixture-capture connected:",
		"asr-fixture-capture progress: frames=1",
		"asr-fixture-capture progress: frames=4",
		"asr-fixture-capture fixture-ready: frames=12",
	} {
		if !strings.Contains(progressText, want) {
			t.Fatalf("progress missing %q:\n%s", want, progressText)
		}
	}
	for _, forbidden := range []string{"ICEi", "202122", "payload_base64", "payload_hex", "transcript", "?token=", "test-token"} {
		if strings.Contains(progressText, forbidden) {
			t.Fatalf("progress leaked forbidden content %q:\n%s", forbidden, progressText)
		}
	}
}

func TestASRFixtureCaptureFromListenerReportsSafeAuthFailures(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg, err := gatewayconfig.LoadFile("../../configs/stackchan-gateway.example.yaml", gatewayconfig.OSLookupEnv)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "spoken-opus.json")
	var progress bytes.Buffer
	errCh := make(chan error, 1)

	go func() {
		_, err := captureASRFixtureFromListener(context.Background(), listener, asrFixtureCaptureOptions{
			Config:              cfg,
			Path:                cfg.Server.WebsocketPath,
			OutputPath:          outputPath,
			MaxFrames:           12,
			Timeout:             250 * time.Millisecond,
			Progress:            &progress,
			ProgressEveryFrames: 4,
		})
		errCh <- err
	}()

	headers := captureHeaders()
	headers.Set(xiaozhi.HeaderAuthorization, "wrong-token")
	headers.Set(xiaozhi.HeaderClientID, "wrong-client")
	_, response, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+cfg.Server.WebsocketPath, headers)
	if err == nil {
		t.Fatal("Dial() error = nil, want auth failure")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("response = %v, want forbidden", response)
	}
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "captured no xiaozhi Opus frames") {
			t.Fatalf("capture error = %v, want no frames timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for capture to stop")
	}

	progressText := progress.String()
	for _, want := range []string{
		"asr-fixture-capture auth-failed:",
		"status=403",
		"code=INVALID_AUTHORIZATION",
		"has_authorization=true",
		"has_protocol_version=true",
		"has_device_id=true",
		"has_client_id=true",
	} {
		if !strings.Contains(progressText, want) {
			t.Fatalf("progress missing %q:\n%s", want, progressText)
		}
	}
	for _, forbidden := range []string{"wrong-token", "wrong-client", "test-token", "Authorization:"} {
		if strings.Contains(progressText, forbidden) {
			t.Fatalf("progress leaked forbidden content %q:\n%s", forbidden, progressText)
		}
	}
}

func TestASRFixtureCaptureFromListenerRequiresAdvertiseURLForWildcardListener(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg, err := gatewayconfig.LoadFile("../../configs/stackchan-gateway.example.yaml", gatewayconfig.OSLookupEnv)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	_, err = captureASRFixtureFromListener(context.Background(), listener, asrFixtureCaptureOptions{
		Config:     cfg,
		Path:       cfg.Server.WebsocketPath,
		OutputPath: filepath.Join(t.TempDir(), "spoken-opus.json"),
		MaxFrames:  12,
		Timeout:    2 * time.Second,
	})
	if err == nil {
		t.Fatal("captureASRFixtureFromListener() error = nil, want advertise URL requirement")
	}
	if !strings.Contains(err.Error(), "advertise URL is required") || !strings.Contains(err.Error(), "--advertise-url") {
		t.Fatalf("captureASRFixtureFromListener() error = %v, want advertise URL guidance", err)
	}
}

func TestASRFixtureAdvertiseURLValidation(t *testing.T) {
	tests := []struct {
		name        string
		listener    string
		path        string
		advertised  string
		want        string
		wantErrText string
	}{
		{
			name:       "explicit safe URL",
			listener:   "0.0.0.0:8080",
			path:       "/xiaozhi/v1/ws",
			advertised: "wss://stackchan.example.com/xiaozhi/v1/ws",
			want:       "wss://stackchan.example.com/xiaozhi/v1/ws",
		},
		{
			name:     "derive concrete listener",
			listener: "127.0.0.1:8080",
			path:     "/xiaozhi/v1/ws",
			want:     "ws://127.0.0.1:8080/xiaozhi/v1/ws",
		},
		{
			name:        "unspecified ipv4 listener requires explicit URL",
			listener:    "0.0.0.0:8080",
			path:        "/xiaozhi/v1/ws",
			wantErrText: "advertise URL is required",
		},
		{
			name:        "unspecified ipv6 listener requires explicit URL",
			listener:    "[::]:8080",
			path:        "/xiaozhi/v1/ws",
			wantErrText: "advertise URL is required",
		},
		{
			name:        "query parameters rejected",
			listener:    "0.0.0.0:8080",
			path:        "/xiaozhi/v1/ws",
			advertised:  "wss://stackchan.example.com/xiaozhi/v1/ws?token=secret",
			wantErrText: "must not include",
		},
		{
			name:        "missing advertised path rejected",
			listener:    "0.0.0.0:8080",
			path:        "/xiaozhi/v1/ws",
			advertised:  "wss://stackchan.example.com",
			wantErrText: "path is required",
		},
		{
			name:        "mismatched advertised path rejected",
			listener:    "0.0.0.0:8080",
			path:        "/xiaozhi/v1/ws",
			advertised:  "wss://stackchan.example.com/wrong/ws",
			wantErrText: "must match capture path",
		},
		{
			name:        "non websocket scheme rejected",
			listener:    "0.0.0.0:8080",
			path:        "/xiaozhi/v1/ws",
			advertised:  "https://stackchan.example.com/xiaozhi/v1/ws",
			wantErrText: "scheme must be ws or wss",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveASRFixtureAdvertiseURL(tt.listener, tt.path, tt.advertised)
			if tt.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("resolveASRFixtureAdvertiseURL() error = %v, want %q", err, tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveASRFixtureAdvertiseURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveASRFixtureAdvertiseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestASRFixtureCaptureCommandRejectsMissingOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"asr-fixture-capture",
		"--output", "",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("run() code = 0, want missing output failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--output is required") {
		t.Fatalf("stderr = %q, want missing output failure", stderr.String())
	}
}

func TestASRFixtureCaptureCommandRejectsUnignoredOutputPath(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"asr-fixture-capture",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--listen", "127.0.0.1:0",
		"--output", filepath.Join(t.TempDir(), "spoken-opus.json"),
		"--timeout-ms", "1",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("run() code = 0, want unignored output failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "output path is not ignored by git") {
		t.Fatalf("stderr = %q, want git ignored output failure", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty output on failure", stdout.String())
	}
}

func TestASRFixtureCaptureOutputAllowsPersistentRuntimeFixturePath(t *testing.T) {
	if err := ensureASRFixtureCaptureOutputIgnored("/var/lib/a21-air/fixtures/asr/spoken-opus.json"); err != nil {
		t.Fatalf("ensureASRFixtureCaptureOutputIgnored() error = %v, want persistent runtime fixture path allowed", err)
	}
	if err := ensureASRFixtureCaptureOutputIgnored("/var/lib/a21-air/traces/spoken-opus.json"); err == nil {
		t.Fatal("ensureASRFixtureCaptureOutputIgnored() error = nil, want non-fixture runtime path rejected")
	}
	if err := ensureASRFixtureCaptureOutputIgnored("/var/lib/a21-air/fixtures/asr/../spoken-opus.json"); err == nil {
		t.Fatal("ensureASRFixtureCaptureOutputIgnored() error = nil, want traversal-like runtime path rejected")
	}
}

func TestASRFixtureCaptureCommandRejectsWildcardListenWithoutAdvertiseURL(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"asr-fixture-capture",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--listen", "0.0.0.0:0",
		"--output", ignoredASRFixtureOutputPath("wildcard-listen-spoken-opus.json"),
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("run() code = 0, want advertise URL failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "advertise URL is required") || !strings.Contains(stderr.String(), "--advertise-url") {
		t.Fatalf("stderr = %q, want advertise URL guidance", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty output on failure", stdout.String())
	}
}

func TestASRFixtureCaptureCommandDoesNotRequireAdminToken(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"asr-fixture-capture",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--listen", "0.0.0.0:0",
		"--output", ignoredASRFixtureOutputPath("admin-skip-spoken-opus.json"),
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("run() code = 0, want advertise URL failure; stdout = %s", stdout.String())
	}
	if strings.Contains(stderr.String(), "STACKCHAN_ADMIN_TOKEN") {
		t.Fatalf("stderr = %q, capture should not require admin token", stderr.String())
	}
	if !strings.Contains(stderr.String(), "advertise URL is required") {
		t.Fatalf("stderr = %q, want capture to reach advertise URL validation", stderr.String())
	}
}

func sendCaptureFrames(t *testing.T, addr string, path string, frames [][]byte) {
	t.Helper()

	conn, response, err := websocket.DefaultDialer.Dial("ws://"+addr+path, captureHeaders())
	if err != nil {
		t.Fatalf("Dial() error = %v, response=%v", err, response)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{
		"type":"hello",
		"version":1,
		"features":{"mcp":true},
		"transport":"websocket",
		"audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60}
	}`)); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	for index, frame := range frames {
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Fatalf("write binary frame %d: %v", index, err)
		}
	}
}

func repeatedASROpusFrames(count int, frame []byte) [][]byte {
	frames := make([][]byte, count)
	for index := range frames {
		frames[index] = append([]byte(nil), frame...)
	}
	return frames
}

func captureHeaders() http.Header {
	headers := http.Header{}
	headers.Set(xiaozhi.HeaderAuthorization, "test-token")
	headers.Set(xiaozhi.HeaderProtocolVersion, "1")
	headers.Set(xiaozhi.HeaderDeviceID, "stackchan-s3-main")
	headers.Set(xiaozhi.HeaderClientID, "stackchan-s3-main-client")
	return headers
}

func ignoredASRFixtureOutputPath(name string) string {
	return filepath.Join("..", "..", "var", "fixtures", "asr", name)
}

func receiveCaptureResult(t *testing.T, resultCh <-chan asrFixtureCaptureResult, errCh <-chan error) asrFixtureCaptureResult {
	t.Helper()

	select {
	case result := <-resultCh:
		return result
	case err := <-errCh:
		t.Fatalf("capture error = %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for capture result")
	}
	return asrFixtureCaptureResult{}
}
