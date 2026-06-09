package agents

import "testing"

func TestParseModeCommandRecognizesProfessionalEntryAndExit(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		wantMode   Mode
		wantAction string
		wantText   string
	}{
		{
			name:       "enter professional",
			transcript: "请进入专业模式。",
			wantMode:   ModeProfessional,
			wantAction: ModeCommandActionEnterProfessional,
			wantText:   "已进入专业模式。",
		},
		{
			name:       "exit professional",
			transcript: "小智，退出专业模式！",
			wantMode:   ModeCasual,
			wantAction: ModeCommandActionExitProfessional,
			wantText:   "已退出专业模式。",
		},
		{
			name:       "english entry",
			transcript: "Stack-chan, enable professional mode.",
			wantMode:   ModeProfessional,
			wantAction: ModeCommandActionEnterProfessional,
			wantText:   "已进入专业模式。",
		},
		{
			name:       "enter tool",
			transcript: "请进入 OpenClaw 模式。",
			wantMode:   ModeTool,
			wantAction: ModeCommandActionEnterTool,
			wantText:   "已进入工具模式。",
		},
		{
			name:       "enter roleplay",
			transcript: "请进入 Hermes 模式。",
			wantMode:   ModeRoleplay,
			wantAction: ModeCommandActionEnterRoleplay,
			wantText:   "已进入角色模式。",
		},
		{
			name:       "exit tool",
			transcript: "退出工具模式。",
			wantMode:   ModeCasual,
			wantAction: ModeCommandActionExitTool,
			wantText:   "已退出工具模式。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := ParseModeCommand(tt.transcript)
			if !command.Handled || command.Mode != tt.wantMode || command.Action != tt.wantAction || command.SpokenText != tt.wantText {
				t.Fatalf("command = %+v, want mode %s action %s text %q", command, tt.wantMode, tt.wantAction, tt.wantText)
			}
			if (tt.wantAction == ModeCommandActionExitProfessional || tt.wantAction == ModeCommandActionExitRoleplay || tt.wantAction == ModeCommandActionExitTool) && !command.ClearOverride {
				t.Fatalf("command = %+v, want exit to clear override", command)
			}
		})
	}
}

func TestParseModeCommandAvoidsBroadMentions(t *testing.T) {
	for _, transcript := range []string{
		"专业模式是什么？",
		"我想了解一下专业模式。",
		"普通模式和专业模式有什么区别？",
		"OpenClaw 模式有什么用？",
		"Hermes 模式是什么？",
		"",
	} {
		if command := ParseModeCommand(transcript); command.Handled {
			t.Fatalf("ParseModeCommand(%q) = %+v, want not handled", transcript, command)
		}
	}
}
