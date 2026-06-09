package stackchan

const (
	DefaultLifecycleLEDListeningR = 0
	DefaultLifecycleLEDListeningG = 168
	DefaultLifecycleLEDListeningB = 0

	DefaultLifecycleLEDThinkingR = 168
	DefaultLifecycleLEDThinkingG = 112
	DefaultLifecycleLEDThinkingB = 0

	DefaultLifecycleLEDSpeakingR = 0
	DefaultLifecycleLEDSpeakingG = 0
	DefaultLifecycleLEDSpeakingB = 168

	DefaultLifecycleLEDIdleR = 0
	DefaultLifecycleLEDIdleG = 0
	DefaultLifecycleLEDIdleB = 0
)

func DefaultLifecycleLEDs() map[string]LEDCommand {
	return map[string]LEDCommand{
		SceneListening: {
			R:      DefaultLifecycleLEDListeningR,
			G:      DefaultLifecycleLEDListeningG,
			B:      DefaultLifecycleLEDListeningB,
			Reason: "listen_start",
		},
		SceneThinking: {
			R:      DefaultLifecycleLEDThinkingR,
			G:      DefaultLifecycleLEDThinkingG,
			B:      DefaultLifecycleLEDThinkingB,
			Reason: "thinking_start",
		},
		SceneSpeaking: {
			R:      DefaultLifecycleLEDSpeakingR,
			G:      DefaultLifecycleLEDSpeakingG,
			B:      DefaultLifecycleLEDSpeakingB,
			Reason: "speaking_start",
		},
		SceneIdle: {
			R:      DefaultLifecycleLEDIdleR,
			G:      DefaultLifecycleLEDIdleG,
			B:      DefaultLifecycleLEDIdleB,
			Reason: "idle_start",
		},
	}
}

func LifecycleLEDReason(lifecycle string) string {
	switch lifecycle {
	case SceneListening:
		return "listen_start"
	case SceneThinking:
		return "thinking_start"
	case SceneSpeaking:
		return "speaking_start"
	case SceneIdle:
		return "idle_start"
	default:
		return "lifecycle_" + lifecycle
	}
}
