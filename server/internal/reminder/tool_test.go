package reminder

import (
	"bytes"
	"context"
	"testing"

	servicetools "stackchan-gateway/internal/tools"
)

func TestRegisterAnnounceToolReturnsBoundedSafePayload(t *testing.T) {
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{AnnounceToolName},
		AllowedPermissions: []string{servicetools.PermissionDeviceControl},
	})
	if err := RegisterAnnounceTool(registry, AnnounceToolOptions{
		MaxTitleRunes:   12,
		MaxMessageRunes: 40,
	}); err != nil {
		t.Fatalf("RegisterAnnounceTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: AnnounceToolName,
		Arguments: map[string]any{
			"title":   "喝水",
			"message": "该喝水了，保存一下当前工作。",
			"urgency": "high",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(result.Payload, []byte(`"ok":true`)) ||
		!bytes.Contains(result.Payload, []byte(`"title":"喝水"`)) ||
		!bytes.Contains(result.Payload, []byte(`"urgency":"high"`)) ||
		!bytes.Contains(result.Payload, []byte(`"message_chars":14`)) {
		t.Fatalf("payload = %s, want bounded safe reminder announcement", string(result.Payload))
	}
	if bytes.Contains(result.Payload, []byte("保存一下当前工作")) {
		t.Fatalf("payload leaked full reminder message: %s", string(result.Payload))
	}
}

func TestRegisterAnnounceToolRejectsUnsafeArguments(t *testing.T) {
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{AnnounceToolName},
		AllowedPermissions: []string{servicetools.PermissionDeviceControl},
	})
	if err := RegisterAnnounceTool(registry, AnnounceToolOptions{
		MaxTitleRunes:   4,
		MaxMessageRunes: 6,
	}); err != nil {
		t.Fatalf("RegisterAnnounceTool() error = %v", err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
	}{
		{name: "missing title", arguments: map[string]any{"message": "hello"}},
		{name: "long title", arguments: map[string]any{"title": "超过四个字"}},
		{name: "long message", arguments: map[string]any{"title": "喝水", "message": "这条消息太长了"}},
		{name: "bad urgency", arguments: map[string]any{"title": "喝水", "urgency": "panic"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := registry.ExecuteTool(context.Background(), servicetools.Call{
				Name:      AnnounceToolName,
				Arguments: tc.arguments,
			})
			if servicetools.ErrorCode(err) != servicetools.ErrorCodeToolFailed {
				t.Fatalf("ExecuteTool() error = %v, want tool failed", err)
			}
		})
	}
}
