package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stackchan-gateway/internal/feishu"
)

func TestFeishuSmokeCommandSendsAllowlistedTextWithoutLeakingSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case feishu.TenantAccessTokenInternalPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token","expire":7200}`))
		case feishu.MessageCreatePath:
			if r.URL.Query().Get("receive_id_type") != "chat_id" {
				t.Fatalf("receive_id_type = %q, want chat_id", r.URL.Query().Get("receive_id_type"))
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if body["receive_id"] != "oc_lab" || body["msg_type"] != "text" {
				t.Fatalf("message body = %#v", body)
			}
			content := body["content"].(string)
			if !strings.Contains(content, "StackChan smoke test") {
				t.Fatalf("content = %s, want smoke text", content)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"message_id":"om_123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	setFeishuSmokeEnv(t)
	configPath := writeFeishuSmokeConfig(t, server.URL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"feishu-smoke",
		"--config", configPath,
		"--target", "lab_group",
		"--text", "StackChan smoke test",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("feishu-smoke code = %d, stderr = %s", code, stderr.String())
	}
	for _, want := range []string{"feishu smoke OK:", "target=lab_group", "message_id=om_123"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	for _, forbidden := range []string{"feishu-secret", "tenant-token", "oc_lab", "StackChan smoke test"} {
		if strings.Contains(stdout.String()+stderr.String(), forbidden) {
			t.Fatalf("command output leaked %q: stdout=%q stderr=%q", forbidden, stdout.String(), stderr.String())
		}
	}
}

func TestFeishuSmokeCommandRejectsUnknownTargetBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Feishu HTTP request to %s", r.URL.Path)
	}))
	defer server.Close()
	setFeishuSmokeEnv(t)
	configPath := writeFeishuSmokeConfig(t, server.URL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"feishu-smoke",
		"--config", configPath,
		"--target", "secret_group",
		"--text", "StackChan smoke test",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("feishu-smoke code = 0, want unknown target failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "target is not allowlisted") {
		t.Fatalf("stderr = %q, want allowlist failure", stderr.String())
	}
	for _, forbidden := range []string{"feishu-secret", "oc_lab", "StackChan smoke test"} {
		if strings.Contains(stdout.String()+stderr.String(), forbidden) {
			t.Fatalf("command output leaked %q: stdout=%q stderr=%q", forbidden, stdout.String(), stderr.String())
		}
	}
}

func setFeishuSmokeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("FEISHU_APP_ID", "cli_stackchan")
	t.Setenv("FEISHU_APP_SECRET", "feishu-secret")
	t.Setenv("FEISHU_LAB_RECEIVE_ID", "oc_lab")
}

func writeFeishuSmokeConfig(t *testing.T, baseURL string) string {
	t.Helper()
	content := fmt.Sprintf(`
server:
  public_base_url: "https://stackchan.example.internal"
  listen_addr: "127.0.0.1:8080"
  websocket_path: "/xiaozhi/v1/ws"
  admin_addr: "127.0.0.1:8081"
  admin_token_env: "STACKCHAN_ADMIN_TOKEN"
  metrics_addr: ""
  shutdown_timeout_ms: 5000
devices:
  - device_id: "stackchan-s3-main"
    client_id: "stackchan-s3-main-client"
    auth_token_env: "STACKCHAN_MAIN_AUTH_TOKEN"
    default_mode: "auto"
    allow_mcp_tools: []
audio:
  uplink_sample_rate_hz: 16000
  downlink_sample_rate_hz: 24000
  channels: 1
  frame_duration_ms: 60
  downlink_queue_ms: 1200
  max_turn_ms: 30000
providers:
  default_profile: "cn-low-latency-cascade"
  auto_fallback:
    enabled: false
    profiles: []
    yellow_first_audio_ms: 0
    consecutive_yellow: 0
    consecutive_errors: 0
  profiles:
    cn-low-latency-cascade:
      asr: "mock"
      llm: "mock"
      tts: "mock"
agent:
  default_mode: "casual"
  persona_path: "./configs/persona.stackchan.yaml"
  memory_db_path: "./var/memory/stackchan-memory.sqlite3"
  memory_max_items: 5
  recent_turns: 8
tools:
  feishu:
    enabled: true
    base_url: "%s"
    app_id_env: "FEISHU_APP_ID"
    app_secret_env: "FEISHU_APP_SECRET"
    allowed_targets:
      - target_id: "lab_group"
        description: "Lab group"
        receive_id_type: "chat_id"
        receive_id_env: "FEISHU_LAB_RECEIVE_ID"
    timeout_ms: 1500
    max_text_chars: 240
stackchan:
  body:
    min_command_gap_ms: 160
    max_commands_per_turn: 16
    yaw_min_deg: -45
    yaw_max_deg: 45
    pitch_min_deg: 0
    pitch_max_deg: 45
    default_speed: 150
  display:
    scene_ttl_ms: 1800
    max_caption_chars: 48
observability:
  trace_jsonl_path: "./var/traces/turns.jsonl"
  redact_secrets: true
`, baseURL)
	path := filepath.Join(t.TempDir(), "stackchan-gateway.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write Feishu smoke config: %v", err)
	}
	return path
}
