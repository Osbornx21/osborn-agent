package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stackchan-gateway/internal/session"
)

func TestLoadPersonaFileAndBuildPromptWithMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persona.yaml")
	if err := os.WriteFile(path, []byte(`
name: "Stack-chan"
identity: "桌面机器人"
core_rules:
  - "回答要短。"
style:
  tone: "温柔"
`), 0o600); err != nil {
		t.Fatalf("write persona: %v", err)
	}
	persona, err := LoadPersonaFile(path)
	if err != nil {
		t.Fatalf("LoadPersonaFile() error = %v", err)
	}
	store := NewStaticMemoryStore([]Memory{
		{ID: "low", DeviceID: "stackchan-s3-main", Type: MemoryEpisodic, Content: "用户今天在调试设备。", Importance: 1, UpdatedAt: time.Now().Add(-time.Hour)},
		{ID: "name", DeviceID: "stackchan-s3-main", Type: MemoryUserProfile, Content: "用户偏好的称呼是阿豪。", Importance: 5, UpdatedAt: time.Now()},
	})
	builder := NewPromptBuilder(PromptBuilderOptions{
		Persona:        persona,
		MemoryStore:    store,
		MemoryMaxItems: 1,
	})

	contextOut, err := builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_agent",
		DeviceID:   "stackchan-s3-main",
		Generation: 1,
		Transcript: "你还记得我叫什么吗？",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext() error = %v", err)
	}
	if contextOut.MemoryCount != 1 || contextOut.PersonaName != "Stack-chan" {
		t.Fatalf("context = %+v, want one memory and persona name", contextOut)
	}
	for _, want := range []string{
		"You are Stack-chan.",
		"桌面机器人",
		"用户偏好的称呼是阿豪。",
		"语音输出契约:",
		"默认用中文短句回答，内容要能直接朗读",
		"你还记得我叫什么吗？",
	} {
		if !strings.Contains(contextOut.Text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, contextOut.Text)
		}
	}
	if strings.Contains(contextOut.Text, "用户今天在调试设备。") {
		t.Fatalf("prompt included low-importance memory despite max=1:\n%s", contextOut.Text)
	}
}

func TestPromptBuilderFiltersMemoriesByDevice(t *testing.T) {
	builder := NewPromptBuilder(PromptBuilderOptions{
		Persona: Persona{Name: "Stack-chan", Identity: "桌面机器人"},
		MemoryStore: NewStaticMemoryStore([]Memory{
			{ID: "wrong-device", DeviceID: "other-device", Type: MemoryUserProfile, Content: "不该出现", Importance: 5},
			{ID: "right-device", DeviceID: "stackchan-s3-main", Type: MemoryUserProfile, Content: "应该出现", Importance: 4},
		}),
		MemoryMaxItems: 5,
	})

	contextOut, err := builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_agent",
		DeviceID:   "stackchan-s3-main",
		Generation: 1,
		Transcript: "测试",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext() error = %v", err)
	}
	if strings.Contains(contextOut.Text, "不该出现") || !strings.Contains(contextOut.Text, "应该出现") {
		t.Fatalf("prompt memory filtering failed:\n%s", contextOut.Text)
	}
}

