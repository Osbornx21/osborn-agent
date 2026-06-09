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
)

func TestDeviceProvisioningCheckFromListenerReadyForCapture(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg := loadProvisioningConfig(t)
	listener := listenProvisioning(t)
	var progress bytes.Buffer
	resultCh := make(chan deviceProvisioningCheckResult, 1)
	errCh := make(chan error, 1)

	go func() {
		result, err := checkDeviceProvisioningFromListener(context.Background(), listener, deviceProvisioningCheckOptions{
			Config:   cfg,
			Path:     cfg.Server.WebsocketPath,
			Timeout:  2 * time.Second,
			Progress: &progress,
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	sendProvisioningHello(t, listener.Addr().String(), cfg.Server.WebsocketPath, captureHeaders())
	result := receiveProvisioningResult(t, resultCh, errCh)
	if !result.ReadyForCapture || !result.Connected || !result.HelloSeen || !result.DeviceIDMatch || !result.ClientIDMatch {
		t.Fatalf("result = %+v, want ready provisioning", result)
	}
	progressText := progress.String()
	for _, want := range []string{
		"device-provisioning-check ready:",
		"auth_env=STACKCHAN_MAIN_AUTH_TOKEN",
		"device-provisioning-check connected:",
		"device_id_match=true",
		"client_id_match=true",
		"device-provisioning-check hello: ready_for_capture=true",
	} {
		if !strings.Contains(progressText, want) {
			t.Fatalf("progress missing %q:\n%s", want, progressText)
		}
	}
	if strings.Contains(progressText, "test-token") {
		t.Fatalf("progress leaked token:\n%s", progressText)
	}
}

func TestDeviceProvisioningCheckFromListenerReportsIdentityMismatchSafely(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg := loadProvisioningConfig(t)
	listener := listenProvisioning(t)
	var progress bytes.Buffer
	resultCh := make(chan provisioningCheckOutcome, 1)

	go func() {
		result, err := checkDeviceProvisioningFromListener(context.Background(), listener, deviceProvisioningCheckOptions{
			Config:   cfg,
			Path:     cfg.Server.WebsocketPath,
			Timeout:  2 * time.Second,
			Progress: &progress,
		})
		resultCh <- provisioningCheckOutcome{Result: result, Err: err}
	}()

	headers := captureHeaders()
	headers.Set(xiaozhi.HeaderDeviceID, "new-stackchan-device")
	headers.Set(xiaozhi.HeaderClientID, "new-stackchan-client")
	sendProvisioningHello(t, listener.Addr().String(), cfg.Server.WebsocketPath, headers)

	outcome := receiveProvisioningOutcome(t, resultCh)
	if outcome.Err == nil || !strings.Contains(outcome.Err.Error(), "device provisioning incomplete") {
		t.Fatalf("check error = %v, want identity mismatch", outcome.Err)
	}
	if outcome.Result.ReadyForCapture || !outcome.Result.Connected || !outcome.Result.HelloSeen {
		t.Fatalf("result = %+v, want connected but not ready", outcome.Result)
	}
	if outcome.Result.DeviceIDMatch || outcome.Result.ClientIDMatch {
		t.Fatalf("result = %+v, want identity mismatch flags", outcome.Result)
	}
	progressText := progress.String()
	for _, want := range []string{
		"device_id_match=false",
		"client_id_match=false",
		"device_id_sha256=",
		"client_id_sha256=",
		"ready_for_capture=false",
	} {
		if !strings.Contains(progressText, want) {
			t.Fatalf("progress missing %q:\n%s", want, progressText)
		}
	}
	for _, forbidden := range []string{"new-stackchan-device", "new-stackchan-client", "test-token"} {
		if strings.Contains(progressText, forbidden) {
			t.Fatalf("progress leaked forbidden content %q:\n%s", forbidden, progressText)
		}
	}
}

func TestDeviceProvisioningCheckFromListenerCanShowIdentityWhenExplicit(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg := loadProvisioningConfig(t)
	listener := listenProvisioning(t)
	var progress bytes.Buffer
	resultCh := make(chan provisioningCheckOutcome, 1)

	go func() {
		result, err := checkDeviceProvisioningFromListener(context.Background(), listener, deviceProvisioningCheckOptions{
			Config:             cfg,
			Path:               cfg.Server.WebsocketPath,
			Timeout:            2 * time.Second,
			Progress:           &progress,
			ShowDeviceIdentity: true,
		})
		resultCh <- provisioningCheckOutcome{Result: result, Err: err}
	}()

	headers := captureHeaders()
	headers.Set(xiaozhi.HeaderDeviceID, "new-stackchan-device")
	headers.Set(xiaozhi.HeaderClientID, "new-stackchan-client")
	sendProvisioningHello(t, listener.Addr().String(), cfg.Server.WebsocketPath, headers)
	_ = receiveProvisioningOutcome(t, resultCh)

	progressText := progress.String()
	for _, want := range []string{"device_id=new-stackchan-device", "client_id=new-stackchan-client"} {
		if !strings.Contains(progressText, want) {
			t.Fatalf("progress missing explicit identity %q:\n%s", want, progressText)
		}
	}
	if strings.Contains(progressText, "test-token") {
		t.Fatalf("progress leaked token:\n%s", progressText)
	}
}

func TestDeviceProvisioningCheckFromListenerReportsSafeAuthFailure(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")

	cfg := loadProvisioningConfig(t)
	listener := listenProvisioning(t)
	var progress bytes.Buffer
	resultCh := make(chan provisioningCheckOutcome, 1)

	go func() {
		result, err := checkDeviceProvisioningFromListener(context.Background(), listener, deviceProvisioningCheckOptions{
			Config:   cfg,
			Path:     cfg.Server.WebsocketPath,
			Timeout:  2 * time.Second,
			Progress: &progress,
		})
		resultCh <- provisioningCheckOutcome{Result: result, Err: err}
	}()

	headers := captureHeaders()
	headers.Set(xiaozhi.HeaderAuthorization, "wrong-token")
	_, response, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+cfg.Server.WebsocketPath, headers)
	if err == nil {
		t.Fatal("Dial() error = nil, want auth failure")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("response = %v, want forbidden", response)
	}

	outcome := receiveProvisioningOutcome(t, resultCh)
	if outcome.Err == nil || !strings.Contains(outcome.Err.Error(), "device auth failed") {
		t.Fatalf("check error = %v, want auth failure", outcome.Err)
	}
	if outcome.Result.AuthFailures != 1 || outcome.Result.LastAuthCode != "INVALID_AUTHORIZATION" {
		t.Fatalf("result = %+v, want one invalid auth failure", outcome.Result)
	}
	progressText := progress.String()
	for _, want := range []string{
		"device-provisioning-check auth-failed:",
		"status=403",
		"code=INVALID_AUTHORIZATION",
		"has_authorization=true",
		"has_device_id=true",
		"has_client_id=true",
	} {
		if !strings.Contains(progressText, want) {
			t.Fatalf("progress missing %q:\n%s", want, progressText)
		}
	}
	for _, forbidden := range []string{"wrong-token", "test-token", "Authorization:"} {
		if strings.Contains(progressText, forbidden) {
			t.Fatalf("progress leaked forbidden content %q:\n%s", forbidden, progressText)
		}
	}
}

func TestDeviceProvisioningCheckCommandDoesNotRequireAdminToken(t *testing.T) {
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "test-token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"device-provisioning-check",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--listen", "0.0.0.0:0",
		"--advertise-url", "ws://127.0.0.1:1/xiaozhi/v1/ws",
		"--timeout-ms", "1",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("run() code = 0, want timeout failure without admin token; stdout = %s", stdout.String())
	}
	if strings.Contains(stderr.String(), "STACKCHAN_ADMIN_TOKEN") {
		t.Fatalf("stderr = %q, provisioning check should not require admin token", stderr.String())
	}
	if !strings.Contains(stderr.String(), "observed no xiaozhi websocket connection") {
		t.Fatalf("stderr = %q, want provisioning timeout", stderr.String())
	}
}

type provisioningCheckOutcome struct {
	Result deviceProvisioningCheckResult
	Err    error
}

func loadProvisioningConfig(t *testing.T) *gatewayconfig.Config {
	t.Helper()
	cfg, err := gatewayconfig.LoadFile("../../configs/stackchan-gateway.example.yaml", gatewayconfig.OSLookupEnv)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	return cfg
}

func listenProvisioning(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return listener
}

func sendProvisioningHello(t *testing.T, addr string, path string, headers http.Header) {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial("ws://"+addr+path, headers)
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
}

func receiveProvisioningResult(t *testing.T, resultCh <-chan deviceProvisioningCheckResult, errCh <-chan error) deviceProvisioningCheckResult {
	t.Helper()
	select {
	case result := <-resultCh:
		return result
	case err := <-errCh:
		t.Fatalf("provisioning check error = %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for provisioning result")
	}
	return deviceProvisioningCheckResult{}
}

func receiveProvisioningOutcome(t *testing.T, resultCh <-chan provisioningCheckOutcome) provisioningCheckOutcome {
	t.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for provisioning outcome")
	}
	return provisioningCheckOutcome{}
}

func TestProvisioningFingerprintIsStableAndShort(t *testing.T) {
	got := provisioningFingerprint(filepath.Base("/tmp/new-stackchan-device"))
	if len(got) != 12 {
		t.Fatalf("fingerprint length = %d, want 12", len(got))
	}
	if got != provisioningFingerprint("new-stackchan-device") {
		t.Fatalf("fingerprint is not stable")
	}
}
