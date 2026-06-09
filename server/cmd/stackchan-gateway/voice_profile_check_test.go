package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVoiceProfileCheckAcceptsDefaultRealtimeVoiceProfile(t *testing.T) {
	setVoiceProfileCheckEnv(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"voice-profile-check",
		"--config", "../../configs/stackchan-gateway.example.yaml",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("voice-profile-check code = %d, stderr = %s", code, stderr.String())
	}
	for _, want := range []string{
		"voice profile OK:",
		"profile=siliconflow-dashscope-voice",
		"asr=dashscope-asr",
		"llm=siliconflow-llm",
		"llm_model=Qwen/Qwen3.5-35B-A3B",
		"tts=dashscope-tts",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "dashscope-key") || strings.Contains(stdout.String(), "siliconflow-key") {
		t.Fatalf("stdout leaked provider key: %s", stdout.String())
	}
}

func TestVoiceProfileCheckRejectsRealtimeReasoningModelOverride(t *testing.T) {
	setVoiceProfileCheckEnv(t)
	t.Setenv("SILICONFLOW_LLM_MODEL", "deepseek-ai/DeepSeek-R1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"voice-profile-check",
		"--config", "../../configs/stackchan-gateway.example.yaml",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("voice-profile-check code = 0, want reasoning model failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not allowed on the realtime voice path") || !strings.Contains(stderr.String(), `token "r1"`) {
		t.Fatalf("stderr = %q, want realtime model policy failure", stderr.String())
	}
	if strings.Contains(stderr.String(), "dashscope-key") || strings.Contains(stderr.String(), "siliconflow-key") {
		t.Fatalf("stderr leaked provider key: %s", stderr.String())
	}
}

func TestVoiceProfileCheckRejectsMockProfile(t *testing.T) {
	setVoiceProfileCheckEnv(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"voice-profile-check",
		"--config", "../../configs/stackchan-gateway.example.yaml",
		"--profile", "cn-low-latency-cascade",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("voice-profile-check code = 0, want mock profile failure; stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "must not use mock provider") {
		t.Fatalf("stderr = %q, want mock provider failure", stderr.String())
	}
}

func TestVoiceProfileCheckIgnoresLegacyA21ModelEnv(t *testing.T) {
	setVoiceProfileCheckEnv(t)
	t.Setenv("A21_SILICONFLOW_MODEL", "Pro/deepseek-ai/DeepSeek-V3")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"voice-profile-check",
		"--config", "../../configs/stackchan-gateway.example.yaml",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("voice-profile-check code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "llm_model=Qwen/Qwen3.5-35B-A3B") {
		t.Fatalf("stdout = %q, want default SiliconFlow model despite legacy env", stdout.String())
	}
	if strings.Contains(stdout.String(), "DeepSeek-V3") {
		t.Fatalf("stdout used legacy model env: %s", stdout.String())
	}
}

func setVoiceProfileCheckEnv(t *testing.T) {
	t.Helper()
	t.Setenv("STACKCHAN_MAIN_AUTH_TOKEN", "main-token")
	t.Setenv("STACKCHAN_ADMIN_TOKEN", "admin-token")
	t.Setenv("DASHSCOPE_API_KEY", "dashscope-key")
	t.Setenv("SILICONFLOW_API_KEY", "siliconflow-key")
}
