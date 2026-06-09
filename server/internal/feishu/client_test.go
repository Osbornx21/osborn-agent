package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFeishuClientFetchesTenantTokenAndSendsTextMessage(t *testing.T) {
	var tokenCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case TenantAccessTokenInternalPath:
			atomic.AddInt32(&tokenCalls, 1)
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s, want POST", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode token body: %v", err)
			}
			if body["app_id"] != "cli_stackchan" || body["app_secret"] != "app-secret" {
				t.Fatalf("token body = %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token","expire":7200}`))
		case MessageCreatePath:
			if r.URL.Query().Get("receive_id_type") != "chat_id" {
				t.Fatalf("receive_id_type = %q, want chat_id", r.URL.Query().Get("receive_id_type"))
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			if body["receive_id"] != "oc_lab" || body["msg_type"] != "text" {
				t.Fatalf("message body = %#v", body)
			}
			content, ok := body["content"].(string)
			if !ok {
				t.Fatalf("content = %#v, want JSON string", body["content"])
			}
			var contentBody map[string]string
			if err := json.Unmarshal([]byte(content), &contentBody); err != nil {
				t.Fatalf("content is not JSON string: %v", err)
			}
			if contentBody["text"] != "StackChan 测试消息" {
				t.Fatalf("content text = %q", contentBody["text"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"message_id":"om_123","chat_id":"oc_lab"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL + "/", AppID: "cli_stackchan", AppSecret: "app-secret"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	response, err := client.SendTextMessage(context.Background(), SendTextRequest{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_lab",
		Text:          "StackChan 测试消息",
	})
	if err != nil {
		t.Fatalf("SendTextMessage() error = %v", err)
	}
	if response.MessageID != "om_123" {
		t.Fatalf("message id = %q, want om_123", response.MessageID)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
}

func TestFeishuClientErrorsDoNotLeakSecretsOrProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == TenantAccessTokenInternalPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token","expire":7200}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`provider-body-with-app-secret-must-not-leak`))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL, AppID: "cli_stackchan", AppSecret: "app-secret-must-not-leak"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.SendTextMessage(context.Background(), SendTextRequest{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_lab",
		Text:          "hello",
	})
	if err == nil {
		t.Fatal("SendTextMessage() error = nil, want status error")
	}
	for _, forbidden := range []string{"app-secret-must-not-leak", "provider-body-with-app-secret-must-not-leak"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error leaked %q: %v", forbidden, err)
		}
	}
}

func TestNewFeishuClientRequiresURLAndCredentials(t *testing.T) {
	if _, err := NewClient(ClientOptions{AppID: "cli_stackchan", AppSecret: "secret"}); !errors.Is(err, ErrMissingBaseURL) {
		t.Fatalf("missing URL error = %v, want ErrMissingBaseURL", err)
	}
	if _, err := NewClient(ClientOptions{BaseURL: "https://open.feishu.cn", AppSecret: "secret"}); !errors.Is(err, ErrMissingAppID) {
		t.Fatalf("missing app ID error = %v, want ErrMissingAppID", err)
	}
	if _, err := NewClient(ClientOptions{BaseURL: "https://open.feishu.cn", AppID: "cli_stackchan"}); !errors.Is(err, ErrMissingAppSecret) {
		t.Fatalf("missing app secret error = %v, want ErrMissingAppSecret", err)
	}
	if _, err := NewClient(ClientOptions{BaseURL: "file:///tmp/feishu", AppID: "cli_stackchan", AppSecret: "secret"}); err == nil {
		t.Fatal("NewClient() accepted non-http URL")
	}
}
