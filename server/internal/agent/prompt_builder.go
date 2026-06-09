package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/session"
)

type PromptBuilderOptions struct {
	Persona         Persona
	MemoryStore     MemoryStore
	MemoryMaxItems  int
	RecentTurnStore RecentTurnReader
	RecentTurns     int
	OwnerUserID     string
}

type PromptBuilder struct {
	persona         Persona
	memoryStore     MemoryStore
	memoryMaxItems  int
	recentTurnStore RecentTurnReader
	recentTurns     int
	ownerUserID     string
}

func NewPromptBuilder(options PromptBuilderOptions) *PromptBuilder {
	memoryMaxItems := options.MemoryMaxItems
	if memoryMaxItems <= 0 {
		memoryMaxItems = 5
	}
	ownerUserID := strings.TrimSpace(options.OwnerUserID)
	if ownerUserID == "" {
		ownerUserID = "owner"
	}
	store := options.MemoryStore
	if store == nil {
		store = NewStaticMemoryStore(nil)
	}
	return &PromptBuilder{
		persona:         options.Persona,
		memoryStore:     store,
		memoryMaxItems:  memoryMaxItems,
		recentTurnStore: options.RecentTurnStore,
		recentTurns:     options.RecentTurns,
		ownerUserID:     ownerUserID,
	}
}

func (b *PromptBuilder) BuildLLMContext(ctx context.Context, request session.LLMContextRequest) (session.LLMContext, error) {
	if b == nil {
		text := strings.TrimSpace(request.Transcript)
		contextOut := session.LLMContext{Text: text}
		if text != "" {
			contextOut.Messages = []providers.LLMMessage{{Role: "user", Content: text}}
		}
		return contextOut, nil
	}
	memories, err := b.memoryStore.Retrieve(ctx, MemoryQuery{
		UserID:    b.ownerUserID,
		DeviceID:  request.DeviceID,
		SessionID: request.SessionID,
		Query:     request.Transcript,
		Limit:     b.memoryMaxItems,
	})
	if err != nil {
		return session.LLMContext{}, err
	}

	recentTurns, err := b.loadRecentTurns(ctx, request.DeviceID)
	if err != nil {
		return session.LLMContext{}, err
	}

	currentTurns, olderTurns := splitRecentTurnsBySession(recentTurns, request.SessionID)
	historyTurns, referenceTurns, historyMode := selectProviderHistoryTurns(currentTurns, olderTurns)
	systemText := appendAgentModeContext(b.renderProviderSystemContext(memories, referenceTurns, historyMode), request.AgentMode)
	legacySystemText := appendAgentModeContext(b.renderSystemContext(memories, recentTurns, request.SessionID), request.AgentMode)
	userText := strings.TrimSpace(request.Transcript)
	text := renderLegacyPrompt(legacySystemText, userText)
	messages := buildProviderMessages(systemText, historyTurns, userText)
	return session.LLMContext{
		Text:            text,
		Messages:        messages,
		MemoryCount:     len(memories),
		RecentTurnCount: len(recentTurns),
		PersonaName:     b.persona.Name,
	}, nil
}

func (b *PromptBuilder) loadRecentTurns(ctx context.Context, deviceID string) ([]RecentTurn, error) {
	if b.recentTurnStore == nil || b.recentTurns <= 0 {
		return nil, nil
	}
	return b.recentTurnStore.RecentTurns(ctx, deviceID, b.recentTurns)
}

