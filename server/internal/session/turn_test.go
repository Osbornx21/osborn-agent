package session

import (
	"testing"
	"time"

	"stackchan-gateway/internal/stackchan"
)

func TestTurnIsCurrent(t *testing.T) {
	s := readySession(t)
	turn := s.CurrentTurn()

	if !turn.IsCurrent(s) {
		t.Fatal("current turn reported stale")
	}

	s.Abort()

	if turn.IsCurrent(s) {
		t.Fatal("old turn reported current after abort")
	}
}

func TestTurnCarriesSessionIdentity(t *testing.T) {
	s := readySession(t)
	turn := s.CurrentTurn()

	if turn.SessionID != "sess_1" {
		t.Fatalf("session id = %q, want sess_1", turn.SessionID)
	}
	if turn.DeviceID != "stackchan-s3-main" {
		t.Fatalf("device id = %q, want stackchan-s3-main", turn.DeviceID)
	}
	if turn.Generation != 1 {
		t.Fatalf("generation = %d, want 1", turn.Generation)
	}
	if turn.State != StateIdle {
		t.Fatalf("state = %s, want %s", turn.State, StateIdle)
	}
}

func TestTurnBuildsProviderRequests(t *testing.T) {
	turn := Turn{
		SessionID:  "sess_provider",
		DeviceID:   "stackchan-s3-main",
		Generation: 9,
		State:      StateProcessing,
	}
	now := time.Now()

	asr := turn.ASRStartRequest(now)
	if asr.SessionID != turn.SessionID || asr.DeviceID != turn.DeviceID || asr.Generation != turn.Generation || !asr.StartedAt.Equal(now) {
		t.Fatalf("ASRStartRequest() = %+v, want turn identity and timestamp", asr)
	}

	llm := turn.LLMRequest("你好", now)
	if llm.Text != "你好" || llm.SessionID != turn.SessionID || llm.Generation != turn.Generation || !llm.CreatedAt.Equal(now) {
		t.Fatalf("LLMRequest() = %+v, want text and turn identity", llm)
	}

	tts := turn.TTSRequest("我准备好了。", now)
	if tts.Text != "我准备好了。" || tts.DeviceID != turn.DeviceID || tts.Generation != turn.Generation || !tts.CreatedAt.Equal(now) {
		t.Fatalf("TTSRequest() = %+v, want text and turn identity", tts)
	}

	if id := turn.ProviderRequestID("tts"); id != "sess_provider:9:tts" {
		t.Fatalf("ProviderRequestID(tts) = %q, want sess_provider:9:tts", id)
	}
}

func TestTurnBuildsStackChanCommandsWithGeneration(t *testing.T) {
	turn := Turn{
		SessionID:  "sess_stackchan",
		DeviceID:   "stackchan-s3-main",
		Generation: 11,
		State:      StateSpeaking,
	}

	motion := turn.StackChanMotionCommand(15, 8, 150, stackchan.PriorityNormal, "assistant_speaking")
	if motion.Generation != turn.Generation || motion.Yaw != 15 || motion.Pitch != 8 || motion.Speed != 150 || motion.Reason != "assistant_speaking" {
		t.Fatalf("StackChanMotionCommand() = %+v, want turn generation and motion fields", motion)
	}

	scene := turn.StackChanSceneRequest(stackchan.SceneSpeaking, "curious", "我在查一下。", "cyan")
	if scene.SessionID != turn.SessionID || scene.Generation != turn.Generation || scene.Scene != stackchan.SceneSpeaking || scene.Caption != "我在查一下。" {
		t.Fatalf("StackChanSceneRequest() = %+v, want turn identity and scene fields", scene)
	}
}