func TestPromptBuilderIncludesRecentConversation(t *testing.T) {
	recent := NewInMemoryRecentTurnStore(8)
	if err := recent.AppendRecentTurn(context.Background(), RecentTurn{
		DeviceID:      "stackchan-s3-main",
		UserText:      "我刚才问你今天的计划。",
		AssistantText: "我说先验收语音链路。",
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("AppendRecentTurn() error = %v", err)
	}
	if err := recent.AppendRecentTurn(context.Background(), RecentTurn{
		DeviceID:      "other-device",
		UserText:      "不该出现",
		AssistantText: "不该出现",
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("AppendRecentTurn(other) error = %v", err)
	}
	builder := NewPromptBuilder(PromptBuilderOptions{
		Persona:         Persona{Name: "Stack-chan", Identity: "桌面机器人"},
		RecentTurnStore: recent,
		RecentTurns:     4,
	})

	contextOut, err := builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_recent",
		DeviceID:   "stackchan-s3-main",
		Generation: 2,
		Transcript: "那下一步呢？",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext() error = %v", err)
	}
	if contextOut.RecentTurnCount != 1 {
		t.Fatalf("RecentTurnCount = %d, want 1", contextOut.RecentTurnCount)
	}
	for _, want := range []string{
		"当前会话最近对话（从旧到新）:",
		"用户: 我刚才问你今天的计划。",
		"助手: 我说先验收语音链路。",
		"如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须优先根据当前会话最近对话补全语境再回答。",
		"当前用户消息:\n那下一步呢？",
	} {
		if !strings.Contains(contextOut.Text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, contextOut.Text)
		}
	}
	if strings.Contains(contextOut.Text, "不该出现") {
		t.Fatalf("prompt included other device recent turn:\n%s", contextOut.Text)
	}
	if len(contextOut.Messages) != 4 {
		t.Fatalf("Messages = %+v, want system context, recent chat history and current user message", contextOut.Messages)
	}
	if contextOut.Messages[0].Role != "system" || !strings.Contains(contextOut.Messages[0].Content, "语音输出契约") {
		t.Fatalf("system message = %+v, want voice contract context", contextOut.Messages[0])
	}
	if strings.Contains(contextOut.Messages[0].Content, "用户: 我刚才问你今天的计划。") {
		t.Fatalf("system message should not embed current-session chat history:\n%s", contextOut.Messages[0].Content)
	}
	if contextOut.Messages[1].Role != "user" || contextOut.Messages[1].Content != "我刚才问你今天的计划。" {
		t.Fatalf("recent user message = %+v, want prior user turn", contextOut.Messages[1])
	}
	if contextOut.Messages[2].Role != "assistant" || contextOut.Messages[2].Content != "我说先验收语音链路。" {
		t.Fatalf("recent assistant message = %+v, want prior assistant turn", contextOut.Messages[2])
	}
	if contextOut.Messages[3].Role != "user" || contextOut.Messages[3].Content != "那下一步呢？" {
		t.Fatalf("current user message = %+v, want current transcript only", contextOut.Messages[3])
	}
	if strings.Contains(contextOut.Messages[0].Content, "当前用户消息") {
		t.Fatalf("system message should not embed the current user marker:\n%s", contextOut.Messages[0].Content)
	}
}

func TestPromptBuilderIncludesAgentModeContext(t *testing.T) {
	builder := NewPromptBuilder(PromptBuilderOptions{
		Persona: Persona{Name: "Stack-chan", Identity: "桌面机器人"},
	})

	contextOut, err := builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_professional",
		DeviceID:   "stackchan-s3-main",
		Generation: 1,
		Transcript: "帮我查一下专业资料。",
		AgentMode:  "professional",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext() error = %v", err)
	}
	if len(contextOut.Messages) != 2 {
		t.Fatalf("Messages = %+v, want system and current user", contextOut.Messages)
	}
	for _, want := range []string{
		"当前模式: professional",
		"优先使用已授权专业知识工具",
		"工具不可用时必须说明只能做一般分析",
	} {
		if !strings.Contains(contextOut.Messages[0].Content, want) {
			t.Fatalf("system message missing %q:\n%s", want, contextOut.Messages[0].Content)
		}
		if !strings.Contains(contextOut.Text, want) {
			t.Fatalf("legacy prompt missing %q:\n%s", want, contextOut.Text)
		}
	}
	if contextOut.Messages[1].Role != "user" || contextOut.Messages[1].Content != "帮我查一下专业资料。" {
		t.Fatalf("current user message = %+v, want transcript only", contextOut.Messages[1])
	}
}

