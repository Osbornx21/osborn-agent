package providers

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type providerFixtureSpec struct {
	Name  string
	Dir   string
	Files []string
}

func TestProviderFixturesArePresentAndSanitized(t *testing.T) {
	specs := []providerFixtureSpec{
		{
			Name: "dashscope_llm",
			Dir:  "dashscope/testdata/llm",
			Files: llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_usage_before_done_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			),
		},
		{
			Name: "dashscope_asr",
			Dir:  "dashscope/testdata/asr",
			Files: realtimeASRFixtureFiles(
				"ws_headers.json",
				"ws_client_start_or_session_update.json",
				"ws_client_audio_append.json",
				"ws_client_finish_or_commit.json",
				"ws_server_started_or_session_updated.json",
				"ws_server_first_result.json",
				"ws_server_finished_or_completed.json",
				"ws_error_event.json",
				"http_error_handshake_auth_401.json",
				"audio_format.json",
				"cancel_expected.md",
			),
		},
		{
			Name: "dashscope_tts",
			Dir:  "dashscope/testdata/tts",
			Files: realtimeTTSFixtureFiles(
				"ws_headers.json",
				"ws_client_start_or_session_update.json",
				"ws_client_text_append_or_continue.json",
				"ws_client_finish_or_text_done.json",
				"ws_server_started_or_session_updated.json",
				"ws_server_first_audio_delta.json",
				"ws_server_audio_done_or_task_finished.json",
				"ws_error_event.json",
				"http_error_handshake_auth_401.json",
				"audio_format.json",
				"cancel_expected.md",
			),
		},
		{
			Name: "doubao_llm",
			Dir:  "doubao/testdata/llm",
			Files: llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			),
		},
		{
			Name: "doubao_asr",
			Dir:  "doubao/testdata/asr",
			Files: realtimeASRFixtureFiles(
				"ws_headers.json",
				"ws_client_start_or_session_update.json",
				"ws_client_audio_append.json",
				"ws_client_finish_or_commit.json",
				"ws_server_started_or_session_updated.json",
				"ws_server_first_result.json",
				"ws_server_finished_or_completed.json",
				"ws_error_event.json",
				"http_error_handshake_auth_401.json",
				"audio_format.json",
				"ws_error_resource_mismatch_55000000.json",
				"cancel_expected.md",
			),
		},
		{
			Name: "doubao_tts",
			Dir:  "doubao/testdata/tts",
			Files: realtimeTTSFixtureFiles(
				"ws_headers.json",
				"ws_client_start_or_session_update.json",
				"ws_client_text_append_or_continue.json",
				"ws_client_finish_or_text_done.json",
				"ws_server_started_or_session_updated.json",
				"ws_server_first_audio_delta.json",
				"ws_server_audio_done_or_task_finished.json",
				"ws_error_event.json",
				"http_error_handshake_auth_401.json",
				"audio_format.json",
				"ws_error_resource_mismatch_55000000.json",
				"cancel_expected.md",
			),
		},
		{
			Name: "minimax_llm",
			Dir:  "minimax/testdata/llm",
			Files: llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			),
		},
		{
			Name: "minimax_tts_http",
			Dir:  "minimax/testdata/tts_http",
			Files: []string{
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"audio_format.json",
				"cancel_expected.md",
			},
		},
		{
			Name: "minimax_tts_ws",
			Dir:  "minimax/testdata/tts_ws",
			Files: append(realtimeTTSFixtureFiles(
				"ws_headers.json",
				"ws_client_start_or_session_update.json",
				"ws_client_text_append_or_continue.json",
				"ws_client_finish_or_text_done.json",
				"ws_server_started_or_session_updated.json",
				"ws_server_first_audio_delta.json",
				"ws_server_audio_done_or_task_finished.json",
				"ws_error_event.json",
				"http_error_handshake_auth_401.json",
				"audio_format.json",
				"cancel_expected.md",
			), "ws_server_connected_success.json"),
		},
		{
			Name: "stepfun_llm",
			Dir:  "stepfun/testdata/llm",
			Files: append(llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			), "sse_reasoning_delta_chunk.sse"),
		},
		{
			Name: "siliconflow_llm",
			Dir:  "siliconflow/testdata/llm",
			Files: append(llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			), "sse_reasoning_content_delta_chunk.sse", "sse_usage_before_done_chunk.sse"),
		},
		{
			Name: "moonshot_llm",
			Dir:  "moonshot/testdata/llm",
			Files: append(llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			), "sse_reasoning_content_delta_chunk.sse", "sse_usage_before_done_chunk.sse"),
		},
		{
			Name: "deepseek_llm",
			Dir:  "deepseek/testdata/llm",
			Files: append(llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			), "sse_usage_before_done_chunk.sse"),
		},
		{
			Name: "anthropic_llm",
			Dir:  "anthropic/testdata/llm",
			Files: append(llmFixtureFiles(
				"http_headers.json",
				"http_request_nonstream.json",
				"http_response_nonstream.json",
				"http_request_stream.json",
				"sse_first_chunk.sse",
				"sse_delta_chunk.sse",
				"sse_finish_chunk.sse",
				"http_error_auth_401.json",
				"http_error_rate_limit_or_overload.json",
				"cancel_expected.md",
			), "sse_error_event.sse"),
		},
	}

	for _, spec := range specs {
		t.Run(spec.Name, func(t *testing.T) {
			for _, name := range spec.Files {
				requireSanitizedFixture(t, spec, name)
				if strings.HasSuffix(name, "_headers.json") {
					requireHeadersFixture(t, filepath.Join(spec.Dir, name))
				}
			}
		})
	}
}

