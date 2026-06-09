package stackchan

import (
	"errors"
	"sort"
	"strings"
)

const (
	ToolExpress                = "stackchan.express"
	ToolExpressionSequence     = "stackchan.expression_sequence"
	ToolPlayExpressionSequence = "stackchan.play_expression_sequence"

	MaxExpressionSequenceCues = 3

	CueAttentive = "attentive"
	CueNod       = "nod"
	CueCelebrate = "celebrate"
	CueThinking  = "thinking"
	CueSettle    = "settle"
)

var ErrExpressionCueInvalid = errors.New("stackchan expression cue is invalid")

type ExpressionPlan struct {
	Cue    string
	Motion *MotionCommand
	LED    *LEDCommand
	Scene  ScenePolicy
}

type ExpressionPolicy struct {
	Motion *MotionCommand
	LED    *LEDCommand
	Scene  ScenePolicy
}

func ExpressionForCue(cue string) (ExpressionPlan, error) {
	return ExpressionForCueWithPolicies(cue, nil)
}

func ExpressionForCueWithPolicies(cue string, policies map[string]ExpressionPolicy) (ExpressionPlan, error) {
	normalizedCue := normalizeCue(cue)
	plan, err := defaultExpressionForCue(normalizedCue)
	if err != nil {
		return ExpressionPlan{}, err
	}
	if policy, ok := policies[normalizedCue]; ok {
		plan = mergeExpressionPolicy(plan, policy)
	}
	return plan, nil
}

func IsExpressionCue(cue string) bool {
	_, err := defaultExpressionForCue(normalizeCue(cue))
	return err == nil
}

func defaultExpressionForCue(cue string) (ExpressionPlan, error) {
	switch cue {
	case CueAttentive:
		return ExpressionPlan{
			Cue:    CueAttentive,
			Motion: &MotionCommand{Yaw: 0, Pitch: 12, Speed: 180, Priority: PriorityNormal, Reason: "stackchan_express"},
			LED:    &LEDCommand{R: 0, G: 96, B: 168, Priority: PriorityNormal, Reason: "stackchan_express"},
			Scene: ScenePolicy{
				Scene:   SceneListening,
				Emotion: EmotionCurious,
				Caption: "我在。",
				Accent:  AccentCyan,
				Motion:  &SceneMotion{Preset: MotionPresetAttentive, Intensity: 0.3},
			},
		}, nil
	case CueNod:
		return ExpressionPlan{
			Cue:    CueNod,
			Motion: &MotionCommand{Yaw: 0, Pitch: 16, Speed: 220, Priority: PriorityNormal, Reason: "stackchan_express"},
			LED:    &LEDCommand{R: 0, G: 168, B: 96, Priority: PriorityNormal, Reason: "stackchan_express"},
			Scene: ScenePolicy{
				Scene:   SceneSpeaking,
				Emotion: EmotionReady,
				Caption: "我点头。",
				Accent:  AccentGreen,
				Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.35},
			},
		}, nil
	case CueCelebrate:
		return ExpressionPlan{
			Cue:    CueCelebrate,
			Motion: &MotionCommand{Yaw: 8, Pitch: 10, Speed: 260, Priority: PriorityNormal, Reason: "stackchan_express"},
			LED:    &LEDCommand{R: 48, G: 168, B: 96, Priority: PriorityNormal, Reason: "stackchan_express"},
			Scene: ScenePolicy{
				Scene:   SceneSpeaking,
				Emotion: EmotionHappy,
				Caption: "太好了。",
				Accent:  AccentGreen,
				Motion:  &SceneMotion{Preset: MotionPresetNodSoft, Intensity: 0.45},
			},
		}, nil
	case CueThinking:
		return ExpressionPlan{
			Cue:    CueThinking,
			Motion: &MotionCommand{Yaw: 0, Pitch: 8, Speed: 150, Priority: PriorityNormal, Reason: "stackchan_express"},
			LED:    &LEDCommand{R: 168, G: 112, B: 0, Priority: PriorityNormal, Reason: "stackchan_express"},
			Scene: ScenePolicy{
				Scene:   SceneThinking,
				Emotion: EmotionCurious,
				Caption: "我在想。",
				Accent:  AccentAmber,
				Motion:  &SceneMotion{Preset: MotionPresetAttentive, Intensity: 0.25},
			},
		}, nil
	case CueSettle:
		return ExpressionPlan{
			Cue:    CueSettle,
			Motion: &MotionCommand{Yaw: 0, Pitch: 0, Speed: 150, Priority: PriorityNormal, Reason: "stackchan_express"},
			LED:    &LEDCommand{R: 0, G: 24, B: 32, Priority: PriorityNormal, Reason: "stackchan_express"},
			Scene: ScenePolicy{
				Scene:   SceneIdle,
				Emotion: EmotionNeutral,
				Accent:  AccentDefault,
				Motion:  &SceneMotion{Preset: MotionPresetNone},
			},
		}, nil
	default:
		return ExpressionPlan{}, ErrExpressionCueInvalid
	}
}