func TestPromptBuilderSeparatesCurrentSessionRecentConversationFromOlderDeviceContext(t *testing.T) {
	recent := NewInMemoryRecentTurnStore(8)
	for _, turn := range []RecentTurn{
		{
			SessionID:     "sess_old",
			DeviceID:      "stackchan-s3-main",
			UserText:      "昨天我们在聊历史材料。",
			AssistantText: "我会把历史材料只当背景。",
			CreatedAt:     time.Now().Add(-time.Hour),
		},
		{
			SessionID:     "sess_current",
			DeviceID:      "stackchan-s3-main",
			UserText:      "刚才我们在验收连续对话。",
			AssistantText: "我确认最近对话已经进入提示词。",
			CreatedAt:     time.Now(),
		},
	} {
		if err := recent.AppendRecentTurn(context.Background(), turn); err != nil {
			t.Fatalf("AppendRecentTurn() error = %v", err)
		}
	}
	builder := NewPromptBuilder(PromptBuilderOptions{
		Persona:         Persona{Name: "Stack-chan", Identity: "桌面机器人"},
		RecentTurnStore: recent,
		RecentTurns:     8,
	})

	contextOut, err := builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_current",
		DeviceID:   "stackchan-s3-main",
		Generation: 3,
		Transcript: "那刚才那个问题继续。",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext() error = %v", err)
	}

	for _, want := range []string{
		"当前会话最近对话（从旧到新）:",
		"用户: 刚才我们在验收连续对话。",
		"较早跨会话参考（只在相关时使用）:",
		"用户: 昨天我们在聊历史材料。",
		"如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须优先根据当前会话最近对话补全语境再回答。",
	} {
		if !strings.Contains(contextOut.Text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, contextOut.Text)
		}
	}
	if strings.Index(contextOut.Text, "当前会话最近对话") > strings.Index(contextOut.Text, "较早跨会话参考") {
		t.Fatalf("prompt should put current-session context before older cross-session context:\n%s", contextOut.Text)
	}
}

func TestPromptBuilderTreatsRecentDeviceContextAsContinuityWhenCurrentSessionHasNoTurns(t *testing.T) {
	recent := NewInMemoryRecentTurnStore(8)
	if err := recent.AppendRecentTurn(context.Background(), RecentTurn{
		SessionID:     "sess_previous_connection",
		DeviceID:      "stackchan-s3-main",
		UserText:      "我们刚刚说到动作控制太频繁。",
		AssistantText: "我会先降低默认动作干扰。",
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("AppendRecentTurn() error = %v", err)
	}
	builder := NewPromptBuilder(PromptBuilderOptions{
		Persona:         Persona{Name: "Stack-chan", Identity: "桌面机器人"},
		RecentTurnStore: recent,
		RecentTurns:     8,
	})

	contextOut, err := builder.BuildLLMContext(context.Background(), session.LLMContextRequest{
		SessionID:  "sess_after_reconnect",
		DeviceID:   "stackchan-s3-main",
		Generation: 1,
		Transcript: "继续刚才那个。",
	})
	if err != nil {
		t.Fatalf("BuildLLMContext() error = %v", err)
	}

	for _, want := range []string{
		"最近对话（可能来自上一连接，从旧到新）:",
		"用户: 我们刚刚说到动作控制太频繁。",
		"如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须根据最近对话补全语境再回答。",
	} {
		if !strings.Contains(contextOut.Text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, contextOut.Text)
		}
	}
	if strings.Contains(contextOut.Text, "较早跨会话参考") {
		t.Fatalf("prompt should not down-rank the only available reconnect context:\n%s", contextOut.Text)
	}
	if len(contextOut.Messages) != 4 {
		t.Fatalf("Messages = %+v, want previous connection chat history plus current user", contextOut.Messages)
	}
	if contextOut.Messages[0].Role != "system" || !strings.Contains(contextOut.Messages[0].Content, "可能来自上一连接") {
		t.Fatalf("system message = %+v, want previous-connection continuity instruction", contextOut.Messages[0])
	}
	if strings.Contains(contextOut.Messages[0].Content, "用户: 我们刚刚说到动作控制太频繁。") {
		t.Fatalf("system message should not embed previous-connection chat history:\n%s", contextOut.Messages[0].Content)
	}
	if contextOut.Messages[1].Role != "user" || contextOut.Messages[1].Content != "我们刚刚说到动作控制太频繁。" {
		t.Fatalf("recent user message = %+v, want previous connection user turn", contextOut.Messages[1])
	}
	if contextOut.Messages[2].Role != "assistant" || contextOut.Messages[2].Content != "我会先降低默认动作干扰。" {
		t.Fatalf("recent assistant message = %+v, want previous connection assistant turn", contextOut.Messages[2])
	}
	if contextOut.Messages[3].Role != "user" || contextOut.Messages[3].Content != "继续刚才那个。" {
		t.Fatalf("current user message = %+v, want current transcript", contextOut.Messages[3])
	}
}