func (b *PromptBuilder) renderSystemContext(memories []Memory, recentTurns []RecentTurn, currentSessionID string) string {
	var out strings.Builder
	b.writeBaseSystemContext(&out, memories)
	if len(recentTurns) > 0 {
		currentTurns, olderTurns := splitRecentTurnsBySession(recentTurns, currentSessionID)
		if len(currentTurns) > 0 {
			out.WriteString("当前会话最近对话（从旧到新）:\n")
			writeRecentTurns(&out, currentTurns)
			if len(olderTurns) > 0 {
				out.WriteString("较早跨会话参考（只在相关时使用）:\n")
				writeRecentTurns(&out, olderTurns)
			}
			out.WriteString("如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须优先根据当前会话最近对话补全语境再回答。\n")
			out.WriteString("不要机械复述最近对话；较早跨会话内容只在确实相关时使用。\n")
		} else {
			out.WriteString("最近对话（可能来自上一连接，从旧到新）:\n")
			writeRecentTurns(&out, olderTurns)
			out.WriteString("如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须根据最近对话补全语境再回答。\n")
			out.WriteString("不要机械复述最近对话；只在回答当前问题需要时使用它。\n")
		}
	}
	return strings.TrimSpace(out.String())
}

func (b *PromptBuilder) renderProviderSystemContext(memories []Memory, referenceTurns []RecentTurn, historyMode string) string {
	var out strings.Builder
	b.writeBaseSystemContext(&out, memories)
	if len(referenceTurns) > 0 {
		out.WriteString("较早跨会话参考（只在相关时使用）:\n")
		writeRecentTurns(&out, referenceTurns)
	}
	switch historyMode {
	case "current":
		out.WriteString("当前会话最近对话已作为 chat history 提供。")
		out.WriteString("如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须优先根据 chat history 补全语境再回答。\n")
		out.WriteString("不要机械复述最近对话；较早跨会话内容只在确实相关时使用。\n")
	case "previous":
		out.WriteString("最近对话已作为 chat history 提供，可能来自上一连接。")
		out.WriteString("如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须根据 chat history 补全语境再回答。\n")
		out.WriteString("不要机械复述最近对话；只在回答当前问题需要时使用它。\n")
	}
	return strings.TrimSpace(out.String())
}

func (b *PromptBuilder) writeBaseSystemContext(out *strings.Builder, memories []Memory) {
	out.WriteString("You are ")
	out.WriteString(nonEmpty(b.persona.Name, "Stack-chan"))
	out.WriteString(".\n")
	if b.persona.Identity != "" {
		out.WriteString("Identity: ")
		out.WriteString(b.persona.Identity)
		out.WriteByte('\n')
	}
	if len(b.persona.CoreRules) > 0 {
		out.WriteString("Rules:\n")
		for _, rule := range b.persona.CoreRules {
			rule = strings.TrimSpace(rule)
			if rule == "" {
				continue
			}
			out.WriteString("- ")
			out.WriteString(rule)
			out.WriteByte('\n')
		}
	}
	if len(b.persona.Style) > 0 {
		out.WriteString("Style:\n")
		keys := make([]string, 0, len(b.persona.Style))
		for key := range b.persona.Style {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := strings.TrimSpace(b.persona.Style[key])
			if value == "" {
				continue
			}
			out.WriteString("- ")
			out.WriteString(key)
			out.WriteString(": ")
			out.WriteString(value)
			out.WriteByte('\n')
		}
	}
	out.WriteString("语音输出契约:\n")
	out.WriteString("- 默认用中文短句回答，内容要能直接朗读；用户明确要求时可以分点。\n")
	out.WriteString("- 不要输出 JSON 或工具调用文本；需要工具时只通过系统提供的 tool schema。\n")
	if len(memories) > 0 {
		out.WriteString("Relevant memories:\n")
		for _, memory := range memories {
			content := strings.TrimSpace(memory.Content)
			if content == "" {
				continue
			}
			out.WriteString("- [")
			out.WriteString(nonEmpty(memory.Type, "memory"))
			out.WriteString("] ")
			out.WriteString(content)
			out.WriteByte('\n')
		}
	}
}

func appendAgentModeContext(systemText string, mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" || mode == "casual" || mode == "auto" {
		return strings.TrimSpace(systemText)
	}
	modeText := agentModeSystemContext(mode)
	if modeText == "" {
		return strings.TrimSpace(systemText)
	}
	systemText = strings.TrimSpace(systemText)
	if systemText == "" {
		return modeText
	}
	return systemText + "\n" + modeText
}