func mergeExpressionPolicy(base ExpressionPlan, override ExpressionPolicy) ExpressionPlan {
	if override.Motion != nil {
		motion := *override.Motion
		motion.Priority = PriorityNormal
		motion.Reason = "stackchan_express"
		base.Motion = &motion
	}
	if override.LED != nil {
		led := *override.LED
		led.Priority = PriorityNormal
		led.Reason = "stackchan_express"
		base.LED = &led
	}
	base.Scene = mergeScenePolicy(base.Scene, override.Scene)
	return base
}

func ExpressionToolDescription() string {
	return "Trigger one safe semantic StackChan body/display expression cue."
}

func ExpressionToolInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cue": map[string]any{
				"type":        "string",
				"description": "Semantic expression cue. The gateway maps it to bounded head, LED and screen commands.",
				"enum":        []any{CueAttentive, CueNod, CueCelebrate, CueThinking, CueSettle},
			},
		},
		"required":             []any{"cue"},
		"additionalProperties": false,
	}
}

func ExpressionSequenceToolDescription() string {
	return "Trigger a short safe sequence of semantic StackChan expression cues."
}

func ExpressionSequenceToolInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cues": map[string]any{
				"type":        "array",
				"description": "One to three semantic expression cues. The gateway maps each cue to bounded head, LED and screen commands.",
				"minItems":    1,
				"maxItems":    MaxExpressionSequenceCues,
				"items": map[string]any{
					"type": "string",
					"enum": []any{CueAttentive, CueNod, CueCelebrate, CueThinking, CueSettle},
				},
			},
		},
		"required":             []any{"cues"},
		"additionalProperties": false,
	}
}

func ExpressionSequencePresetToolDescription() string {
	return "Play one operator-configured StackChan expression sequence preset."
}

func ExpressionSequencePresetToolInputSchema(sequenceIDs []string) map[string]any {
	sequences := make([]string, 0, len(sequenceIDs))
	for _, sequenceID := range sequenceIDs {
		sequenceID = normalizeExpressionSequenceID(sequenceID)
		if sequenceID != "" {
			sequences = append(sequences, sequenceID)
		}
	}
	sort.Strings(sequences)
	enum := make([]any, 0, len(sequences))
	for _, sequence := range sequences {
		enum = append(enum, sequence)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sequence": map[string]any{
				"type":        "string",
				"description": "Configured expression sequence preset id. Only ids configured by the operator are accepted.",
				"enum":        enum,
			},
		},
		"required":             []any{"sequence"},
		"additionalProperties": false,
	}
}

func normalizeCue(cue string) string {
	return strings.ToLower(strings.TrimSpace(cue))
}

func normalizeExpressionSequenceID(sequenceID string) string {
	return strings.ToLower(strings.TrimSpace(sequenceID))
}
