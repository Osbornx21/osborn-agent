package stackchan

const (
	PriorityNormal = "normal"
	PriorityAdmin  = "admin"

	DefaultYawMinDeg     = -45
	DefaultYawMaxDeg     = 45
	DefaultPitchMinDeg   = 0
	DefaultPitchMaxDeg   = 45
	DefaultSpeedMin      = 100
	DefaultSpeedMax      = 1000
	DefaultSpeed         = 150
	DefaultLEDChannelMin = 0
	DefaultLEDChannelMax = 168
)

type MotionCommand struct {
	Generation int64  `json:"generation"`
	Yaw        int    `json:"yaw"`
	Pitch      int    `json:"pitch"`
	Speed      int    `json:"speed"`
	Priority   string `json:"priority"`
	Reason     string `json:"reason"`
}

type MotionLimits struct {
	YawMinDeg    int
	YawMaxDeg    int
	PitchMinDeg  int
	PitchMaxDeg  int
	SpeedMin     int
	SpeedMax     int
	DefaultSpeed int
}

type LEDCommand struct {
	Generation int64  `json:"generation"`
	R          int    `json:"r"`
	G          int    `json:"g"`
	B          int    `json:"b"`
	Priority   string `json:"priority"`
	Reason     string `json:"reason"`
}

func DefaultMotionLimits() MotionLimits {
	return MotionLimits{
		YawMinDeg:    DefaultYawMinDeg,
		YawMaxDeg:    DefaultYawMaxDeg,
		PitchMinDeg:  DefaultPitchMinDeg,
		PitchMaxDeg:  DefaultPitchMaxDeg,
		SpeedMin:     DefaultSpeedMin,
		SpeedMax:     DefaultSpeedMax,
		DefaultSpeed: DefaultSpeed,
	}
}

func (l MotionLimits) withDefaults() MotionLimits {
	defaults := DefaultMotionLimits()
	if l.YawMinDeg == 0 && l.YawMaxDeg == 0 {
		l.YawMinDeg = defaults.YawMinDeg
		l.YawMaxDeg = defaults.YawMaxDeg
	}
	if l.PitchMinDeg == 0 && l.PitchMaxDeg == 0 {
		l.PitchMinDeg = defaults.PitchMinDeg
		l.PitchMaxDeg = defaults.PitchMaxDeg
	}
	if l.SpeedMin == 0 {
		l.SpeedMin = defaults.SpeedMin
	}
	if l.SpeedMax == 0 {
		l.SpeedMax = defaults.SpeedMax
	}
	if l.DefaultSpeed == 0 {
		l.DefaultSpeed = defaults.DefaultSpeed
	}
	return l
}

func ClampMotion(command MotionCommand, limits MotionLimits) MotionCommand {
	limits = limits.withDefaults()
	command.Yaw = clampInt(command.Yaw, limits.YawMinDeg, limits.YawMaxDeg)
	command.Pitch = clampInt(command.Pitch, limits.PitchMinDeg, limits.PitchMaxDeg)
	if command.Speed == 0 {
		command.Speed = limits.DefaultSpeed
	}
	command.Speed = clampInt(command.Speed, limits.SpeedMin, limits.SpeedMax)
	if command.Priority == "" {
		command.Priority = PriorityNormal
	}
	return command
}

func (c MotionCommand) MCPArguments() map[string]any {
	return map[string]any{
		"yaw":   c.Yaw,
		"pitch": c.Pitch,
		"speed": c.Speed,
	}
}

func ClampLED(command LEDCommand) LEDCommand {
	command.R = clampInt(command.R, DefaultLEDChannelMin, DefaultLEDChannelMax)
	command.G = clampInt(command.G, DefaultLEDChannelMin, DefaultLEDChannelMax)
	command.B = clampInt(command.B, DefaultLEDChannelMin, DefaultLEDChannelMax)
	if command.Priority == "" {
		command.Priority = PriorityNormal
	}
	return command
}

func (c LEDCommand) MCPArguments() map[string]any {
	return map[string]any{
		"red":   c.R,
		"green": c.G,
		"blue":  c.B,
	}
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