func agentModeSystemContext(mode string) string {
	switch mode {
	case "roleplay":
		return strings.Join([]string{
			"当前模式: roleplay",
			"- 角色模式下保持用户已设定的人设、场景和语气，但不要牺牲事实准确性。",
			"- 外部角色 bridge 不可用时，直接用当前模型短答，不要声称已经调用 Hermes。",
		}, "\n")
	case "tool":
		return strings.Join([]string{
			"当前模式: tool",
			"- 工具模式下优先判断请求是否适合已授权工具；需要工具时只通过系统提供的 tool schema。",
			"- 没有合适工具或工具不可用时，必须直接说明可做的一般分析或下一步，不要编造执行结果。",
		}, "\n")
	case "professional":
		return strings.Join([]string{
			"当前模式: professional",
			"- 专业模式下优先使用已授权专业知识工具，例如 v21.voice_query；回答要短、结论先行。",
			"- 工具不可用时必须说明只能做一般分析，不要伪造检索、引用或证据。",
		}, "\n")
	default:
		return ""
	}
}

func renderLegacyPrompt(systemText string, transcript string) string {
	var out strings.Builder
	systemText = strings.TrimSpace(systemText)
	if systemText != "" {
		out.WriteString(systemText)
		out.WriteByte('\n')
	}
	out.WriteString("当前用户消息:\n")
	out.WriteString(strings.TrimSpace(transcript))
	return out.String()
}

func selectProviderHistoryTurns(currentTurns []RecentTurn, olderTurns []RecentTurn) ([]RecentTurn, []RecentTurn, string) {
	if len(currentTurns) > 0 {
		return currentTurns, olderTurns, "current"
	}
	if len(olderTurns) > 0 {
		return olderTurns, nil, "previous"
	}
	return nil, nil, ""
}

func buildProviderMessages(systemText string, historyTurns []RecentTurn, userText string) []providers.LLMMessage {
	messages := make([]providers.LLMMessage, 0, 1+len(historyTurns)*2+1)
	systemText = strings.TrimSpace(systemText)
	if systemText != "" {
		messages = append(messages, providers.LLMMessage{Role: "system", Content: systemText})
	}
	for _, turn := range historyTurns {
		priorUser := strings.TrimSpace(turn.UserText)
		priorAssistant := strings.TrimSpace(turn.AssistantText)
		if priorUser == "" || priorAssistant == "" {
			continue
		}
		messages = append(messages,
			providers.LLMMessage{Role: "user", Content: priorUser},
			providers.LLMMessage{Role: "assistant", Content: priorAssistant},
		)
	}
	userText = strings.TrimSpace(userText)
	if userText != "" {
		messages = append(messages, providers.LLMMessage{Role: "user", Content: userText})
	}
	return messages
}

func splitRecentTurnsBySession(turns []RecentTurn, currentSessionID string) ([]RecentTurn, []RecentTurn) {
	currentSessionID = strings.TrimSpace(currentSessionID)
	current := make([]RecentTurn, 0, len(turns))
	older := make([]RecentTurn, 0, len(turns))
	for _, turn := range turns {
		sessionID := strings.TrimSpace(turn.SessionID)
		if currentSessionID == "" || sessionID == "" || sessionID == currentSessionID {
			current = append(current, turn)
			continue
		}
		older = append(older, turn)
	}
	return current, older
}

func writeRecentTurns(out *strings.Builder, turns []RecentTurn) {
	for _, turn := range turns {
		userText := strings.TrimSpace(turn.UserText)
		assistantText := strings.TrimSpace(turn.AssistantText)
		if userText == "" || assistantText == "" {
			continue
		}
		out.WriteString("- 用户: ")
		out.WriteString(userText)
		out.WriteByte('\n')
		out.WriteString("  助手: ")
		out.WriteString(assistantText)
		out.WriteByte('\n')
	}
}

func nonEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func (m Memory) String() string {
	return fmt.Sprintf("%s:%s", m.Type, m.Content)
}
