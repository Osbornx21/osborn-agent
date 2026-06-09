package mcp

const (
	ToolGetHeadAngles    = "self.robot.get_head_angles"
	ToolSetHeadAngles    = "self.robot.set_head_angles"
	ToolSetLEDColor      = "self.robot.set_led_color"
	ToolDeviceStatus     = "self.get_device_status"
	ToolSetVolume        = "self.audio_speaker.set_volume"
	ToolScreenBrightness = "self.screen.set_brightness"
	ToolScreenTheme      = "self.screen.set_theme"
	ToolSetScreenScene   = "self.screen.set_scene"
	ToolTakePhoto        = "self.camera.take_photo"
)

var DefaultAllowedTools = []string{
	ToolGetHeadAngles,
	ToolSetHeadAngles,
	ToolSetLEDColor,
	ToolDeviceStatus,
	ToolSetVolume,
	ToolScreenBrightness,
	ToolScreenTheme,
	ToolSetScreenScene,
}

type Allowlist struct {
	allowed map[string]struct{}
}

func NewAllowlist(tools []string) Allowlist {
	allowlist := Allowlist{allowed: make(map[string]struct{}, len(tools))}
	for _, tool := range tools {
		if tool == "" {
			continue
		}
		allowlist.allowed[tool] = struct{}{}
	}
	return allowlist
}

func NewDefaultAllowlist() Allowlist {
	return NewAllowlist(DefaultAllowedTools)
}

func (a Allowlist) Allows(tool string) bool {
	_, ok := a.allowed[tool]
	return ok
}
