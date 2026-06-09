package camera

import (
	"bytes"
	"context"
	"testing"

	servicetools "stackchan-gateway/internal/tools"
)

func TestRequestCaptureToolReturnsConfirmationWithoutCapturing(t *testing.T) {
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{RequestCaptureToolName},
		AllowedPermissions: []string{servicetools.PermissionDeviceControl},
	})
	if err := RegisterRequestCaptureTool(registry, RequestCaptureToolOptions{MaxReasonRunes: 40}); err != nil {
		t.Fatalf("RegisterRequestCaptureTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: RequestCaptureToolName,
		Arguments: map[string]any{
			"reason": "看一下桌面上的设备状态",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"requires_confirmation":true`)) || !bytes.Contains(result.Payload, []byte(`"ok":false`)) {
		t.Fatalf("payload = %s, want safe confirmation request", string(result.Payload))
	}
	if bytes.Contains(result.Payload, []byte("桌面")) {
		t.Fatalf("payload leaked reason text: %s", string(result.Payload))
	}
	if result.SafeSummary != "confirmation_required reason_chars=11" {
		t.Fatalf("safe summary = %q, want bounded reason count", result.SafeSummary)
	}
}

func TestRequestCaptureToolValidatesReason(t *testing.T) {
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{RequestCaptureToolName},
		AllowedPermissions: []string{servicetools.PermissionDeviceControl},
	})
	if err := RegisterRequestCaptureTool(registry, RequestCaptureToolOptions{MaxReasonRunes: 4}); err != nil {
		t.Fatalf("RegisterRequestCaptureTool() error = %v", err)
	}

	_, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      RequestCaptureToolName,
		Arguments: map[string]any{"reason": "太长的理由"},
	})
	if err == nil || servicetools.ErrorCode(err) != servicetools.ErrorCodeToolFailed {
		t.Fatalf("ExecuteTool() error = %v, want service tool failure", err)
	}
}
