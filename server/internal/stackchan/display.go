package stackchan

const (
	DefaultSceneTTLMS      = 1800
	DefaultMaxCaptionChars = 48
)

type DisplayOptions struct {
	SceneTTLMS      int
	MaxCaptionChars int
	LifecycleScenes map[string]ScenePolicy
	EventScenes     map[string]ScenePolicy
	Cards           map[string]DisplayCardPolicy
}

type ScenePolicy struct {
	Scene   string
	Emotion string
	Caption string
	Accent  string
	Motion  *SceneMotion
}

type DisplayCardPolicy struct {
	ScenePolicy
	AllowCaption    bool
	MaxCaptionChars int
}

func DefaultDisplayOptions() DisplayOptions {
	return DisplayOptions{
		SceneTTLMS:      DefaultSceneTTLMS,
		MaxCaptionChars: DefaultMaxCaptionChars,
	}
}

func (o DisplayOptions) withDefaults() DisplayOptions {
	defaults := DefaultDisplayOptions()
	if o.SceneTTLMS <= 0 {
		o.SceneTTLMS = defaults.SceneTTLMS
	}
	if o.MaxCaptionChars <= 0 {
		o.MaxCaptionChars = defaults.MaxCaptionChars
	}
	o.LifecycleScenes = cloneScenePolicies(o.LifecycleScenes)
	o.EventScenes = cloneScenePolicies(o.EventScenes)
	o.Cards = cloneDisplayCardPolicies(o.Cards)
	return o
}

func cloneScenePolicies(policies map[string]ScenePolicy) map[string]ScenePolicy {
	if len(policies) == 0 {
		return nil
	}
	clone := make(map[string]ScenePolicy, len(policies))
	for scene, policy := range policies {
		if policy.Motion != nil {
			motion := *policy.Motion
			policy.Motion = &motion
		}
		clone[scene] = policy
	}
	return clone
}

func cloneDisplayCardPolicies(policies map[string]DisplayCardPolicy) map[string]DisplayCardPolicy {
	if len(policies) == 0 {
		return nil
	}
	clone := make(map[string]DisplayCardPolicy, len(policies))
	for card, policy := range policies {
		if policy.Motion != nil {
			motion := *policy.Motion
			policy.Motion = &motion
		}
		clone[normalizeDisplayCardID(card)] = policy
	}
	return clone
}

func ShortenCaption(caption string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(caption)
	if len(runes) <= maxChars {
		return caption
	}
	return string(runes[:maxChars])
}
