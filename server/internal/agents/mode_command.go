package agents

import "strings"

const (
	ModeCommandActionEnterProfessional = "enter_professional"
	ModeCommandActionExitProfessional  = "exit_professional"
	ModeCommandActionEnterRoleplay     = "enter_roleplay"
	ModeCommandActionExitRoleplay      = "exit_roleplay"
	ModeCommandActionEnterTool         = "enter_tool"
	ModeCommandActionExitTool          = "exit_tool"
)

type ModeCommand struct {
	Handled       bool
	Mode          Mode
	ClearOverride bool
	Action        string
	SpokenText    string
}

func ParseModeCommand(transcript string) ModeCommand {
	command := normalizeModeCommandTranscript(transcript)
	command = trimModeCommandPrefixes(command)
	switch {
	case isEnterProfessionalCommand(command):
		return ModeCommand{
			Handled:    true,
			Mode:       ModeProfessional,
			Action:     ModeCommandActionEnterProfessional,
			SpokenText: "已进入专业模式。",
		}
	case isEnterRoleplayCommand(command):
		return ModeCommand{
			Handled:    true,
			Mode:       ModeRoleplay,
			Action:     ModeCommandActionEnterRoleplay,
			SpokenText: "已进入角色模式。",
		}
	case isEnterToolCommand(command):
		return ModeCommand{
			Handled:    true,
			Mode:       ModeTool,
			Action:     ModeCommandActionEnterTool,
			SpokenText: "已进入工具模式。",
		}
	case isExitProfessionalCommand(command):
		return ModeCommand{
			Handled:       true,
			Mode:          ModeCasual,
			ClearOverride: true,
			Action:        ModeCommandActionExitProfessional,
			SpokenText:    "已退出专业模式。",
		}
	case isExitRoleplayCommand(command):
		return ModeCommand{
			Handled:       true,
			Mode:          ModeCasual,
			ClearOverride: true,
			Action:        ModeCommandActionExitRoleplay,
			SpokenText:    "已退出角色模式。",
		}
	case isExitToolCommand(command):
		return ModeCommand{
			Handled:       true,
			Mode:          ModeCasual,
			ClearOverride: true,
			Action:        ModeCommandActionExitTool,
			SpokenText:    "已退出工具模式。",
		}
	default:
		return ModeCommand{}
	}
}

func isEnterProfessionalCommand(command string) bool {
	switch command {
	case "进入专业模式", "切换到专业模式", "开启专业模式", "打开专业模式", "启动专业模式", "启用专业模式",
		"进入专家模式", "切换到专家模式", "开启专家模式", "打开专家模式", "启动专家模式", "启用专家模式",
		"enterprofessionalmode", "switchtoprofessionalmode", "enableprofessionalmode":
		return true
	default:
		return false
	}
}

func isEnterRoleplayCommand(command string) bool {
	switch command {
	case "进入角色模式", "切换到角色模式", "开启角色模式", "打开角色模式", "启动角色模式", "启用角色模式",
		"进入扮演模式", "切换到扮演模式", "开启扮演模式", "打开扮演模式", "启动扮演模式", "启用扮演模式",
		"进入hermes模式", "切换到hermes模式", "开启hermes模式", "打开hermes模式", "启动hermes模式", "启用hermes模式",
		"enterroleplaymode", "switchtoroleplaymode", "enableroleplaymode", "enterhermesmode", "switchtohermesmode", "enablehermesmode":
		return true
	default:
		return false
	}
}

func isEnterToolCommand(command string) bool {
	switch command {
	case "进入工具模式", "切换到工具模式", "开启工具模式", "打开工具模式", "启动工具模式", "启用工具模式",
		"进入openclaw模式", "切换到openclaw模式", "开启openclaw模式", "打开openclaw模式", "启动openclaw模式", "启用openclaw模式",
		"entertoolmode", "switchtotoolmode", "enabletoolmode", "enteropenclawmode", "switchtoopenclawmode", "enableopenclawmode":
		return true
	default:
		return false
	}
}

func isExitProfessionalCommand(command string) bool {
	switch command {
	case "退出专业模式", "关闭专业模式", "离开专业模式", "结束专业模式",
		"退出专家模式", "关闭专家模式", "离开专家模式", "结束专家模式",
		"回到日常模式", "切换到日常模式", "恢复日常模式",
		"回到普通模式", "切换到普通模式", "恢复普通模式",
		"exitprofessionalmode", "disableprofessionalmode", "leaveprofessionalmode":
		return true
	default:
		return false
	}
}

func isExitRoleplayCommand(command string) bool {
	switch command {
	case "退出角色模式", "关闭角色模式", "离开角色模式", "结束角色模式",
		"退出扮演模式", "关闭扮演模式", "离开扮演模式", "结束扮演模式",
		"退出hermes模式", "关闭hermes模式", "离开hermes模式", "结束hermes模式",
		"exitroleplaymode", "disableroleplaymode", "leaveroleplaymode", "exithermesmode", "disablehermesmode", "leavehermesmode":
		return true
	default:
		return false
	}
}

func isExitToolCommand(command string) bool {
	switch command {
	case "退出工具模式", "关闭工具模式", "离开工具模式", "结束工具模式",
		"退出openclaw模式", "关闭openclaw模式", "离开openclaw模式", "结束openclaw模式",
		"exittoolmode", "disabletoolmode", "leavetoolmode", "exitopenclawmode", "disableopenclawmode", "leaveopenclawmode":
		return true
	default:
		return false
	}
}

func trimModeCommandPrefixes(command string) string {
	for {
		trimmed := command
		for _, prefix := range []string{"stackchan", "小智同学", "小智", "请", "麻烦", "帮我"} {
			trimmed = strings.TrimPrefix(trimmed, prefix)
		}
		if trimmed == command {
			return command
		}
		command = trimmed
	}
}

func normalizeModeCommandTranscript(transcript string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r',
			'，', '。', '！', '？', '、', '：', '；',
			',', '.', '!', '?', ':', ';', '-',
			'“', '”', '"', '\'', '‘', '’',
			'（', '）', '(', ')', '【', '】', '[', ']':
			return -1
		default:
			return r
		}
	}, strings.ToLower(strings.TrimSpace(transcript)))
}
