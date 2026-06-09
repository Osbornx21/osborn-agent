package session

import (
	"context"
	"strconv"
	"time"

	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/stackchan"
)

type Turn struct {
	SessionID  string
	DeviceID   string
	Generation int64
	State      State
}

type ProviderRequestIDs struct {
	ASR string
	LLM string
	TTS string
}

type DownlinkDrainMarker struct {
	Generation int64
	Reason     string
}

type TurnRuntime struct {
	Turn                  Turn
	Context               context.Context
	cancel                context.CancelFunc
	Providers             VoiceProviderSet
	ProviderRequestIDs    ProviderRequestIDs
	LLMToolNameAliases    map[string]string
	DownlinkDrainMarker   DownlinkDrainMarker
	StartedAt             time.Time
	SpeechFinalAt         time.Time
	CanceledAt            time.Time
	FirstUplinkAudio      bool
	FirstLLMToken         bool
	FirstTTSAudio         bool
	FirstDownlinkAudio    bool
	FirstAudibleLatencyMS int64
	TTSStarted            bool
	TTSStopSent           bool
	AssistantText         string
	ASRFinalHandled       bool
	AutoListenTail        bool
	ASRCompletion         chan error
	CompletionRecorded    bool
}

func NewTurnRuntime(parent context.Context, turn Turn) *TurnRuntime {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &TurnRuntime{
		Turn:          turn,
		Context:       ctx,
		cancel:        cancel,
		StartedAt:     time.Now(),
		ASRCompletion: make(chan error, 1),
		DownlinkDrainMarker: DownlinkDrainMarker{
			Generation: turn.Generation,
			Reason:     "turn_active",
		},
	}
}

func (r *TurnRuntime) Cancel(reason string) {
	if r == nil {
		return
	}
	r.DownlinkDrainMarker = DownlinkDrainMarker{
		Generation: r.Turn.Generation,
		Reason:     reason,
	}
	if r.CanceledAt.IsZero() {
		r.CanceledAt = time.Now()
	}
	r.cancel()
}

func (t Turn) IsCurrent(s *Session) bool {
	return s.AcceptsGeneration(t.Generation)
}

func (t Turn) ProviderRequestID(provider string) string {
	return t.SessionID + ":" + strconv.FormatInt(t.Generation, 10) + ":" + provider
}

func (t Turn) ASRStartRequest(startedAt time.Time) providers.ASRStartRequest {
	return providers.ASRStartRequest{
		SessionID:  t.SessionID,
		DeviceID:   t.DeviceID,
		Generation: t.Generation,
		StartedAt:  startedAt,
	}
}

func (t Turn) LLMRequest(text string, createdAt time.Time) providers.LLMRequest {
	return providers.LLMRequest{
		SessionID:  t.SessionID,
		DeviceID:   t.DeviceID,
		Generation: t.Generation,
		Text:       text,
		CreatedAt:  createdAt,
	}
}

func (t Turn) TTSRequest(text string, createdAt time.Time) providers.TTSRequest {
	return providers.TTSRequest{
		SessionID:  t.SessionID,
		DeviceID:   t.DeviceID,
		Generation: t.Generation,
		Text:       text,
		CreatedAt:  createdAt,
	}
}

func (t Turn) StackChanMotionCommand(yaw int, pitch int, speed int, priority string, reason string) stackchan.MotionCommand {
	return stackchan.MotionCommand{
		Generation: t.Generation,
		Yaw:        yaw,
		Pitch:      pitch,
		Speed:      speed,
		Priority:   priority,
		Reason:     reason,
	}
}

func (t Turn) StackChanSceneRequest(scene string, emotion string, caption string, accent string) stackchan.SceneRequest {
	return stackchan.SceneRequest{
		SessionID:  t.SessionID,
		Generation: t.Generation,
		Scene:      scene,
		Emotion:    emotion,
		Caption:    caption,
		Accent:     accent,
	}
}