func llmFixtureFiles(names ...string) []string {
	return names
}

func realtimeASRFixtureFiles(names ...string) []string {
	return names
}

func realtimeTTSFixtureFiles(names ...string) []string {
	return names
}

func requireSanitizedFixture(t *testing.T, spec providerFixtureSpec, name string) {
	t.Helper()

	path := filepath.Join(spec.Dir, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s/%s missing: %v", spec.Name, name, err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		t.Fatalf("fixture %s is empty", path)
	}
	assertNoSecretLikeText(t, path, string(content))

	switch filepath.Ext(name) {
	case ".json":
		var value any
		if err := json.Unmarshal(content, &value); err != nil {
			t.Fatalf("fixture %s is not valid JSON: %v", path, err)
		}
	case ".sse":
		requireValidSSEFixture(t, path, string(content))
	case ".md":
		lower := strings.ToLower(string(content))
		if !strings.Contains(lower, "cancel") || !strings.Contains(lower, "close") {
			t.Fatalf("cancel fixture %s must describe cancel and close behavior", path)
		}
	}
}

func requireHeadersFixture(t *testing.T, path string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("headers fixture %s missing: %v", path, err)
	}
	var headers map[string]string
	if err := json.Unmarshal(content, &headers); err != nil {
		t.Fatalf("headers fixture %s is not a string map: %v", path, err)
	}
	if len(headers) == 0 {
		t.Fatalf("headers fixture %s is empty", path)
	}
	for name, value := range headers {
		lowerName := strings.ToLower(name)
		if strings.Contains(lowerName, "authorization") || strings.Contains(lowerName, "api-key") || strings.Contains(lowerName, "x-api-key") {
			if !strings.Contains(value, "<") && !strings.Contains(value, "$") {
				t.Fatalf("auth header %s in %s must use a placeholder value, got %q", name, path, value)
			}
		}
	}
}

func requireValidSSEFixture(t *testing.T, path string, content string) {
	t.Helper()

	scanner := bufio.NewScanner(strings.NewReader(content))
	dataLines := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			t.Fatalf("SSE fixture %s has unsupported line %q", path, line)
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			dataLines++
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			t.Fatalf("SSE fixture %s has invalid data JSON %q: %v", path, payload, err)
		}
		dataLines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE fixture %s: %v", path, err)
	}
	if dataLines == 0 {
		t.Fatalf("SSE fixture %s has no data lines", path)
	}
}

func assertNoSecretLikeText(t *testing.T, path string, content string) {
	t.Helper()

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`sk-proj-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`Bearer\s+[A-Za-z0-9._-]{20,}`),
		regexp.MustCompile("OSS" + "AccessKeyId" + "="),
		regexp.MustCompile("Signature" + "="),
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`),
	}
	for _, pattern := range patterns {
		if pattern.MatchString(content) {
			t.Fatalf("fixture %s contains secret-like text matching %s", path, pattern.String())
		}
	}
}
