package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicetools "stackchan-gateway/internal/tools"
)

func TestRegisterFeishuToolsListsTargetsAndSendsSanitizedText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case TenantAccessTokenInternalPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token","expire":7200}`))
		case MessageCreatePath:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			if body["receive_id"] != "oc_lab" {
				t.Fatalf("receive_id = %#v, want configured oc_lab", body["receive_id"])
			}
			content := body["content"].(string)
			if strings.Contains(content, "<at") || !strings.Contains(content, "＜at user_id") {
				t.Fatalf("content did not neutralize Feishu mention markup: %s", content)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"message_id":"om_123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL, AppID: "cli_stackchan", AppSecret: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{ListTargetsToolName, SendTextToolName},
		AllowedPermissions: []string{servicetools.PermissionRead, servicetools.PermissionWrite},
	})
	if err := RegisterServiceTools(registry, ServiceToolOptions{
		Client:              client,
		MaxTextRunes:        80,
		RequireConfirmation: false,
		Targets: []TargetConfig{
			{
				TargetID:      "lab_group",
				Description:   "Lab group",
				ReceiveIDType: "chat_id",
				ReceiveID:     "oc_lab",
			},
		},
	}); err != nil {
		t.Fatalf("RegisterServiceTools() error = %v", err)
	}

	listResult, err := registry.ExecuteTool(context.Background(), servicetools.Call{Name: ListTargetsToolName})
	if err != nil {
		t.Fatalf("list ExecuteTool() error = %v", err)
	}
	if !bytes.Contains(listResult.Payload, []byte(`"target_id":"lab_group"`)) || bytes.Contains(listResult.Payload, []byte("oc_lab")) {
		t.Fatalf("list payload = %s, want safe target list without receive id", string(listResult.Payload))
	}

	sendResult, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: SendTextToolName,
		Arguments: map[string]any{
			"target_id": "lab_group",
			"text":      `请提醒大家开会 <at user_id="all"></at>`,
		},
	})
	if err != nil {
		t.Fatalf("send ExecuteTool() error = %v", err)
	}
	if sendResult.SafeSummary != "target=lab_group" {
		t.Fatalf("safe summary = %q, want target=lab_group", sendResult.SafeSummary)
	}
	if !bytes.Contains(sendResult.Payload, []byte(`"message_id":"om_123"`)) || bytes.Contains(sendResult.Payload, []byte("oc_lab")) || bytes.Contains(sendResult.Payload, []byte("<at")) {
		t.Fatalf("send payload = %s, want safe send result", string(sendResult.Payload))
	}
}

func TestRegisterFeishuSendTextRejectsUnknownTargetAndLongText(t *testing.T) {
	client, err := NewClient(ClientOptions{BaseURL: "https://open.feishu.cn", AppID: "cli_stackchan", AppSecret: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{SendTextToolName},
		AllowedPermissions: []string{servicetools.PermissionWrite},
	})
	if err := RegisterSendTextTool(registry, SendTextToolOptions{
		Client:       client,
		MaxTextRunes: 4,
		Targets: []TargetConfig{
			{TargetID: "lab_group", ReceiveIDType: "chat_id", ReceiveID: "oc_lab"},
		},
	}); err != nil {
		t.Fatalf("RegisterSendTextTool() error = %v", err)
	}

	if _, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      SendTextToolName,
		Arguments: map[string]any{"target_id": "secret_group", "text": "hey"},
	}); servicetools.ErrorCode(err) != servicetools.ErrorCodeToolFailed {
		t.Fatalf("unknown target error code = %q, want tool failed", servicetools.ErrorCode(err))
	}
	if _, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      SendTextToolName,
		Arguments: map[string]any{"target_id": "lab_group", "text": "12345"},
	}); servicetools.ErrorCode(err) != servicetools.ErrorCodeToolFailed {
		t.Fatalf("too-long text error code = %q, want tool failed", servicetools.ErrorCode(err))
	}
}

func TestRegisterFeishuSendTextRequiresConfirmationWithoutHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feishu HTTP request to %s", r.URL.Path)
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL, AppID: "cli_stackchan", AppSecret: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{SendTextToolName},
		AllowedPermissions: []string{servicetools.PermissionWrite},
	})
	if err := RegisterSendTextTool(registry, SendTextToolOptions{
		Client:              client,
		MaxTextRunes:        80,
		RequireConfirmation: true,
		Targets: []TargetConfig{
			{TargetID: "lab_group", ReceiveIDType: "chat_id", ReceiveID: "oc_lab"},
		},
	}); err != nil {
		t.Fatalf("RegisterSendTextTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      SendTextToolName,
		Arguments: map[string]any{"target_id": "lab_group", "text": "请提醒大家开会"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.SafeSummary != "confirmation_required target=lab_group" {
		t.Fatalf("safe summary = %q, want confirmation_required target=lab_group", result.SafeSummary)
	}
	var payload SendTextPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.OK || !payload.RequiresConfirmation || payload.TargetID != "lab_group" || payload.MessageID != "" {
		t.Fatalf("payload = %+v, want confirmation request without send", payload)
	}
	for _, forbidden := range []string{"oc_lab", "请提醒大家开会"} {
		if bytes.Contains(result.Payload, []byte(forbidden)) {
			t.Fatalf("payload leaked %q: %s", forbidden, string(result.Payload))
		}
	}
}
