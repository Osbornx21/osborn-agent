package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	servicetools "stackchan-gateway/internal/tools"
)

func TestMemoryLookupToolReturnsScopedMemoriesWithoutMetadata(t *testing.T) {
	now := time.Now()
	store := NewStaticMemoryStore([]Memory{
		{
			ID:           "name",
			UserID:       "owner",
			DeviceID:     "stackchan-s3-main",
			Type:         MemoryUserProfile,
			Content:      "用户偏好的称呼是阿豪。",
			Importance:   5,
			Confidence:   1,
			UpdatedAt:    now,
			MetadataJSON: `{"secret":"must-not-leak"}`,
		},
		{
			ID:         "global-pref",
			UserID:     "owner",
			Type:       MemoryUserProfile,
			Content:    "用户喜欢低延迟语音。",
			Importance: 4,
			Confidence: 0.9,
			UpdatedAt:  now.Add(-time.Minute),
		},
		{
			ID:         "other-device",
			UserID:     "owner",
			DeviceID:   "other-device",
			Type:       MemoryUserProfile,
			Content:    "不应出现。",
			Importance: 5,
			Confidence: 1,
			UpdatedAt:  now.Add(time.Minute),
		},
		{
			ID:         "other-user",
			UserID:     "other-user",
			DeviceID:   "stackchan-s3-main",
			Type:       MemoryUserProfile,
			Content:    "也不应出现。",
			Importance: 5,
			Confidence: 1,
			UpdatedAt:  now.Add(time.Minute),
		},
	})
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	if err := RegisterMemoryLookupTool(registry, MemoryLookupToolOptions{
		Store:        store,
		OwnerUserID:  "owner",
		DefaultLimit: 2,
		MaxLimit:     2,
	}); err != nil {
		t.Fatalf("RegisterMemoryLookupTool() error = %v", err)
	}
	if !registry.HasTool(MemoryLookupToolName) {
		t.Fatalf("registry missing %s", MemoryLookupToolName)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		SessionID: "sess_memory_tool",
		DeviceID:  "stackchan-s3-main",
		Name:      MemoryLookupToolName,
		Arguments: map[string]any{
			"query":     "低延迟",
			"limit":     99,
			"device_id": "other-device",
			"user_id":   "other-user",
		},
	})

	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.SafeSummary != "memory_count=2" {
		t.Fatalf("SafeSummary = %q, want memory_count=2", result.SafeSummary)
	}
	payload := decodeMemoryLookupPayload(t, result.Payload)
	if payload.Count != 2 || len(payload.Memories) != 2 {
		t.Fatalf("payload = %+v, want two scoped memories", payload)
	}
	text := string(result.Payload)
	for _, want := range []string{"用户偏好的称呼是阿豪。", "用户喜欢低延迟语音。"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"must-not-leak", "不应出现", "也不应出现", "metadata"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("payload leaked %q: %s", forbidden, text)
		}
	}
}

func TestMemoryLookupToolRequiresCurrentDeviceScope(t *testing.T) {
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	if err := RegisterMemoryLookupTool(registry, MemoryLookupToolOptions{
		Store: NewStaticMemoryStore([]Memory{{
			ID:         "name",
			UserID:     "owner",
			DeviceID:   "stackchan-s3-main",
			Type:       MemoryUserProfile,
			Content:    "用户偏好的称呼是阿豪。",
			Importance: 5,
		}}),
	}); err != nil {
		t.Fatalf("RegisterMemoryLookupTool() error = %v", err)
	}

	_, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: MemoryLookupToolName,
		Arguments: map[string]any{
			"device_id": "stackchan-s3-main",
		},
	})

	if code := servicetools.ErrorCode(err); code != servicetools.ErrorCodeToolFailed {
		t.Fatalf("error code = %q, want %q", code, servicetools.ErrorCodeToolFailed)
	}
}

func decodeMemoryLookupPayload(t *testing.T, data json.RawMessage) memoryLookupPayload {
	t.Helper()
	var payload memoryLookupPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode memory lookup payload: %v", err)
	}
	return payload
}
