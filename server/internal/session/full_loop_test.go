package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"stackchan-gateway/internal/audio"
	"stackchan-gateway/internal/camera"
	"stackchan-gateway/internal/homeassistant"
	"stackchan-gateway/internal/mcp"
	"stackchan-gateway/internal/observability"
	"stackchan-gateway/internal/protocol/xiaozhi"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/reminder"
	"stackchan-gateway/internal/search"
	"stackchan-gateway/internal/stackchan"
	servicetools "stackchan-gateway/internal/tools"
)

func TestVoiceLoopConnectsMockProvidersToXiaozhiDownlink(t *testing.T) {
	ctx := context.Background()
	session := New("sess_voice", "stackchan-s3-main", "client-1")
	registry := providers.NewRegistry(providers.MockConfig{
		ASRFinalDelayMS:      1,
		LLMFirstTokenDelayMS: 1,
		TTSFirstFrameDelayMS: 1,
		TTSFrameCount:        1,
	})
	asr, err := registry.ASRProvider("mock")
	if err != nil {
		t.Fatalf("ASRProvider(mock) error = %v", err)
	}
	llm, err := registry.LLMProvider("mock")
	if err != nil {
		t.Fatalf("LLMProvider(mock) error = %v", err)
	}
	tts, err := registry.TTSProvider("mock")
	if err != nil {
		t.Fatalf("TTSProvider(mock) error = %v", err)
	}

	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:         session,
		ASR:             asr,
		LLM:             llm,
		TTS:             tts,
		Downlink:        downlink,
		Pacer:           audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:   traceRecorder,
		Metrics:         observability.NewMetrics(),
		ASRProviderName: providers.ProviderMock,
		LLMProviderName: providers.ProviderMock,
		TTSProviderName: providers.ProviderMock,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}

	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	frame := audio.NewOpusFrame([]byte{0x01, 0x02, 0x03}, xiaozhi.XiaozhiUplinkSampleRateHz, xiaozhi.XiaozhiFrameDurationMS, time.Now())
	if err := loop.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}
	if err := loop.HandleListenStop(ctx); err != nil {
		t.Fatalf("HandleListenStop() error = %v", err)
	}

	sequence := downlink.Sequence()
	want := []string{
		"json:hello",
		"json:stt:你好，我是 StackChan。",
		"json:tts:start",
		"json:tts:sentence_start:你好，我准备好了。",
		"binary",
		"json:tts:stop",
	}
	if len(sequence) != len(want) {
		t.Fatalf("sequence len = %d, want %d; sequence=%v", len(sequence), len(want), sequence)
	}
	for i := range want {
		if sequence[i] != want[i] {
			t.Fatalf("sequence[%d] = %q, want %q; sequence=%v", i, sequence[i], want[i], sequence)
		}
	}
	if session.State() != StateIdle {
		t.Fatalf("session state = %s, want %s", session.State(), StateIdle)
	}

	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	assertTraceContainsEvents(t, traceEvents, []string{
		"hello_received",
		"listen_start",
		"first_uplink_audio",
		"listen_stop_received",
		"asr_finish_sent",
		"speech_final",
		"llm_request",
		"first_llm_token",
		"tts_request",
		"first_tts_audio",
		"first_downlink_audio_sent",
		"tts_stop_sent",
		"turn_complete",
	})
	assertTraceDoesNotContainField(t, traceEvents, "text")
}

func TestVoiceLoopTracesTTSAudioQualityWithoutAudioPayload(t *testing.T) {
	ctx := context.Background()
	traceOutput := &lockedBuffer{}
	stats, err := audio.AnalyzePCM16LE([]byte{0x00, 0x00, 0x00, 0x80, 0xff, 0x7f}, audio.PCM16AnalysisOptions{
		SampleRateHz:         24000,
		Channels:             1,
		SilenceThresholdDBFS: -50,
	})
	if err != nil {
		t.Fatalf("AnalyzePCM16LE() error = %v", err)
	}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:         New("sess_tts_audio_quality", "stackchan-s3-main", "client-1"),
		ASR:             autoFinalASRProvider{text: "你好。"},
		LLM:             &recordingLLMProvider{},
		TTS:             qualityTTSProvider{stats: stats},
		Downlink:        &recordingDownlink{},
		Pacer:           audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:   observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput, RedactSecrets: true}),
		TTSProviderName: "quality-tts",
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}

	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	frame := audio.NewOpusFrame([]byte{0x01, 0x02, 0x03}, xiaozhi.XiaozhiUplinkSampleRateHz, xiaozhi.XiaozhiFrameDurationMS, time.Now())
	if err := loop.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}

	traceEvents := waitForTraceEvent(t, traceOutput, "tts_audio_quality", time.Second)
	event := findTraceEventByFields(t, traceEvents, "tts_audio_quality", map[string]any{
		"sample_rate_hz": float64(24000),
		"channels":       float64(1),
		"sample_count":   float64(3),
		"peak_dbfs":      float64(0),
	})
	if event.Provider != "quality-tts" {
		t.Fatalf("tts_audio_quality provider = %q, want quality-tts", event.Provider)
	}
	for _, forbidden := range []string{"text", "audio", "pcm", "opus", "payload"} {
		assertTraceDoesNotContainField(t, []observability.TraceEvent{event}, forbidden)
	}
}

func TestVoiceLoopProcessesASRFinalWithoutClientListenStop(t *testing.T) {
	ctx := context.Background()
	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       New("sess_asr_auto_final", "stackchan-s3-main", "client-1"),
		ASR:           autoFinalASRProvider{text: "你好。"},
		LLM:           &recordingLLMProvider{},
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	frame := audio.NewOpusFrame([]byte{0x01, 0x02, 0x03}, xiaozhi.XiaozhiUplinkSampleRateHz, xiaozhi.XiaozhiFrameDurationMS, time.Now())
	if err := loop.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}

	traceEvents := waitForTraceEvent(t, traceOutput, "turn_complete", time.Second)
	assertTraceContainsEvents(t, traceEvents, []string{
		"hello_received",
		"listen_start",
		"first_uplink_audio",
		"speech_final",
		"first_downlink_audio_sent",
		"turn_complete",
	})
	for _, event := range traceEvents {
		if event.Event == "listen_stop_received" {
			t.Fatalf("trace unexpectedly required client listen stop: %+v", event)
		}
	}
	sequence := downlink.Sequence()
	if !containsSequenceItem(sequence, "json:stt:你好。") || !containsSequenceItem(sequence, "json:tts:start") || !containsSequenceItem(sequence, "binary") {
		t.Fatalf("downlink sequence = %v, want STT and TTS without client stop", sequence)
	}
}

func TestVoiceLoopTimesOutAutoListenTailAfterSpeaking(t *testing.T) {
	ctx := context.Background()
	traceOutput := &lockedBuffer{}
	tailStream := newBlockingASRStream()
	asr := &twoStageASRProvider{
		first:  autoFinalASRProvider{text: "你好。"},
		second: tailStream,
	}
	session := New("sess_auto_listen_tail_timeout", "stackchan-s3-main", "client-1")
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:               session,
		ASR:                   asr,
		LLM:                   &recordingLLMProvider{},
		TTS:                   providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:              &recordingDownlink{},
		Pacer:                 audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:         observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput}),
		AutoListenTailTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	frame := audio.NewOpusFrame([]byte{0x01, 0x02, 0x03}, xiaozhi.XiaozhiUplinkSampleRateHz, xiaozhi.XiaozhiFrameDurationMS, time.Now())
	if err := loop.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}
	waitForTraceEvent(t, traceOutput, "tts_stop_sent", time.Second)
	if session.State() != StateIdle {
		t.Fatalf("session state after first turn = %s, want idle", session.State())
	}

	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("tail HandleListenStart() error = %v", err)
	}
	if err := loop.AcceptOpus(frame); err != nil {
		t.Fatalf("tail AcceptOpus() error = %v", err)
	}
	select {
	case <-tailStream.closed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tail ASR stream close")
	}
	waitForTraceEvent(t, traceOutput, "asr_tail_listen_timeout", time.Second)
	events := waitForTraceEvent(t, traceOutput, "turn_complete", time.Second)
	assertTraceContainsEvents(t, events, []string{"asr_tail_listen_timeout", "turn_complete"})
	if !traceHasTurnCompleteError(events, "asr_tail_listen_timeout") {
		t.Fatalf("trace missing tail timeout turn_complete; events=%v", traceEventNames(events))
	}
	if session.State() != StateIdle {
		t.Fatalf("session state after tail timeout = %s, want idle", session.State())
	}
}

func TestVoiceLoopResolvesProviderProfileForEachTurn(t *testing.T) {
	ctx := context.Background()
	session := New("sess_provider_switch", "stackchan-s3-main", "client-1")
	resolver := &switchingProviderResolver{
		current: VoiceProviderSet{
			Profile: "primary",
			ASRName: providers.ProviderMock,
			LLMName: providers.ProviderMock,
			TTSName: providers.ProviderMock,
			ASR:     providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
			LLM:     providers.NewMockLLMProvider(providers.MockConfig{LLMFirstTokenDelayMS: 1}),
			TTS:     providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		},
	}

	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:          session,
		ProviderResolver: resolver,
		ProviderObserver: resolver,
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:    traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	resolver.Set(VoiceProviderSet{
		Profile: "fallback",
		ASRName: providers.ProviderMock,
		LLMName: providers.ProviderMock,
		TTSName: providers.ProviderMock,
		ASR:     providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
		LLM:     providers.NewMockLLMProvider(providers.MockConfig{LLMFirstTokenDelayMS: 1}),
		TTS:     providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
	})
	runMockVoiceTurn(t, loop)

	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	var profiles []string
	for _, event := range traceEvents {
		if event.Event != "listen_start" {
			continue
		}
		profile, _ := event.Fields["provider_profile"].(string)
		profiles = append(profiles, profile)
	}
	if len(profiles) != 2 || profiles[0] != "primary" || profiles[1] != "fallback" {
		t.Fatalf("listen_start profiles = %v, want [primary fallback]", profiles)
	}
	outcomes := resolver.Outcomes()
	if len(outcomes) != 2 || outcomes[0].Profile != "primary" || outcomes[1].Profile != "fallback" {
		t.Fatalf("provider outcomes = %+v, want primary then fallback", outcomes)
	}
	for _, outcome := range outcomes {
		if outcome.Error {
			t.Fatalf("provider outcome error = %+v, want success", outcome)
		}
		if outcome.FirstAudibleLatencyMS <= 0 {
			t.Fatalf("provider outcome latency = %+v, want first audible latency", outcome)
		}
	}
}

func TestVoiceLoopBuildsLLMContextBeforeProviderRequest(t *testing.T) {
	ctx := context.Background()
	session := New("sess_llm_context", "stackchan-s3-main", "client-1")
	llm := &recordingLLMProvider{}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
		LLM:     llm,
		TTS:     providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		LLMContextBuilder: staticLLMContextBuilder{
			context: LLMContext{
				Text: "Persona and memory context\nCurrent user message:\n你好，我是阿豪",
				Messages: []providers.LLMMessage{
					{Role: "system", Content: "Persona and memory context"},
					{Role: "user", Content: "你好，我是阿豪"},
				},
				MemoryCount: 2,
				PersonaName: "Stack-chan",
			},
		},
		Downlink:      &recordingDownlink{},
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	if got := llm.LastText(); got != "Persona and memory context\nCurrent user message:\n你好，我是阿豪" {
		t.Fatalf("LLM request text = %q, want context text", got)
	}
	request := llm.LastRequest()
	if len(request.Messages) != 2 {
		t.Fatalf("LLM request messages = %+v, want system plus user", request.Messages)
	}
	if request.Messages[0].Role != "system" || request.Messages[0].Content != "Persona and memory context" {
		t.Fatalf("system message = %+v, want context only", request.Messages[0])
	}
	if request.Messages[1].Role != "user" || request.Messages[1].Content != "你好，我是阿豪" {
		t.Fatalf("user message = %+v, want current transcript only", request.Messages[1])
	}
	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	var found bool
	for _, event := range traceEvents {
		if event.Event != "llm_request" {
			continue
		}
		found = true
		if event.Fields["memory_count"] != float64(2) {
			t.Fatalf("llm_request fields = %+v, want memory_count 2", event.Fields)
		}
		if event.Fields["message_count"] != float64(2) {
			t.Fatalf("llm_request fields = %+v, want message_count 2", event.Fields)
		}
		if _, ok := event.Fields["text"]; ok {
			t.Fatalf("llm_request leaked text field: %+v", event.Fields)
		}
	}
	if !found {
		t.Fatalf("trace missing llm_request; events=%v", traceEventNames(traceEvents))
	}
}

func TestVoiceLoopRecordsRecentConversationAfterSuccessfulTurn(t *testing.T) {
	ctx := context.Background()
	session := New("sess_recent_record", "stackchan-s3-main", "client-1")
	recorder := &recordingConversationRecorder{}
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{{
		{Text: "我记得，下一步是验收动作。", IsFinal: true},
	}}}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:              session,
		ASR:                  fixedTranscriptASRProvider{text: "你还记得下一步是什么吗？"},
		LLM:                  llm,
		TTS:                  providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		ConversationRecorder: recorder,
		Downlink:             &recordingDownlink{},
		Pacer:                audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	requests := recorder.Requests()
	if len(requests) != 1 {
		t.Fatalf("conversation record requests = %+v, want one successful turn", requests)
	}
	request := requests[0]
	if request.DeviceID != "stackchan-s3-main" || request.UserText != "你还记得下一步是什么吗？" {
		t.Fatalf("conversation record request = %+v, want current device user text", request)
	}
	if request.AssistantText != "我记得，下一步是验收动作。" {
		t.Fatalf("assistant text = %q, want spoken LLM text", request.AssistantText)
	}
}

func TestVoiceLoopUsesRecordedRecentConversationOnNextTurn(t *testing.T) {
	ctx := context.Background()
	session := New("sess_recent_context", "stackchan-s3-main", "client-1")
	recorder := &recordingConversationRecorder{}
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{
		{{Text: "我们刚才在验收语音链路。", IsFinal: true}},
		{{Text: "下一步是继续做物理验收。", IsFinal: true}},
	}}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR: &scriptedASRProvider{texts: []string{
			"刚才我们在做什么？",
			"那下一步呢？",
		}},
		LLM:                  llm,
		TTS:                  providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		LLMContextBuilder:    recentConversationTestContextBuilder{recorder: recorder},
		ConversationRecorder: recorder,
		Downlink:             &recordingDownlink{},
		Pacer:                audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	runMockVoiceTurn(t, loop)

	requests := llm.Requests()
	if len(requests) != 2 {
		t.Fatalf("LLM requests = %d, want two turns", len(requests))
	}
	if strings.Contains(requests[0].Text, "最近对话") {
		t.Fatalf("first LLM request unexpectedly had recent context:\n%s", requests[0].Text)
	}
	for _, want := range []string{
		"最近对话（从旧到新）:",
		"用户: 刚才我们在做什么？",
		"助手: 我们刚才在验收语音链路。",
		"如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须先根据最近对话补全语境再回答。",
		"当前用户消息:\n那下一步呢？",
	} {
		if !strings.Contains(requests[1].Text, want) {
			t.Fatalf("second LLM request missing %q:\n%s", want, requests[1].Text)
		}
	}
}

func TestVoiceLoopHandlesAgentModeCommandBeforeLLM(t *testing.T) {
	ctx := context.Background()
	session := New("sess_agent_mode_command", "stackchan-s3-main", "client-1")
	llm := &scriptedLLMProvider{}
	memoryWriter := &recordingMemoryWriter{result: MemoryWriteResult{WrittenCount: 1}}
	downlink := &recordingDownlink{}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	modeCommands := &recordingAgentModeCommandHandler{
		result: AgentModeCommandResult{
			Handled:    true,
			Mode:       "professional",
			Action:     "enter_professional",
			SpokenText: "已进入专业模式。",
		},
	}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:           session,
		ASR:               fixedTranscriptASRProvider{text: "请进入专业模式。"},
		LLM:               llm,
		TTS:               providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentModeCommands: modeCommands,
		MemoryWriter:      memoryWriter,
		Downlink:          downlink,
		Pacer:             audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:     traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	requests := modeCommands.Requests()
	if len(requests) != 1 || requests[0].Transcript != "请进入专业模式。" || requests[0].DeviceID != "stackchan-s3-main" {
		t.Fatalf("mode command requests = %+v, want one current-device transcript", requests)
	}
	if llmRequests := llm.Requests(); len(llmRequests) != 0 {
		t.Fatalf("LLM requests = %+v, want mode command to skip LLM", llmRequests)
	}
	if memoryRequests := memoryWriter.Requests(); len(memoryRequests) != 0 {
		t.Fatalf("memory requests = %+v, want mode command not written as memory", memoryRequests)
	}
	sequence := downlink.Sequence()
	want := []string{
		"json:hello",
		"json:stt:请进入专业模式。",
		"json:tts:start",
		"json:tts:sentence_start:已进入专业模式。",
		"binary",
		"json:tts:stop",
	}
	if len(sequence) != len(want) {
		t.Fatalf("sequence len = %d, want %d; sequence=%v", len(sequence), len(want), sequence)
	}
	for i := range want {
		if sequence[i] != want[i] {
			t.Fatalf("sequence[%d] = %q, want %q; sequence=%v", i, sequence[i], want[i], sequence)
		}
	}
	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	assertTraceContainsEvents(t, traceEvents, []string{
		"speech_final",
		"agent_mode_command",
		"tts_request",
		"turn_complete",
	})
	for _, event := range traceEvents {
		if event.Event == "llm_request" {
			t.Fatalf("trace contains llm_request for mode command: %+v", event)
		}
	}
}

func TestVoiceLoopHandlesProviderProfileCommandBeforeLLM(t *testing.T) {
	ctx := context.Background()
	session := New("sess_provider_profile_command", "stackchan-s3-main", "client-1")
	llm := &scriptedLLMProvider{}
	memoryWriter := &recordingMemoryWriter{result: MemoryWriteResult{WrittenCount: 1}}
	downlink := &recordingDownlink{}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	providerCommands := &recordingProviderProfileCommandHandler{
		result: ProviderProfileCommandResult{
			Handled:    true,
			Profile:    "doubao-dashscope-voice",
			Action:     "set",
			SpokenText: "已切到字节豆包语音链路。",
		},
	}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:                 session,
		ASR:                     fixedTranscriptASRProvider{text: "请切到字节模型。"},
		LLM:                     llm,
		TTS:                     providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		ProviderProfileCommands: providerCommands,
		MemoryWriter:            memoryWriter,
		Downlink:                downlink,
		Pacer:                   audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:           traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	requests := providerCommands.Requests()
	if len(requests) != 1 || requests[0].Transcript != "请切到字节模型。" || requests[0].DeviceID != "stackchan-s3-main" {
		t.Fatalf("provider command requests = %+v, want one current-device transcript", requests)
	}
	if llmRequests := llm.Requests(); len(llmRequests) != 0 {
		t.Fatalf("LLM requests = %+v, want provider command to skip LLM", llmRequests)
	}
	if memoryRequests := memoryWriter.Requests(); len(memoryRequests) != 0 {
		t.Fatalf("memory requests = %+v, want provider command not written as memory", memoryRequests)
	}
	sequence := downlink.Sequence()
	want := []string{
		"json:hello",
		"json:stt:请切到字节模型。",
		"json:tts:start",
		"json:tts:sentence_start:已切到字节豆包语音链路。",
		"binary",
		"json:tts:stop",
	}
	if len(sequence) != len(want) {
		t.Fatalf("sequence len = %d, want %d; sequence=%v", len(sequence), len(want), sequence)
	}
	for i := range want {
		if sequence[i] != want[i] {
			t.Fatalf("sequence[%d] = %q, want %q; sequence=%v", i, sequence[i], want[i], sequence)
		}
	}
	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	assertTraceContainsEvents(t, traceEvents, []string{
		"speech_final",
		"provider_profile_command",
		"tts_request",
		"turn_complete",
	})
	for _, event := range traceEvents {
		if event.Event == "llm_request" {
			t.Fatalf("trace contains llm_request for provider command: %+v", event)
		}
	}
}

func TestVoiceLoopPassesAgentModeToLLMContextBuilder(t *testing.T) {
	ctx := context.Background()
	session := New("sess_agent_mode_context", "stackchan-s3-main", "client-1")
	llm := &recordingLLMProvider{}
	contextBuilder := &recordingLLMContextBuilder{
		result: LLMContext{
			Text: "mode-aware prompt",
			Messages: []providers.LLMMessage{
				{Role: "system", Content: "当前模式: tool"},
				{Role: "user", Content: "查一下可用工具"},
			},
		},
	}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:           session,
		ASR:               fixedTranscriptASRProvider{text: "查一下可用工具"},
		LLM:               llm,
		TTS:               providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:          &recordingDownlink{},
		Pacer:             audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		LLMContextBuilder: contextBuilder,
		AgentModeReader:   fixedAgentModeReader{mode: "tool"},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	requests := contextBuilder.Requests()
	if len(requests) != 1 {
		t.Fatalf("context builder requests = %+v, want one", requests)
	}
	if requests[0].AgentMode != "tool" || requests[0].Transcript != "查一下可用工具" {
		t.Fatalf("context request = %+v, want active tool mode and transcript", requests[0])
	}
	llmRequest := llm.LastRequest()
	if len(llmRequest.Messages) != 2 || !strings.Contains(llmRequest.Messages[0].Content, "当前模式: tool") {
		t.Fatalf("LLM messages = %+v, want mode-aware context", llmRequest.Messages)
	}
}

func TestVoiceLoopRoutesAgentRuntimeBeforeLLM(t *testing.T) {
	ctx := context.Background()
	session := New("sess_agent_runtime", "stackchan-s3-main", "client-1")
	llm := &scriptedLLMProvider{}
	memoryWriter := &recordingMemoryWriter{result: MemoryWriteResult{WrittenCount: 1}}
	downlink := &recordingDownlink{}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	agentRuntime := &recordingAgentRuntimeRouter{
		result: AgentRuntimeResult{
			Handled:     true,
			Mode:        "tool",
			Destination: "openclaw",
			Text:        "我会调用桌面工具继续。",
			ToolCalls: []providers.ToolCall{{
				ID:        "openclaw_0",
				Name:      "memory.lookup",
				Arguments: map[string]any{"query": "桌面偏好"},
			}},
		},
	}
	toolOrchestrator := newRecordingToolOrchestrator()
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:          session,
		ASR:              fixedTranscriptASRProvider{text: "帮我分析桌面状态"},
		LLM:              llm,
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime:     agentRuntime,
		MemoryWriter:     memoryWriter,
		ToolOrchestrator: toolOrchestrator,
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder:    traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	runtimeRequests := agentRuntime.Requests()
	if len(runtimeRequests) != 1 || runtimeRequests[0].Transcript != "帮我分析桌面状态" || runtimeRequests[0].DeviceID != "stackchan-s3-main" {
		t.Fatalf("agent runtime requests = %+v, want current voice turn", runtimeRequests)
	}
	if llmRequests := llm.Requests(); len(llmRequests) != 0 {
		t.Fatalf("LLM requests = %+v, want agent route to skip LLM", llmRequests)
	}
	memoryRequests := memoryWriter.Requests()
	if len(memoryRequests) != 1 || memoryRequests[0].Transcript != "帮我分析桌面状态" {
		t.Fatalf("memory requests = %+v, want original transcript writeback", memoryRequests)
	}
	toolRequests := toolOrchestrator.WaitRequests(t, 1)
	if len(toolRequests[0].Calls) != 1 || toolRequests[0].Calls[0].Name != "memory.lookup" || toolRequests[0].Calls[0].Arguments["query"] != "桌面偏好" {
		t.Fatalf("tool requests = %+v, want OpenClaw tool intent through orchestrator", toolRequests)
	}
	sequence := downlink.Sequence()
	want := []string{
		"json:hello",
		"json:stt:帮我分析桌面状态",
		"json:tts:start",
		"json:tts:sentence_start:我会调用桌面工具继续。",
		"binary",
		"json:tts:stop",
	}
	if len(sequence) != len(want) {
		t.Fatalf("sequence len = %d, want %d; sequence=%v", len(sequence), len(want), sequence)
	}
	for i := range want {
		if sequence[i] != want[i] {
			t.Fatalf("sequence[%d] = %q, want %q; sequence=%v", i, sequence[i], want[i], sequence)
		}
	}
	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	assertTraceContainsEvents(t, traceEvents, []string{
		"speech_final",
		"agent_route",
		"llm_tool_call",
		"tts_request",
		"turn_complete",
	})
	for _, event := range traceEvents {
		if event.Event == "llm_request" {
			t.Fatalf("trace contains llm_request for agent route: %+v", event)
		}
	}
}

func TestVoiceLoopFallsBackToLLMWhenAgentRuntimeReturnsBlankResponse(t *testing.T) {
	ctx := context.Background()
	session := New("sess_agent_runtime_blank", "stackchan-s3-main", "client-1")
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{{
		{Text: "我继续走普通对话。", IsFinal: true},
	}}}
	downlink := &recordingDownlink{}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	agentRuntime := &recordingAgentRuntimeRouter{
		result: AgentRuntimeResult{
			Handled:     true,
			Mode:        "tool",
			Destination: "openclaw",
			Text:        "   ",
		},
	}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "继续正常回答"},
		LLM:           llm,
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime:  agentRuntime,
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	if requests := agentRuntime.Requests(); len(requests) != 1 {
		t.Fatalf("agent runtime requests = %+v, want one attempted route", requests)
	}
	if llmRequests := llm.Requests(); len(llmRequests) != 1 || llmRequests[0].Text != "继续正常回答" {
		t.Fatalf("LLM requests = %+v, want fallback to normal LLM path", llmRequests)
	}
	sequence := downlink.Sequence()
	if !containsSequenceItem(sequence, "json:tts:sentence_start:我继续走普通对话。") {
		t.Fatalf("sequence = %v, want LLM text spoken", sequence)
	}
	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	assertTraceContainsEvents(t, traceEvents, []string{
		"speech_final",
		"llm_request",
		"tts_request",
		"turn_complete",
	})
	for _, event := range traceEvents {
		if event.Event == "agent_route" {
			t.Fatalf("trace contains agent_route for blank bridge response: %+v", event)
		}
	}
}

func TestVoiceLoopTracesAgentRuntimeSkippedReason(t *testing.T) {
	ctx := context.Background()
	session := New("sess_agent_runtime_skipped", "stackchan-s3-main", "client-1")
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{{
		{Text: "我继续走普通对话。", IsFinal: true},
	}}}
	downlink := &recordingDownlink{}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	agentRuntime := &recordingAgentRuntimeRouter{
		result: AgentRuntimeResult{
			Handled:     false,
			Mode:        "tool",
			Destination: "openclaw",
			SkipReason:  "runtime_rate_limited",
		},
	}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "继续正常回答"},
		LLM:           llm,
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime:  agentRuntime,
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	if llmRequests := llm.Requests(); len(llmRequests) != 1 || llmRequests[0].Text != "继续正常回答" {
		t.Fatalf("LLM requests = %+v, want fallback to normal LLM path", llmRequests)
	}
	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	assertTraceContainsEvents(t, traceEvents, []string{
		"agent_route_skipped",
		"llm_request",
		"tts_request",
	})
	for _, event := range traceEvents {
		if event.Event != "agent_route_skipped" {
			continue
		}
		if event.Fields["reason"] != "runtime_rate_limited" || event.Fields["mode"] != "tool" || event.Fields["destination"] != "openclaw" {
			t.Fatalf("agent_route_skipped fields = %+v, want safe reason/mode/destination", event.Fields)
		}
		assertTraceDoesNotContainField(t, []observability.TraceEvent{event}, "text")
		assertTraceDoesNotContainField(t, []observability.TraceEvent{event}, "transcript")
		return
	}
	t.Fatalf("trace events = %v, missing agent_route_skipped", traceEventNames(traceEvents))
}

func TestVoiceLoopFallsBackToLLMWhenAgentRuntimeErrors(t *testing.T) {
	ctx := context.Background()
	session := New("sess_agent_runtime_error", "stackchan-s3-main", "client-1")
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{{
		{Text: "我继续用普通对话处理。", IsFinal: true},
	}}}
	downlink := &recordingDownlink{}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	agentRuntime := &recordingAgentRuntimeRouter{
		err: errors.New("openclaw provider leaked-token should never reach trace"),
	}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "继续正常回答"},
		LLM:           llm,
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime:  agentRuntime,
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	if requests := agentRuntime.Requests(); len(requests) != 1 {
		t.Fatalf("agent runtime requests = %+v, want one attempted route", requests)
	}
	if llmRequests := llm.Requests(); len(llmRequests) != 1 || llmRequests[0].Text != "继续正常回答" {
		t.Fatalf("LLM requests = %+v, want fallback to normal LLM path", llmRequests)
	}
	sequence := downlink.Sequence()
	if !containsSequenceItem(sequence, "json:tts:sentence_start:我继续用普通对话处理。") {
		t.Fatalf("sequence = %v, want fallback LLM text spoken", sequence)
	}
	traceBytes := traceOutput.Bytes()
	if bytes.Contains(traceBytes, []byte("leaked-token")) || bytes.Contains(traceBytes, []byte("provider leaked")) {
		t.Fatalf("trace leaked bridge error details: %s", string(traceBytes))
	}
	traceEvents := decodeTraceEvents(t, traceBytes)
	assertTraceContainsEvents(t, traceEvents, []string{
		"speech_final",
		"agent_route_error",
		"llm_request",
		"tts_request",
		"turn_complete",
	})
	for _, event := range traceEvents {
		if event.Event == "agent_route_error" && event.ErrorCode != "agent_route_failed" {
			t.Fatalf("agent route error code = %q, want agent_route_failed", event.ErrorCode)
		}
		if event.Event == "agent_route" {
			t.Fatalf("trace contains successful agent_route for failed bridge: %+v", event)
		}
	}
}

func TestVoiceLoopWritesMemoriesAfterSuccessfulTurn(t *testing.T) {
	ctx := context.Background()
	session := New("sess_memory_write", "stackchan-s3-main", "client-1")
	memoryWriter := &recordingMemoryWriter{result: MemoryWriteResult{WrittenCount: 1}}
	var traceOutput bytes.Buffer
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        &traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "叫我阿豪，我喜欢可打断的语音交互。"},
		LLM:           providers.NewMockLLMProvider(providers.MockConfig{LLMFirstTokenDelayMS: 1}),
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		MemoryWriter:  memoryWriter,
		Downlink:      &recordingDownlink{},
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	requests := memoryWriter.Requests()
	if len(requests) != 1 {
		t.Fatalf("memory write requests len = %d, want 1", len(requests))
	}
	if requests[0].Transcript != "叫我阿豪，我喜欢可打断的语音交互。" || requests[0].DeviceID != "stackchan-s3-main" {
		t.Fatalf("memory write request = %+v, want transcript and device scope", requests[0])
	}
	traceEvents := decodeTraceEvents(t, traceOutput.Bytes())
	var found bool
	for _, event := range traceEvents {
		if event.Event != "memory_written" {
			continue
		}
		found = true
		if event.Fields["written_count"] != float64(1) {
			t.Fatalf("memory_written fields = %+v, want written_count 1", event.Fields)
		}
		if _, ok := event.Fields["text"]; ok {
			t.Fatalf("memory_written leaked text field: %+v", event.Fields)
		}
	}
	if !found {
		t.Fatalf("trace missing memory_written; events=%v", traceEventNames(traceEvents))
	}
}

func TestVoiceLoopSendsMCPInitializeWhenHelloAdvertisesMCP(t *testing.T) {
	ctx := context.Background()
	session := New("sess_mcp_init", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker, err := mcp.NewBroker(mcp.BrokerOptions{
		SessionID: session.ID(),
		Downlink:  downlink,
	})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:   session,
		ASR:       providers.NewMockASRProvider(providers.MockConfig{}),
		LLM:       providers.NewMockLLMProvider(providers.MockConfig{}),
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}

	if _, err := loop.HandleHelloWithFeatures(ctx, xiaozhi.DeviceFeatures{MCP: true}); err != nil {
		t.Fatalf("HandleHelloWithFeatures() error = %v", err)
	}

	sequence := downlink.Sequence()
	want := []string{"json:hello", "json:mcp"}
	if len(sequence) != len(want) {
		t.Fatalf("sequence = %v, want %v", sequence, want)
	}
	for i := range want {
		if sequence[i] != want[i] {
			t.Fatalf("sequence[%d] = %q, want %q; sequence=%v", i, sequence[i], want[i], sequence)
		}
	}

	message := downlink.MCPMessage(t, 0)
	if message.Method != mcp.MethodInitialize {
		t.Fatalf("mcp method = %q, want %q", message.Method, mcp.MethodInitialize)
	}
	if !message.IsRequest() {
		t.Fatal("initialize message is not a JSON-RPC request")
	}

	response, err := mcp.NewResultResponse(*message.ID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("NewResultResponse() error = %v", err)
	}
	responseRaw, err := response.Raw()
	if err != nil {
		t.Fatalf("response Raw() error = %v", err)
	}
	if err := loop.HandleMCPPayload(responseRaw); err != nil {
		t.Fatalf("HandleMCPPayload() error = %v", err)
	}

	listRequest := downlink.MCPMessage(t, 1)
	if listRequest.Method != mcp.MethodToolsList {
		t.Fatalf("second mcp method = %q, want %q", listRequest.Method, mcp.MethodToolsList)
	}
}

func TestVoiceLoopSendsDisplayLifecycleScenesDuringVoiceTurn(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_lifecycle", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "低延迟语音链路现在怎么样？"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{
			{Text: "现在链路很稳。", IsFinal: true, CreatedAt: time.Now()},
		}},
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:     broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{SceneTTLMS: 900, MaxCaptionChars: 12}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	scenes := waitForScreenScenes(t, downlink, []string{
		stackchan.SceneListening,
		stackchan.SceneThinking,
		stackchan.SceneSpeaking,
		stackchan.SceneIdle,
	}, time.Second)
	for index, want := range []struct {
		scene   string
		emotion string
		accent  string
	}{
		{scene: stackchan.SceneListening, emotion: stackchan.EmotionCurious, accent: stackchan.AccentCyan},
		{scene: stackchan.SceneThinking, emotion: stackchan.EmotionCurious, accent: stackchan.AccentAmber},
		{scene: stackchan.SceneSpeaking, emotion: stackchan.EmotionWarm, accent: stackchan.AccentGreen},
		{scene: stackchan.SceneIdle, emotion: stackchan.EmotionNeutral, accent: stackchan.AccentDefault},
	} {
		if scenes[index].Arguments["scene"] != want.scene || scenes[index].Arguments["emotion"] != want.emotion || scenes[index].Arguments["accent"] != want.accent {
			t.Fatalf("scene[%d] args = %#v, want %s/%s/%s", index, scenes[index].Arguments, want.scene, want.emotion, want.accent)
		}
		if scenes[index].Arguments["ttl_ms"] != float64(900) {
			t.Fatalf("scene[%d] ttl = %#v, want configured ttl 900; args=%#v", index, scenes[index].Arguments["ttl_ms"], scenes[index].Arguments)
		}
	}
	if scenes[1].Arguments["caption"] != "我在想。" || scenes[2].Arguments["caption"] != "我在说。" {
		t.Fatalf("thinking/speaking captions = %#v / %#v, want concise lifecycle captions", scenes[1].Arguments["caption"], scenes[2].Arguments["caption"])
	}
}

func TestVoiceLoopUsesConfiguredDisplayLifecycleScenePolicy(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_configured", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "你先想一下。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{
			{Text: "想好了。", IsFinal: true, CreatedAt: time.Now()},
		}},
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      700,
			MaxCaptionChars: 32,
			LifecycleScenes: map[string]stackchan.ScenePolicy{
				stackchan.SceneThinking: {
					Emotion: stackchan.EmotionReady,
					Caption: "我认真想一下。",
					Accent:  stackchan.AccentRed,
					Motion:  &stackchan.SceneMotion{Preset: stackchan.MotionPresetNodSoft, Intensity: 0.4},
				},
				stackchan.SceneSpeaking: {
					Emotion: stackchan.EmotionHappy,
					Caption: "我来回答。",
					Accent:  stackchan.AccentGreen,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	scenes := waitForScreenScenes(t, downlink, []string{
		stackchan.SceneListening,
		stackchan.SceneThinking,
		stackchan.SceneSpeaking,
		stackchan.SceneIdle,
	}, time.Second)
	thinking := scenes[1].Arguments
	if thinking["emotion"] != stackchan.EmotionReady || thinking["caption"] != "我认真想一下。" || thinking["accent"] != stackchan.AccentRed {
		t.Fatalf("thinking args = %#v, want configured lifecycle policy", thinking)
	}
	motion, ok := thinking["motion"].(map[string]any)
	if !ok || motion["preset"] != stackchan.MotionPresetNodSoft || motion["intensity"] != float64(0.4) {
		t.Fatalf("thinking motion = %#v, want configured nod motion", thinking["motion"])
	}
	speaking := scenes[2].Arguments
	if speaking["emotion"] != stackchan.EmotionHappy || speaking["caption"] != "我来回答。" || speaking["accent"] != stackchan.AccentGreen {
		t.Fatalf("speaking args = %#v, want configured speaking policy", speaking)
	}
}

func TestVoiceLoopSendsConfiguredExpressionCueForLifecycleScene(t *testing.T) {
	ctx := context.Background()
	session := New("sess_expression_lifecycle", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	nowMu := sync.Mutex{}
	now := time.Unix(100, 0)

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:   session,
		ASR:       fixedTranscriptASRProvider{text: "帮我想一下。"},
		LLM:       providers.NewMockLLMProvider(providers.MockConfig{LLMFirstTokenDelayMS: 1}),
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{
			Writer:        traceOutput,
			RedactSecrets: true,
		}),
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			MinCommandGap: time.Nanosecond,
			Now: func() time.Time {
				nowMu.Lock()
				defer nowMu.Unlock()
				now = now.Add(time.Second)
				return now
			},
			MaxCommandsPerTurn: 16,
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
		}),
		ExpressionPolicies: map[string]stackchan.ExpressionPolicy{
			stackchan.CueThinking: {
				Motion: &stackchan.MotionCommand{Yaw: -4, Pitch: 9, Speed: 260},
				LED:    &stackchan.LEDCommand{R: 64, G: 32, B: 8},
				Scene: stackchan.ScenePolicy{
					Scene:   stackchan.SceneThinking,
					Emotion: stackchan.EmotionReady,
					Caption: "我开始想。",
					Accent:  stackchan.AccentAmber,
				},
			},
		},
		LifecycleExpressionCues: map[string]string{
			stackchan.SceneThinking: stackchan.CueThinking,
		},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	waitForMCPToolCall(t, downlink, mcp.ToolSetHeadAngles, map[string]float64{"yaw": -4, "pitch": 9, "speed": 260}, time.Second)
	waitForMCPToolCall(t, downlink, mcp.ToolSetLEDColor, map[string]float64{"red": 64, "green": 32, "blue": 8}, time.Second)
	assertNoScreenSceneCaption(t, downlink, "我开始想。", 150*time.Millisecond)
	traceEvents := waitForTraceEvent(t, traceOutput, "stackchan_expression_cue_dispatch", time.Second)
	event := findTraceEventByFields(t, traceEvents, "stackchan_expression_cue_dispatch", map[string]any{
		"scope":   "lifecycle",
		"trigger": stackchan.SceneThinking,
		"cue":     stackchan.CueThinking,
		"result":  "sent",
		"has_led": true,
	})
	for _, forbidden := range []string{"text", "caption", "arguments", "red", "green", "blue", "yaw", "pitch", "token"} {
		assertTraceDoesNotContainField(t, []observability.TraceEvent{event}, forbidden)
	}
}

func TestVoiceLoopTracesListenStartBodyDispatchesSafely(t *testing.T) {
	ctx := context.Background()
	session := New("sess_listen_body_trace", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
	})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "测试灯。"},
		LLM:           providers.NewMockLLMProvider(providers.MockConfig{}),
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:     broker,
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput, RedactSecrets: true}),
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return session.AcceptsGeneration(generation)
			},
		}),
		ListenStartMotionEnabled: true,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}

	waitForMCPToolCall(t, downlink, mcp.ToolSetHeadAngles, map[string]float64{"yaw": 0, "pitch": 8, "speed": 150}, time.Second)
	waitForMCPToolCall(t, downlink, mcp.ToolSetLEDColor, map[string]float64{"red": 0, "green": 168, "blue": 0}, time.Second)
	traceEvents := waitForTraceEventCount(t, traceOutput, "stackchan_body_dispatch", 2, time.Second)
	motion := findTraceEventByFields(t, traceEvents, "stackchan_body_dispatch", map[string]any{
		"channel": "motion",
		"reason":  "listen_start",
		"result":  "sent",
	})
	led := findTraceEventByFields(t, traceEvents, "stackchan_body_dispatch", map[string]any{
		"channel": "led",
		"reason":  "listen_start",
		"result":  "sent",
	})
	for _, event := range []observability.TraceEvent{motion, led} {
		for _, forbidden := range []string{"text", "arguments", "red", "green", "blue", "yaw", "pitch", "token"} {
			assertTraceDoesNotContainField(t, []observability.TraceEvent{event}, forbidden)
		}
	}
}

func TestVoiceLoopNewIdleListenStartResetsBodySchedulerTurnLimit(t *testing.T) {
	ctx := context.Background()
	session := New("sess_auto_listen_body_limit", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetHeadAngles})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetHeadAngles}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           providers.NewMockASRProvider(providers.MockConfig{ASRFinalDelayMS: 1}),
		LLM:           providers.NewMockLLMProvider(providers.MockConfig{LLMFirstTokenDelayMS: 1}),
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:     broker,
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput, RedactSecrets: true}),
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			MaxCommandsPerTurn: 1,
			MinCommandGap:      time.Nanosecond,
			GenerationIsCurrent: func(generation int64) bool {
				return session.AcceptsGeneration(generation)
			},
		}),
		ListenStartMotionEnabled: true,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	waitForTraceEventCount(t, traceOutput, "turn_complete", 1, time.Second)
	runMockVoiceTurn(t, loop)
	waitForTraceEventCount(t, traceOutput, "turn_complete", 2, time.Second)

	headCalls := waitForMCPToolCallCount(t, downlink, mcp.ToolSetHeadAngles, 2, time.Second)
	if len(headCalls) != 2 {
		t.Fatalf("head call count = %d, want 2", len(headCalls))
	}
	traceEvents := waitForTraceEventCount(t, traceOutput, "stackchan_body_dispatch", 2, time.Second)
	for _, event := range traceEvents {
		if event.Event == "stackchan_body_dispatch" &&
			event.Fields["channel"] == "motion" &&
			event.Fields["reason"] == "listen_start" &&
			event.Fields["result"] != "sent" {
			t.Fatalf("listen-start motion dispatch failed after consecutive auto-listen turns: %+v", event)
		}
	}
}

func TestVoiceLoopDispatchesLifecycleLEDsWithoutLeakingRawColorsToTrace(t *testing.T) {
	ctx := context.Background()
	session := New("sess_lifecycle_leds", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetLEDColor,
	})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetLEDColor}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "测试状态灯。"},
		LLM:           providers.NewMockLLMProvider(providers.MockConfig{}),
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:     broker,
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput, RedactSecrets: true}),
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return session.AcceptsGeneration(generation)
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	ledCalls := waitForMCPToolCallCount(t, downlink, mcp.ToolSetLEDColor, 4, time.Second)
	want := []map[string]float64{
		{"red": 0, "green": 168, "blue": 0},
		{"red": 168, "green": 112, "blue": 0},
		{"red": 0, "green": 0, "blue": 168},
		{"red": 0, "green": 0, "blue": 0},
	}
	for index, wantArgs := range want {
		if !toolArgsMatch(ledCalls[index].Arguments, wantArgs) {
			t.Fatalf("led call[%d] = %#v, want lifecycle args %#v; sequence=%v", index, ledCalls[index].Arguments, wantArgs, downlink.Sequence())
		}
	}

	traceEvents := waitForTraceEventCount(t, traceOutput, "stackchan_body_dispatch", 4, time.Second)
	for _, reason := range []string{"listen_start", "thinking_start", "speaking_start", "idle_start"} {
		event := findTraceEventByFields(t, traceEvents, "stackchan_body_dispatch", map[string]any{
			"channel": "led",
			"reason":  reason,
			"result":  "sent",
		})
		for _, forbidden := range []string{"arguments", "red", "green", "blue", "text", "token"} {
			assertTraceDoesNotContainField(t, []observability.TraceEvent{event}, forbidden)
		}
	}
}

func TestVoiceLoopSkipsStaleListenLEDAfterTailTimeout(t *testing.T) {
	ctx := context.Background()
	session := New("sess_stale_listen_led", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetLEDColor,
	})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetLEDColor}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:   session,
		ASR:       fixedTranscriptASRProvider{text: "测试。"},
		LLM:       providers.NewMockLLMProvider(providers.MockConfig{}),
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return session.AcceptsGeneration(generation)
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	turn, err := session.StartListening()
	if err != nil {
		t.Fatalf("StartListening() error = %v", err)
	}
	idleTurn, err := session.CompleteListeningNoSpeech(turn.Generation)
	if err != nil {
		t.Fatalf("CompleteListeningNoSpeech() error = %v", err)
	}

	loop.sendLifecycleLED(ctx, lifecycleLEDRequest{
		turn:      turn,
		lifecycle: stackchan.SceneListening,
		command: stackchan.LEDCommand{
			R:      0,
			G:      168,
			B:      0,
			Reason: stackchan.LifecycleLEDReason(stackchan.SceneListening),
		},
	})
	if calls := downlink.MCPToolCalls(t, mcp.ToolSetLEDColor); len(calls) != 0 {
		t.Fatalf("stale listen LED calls = %v, want none", calls)
	}

	loop.sendLifecycleLED(ctx, lifecycleLEDRequest{
		turn:      idleTurn,
		lifecycle: stackchan.SceneIdle,
		command: stackchan.LEDCommand{
			R:      0,
			G:      0,
			B:      0,
			Reason: stackchan.LifecycleLEDReason(stackchan.SceneIdle),
		},
	})
	waitForMCPToolCall(t, downlink, mcp.ToolSetLEDColor, map[string]float64{"red": 0, "green": 0, "blue": 0}, time.Second)
}

func TestVoiceLoopSuppressesPostSpeakingIdleLEDWhenAutoListenStarts(t *testing.T) {
	ctx := context.Background()
	session := New("sess_suppress_post_speaking_idle_led", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetLEDColor,
	})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetLEDColor}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:               session,
		ASR:                   fixedTranscriptASRProvider{text: "测试。"},
		LLM:                   providers.NewMockLLMProvider(providers.MockConfig{}),
		TTS:                   providers.NewMockTTSProvider(providers.MockConfig{}),
		Downlink:              downlink,
		Pacer:                 audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:             broker,
		PostSpeakingIdleDelay: 30 * time.Millisecond,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return session.AcceptsGeneration(generation)
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	turn, err := session.StartListening()
	if err != nil {
		t.Fatalf("StartListening() error = %v", err)
	}
	if _, err := session.StopListening(); err != nil {
		t.Fatalf("StopListening() error = %v", err)
	}
	if _, err := session.BeginSpeaking(turn.Generation); err != nil {
		t.Fatalf("BeginSpeaking() error = %v", err)
	}
	completedTurn, err := session.CompleteSpeaking(turn.Generation)
	if err != nil {
		t.Fatalf("CompleteSpeaking() error = %v", err)
	}

	loop.schedulePostSpeakingIdleLifecycle(completedTurn)
	if _, err := session.StartListening(); err != nil {
		t.Fatalf("auto-listen StartListening() error = %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	if calls := downlink.MCPToolCalls(t, mcp.ToolSetLEDColor); len(calls) != 0 {
		t.Fatalf("post-speaking idle LED calls after auto-listen = %v, want none", calls)
	}
}

func TestVoiceLoopCloseSendsIdleLEDWithoutOldGeneration(t *testing.T) {
	ctx := context.Background()
	session := New("sess_close_idle_led", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	traceOutput := &lockedBuffer{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetLEDColor,
	})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetLEDColor}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "测试关闭。"},
		LLM:           providers.NewMockLLMProvider(providers.MockConfig{}),
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:     broker,
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: traceOutput, RedactSecrets: true}),
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return session.AcceptsGeneration(generation)
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	waitForMCPToolCall(t, downlink, mcp.ToolSetLEDColor, map[string]float64{"red": 0, "green": 168, "blue": 0}, time.Second)

	loop.Close(ctx)

	ledCalls := waitForMCPToolCallCount(t, downlink, mcp.ToolSetLEDColor, 2, time.Second)
	if !toolArgsMatch(ledCalls[1].Arguments, map[string]float64{"red": 0, "green": 0, "blue": 0}) {
		t.Fatalf("close led call = %#v, want idle/off; sequence=%v", ledCalls[1].Arguments, downlink.Sequence())
	}
	traceEvents := waitForTraceEventCount(t, traceOutput, "stackchan_body_dispatch", 2, time.Second)
	event := findTraceEventByFields(t, traceEvents, "stackchan_body_dispatch", map[string]any{
		"channel": "led",
		"reason":  "idle_start",
		"result":  "sent",
	})
	if event.ErrorCode != "" {
		t.Fatalf("close idle LED error_code = %q, want empty", event.ErrorCode)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForAgentModeCommand(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_mode_event", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	modeCommands := &recordingAgentModeCommandHandler{
		result: AgentModeCommandResult{
			Handled:    true,
			Mode:       "professional",
			Action:     "enter_professional",
			SpokenText: "已进入专业模式。",
		},
	}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:           session,
		ASR:               fixedTranscriptASRProvider{text: "请进入专业模式。"},
		LLM:               &scriptedLLMProvider{},
		TTS:               providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentModeCommands: modeCommands,
		Downlink:          downlink,
		Pacer:             audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:         broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventAgentModeProfessional: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionReady,
					Caption: "专业模式已开启。",
					Accent:  stackchan.AccentAmber,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	scene := waitForScreenSceneCaption(t, downlink, "专业模式已开启。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionReady || scene.Arguments["accent"] != stackchan.AccentAmber {
		t.Fatalf("mode event scene = %#v, want configured professional mode display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForToolActivity(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_tool_event", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	toolOrchestrator := newRecordingToolOrchestrator()

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "查一下我的偏好。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-memory",
				Name:      "memory.lookup",
				Arguments: map[string]any{"query": "偏好"},
			}},
			Text:      "我查一下。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:        broker,
		ToolOrchestrator: toolOrchestrator,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventToolRunning: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionReady,
					Caption: "我在调用工具。",
					Accent:  stackchan.AccentGreen,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	toolOrchestrator.WaitRequests(t, 1)

	scene := waitForScreenSceneCaption(t, downlink, "我在调用工具。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionReady || scene.Arguments["accent"] != stackchan.AccentGreen {
		t.Fatalf("tool event scene = %#v, want configured tool activity display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForToolSuccess(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_tool_success", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	toolOrchestrator := newRecordingToolOrchestrator()

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "查一下我的偏好。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-memory",
				Name:      "memory.lookup",
				Arguments: map[string]any{"query": "偏好"},
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:        broker,
		ToolOrchestrator: toolOrchestrator,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				"tool.succeeded": {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionHappy,
					Caption: "工具完成。",
					Accent:  stackchan.AccentGreen,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	toolOrchestrator.WaitRequests(t, 1)

	scene := waitForScreenSceneCaption(t, downlink, "工具完成。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionHappy || scene.Arguments["accent"] != stackchan.AccentGreen {
		t.Fatalf("tool success scene = %#v, want configured success display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForToolFailure(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_tool_failure", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	toolOrchestrator := failingToolOrchestrator{notify: make(chan struct{}, 1)}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "查一下我的偏好。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-memory",
				Name:      "memory.lookup",
				Arguments: map[string]any{"query": "偏好"},
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:        broker,
		ToolOrchestrator: &toolOrchestrator,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				"tool.failed": {
					Scene:   stackchan.SceneError,
					Emotion: stackchan.EmotionError,
					Caption: "工具失败。",
					Accent:  stackchan.AccentRed,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	toolOrchestrator.Wait(t)

	scene := waitForScreenSceneCaption(t, downlink, "工具失败。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneError || scene.Arguments["emotion"] != stackchan.EmotionError || scene.Arguments["accent"] != stackchan.AccentRed {
		t.Fatalf("tool failure scene = %#v, want configured failure display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForHomeAssistantStateTool(t *testing.T) {
	assertToolCallDispatchesEventScene(t, toolEventSceneCase{
		SessionID: "sess_display_ha_state",
		ToolName:  homeassistant.GetStateToolName,
		Arguments: map[string]any{"entity_id": "light.desk"},
		Event:     "homeassistant.state",
		Caption:   "我看下设备。",
		Scene:     stackchan.SceneTool,
		Emotion:   stackchan.EmotionCurious,
		Accent:    stackchan.AccentCyan,
	})
}

func TestVoiceLoopSendsDisplayEventSceneForHomeAssistantActionTool(t *testing.T) {
	assertToolCallDispatchesEventScene(t, toolEventSceneCase{
		SessionID: "sess_display_ha_action",
		ToolName:  homeassistant.CallActionToolName,
		Arguments: map[string]any{"action_id": "desk_light_on", "brightness_pct": float64(40)},
		Event:     "homeassistant.action",
		Caption:   "我在控制设备。",
		Scene:     stackchan.SceneTool,
		Emotion:   stackchan.EmotionReady,
		Accent:    stackchan.AccentGreen,
	})
}

func TestVoiceLoopSendsDisplayEventSceneForSearchWebTool(t *testing.T) {
	assertToolCallDispatchesEventScene(t, toolEventSceneCase{
		SessionID: "sess_display_search_web",
		ToolName:  search.WebSearchToolName,
		Arguments: map[string]any{"query": "StackChan xiaozhi", "max_results": float64(2)},
		Event:     "search.web",
		Caption:   "我去搜索。",
		Scene:     stackchan.SceneTool,
		Emotion:   stackchan.EmotionCurious,
		Accent:    stackchan.AccentAmber,
	})
}

func TestVoiceLoopSendsDisplayEventSceneForMemoryWriteback(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_memory_event", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	memoryWriter := &recordingMemoryWriter{result: MemoryWriteResult{WrittenCount: 1}}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:      session,
		ASR:          fixedTranscriptASRProvider{text: "我喜欢低延迟语音。"},
		LLM:          chunkedLLMProvider{chunks: []providers.LLMChunk{{Text: "我记住了。", IsFinal: true, CreatedAt: time.Now()}}},
		TTS:          providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		MemoryWriter: memoryWriter,
		Downlink:     downlink,
		Pacer:        audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:    broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventMemoryUpdated: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionHappy,
					Caption: "我记住了。",
					Accent:  stackchan.AccentGreen,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	scene := waitForScreenSceneCaption(t, downlink, "我记住了。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionHappy || scene.Arguments["accent"] != stackchan.AccentGreen {
		t.Fatalf("memory event scene = %#v, want configured memory display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForCameraTool(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_camera_event", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	toolOrchestrator := newRecordingToolOrchestrator()

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "看一下桌面。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:   "call-camera",
				Name: mcp.ToolTakePhoto,
			}},
			Text:      "我看一眼。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:        broker,
		ToolOrchestrator: toolOrchestrator,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventCameraCapturing: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionCurious,
					Caption: "我看一眼。",
					Accent:  stackchan.AccentCyan,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	toolOrchestrator.WaitRequests(t, 1)

	scene := waitForScreenSceneCaption(t, downlink, "我看一眼。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionCurious || scene.Arguments["accent"] != stackchan.AccentCyan {
		t.Fatalf("camera event scene = %#v, want configured camera display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForReminderTool(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_reminder_event", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	toolOrchestrator := newRecordingToolOrchestrator()

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "提醒到了吗？"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-reminder",
				Name:      reminder.AnnounceToolName,
				Arguments: map[string]any{"title": "喝水", "message": "该喝水了。"},
			}},
			Text:      "提醒到了。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:        broker,
		ToolOrchestrator: toolOrchestrator,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventReminderDue: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionReady,
					Caption: "提醒到了。",
					Accent:  stackchan.AccentAmber,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	toolOrchestrator.WaitRequests(t, 1)

	scene := waitForScreenSceneCaption(t, downlink, "提醒到了。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionReady || scene.Arguments["accent"] != stackchan.AccentAmber {
		t.Fatalf("reminder event scene = %#v, want configured reminder display trigger", scene.Arguments)
	}
}

func TestVoiceLoopDoesNotSendReminderEventSceneForRejectedReminderTool(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_rejected_reminder_event", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "提醒到了吗？"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-reminder",
				Name:      reminder.AnnounceToolName,
				Arguments: map[string]any{"title": "喝水"},
			}},
			Text:      "我查一下。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventReminderDue: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionReady,
					Caption: "提醒到了。",
					Accent:  stackchan.AccentAmber,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	assertNoScreenSceneCaption(t, downlink, "提醒到了。", 50*time.Millisecond)
}

func TestVoiceLoopSendsDisplayEventSceneForAgentRuntimeRoute(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_agent_route", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	agentRuntime := &recordingAgentRuntimeRouter{
		result: AgentRuntimeResult{
			Handled:     true,
			Mode:        "tool",
			Destination: "openclaw",
			Text:        "我会调用桌面工具继续。",
		},
	}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:      session,
		ASR:          fixedTranscriptASRProvider{text: "帮我分析桌面状态"},
		LLM:          &scriptedLLMProvider{},
		TTS:          providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime: agentRuntime,
		Downlink:     downlink,
		Pacer:        audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:    broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventAgentRouteOpenClaw: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionReady,
					Caption: "我在调度工具脑。",
					Accent:  stackchan.AccentCyan,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	scene := waitForScreenSceneCaption(t, downlink, "我在调度工具脑。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionReady || scene.Arguments["accent"] != stackchan.AccentCyan {
		t.Fatalf("agent route event scene = %#v, want configured OpenClaw route display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsDisplayEventSceneForAgentRuntimeSkippedRoute(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_agent_route_skipped", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	agentRuntime := &recordingAgentRuntimeRouter{
		result: AgentRuntimeResult{
			Handled:     false,
			Mode:        "tool",
			Destination: "openclaw",
			SkipReason:  "runtime_rate_limited",
		},
	}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:      session,
		ASR:          fixedTranscriptASRProvider{text: "帮我分析桌面状态"},
		LLM:          chunkedLLMProvider{chunks: []providers.LLMChunk{{Text: "我先用普通对话。", IsFinal: true, CreatedAt: time.Now()}}},
		TTS:          providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime: agentRuntime,
		Downlink:     downlink,
		Pacer:        audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:    broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				"agent_route.skipped": {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionCurious,
					Caption: "我先用普通对话。",
					Accent:  stackchan.AccentAmber,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	scene := waitForScreenSceneCaption(t, downlink, "我先用普通对话。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionCurious || scene.Arguments["accent"] != stackchan.AccentAmber {
		t.Fatalf("agent route skipped event scene = %#v, want configured bridge-skip display trigger", scene.Arguments)
	}
}

func TestVoiceLoopSendsConfiguredExpressionCueForAgentRouteEvent(t *testing.T) {
	ctx := context.Background()
	session := New("sess_expression_agent_route", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	agentRuntime := &recordingAgentRuntimeRouter{
		result: AgentRuntimeResult{
			Handled:     true,
			Mode:        "tool",
			Destination: "openclaw",
			Text:        "我会调用桌面工具继续。",
		},
	}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:      session,
		ASR:          fixedTranscriptASRProvider{text: "帮我分析桌面状态"},
		LLM:          &scriptedLLMProvider{},
		TTS:          providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime: agentRuntime,
		Downlink:     downlink,
		Pacer:        audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:    broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			MinCommandGap:      time.Nanosecond,
			MaxCommandsPerTurn: 16,
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventAgentRouteOpenClaw: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionReady,
					Caption: "我在调度工具脑。",
					Accent:  stackchan.AccentCyan,
				},
			},
		}),
		ExpressionPolicies: map[string]stackchan.ExpressionPolicy{
			stackchan.CueNod: {
				Motion: &stackchan.MotionCommand{Yaw: 6, Pitch: 14, Speed: 240},
				LED:    &stackchan.LEDCommand{R: 12, G: 34, B: 56},
				Scene: stackchan.ScenePolicy{
					Scene:   stackchan.SceneSpeaking,
					Caption: "cue 不应接管屏幕。",
					Emotion: stackchan.EmotionHappy,
				},
			},
		},
		EventExpressionCues: map[string]string{
			stackchan.DisplayEventAgentRouteOpenClaw: stackchan.CueNod,
		},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	waitForMCPToolCall(t, downlink, mcp.ToolSetHeadAngles, map[string]float64{"yaw": 6, "pitch": 14, "speed": 240}, time.Second)
	waitForMCPToolCall(t, downlink, mcp.ToolSetLEDColor, map[string]float64{"red": 12, "green": 34, "blue": 56}, time.Second)
	waitForScreenSceneCaption(t, downlink, "我在调度工具脑。", time.Second)
	assertNoScreenSceneCaption(t, downlink, "cue 不应接管屏幕。", 150*time.Millisecond)
}

func TestVoiceLoopSendsConfiguredExpressionCueForAgentRuntimeSkippedRoute(t *testing.T) {
	ctx := context.Background()
	session := New("sess_expression_agent_route_skipped", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	agentRuntime := &recordingAgentRuntimeRouter{
		result: AgentRuntimeResult{
			Handled:     false,
			Mode:        "tool",
			Destination: "openclaw",
			SkipReason:  "runtime_input_too_long",
		},
	}

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:      session,
		ASR:          fixedTranscriptASRProvider{text: "帮我分析桌面状态"},
		LLM:          chunkedLLMProvider{chunks: []providers.LLMChunk{{Text: "我先用普通对话。", IsFinal: true, CreatedAt: time.Now()}}},
		TTS:          providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		AgentRuntime: agentRuntime,
		Downlink:     downlink,
		Pacer:        audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:    broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			MinCommandGap:      time.Nanosecond,
			MaxCommandsPerTurn: 16,
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				"agent_route.skipped": {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionCurious,
					Caption: "我先用普通对话。",
					Accent:  stackchan.AccentAmber,
				},
			},
		}),
		ExpressionPolicies: map[string]stackchan.ExpressionPolicy{
			stackchan.CueSettle: {
				Motion: &stackchan.MotionCommand{Yaw: 0, Pitch: 0, Speed: 180},
				LED:    &stackchan.LEDCommand{R: 0, G: 64, B: 96},
				Scene: stackchan.ScenePolicy{
					Scene:   stackchan.SceneIdle,
					Caption: "cue 不应接管屏幕。",
					Emotion: stackchan.EmotionNeutral,
				},
			},
		},
		EventExpressionCues: map[string]string{
			"agent_route.skipped": stackchan.CueSettle,
		},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)

	waitForMCPToolCall(t, downlink, mcp.ToolSetHeadAngles, map[string]float64{"yaw": 0, "pitch": 0, "speed": 180}, time.Second)
	waitForMCPToolCall(t, downlink, mcp.ToolSetLEDColor, map[string]float64{"red": 0, "green": 64, "blue": 96}, time.Second)
	waitForScreenSceneCaption(t, downlink, "我先用普通对话。", time.Second)
	assertNoScreenSceneCaption(t, downlink, "cue 不应接管屏幕。", 150*time.Millisecond)
}

func TestVoiceLoopSendsDisplayEventSceneForV21ToolRoute(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_v21_route", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	toolOrchestrator := newRecordingToolOrchestrator()

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "查一下知识库。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:   "call-v21",
				Name: "v21.voice_query",
			}},
			Text:      "我查一下知识库。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:        broker,
		ToolOrchestrator: toolOrchestrator,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				stackchan.DisplayEventAgentRouteV21: {
					Scene:   stackchan.SceneTool,
					Emotion: stackchan.EmotionReady,
					Caption: "我去查知识库。",
					Accent:  stackchan.AccentAmber,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	toolOrchestrator.WaitRequests(t, 1)

	scene := waitForScreenSceneCaption(t, downlink, "我去查知识库。", time.Second)
	if scene.Arguments["scene"] != stackchan.SceneTool || scene.Arguments["emotion"] != stackchan.EmotionReady || scene.Arguments["accent"] != stackchan.AccentAmber {
		t.Fatalf("V21 route event scene = %#v, want configured V21 route display trigger", scene.Arguments)
	}
}

func TestVoiceLoopExecutesSemanticStackChanExpressionToolCallsThroughMCP(t *testing.T) {
	ctx := context.Background()
	session := New("sess_llm_tool_call", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker, err := mcp.NewBroker(mcp.BrokerOptions{
		SessionID: session.ID(),
		Downlink:  downlink,
		Allowlist: mcp.NewAllowlist([]string{mcp.ToolSetHeadAngles, mcp.ToolSetLEDColor}),
	})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetHeadAngles}, {Name: mcp.ToolSetLEDColor}})
	downlink.onJSON = func(event downlinkEvent) {
		if event.Type != xiaozhi.MessageTypeMCP {
			return
		}
		message, err := mcp.ParseMessage(event.Payload)
		if err != nil || message.Method != mcp.MethodToolsCall || message.ID == nil {
			return
		}
		response, err := mcp.NewResultResponse(*message.ID, json.RawMessage(`{"ok":true}`))
		if err != nil {
			t.Errorf("NewResultResponse() error = %v", err)
			return
		}
		responseRaw, err := response.Raw()
		if err != nil {
			t.Errorf("response Raw() error = %v", err)
			return
		}
		if err := broker.HandleDevicePayload(responseRaw); err != nil {
			t.Errorf("HandleDevicePayload() error = %v", err)
		}
	}

	traceOutput := &lockedBuffer{}
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "点头一下。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{
			{
				ToolCalls: []providers.ToolCall{{
					ID:        "call-expression",
					Name:      stackchan.ToolExpress,
					Arguments: map[string]any{"cue": stackchan.CueNod},
				}},
				Text:      "好了。",
				IsFinal:   true,
				CreatedAt: time.Now(),
			},
		}},
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
		MCPBroker:     broker,
		BodyScheduler: stackchanTestBodyScheduler(session),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	waitForMCPToolCall(t, downlink, mcp.ToolSetHeadAngles, map[string]float64{
		"yaw":   0,
		"pitch": 16,
		"speed": 220,
	}, time.Second)
	waitForMCPToolCall(t, downlink, mcp.ToolSetLEDColor, map[string]float64{
		"red":   0,
		"green": 168,
		"blue":  96,
	}, time.Second)

	sequence := downlink.Sequence()
	if !containsSequenceItem(sequence, "json:tts:sentence_start:好了。") {
		t.Fatalf("sequence = %v, want spoken completion text", sequence)
	}
	for _, item := range sequence {
		if strings.Contains(item, "call-expression") {
			t.Fatalf("sequence leaked tool arguments/id: %v", sequence)
		}
	}
	traceEvents := waitForTraceEvent(t, traceOutput, "llm_tool_call", time.Second)
	assertTraceDoesNotContainField(t, traceEvents, "arguments")
}

func TestVoiceLoopKeepsSpeakingWhenLLMToolCallIsRejected(t *testing.T) {
	ctx := context.Background()
	session := New("sess_llm_tool_rejected", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker, err := mcp.NewBroker(mcp.BrokerOptions{
		SessionID: session.ID(),
		Downlink:  downlink,
		Allowlist: mcp.NewAllowlist([]string{mcp.ToolSetHeadAngles}),
	})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetHeadAngles}})

	traceOutput := &lockedBuffer{}
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "执行危险动作。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{
			{
				ToolCalls: []providers.ToolCall{{
					ID:        "call-blocked",
					Name:      "danger.secret",
					Arguments: map[string]any{"token": "must-not-leak"},
				}},
				Text:      "这个动作不执行。",
				IsFinal:   true,
				CreatedAt: time.Now(),
			},
		}},
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
		MCPBroker:     broker,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	sequence := downlink.Sequence()
	if containsSequenceItem(sequence, "json:mcp") {
		t.Fatalf("sequence = %v, rejected tool should not reach device", sequence)
	}
	if !containsSequenceItem(sequence, "json:tts:sentence_start:这个动作不执行。") {
		t.Fatalf("sequence = %v, want spoken fallback text", sequence)
	}
	traceEvents := waitForTraceEvent(t, traceOutput, "llm_tool_call", time.Second)
	var found bool
	for _, event := range traceEvents {
		if event.Event != "llm_tool_call" {
			continue
		}
		found = true
		if event.ErrorCode != mcp.ErrorCodeToolNotAllowed {
			t.Fatalf("tool trace error_code = %q, want %q", event.ErrorCode, mcp.ErrorCodeToolNotAllowed)
		}
		if _, ok := event.Fields["token"]; ok {
			t.Fatalf("tool trace leaked argument key/value: %+v", event.Fields)
		}
	}
	if !found {
		t.Fatalf("trace missing llm_tool_call; events=%v", traceEventNames(traceEvents))
	}
}

func TestVoiceLoopExecutesServiceToolWithoutDeviceMCP(t *testing.T) {
	ctx := context.Background()
	session := New("sess_service_tool_voice", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	executed := make(chan servicetools.Call, 1)
	if err := registry.Register(servicetools.Definition{
		Name:       "memory.lookup",
		Permission: servicetools.PermissionRead,
	}, func(_ context.Context, call servicetools.Call) (servicetools.Result, error) {
		executed <- call
		return servicetools.Result{Payload: json.RawMessage(`{"count":1}`)}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	toolAlias := llmProviderToolAlias("memory.lookup", map[string]string{})

	traceOutput := &lockedBuffer{}
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "查一下我的偏好。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{
			{
				ToolCalls: []providers.ToolCall{{
					ID:        "call-memory",
					Name:      toolAlias,
					Arguments: map[string]any{"query": "低延迟"},
				}},
				Text:      "查到了。",
				IsFinal:   true,
				CreatedAt: time.Now(),
			},
		}},
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
		ServiceTools:  registry,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	select {
	case call := <-executed:
		if call.SessionID != session.ID() || call.DeviceID != "stackchan-s3-main" || call.Generation == 0 {
			t.Fatalf("service tool call = %+v, want turn identity", call)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for service tool execution")
	}
	sequence := downlink.Sequence()
	if containsSequenceItem(sequence, "json:mcp") {
		t.Fatalf("sequence = %v, service tool should not use device MCP", sequence)
	}
	if !containsSequenceItem(sequence, "json:tts:sentence_start:查到了。") {
		t.Fatalf("sequence = %v, want spoken service-tool response text", sequence)
	}
	traceEvents := waitForTraceEvent(t, traceOutput, "llm_tool_call", time.Second)
	for _, event := range traceEvents {
		if event.Event != "llm_tool_call" {
			continue
		}
		if event.ErrorCode != "" {
			t.Fatalf("service tool trace error_code = %q, want empty", event.ErrorCode)
		}
		if event.Fields["result_bytes"] == float64(0) {
			t.Fatalf("service tool trace fields = %+v, want nonzero result bytes", event.Fields)
		}
		assertTraceDoesNotContainField(t, []observability.TraceEvent{event}, "arguments")
		return
	}
	t.Fatalf("trace missing llm_tool_call; events=%v", traceEventNames(traceEvents))
}

func TestVoiceLoopPassesServiceToolDefinitionsToLLMRequest(t *testing.T) {
	ctx := context.Background()
	session := New("sess_service_tool_schema", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	if err := registry.Register(servicetools.Definition{
		Name:        "memory.lookup",
		Description: "Look up scoped user memories.",
		Permission:  servicetools.PermissionRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		},
	}, func(context.Context, servicetools.Call) (servicetools.Result, error) {
		t.Fatal("service tool should not execute in schema request test")
		return servicetools.Result{}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	llm := &recordingLLMProvider{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:      session,
		ASR:          fixedTranscriptASRProvider{text: "你记得我的偏好吗？"},
		LLM:          llm,
		TTS:          providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:     downlink,
		Pacer:        audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		ServiceTools: registry,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	request := llm.LastRequest()
	if len(request.Tools) != 1 {
		t.Fatalf("LLMRequest tools = %+v, want one service tool", request.Tools)
	}
	tool := request.Tools[0]
	if tool.Name == "memory.lookup" || strings.Contains(tool.Name, ".") || tool.Description != "Look up scoped user memories." {
		t.Fatalf("LLM tool = %+v, want provider-safe memory lookup alias", tool)
	}
	if tool.Name != llmProviderToolAlias("memory.lookup", map[string]string{}) {
		t.Fatalf("LLM tool alias = %q, want deterministic memory lookup alias", tool.Name)
	}
	properties, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool schema properties = %#v, want object", tool.InputSchema["properties"])
	}
	query, ok := properties["query"].(map[string]any)
	if !ok || query["type"] != "string" {
		t.Fatalf("tool query schema = %#v, want string property", properties["query"])
	}
}

func TestVoiceLoopHidesRawStackChanAndCameraMCPToolDefinitionsFromVoiceLLMRequest(t *testing.T) {
	ctx := context.Background()
	session := New("sess_raw_stackchan_tool_schema_hidden", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolDeviceStatus,
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
		mcp.ToolTakePhoto,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolDeviceStatus, Description: "Get device status."},
		{Name: mcp.ToolSetHeadAngles, Description: "Move StackChan head."},
		{Name: mcp.ToolSetLEDColor, Description: "Set StackChan LED."},
		{Name: mcp.ToolSetScreenScene, Description: "Set StackChan screen scene."},
		{Name: mcp.ToolTakePhoto, Description: "Take a photo."},
	})
	llm := &recordingLLMProvider{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:   session,
		ASR:       fixedTranscriptASRProvider{text: "你是谁？"},
		LLM:       llm,
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
		BodyScheduler: stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
			GenerationIsCurrent: func(generation int64) bool {
				return session.AcceptsGeneration(generation)
			},
		}),
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	request := llm.LastRequest()
	if hasLLMToolAlias(request.Tools, mcp.ToolTakePhoto) {
		t.Fatalf("LLMRequest tools = %+v, must not expose camera capture in voice hot path", request.Tools)
	}
	for _, rawTool := range []string{mcp.ToolDeviceStatus, mcp.ToolSetHeadAngles, mcp.ToolSetLEDColor, mcp.ToolSetScreenScene} {
		if hasLLMToolAlias(request.Tools, rawTool) {
			t.Fatalf("LLMRequest tools = %+v, must not expose raw StackChan MCP tool %s in voice hot path", request.Tools, rawTool)
		}
	}
	if hasLLMToolAlias(request.Tools, stackchan.ToolExpress) {
		t.Fatalf("LLMRequest tools = %+v, default voice path must not expose model-driven StackChan expression tools", request.Tools)
	}
}

func TestVoiceLoopPassesCameraRequestServiceToolDefinitionWhenRegistered(t *testing.T) {
	ctx := context.Background()
	session := New("sess_camera_request_tool_schema", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{camera.RequestCaptureToolName},
		AllowedPermissions: []string{servicetools.PermissionDeviceControl},
	})
	if err := camera.RegisterRequestCaptureTool(registry, camera.RequestCaptureToolOptions{MaxReasonRunes: 80}); err != nil {
		t.Fatalf("RegisterRequestCaptureTool() error = %v", err)
	}
	llm := &recordingLLMProvider{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:      session,
		ASR:          fixedTranscriptASRProvider{text: "看一下桌面"},
		LLM:          llm,
		TTS:          providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:     downlink,
		Pacer:        audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		ServiceTools: registry,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	request := llm.LastRequest()
	if hasLLMToolAlias(request.Tools, mcp.ToolTakePhoto) {
		t.Fatalf("LLMRequest tools = %+v, must not expose raw camera capture", request.Tools)
	}
	if !hasLLMToolAlias(request.Tools, camera.RequestCaptureToolName) {
		t.Fatalf("LLMRequest tools = %+v, want camera request confirmation tool", request.Tools)
	}
}

func TestVoiceLoopHidesV21ServiceToolDefinitionOutsideProfessionalMode(t *testing.T) {
	request := runServiceToolDefinitionTurn(t, "casual")

	if hasLLMToolAlias(request.Tools, v21VoiceQueryToolName) {
		t.Fatalf("LLMRequest tools = %+v, casual mode must not expose %s", request.Tools, v21VoiceQueryToolName)
	}
	if !hasLLMToolAlias(request.Tools, "memory.lookup") {
		t.Fatalf("LLMRequest tools = %+v, want memory.lookup to remain visible", request.Tools)
	}
}

func TestVoiceLoopExposesV21ServiceToolDefinitionInProfessionalMode(t *testing.T) {
	request := runServiceToolDefinitionTurn(t, "professional")

	if !hasLLMToolAlias(request.Tools, v21VoiceQueryToolName) {
		t.Fatalf("LLMRequest tools = %+v, professional mode must expose %s", request.Tools, v21VoiceQueryToolName)
	}
	if !hasLLMToolAlias(request.Tools, "memory.lookup") {
		t.Fatalf("LLMRequest tools = %+v, want memory.lookup to remain visible", request.Tools)
	}
}

func runServiceToolDefinitionTurn(t *testing.T, mode string) providers.LLMRequest {
	t.Helper()
	ctx := context.Background()
	session := New("sess_service_tool_mode_"+mode, "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead, servicetools.PermissionExternal},
	})
	for _, definition := range []servicetools.Definition{
		{
			Name:        "memory.lookup",
			Description: "Look up scoped user memories.",
			Permission:  servicetools.PermissionRead,
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Name:        v21VoiceQueryToolName,
			Description: "Query V21 professional knowledge.",
			Permission:  servicetools.PermissionExternal,
			InputSchema: map[string]any{"type": "object"},
		},
	} {
		definition := definition
		if err := registry.Register(definition, func(context.Context, servicetools.Call) (servicetools.Result, error) {
			t.Fatal("service tool should not execute in mode-filter schema request test")
			return servicetools.Result{}, nil
		}); err != nil {
			t.Fatalf("Register(%s) error = %v", definition.Name, err)
		}
	}
	llm := &recordingLLMProvider{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:         session,
		ASR:             fixedTranscriptASRProvider{text: "你可以查一下吗？"},
		LLM:             llm,
		TTS:             providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:        downlink,
		Pacer:           audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		ServiceTools:    registry,
		AgentModeReader: fixedAgentModeReader{mode: mode},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)
	return llm.LastRequest()
}

func hasLLMToolAlias(tools []providers.LLMTool, internalName string) bool {
	alias := llmProviderToolAlias(internalName, map[string]string{})
	for _, tool := range tools {
		if tool.Name == alias {
			return true
		}
	}
	return false
}

func TestVoiceLoopPassesStackChanExpressionToolDefinitionToLLMRequest(t *testing.T) {
	ctx := context.Background()
	session := New("sess_llm_stackchan_expression_tool", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetHeadAngles,
		mcp.ToolSetLEDColor,
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetHeadAngles},
		{Name: mcp.ToolSetLEDColor},
		{Name: mcp.ToolSetScreenScene},
	})
	llm := &recordingLLMProvider{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:                        session,
		ASR:                            fixedTranscriptASRProvider{text: "点头确认一下。"},
		LLM:                            llm,
		TTS:                            providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:                       downlink,
		Pacer:                          audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:                      broker,
		BodyScheduler:                  stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{}),
		SceneComposer:                  stackchan.NewSceneComposer(stackchan.DisplayOptions{}),
		ExpressionProviderToolsEnabled: true,
		ExpressionSequences: map[string][]string{
			"agree.quick": {"attentive", "nod"},
		},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	request := llm.LastRequest()
	expressionAlias := llmProviderToolAlias(stackchan.ToolExpress, map[string]string{})
	var expressionTool providers.LLMTool
	for _, tool := range request.Tools {
		if tool.Name == expressionAlias {
			expressionTool = tool
			break
		}
	}
	if expressionTool.Name == "" {
		t.Fatalf("LLMRequest tools = %+v, want stackchan expression tool alias", request.Tools)
	}
	properties, ok := expressionTool.InputSchema["properties"].(map[string]any)
	if !ok || properties["cue"] == nil {
		t.Fatalf("expression tool schema = %+v, want cue property", expressionTool.InputSchema)
	}
	sequenceAlias := llmProviderToolAlias(stackchan.ToolExpressionSequence, map[string]string{})
	var sequenceTool providers.LLMTool
	for _, tool := range request.Tools {
		if tool.Name == sequenceAlias {
			sequenceTool = tool
			break
		}
	}
	if sequenceTool.Name == "" {
		t.Fatalf("LLMRequest tools = %+v, want stackchan expression sequence tool alias", request.Tools)
	}
	sequenceProperties, ok := sequenceTool.InputSchema["properties"].(map[string]any)
	if !ok || sequenceProperties["cues"] == nil {
		t.Fatalf("expression sequence tool schema = %+v, want cues property", sequenceTool.InputSchema)
	}
	presetAlias := llmProviderToolAlias(stackchan.ToolPlayExpressionSequence, map[string]string{})
	var presetTool providers.LLMTool
	for _, tool := range request.Tools {
		if tool.Name == presetAlias {
			presetTool = tool
			break
		}
	}
	if presetTool.Name == "" {
		t.Fatalf("LLMRequest tools = %+v, want stackchan expression sequence preset tool alias", request.Tools)
	}
	presetProperties, ok := presetTool.InputSchema["properties"].(map[string]any)
	if !ok || presetProperties["sequence"] == nil {
		t.Fatalf("expression sequence preset tool schema = %+v, want sequence property", presetTool.InputSchema)
	}
}

func TestVoiceLoopPassesStackChanDisplayCardToolDefinitionToLLMRequest(t *testing.T) {
	ctx := context.Background()
	session := New("sess_llm_stackchan_card_tool", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{
		{Name: mcp.ToolSetScreenScene},
	})
	llm := &recordingLLMProvider{}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:   session,
		ASR:       fixedTranscriptASRProvider{text: "屏幕显示一个简短状态。"},
		LLM:       llm,
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			Cards: map[string]stackchan.DisplayCardPolicy{
				"status.note": {
					ScenePolicy: stackchan.ScenePolicy{
						Scene:   stackchan.SceneTool,
						Caption: "状态卡片。",
					},
					AllowCaption:    true,
					MaxCaptionChars: 24,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	request := llm.LastRequest()
	cardAlias := llmProviderToolAlias(stackchan.ToolShowCard, map[string]string{})
	var cardTool providers.LLMTool
	for _, tool := range request.Tools {
		if tool.Name == cardAlias {
			cardTool = tool
			break
		}
	}
	if cardTool.Name == "" {
		t.Fatalf("LLMRequest tools = %+v, want stackchan display card tool alias", request.Tools)
	}
	properties, ok := cardTool.InputSchema["properties"].(map[string]any)
	if !ok || properties["card"] == nil || properties["caption"] == nil {
		t.Fatalf("display card tool schema = %+v, want card and caption properties", cardTool.InputSchema)
	}
}

func TestVoiceLoopSkipsDisplayScenesWhenScreenSceneToolIsUndiscovered(t *testing.T) {
	ctx := context.Background()
	session := New("sess_display_scene_undiscovered", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{
		mcp.ToolSetScreenScene,
	})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetHeadAngles}})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:   session,
		ASR:       fixedTranscriptASRProvider{text: "你好。"},
		LLM:       chunkedLLMProvider{chunks: []providers.LLMChunk{{Text: "你好。", IsFinal: true, CreatedAt: time.Now()}}},
		TTS:       providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:  downlink,
		Pacer:     audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker: broker,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			LifecycleScenes: map[string]stackchan.ScenePolicy{
				stackchan.SceneListening: {
					Scene:   stackchan.SceneListening,
					Caption: "我在听。",
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	if calls := downlink.MCPToolCalls(t, mcp.ToolSetScreenScene); len(calls) != 0 {
		t.Fatalf("screen scene calls = %+v, want none when device did not discover %s", calls, mcp.ToolSetScreenScene)
	}
}

func TestStackChanExpressionToolRequiresMatchingGatewayCapability(t *testing.T) {
	if !hasStackChanExpressionTarget([]mcp.Tool{{Name: mcp.ToolSetHeadAngles}}, true, false) {
		t.Fatal("head tool with body scheduler should expose expression target")
	}
	if !hasStackChanExpressionTarget([]mcp.Tool{{Name: mcp.ToolSetScreenScene}}, false, true) {
		t.Fatal("screen tool with scene composer should expose expression target")
	}
	if hasStackChanExpressionTarget([]mcp.Tool{{Name: mcp.ToolSetScreenScene}}, true, false) {
		t.Fatal("screen-only device without scene composer must not expose expression target")
	}
	if hasStackChanExpressionTarget([]mcp.Tool{{Name: mcp.ToolSetHeadAngles}}, false, true) {
		t.Fatal("head-only device without body scheduler must not expose expression target")
	}
}

func TestStackChanDisplayCardToolRequiresConfiguredCardsAndScreen(t *testing.T) {
	if !hasStackChanDisplayCardTarget([]mcp.Tool{{Name: mcp.ToolSetScreenScene}}, stackchan.NewSceneComposer(stackchan.DisplayOptions{
		Cards: map[string]stackchan.DisplayCardPolicy{
			"status.note": {ScenePolicy: stackchan.ScenePolicy{Scene: stackchan.SceneTool}},
		},
	})) {
		t.Fatal("screen tool with configured cards should expose display card target")
	}
	if hasStackChanDisplayCardTarget([]mcp.Tool{{Name: mcp.ToolSetScreenScene}}, stackchan.NewSceneComposer(stackchan.DisplayOptions{})) {
		t.Fatal("screen tool without configured cards must not expose display card target")
	}
	if hasStackChanDisplayCardTarget([]mcp.Tool{{Name: mcp.ToolSetHeadAngles}}, stackchan.NewSceneComposer(stackchan.DisplayOptions{
		Cards: map[string]stackchan.DisplayCardPolicy{
			"status.note": {ScenePolicy: stackchan.ScenePolicy{Scene: stackchan.SceneTool}},
		},
	})) {
		t.Fatal("configured cards without screen MCP must not expose display card target")
	}
}

func TestLLMProviderToolAliasUsesProviderSafeNamesAndResolvesCollisions(t *testing.T) {
	used := map[string]string{}
	memoryAlias := llmProviderToolAlias("memory.lookup", used)
	used[memoryAlias] = "memory.lookup"
	if memoryAlias == "memory.lookup" || strings.Contains(memoryAlias, ".") || len(memoryAlias) > 64 {
		t.Fatalf("memory alias = %q, want provider-safe <=64 char alias", memoryAlias)
	}

	safeAlias := llmProviderToolAlias("memory_lookup", used)
	used[safeAlias] = "memory_lookup"
	if safeAlias != "memory_lookup" {
		t.Fatalf("safe alias = %q, want unchanged provider-safe name", safeAlias)
	}

	collidingAlias := llmProviderToolAlias("memory.lookup", map[string]string{
		providerSafeToolName("memory.lookup"): "different.internal",
	})
	if collidingAlias == providerSafeToolName("memory.lookup") || strings.Contains(collidingAlias, ".") || len(collidingAlias) > 64 {
		t.Fatalf("colliding alias = %q, want alternate provider-safe alias", collidingAlias)
	}

	longAlias := llmProviderToolAlias(strings.Repeat("tool.", 40), map[string]string{})
	if strings.Contains(longAlias, ".") || len(longAlias) > 64 {
		t.Fatalf("long alias = %q, want provider-safe <=64 char alias", longAlias)
	}
}

func TestVoiceLoopRunsToolResultFollowUpForToolOnlyLLMResponse(t *testing.T) {
	ctx := context.Background()
	session := New("sess_tool_followup", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	executed := make(chan servicetools.Call, 1)
	if err := registry.Register(servicetools.Definition{
		Name:        "memory.lookup",
		Description: "Look up scoped user memories.",
		Permission:  servicetools.PermissionRead,
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, call servicetools.Call) (servicetools.Result, error) {
		executed <- call
		return servicetools.Result{
			Payload: json.RawMessage(`{"memories":[{"content":"用户喜欢低延迟语音。"}],"count":1}`),
		}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	toolAlias := llmProviderToolAlias("memory.lookup", map[string]string{})
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{
		{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-memory",
				Name:      toolAlias,
				Arguments: map[string]any{"query": "偏好"},
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
		{{
			Text: "你喜欢低延迟语音。",
			ToolCalls: []providers.ToolCall{{
				ID:        "recursive-memory",
				Name:      toolAlias,
				Arguments: map[string]any{"query": "再查一次"},
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
	}}
	traceOutput := &lockedBuffer{}
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "你记得我的偏好吗？"},
		LLM:           llm,
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
		ServiceTools:  registry,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	select {
	case call := <-executed:
		if call.Name != "memory.lookup" || call.DeviceID != "stackchan-s3-main" {
			t.Fatalf("service call = %+v, want current-device memory.lookup", call)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for memory lookup")
	}
	requests := llm.Requests()
	if len(requests) != 2 {
		t.Fatalf("LLM requests = %d, want initial + follow-up", len(requests))
	}
	if len(requests[0].Tools) != 1 {
		t.Fatalf("initial tools = %+v, want memory.lookup schema", requests[0].Tools)
	}
	if requests[0].Tools[0].Name != toolAlias {
		t.Fatalf("initial tool name = %q, want provider-safe alias %q", requests[0].Tools[0].Name, toolAlias)
	}
	if len(requests[1].Tools) != 0 {
		t.Fatalf("follow-up tools = %+v, want no recursive tools", requests[1].Tools)
	}
	if !strings.Contains(requests[1].Text, "用户喜欢低延迟语音") {
		t.Fatalf("follow-up prompt missing tool result: %s", requests[1].Text)
	}
	sequence := downlink.Sequence()
	if !containsSequenceItem(sequence, "json:tts:sentence_start:你喜欢低延迟语音。") {
		t.Fatalf("sequence = %v, want spoken follow-up result", sequence)
	}

	traceEvents := waitForTraceEvent(t, traceOutput, "llm_tool_followup_request", time.Second)
	assertTraceDoesNotContainField(t, traceEvents, "result")
	assertTraceDoesNotContainField(t, traceEvents, "payload")
	if strings.Contains(string(traceOutput.Bytes()), "用户喜欢低延迟语音") {
		t.Fatalf("trace leaked tool result payload: %s", string(traceOutput.Bytes()))
	}
	if !strings.Contains(string(traceOutput.Bytes()), "tool_followup_loop_suppressed") {
		t.Fatalf("trace missing recursive tool suppression event: %s", string(traceOutput.Bytes()))
	}
	if strings.Contains(string(traceOutput.Bytes()), toolAlias) {
		t.Fatalf("trace recorded provider alias instead of internal tool name: %s", string(traceOutput.Bytes()))
	}
	select {
	case call := <-executed:
		t.Fatalf("recursive follow-up tool call executed: %+v", call)
	default:
	}
}

func TestVoiceLoopRecoversSpeechWhenToolOnlyResultIsNotAllowedInFollowUp(t *testing.T) {
	ctx := context.Background()
	session := New("sess_tool_followup_no_speakable_result", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	executed := make(chan servicetools.Call, 1)
	const toolName = "diagnostics.status"
	if err := registry.Register(servicetools.Definition{
		Name:        toolName,
		Description: "Diagnostic status not intended for spoken follow-up.",
		Permission:  servicetools.PermissionRead,
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, call servicetools.Call) (servicetools.Result, error) {
		executed <- call
		return servicetools.Result{
			Payload: json.RawMessage(`{"ok":true,"internal":"not for speech"}`),
		}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	toolAlias := llmProviderToolAlias(toolName, map[string]string{})
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{
		{{
			ToolCalls: []providers.ToolCall{{
				ID:   "call-status",
				Name: toolAlias,
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
		{{
			Text:      "可以继续。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
	}}
	traceOutput := &lockedBuffer{}
	traceRecorder := observability.NewTraceRecorder(observability.TraceRecorderOptions{
		Writer:        traceOutput,
		RedactSecrets: true,
	})
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "你现在还能继续吗？"},
		LLM:           llm,
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		TraceRecorder: traceRecorder,
		ServiceTools:  registry,
		ToolResultFollowUpPolicy: &ToolResultFollowUpPolicy{
			Enabled:        true,
			MaxResults:     3,
			MaxResultBytes: 2048,
			AllowedTools:   []string{"memory.lookup"},
		},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	select {
	case call := <-executed:
		if call.Name != toolName {
			t.Fatalf("service call = %+v, want %s", call, toolName)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for diagnostic tool execution")
	}
	requests := llm.Requests()
	if len(requests) != 2 {
		t.Fatalf("LLM requests = %d, want initial + no-result recovery follow-up", len(requests))
	}
	if len(requests[1].Tools) != 0 {
		t.Fatalf("recovery follow-up tools = %+v, want no recursive tools", requests[1].Tools)
	}
	if !strings.Contains(requests[1].Text, "不要再调用工具") {
		t.Fatalf("recovery follow-up prompt = %q, want no-tool direct-answer instruction", requests[1].Text)
	}
	if !containsSequenceItem(downlink.Sequence(), "json:tts:sentence_start:可以继续。") {
		t.Fatalf("sequence = %v, want spoken recovery answer", downlink.Sequence())
	}
	if strings.Contains(string(traceOutput.Bytes()), "not for speech") {
		t.Fatalf("trace leaked filtered tool payload: %s", string(traceOutput.Bytes()))
	}
}

func TestVoiceLoopCanDisableToolResultFollowUp(t *testing.T) {
	ctx := context.Background()
	session := New("sess_tool_followup_disabled", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	executed := make(chan servicetools.Call, 1)
	if err := registry.Register(servicetools.Definition{
		Name:        "memory.lookup",
		Description: "Look up scoped user memories.",
		Permission:  servicetools.PermissionRead,
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, call servicetools.Call) (servicetools.Result, error) {
		executed <- call
		return servicetools.Result{Payload: json.RawMessage(`{"content":"saved"}`)}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	toolAlias := llmProviderToolAlias("memory.lookup", map[string]string{})
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{
		{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-memory",
				Name:      toolAlias,
				Arguments: map[string]any{"query": "偏好"},
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
		{{
			Text:      "不应被调用。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
	}}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:                  session,
		ASR:                      fixedTranscriptASRProvider{text: "查一下。"},
		LLM:                      llm,
		TTS:                      providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:                 downlink,
		Pacer:                    audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		ServiceTools:             registry,
		ToolResultFollowUpPolicy: &ToolResultFollowUpPolicy{Enabled: false},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	select {
	case <-executed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool execution")
	}
	if len(llm.Requests()) != 1 {
		t.Fatalf("LLM requests = %d, want no follow-up request when disabled", len(llm.Requests()))
	}
	if containsSequenceItem(downlink.Sequence(), "json:tts:sentence_start:不应被调用。") {
		t.Fatalf("sequence = %v, want no spoken disabled follow-up", downlink.Sequence())
	}
}

func TestVoiceLoopAllowsOneFollowUpToolCallWhenConfigured(t *testing.T) {
	ctx := context.Background()
	session := New("sess_tool_followup_recursive", "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedPermissions: []string{servicetools.PermissionRead, servicetools.PermissionExternal},
	})
	executed := make(chan servicetools.Call, 4)
	registerTestServiceTool := func(name string, permission string, payload string) {
		t.Helper()
		if err := registry.Register(servicetools.Definition{
			Name:        name,
			Description: "test tool " + name,
			Permission:  permission,
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, call servicetools.Call) (servicetools.Result, error) {
			executed <- call
			return servicetools.Result{Payload: json.RawMessage(payload)}, nil
		}); err != nil {
			t.Fatalf("Register(%s) error = %v", name, err)
		}
	}
	registerTestServiceTool("memory.lookup", servicetools.PermissionRead, `{"memories":[{"content":"用户喜欢低延迟语音。"}]}`)
	registerTestServiceTool("search.web", servicetools.PermissionRead, `{"results":[{"title":"StackChan","summary":"桌面语音终端。"}]}`)
	registerTestServiceTool("homeassistant.call_action", servicetools.PermissionExternal, `{"state":"changed"}`)

	memoryAlias := llmProviderToolAlias("memory.lookup", map[string]string{})
	searchAlias := llmProviderToolAlias("search.web", map[string]string{})
	llm := &scriptedLLMProvider{scripts: [][]providers.LLMChunk{
		{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-memory",
				Name:      memoryAlias,
				Arguments: map[string]any{"query": "偏好"},
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
		{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-search",
				Name:      searchAlias,
				Arguments: map[string]any{"query": "StackChan 是什么"},
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
		{{
			Text:      "你喜欢低延迟语音，我也查到了 StackChan 的资料。",
			IsFinal:   true,
			CreatedAt: time.Now(),
		}},
	}}
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:       session,
		ASR:           fixedTranscriptASRProvider{text: "结合我的偏好查一下 StackChan。"},
		LLM:           llm,
		TTS:           providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:      downlink,
		Pacer:         audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		ServiceTools:  registry,
		TraceRecorder: observability.NewTraceRecorder(observability.TraceRecorderOptions{Writer: &lockedBuffer{}, RedactSecrets: true}),
		ToolResultFollowUpPolicy: &ToolResultFollowUpPolicy{
			Enabled:        true,
			MaxResults:     3,
			MaxResultBytes: 2048,
			AllowedTools:   []string{"memory.lookup", "search.web"},
			AllowToolCalls: true,
			MaxToolCalls:   1,
		},
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	runMockVoiceTurn(t, loop)

	var first servicetools.Call
	select {
	case first = <-executed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first tool execution")
	}
	var second servicetools.Call
	select {
	case second = <-executed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for follow-up tool execution")
	}
	if first.Name != "memory.lookup" || second.Name != "search.web" {
		t.Fatalf("executed tools = %s, %s; want memory.lookup then search.web", first.Name, second.Name)
	}
	select {
	case call := <-executed:
		t.Fatalf("unexpected extra tool execution: %+v", call)
	default:
	}

	requests := llm.Requests()
	if len(requests) != 3 {
		t.Fatalf("LLM requests = %d, want initial + follow-up tool + final answer", len(requests))
	}
	if !hasLLMToolAlias(requests[1].Tools, "memory.lookup") || !hasLLMToolAlias(requests[1].Tools, "search.web") {
		t.Fatalf("follow-up tools = %+v, want only allowlisted read tool schemas", requests[1].Tools)
	}
	if hasLLMToolAlias(requests[1].Tools, "homeassistant.call_action") {
		t.Fatalf("follow-up tools = %+v, must not include write/action tool schema", requests[1].Tools)
	}
	if len(requests[2].Tools) != 0 {
		t.Fatalf("final answer tools = %+v, want no recursive schemas", requests[2].Tools)
	}
	if !strings.Contains(requests[2].Text, "用户喜欢低延迟语音") || !strings.Contains(requests[2].Text, "StackChan") {
		t.Fatalf("final answer prompt = %s, want both tool results", requests[2].Text)
	}
	spokenSequence := strings.Join(downlink.Sequence(), " ")
	if !strings.Contains(spokenSequence, "你喜欢低延迟语音") || !strings.Contains(spokenSequence, "我也查到了 StackChan") {
		t.Fatalf("sequence = %v, want spoken final answer", downlink.Sequence())
	}
}

func TestBuildToolFollowUpPromptBoundsToolResults(t *testing.T) {
	longResult := strings.Repeat("低延迟", 900)
	prompt, resultCount, resultBytes := buildToolFollowUpPrompt("原始提示", []ToolCallOutcome{
		{Name: "memory.lookup", Result: json.RawMessage(`{"content":"first"}`)},
		{Name: "skip.failure", Result: json.RawMessage(`{"content":"skip"}`), ErrorCode: "failed"},
		{Name: "profile.lookup", Result: json.RawMessage(`{"content":"second"}`)},
		{Name: "notes.lookup", Result: json.RawMessage(`{"content":"third"}`)},
		{Name: "overflow.lookup", Result: json.RawMessage(`{"content":"fourth"}`)},
		{Name: "long.lookup", Result: json.RawMessage(`{"content":"` + longResult + `"}`)},
	})
	if resultCount != defaultToolFollowUpMaxResults {
		t.Fatalf("resultCount = %d, want %d", resultCount, defaultToolFollowUpMaxResults)
	}
	if resultBytes > defaultToolFollowUpMaxResultBytes {
		t.Fatalf("resultBytes = %d, want <= %d", resultBytes, defaultToolFollowUpMaxResultBytes)
	}
	if strings.Contains(prompt, "skip.failure") || strings.Contains(prompt, "overflow.lookup") || strings.Contains(prompt, "long.lookup") {
		t.Fatalf("prompt included skipped or overflow results: %s", prompt)
	}
	if !strings.Contains(prompt, "memory.lookup") || !strings.Contains(prompt, "profile.lookup") || !strings.Contains(prompt, "notes.lookup") {
		t.Fatalf("prompt missing expected bounded results: %s", prompt)
	}

	longPrompt, longCount, longBytes := buildToolFollowUpPrompt("原始提示", []ToolCallOutcome{
		{Name: "long.lookup", Result: json.RawMessage(`{"content":"` + longResult + `"}`)},
	})
	if longCount != 1 {
		t.Fatalf("longCount = %d, want 1", longCount)
	}
	if longBytes > defaultToolFollowUpMaxResultBytes {
		t.Fatalf("longBytes = %d, want <= %d", longBytes, defaultToolFollowUpMaxResultBytes)
	}
	if !strings.Contains(longPrompt, "...") {
		t.Fatalf("long prompt was not visibly truncated: %s", longPrompt)
	}
}

func TestBuildToolFollowUpPromptUsesConfiguredBounds(t *testing.T) {
	prompt, resultCount, resultBytes := buildToolFollowUpPromptWithPolicy("原始提示", []ToolCallOutcome{
		{Name: "memory.lookup", Result: json.RawMessage(`{"content":"first"}`)},
		{Name: "profile.lookup", Result: json.RawMessage(`{"content":"second"}`)},
	}, ToolResultFollowUpPolicy{
		Enabled:        true,
		MaxResults:     1,
		MaxResultBytes: 24,
	})

	if resultCount != 1 {
		t.Fatalf("resultCount = %d, want 1", resultCount)
	}
	if resultBytes > 24 {
		t.Fatalf("resultBytes = %d, want <= 24", resultBytes)
	}
	if !strings.Contains(prompt, "memory.lookup") || strings.Contains(prompt, "profile.lookup") {
		t.Fatalf("prompt = %s, want only first result", prompt)
	}
}

func TestBuildToolFollowUpPromptAllowsOnlyConfiguredTools(t *testing.T) {
	prompt, resultCount, _ := buildToolFollowUpPromptWithPolicy("原始提示", []ToolCallOutcome{
		{Name: "memory.lookup", Result: json.RawMessage(`{"content":"first"}`)},
		{Name: "homeassistant.call_action", Result: json.RawMessage(`{"content":"write result"}`)},
		{Name: "search.web", Result: json.RawMessage(`{"content":"third"}`)},
	}, ToolResultFollowUpPolicy{
		Enabled:        true,
		MaxResults:     3,
		MaxResultBytes: 2048,
		AllowedTools:   []string{"memory.lookup", "search.web"},
	})

	if resultCount != 2 {
		t.Fatalf("resultCount = %d, want two allowlisted results", resultCount)
	}
	if !strings.Contains(prompt, "memory.lookup") || !strings.Contains(prompt, "search.web") {
		t.Fatalf("prompt missing allowlisted results: %s", prompt)
	}
	if strings.Contains(prompt, "homeassistant.call_action") || strings.Contains(prompt, "write result") {
		t.Fatalf("prompt included non-allowlisted write tool result: %s", prompt)
	}
}

func runMockVoiceTurn(t *testing.T, loop *VoiceLoop) {
	t.Helper()
	ctx := context.Background()
	if _, err := loop.HandleListenStart(ctx); err != nil {
		t.Fatalf("HandleListenStart() error = %v", err)
	}
	frame := audio.NewOpusFrame([]byte{0x01, 0x02, 0x03}, xiaozhi.XiaozhiUplinkSampleRateHz, xiaozhi.XiaozhiFrameDurationMS, time.Now())
	if err := loop.AcceptOpus(frame); err != nil {
		t.Fatalf("AcceptOpus() error = %v", err)
	}
	if err := loop.HandleListenStop(ctx); err != nil {
		t.Fatalf("HandleListenStop() error = %v", err)
	}
}

func TestVoiceLoopCloseDecrementsActiveSessionMetricOnce(t *testing.T) {
	ctx := context.Background()
	metrics := observability.NewMetrics()
	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session:  New("sess_metric_close", "stackchan-s3-main", "client-1"),
		ASR:      providers.NewMockASRProvider(providers.MockConfig{}),
		LLM:      providers.NewMockLLMProvider(providers.MockConfig{}),
		TTS:      providers.NewMockTTSProvider(providers.MockConfig{}),
		Downlink: &recordingDownlink{},
		Pacer:    audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		Metrics:  metrics,
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}

	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}
	if got := renderedMetricValue(t, metrics.Render(), observability.MetricSessionsActive); got != 1 {
		t.Fatalf("active sessions after hello = %v, want 1", got)
	}

	loop.Close(ctx)
	loop.Close(ctx)

	if got := renderedMetricValue(t, metrics.Render(), observability.MetricSessionsActive); got != 0 {
		t.Fatalf("active sessions after double close = %v, want 0", got)
	}
}

func TestTextSegmenterFlushesOnChinesePunctuationAndMaxRunes(t *testing.T) {
	segmenter := newTextSegmenter(4)

	first := segmenter.Append("你好呀，世界", false)
	if len(first) != 1 || first[0] != "你好呀，" {
		t.Fatalf("first segments = %v, want punctuation flush", first)
	}

	second := segmenter.Append("准备好了", false)
	if len(second) != 1 || second[0] != "世界准备" {
		t.Fatalf("second segments = %v, want max rune flush", second)
	}

	final := segmenter.Append("。", true)
	if len(final) != 1 || final[0] != "好了。" {
		t.Fatalf("final segments = %v, want final punctuation flush", final)
	}
}

func TestTextSegmenterKeepsShortCommaLeadInWithFollowingClause(t *testing.T) {
	segmenter := newTextSegmenter(defaultSegmentMaxRunes)

	first := segmenter.Append("好的，", false)
	if len(first) != 0 {
		t.Fatalf("first segments = %v, want short comma lead-in buffered", first)
	}

	second := segmenter.Append("我来看看。", false)
	if len(second) != 1 || second[0] != "好的，我来看看。" {
		t.Fatalf("second segments = %v, want short lead-in merged with following clause", second)
	}
}

func TestTextSegmenterFlushesASCIIEndPunctuation(t *testing.T) {
	segmenter := newTextSegmenter(defaultSegmentMaxRunes)

	segments := segmenter.Append("我查到了.", false)
	if len(segments) != 1 || segments[0] != "我查到了." {
		t.Fatalf("segments = %v, want ASCII sentence punctuation flush", segments)
	}
}

func renderedMetricValue(t *testing.T, rendered string, name string) float64 {
	t.Helper()

	for _, line := range strings.Split(rendered, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[0] != name {
			continue
		}
		value, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			t.Fatalf("parse metric %q value %q: %v", name, parts[1], err)
		}
		return value
	}
	t.Fatalf("metric %q not found in:\n%s", name, rendered)
	return 0
}

func stackchanTestBodyScheduler(session *Session) *stackchan.BodyScheduler {
	return stackchan.NewBodyScheduler(stackchan.BodySchedulerOptions{
		MinCommandGap: time.Nanosecond,
		GenerationIsCurrent: func(generation int64) bool {
			return session.AcceptsGeneration(generation)
		},
	})
}

func toolArgNumberEquals(args map[string]any, key string, want float64) bool {
	got, ok := args[key]
	if !ok {
		return false
	}
	switch value := got.(type) {
	case float64:
		return value == want
	case int:
		return float64(value) == want
	default:
		return false
	}
}

func containsSequenceItem(sequence []string, want string) bool {
	for _, item := range sequence {
		if item == want {
			return true
		}
	}
	return false
}

func waitForMCPToolCall(t *testing.T, downlink *recordingDownlink, toolName string, wantArgs map[string]float64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if downlink.HasMCPToolCall(t, toolName, wantArgs) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("did not find MCP tool call %s with args %+v; sequence=%v", toolName, wantArgs, downlink.Sequence())
}

func waitForMCPToolCallCount(t *testing.T, downlink *recordingDownlink, toolName string, wantCount int, timeout time.Duration) []mcp.ToolCallParams {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		calls := downlink.MCPToolCalls(t, toolName)
		if len(calls) >= wantCount {
			return calls[:wantCount]
		}
		time.Sleep(time.Millisecond)
	}
	calls := downlink.MCPToolCalls(t, toolName)
	t.Fatalf("MCP tool call count for %s = %d, want at least %d; sequence=%v", toolName, len(calls), wantCount, downlink.Sequence())
	return nil
}

func toolArgsMatch(args map[string]any, want map[string]float64) bool {
	for key, wantValue := range want {
		if !toolArgNumberEquals(args, key, wantValue) {
			return false
		}
	}
	return true
}

type toolEventSceneCase struct {
	SessionID string
	ToolName  string
	Arguments map[string]any
	Event     string
	Caption   string
	Scene     string
	Emotion   string
	Accent    string
}

func assertToolCallDispatchesEventScene(t *testing.T, tc toolEventSceneCase) {
	t.Helper()
	ctx := context.Background()
	session := New(tc.SessionID, "stackchan-s3-main", "client-1")
	downlink := &recordingDownlink{}
	broker := newRespondingTestBroker(t, session.ID(), downlink, []string{mcp.ToolSetScreenScene})
	broker.StoreTools([]mcp.Tool{{Name: mcp.ToolSetScreenScene}})
	toolOrchestrator := newRecordingToolOrchestrator()

	loop, err := NewVoiceLoop(VoiceLoopOptions{
		Session: session,
		ASR:     fixedTranscriptASRProvider{text: "帮我处理一下。"},
		LLM: chunkedLLMProvider{chunks: []providers.LLMChunk{{
			ToolCalls: []providers.ToolCall{{
				ID:        "call-tool",
				Name:      tc.ToolName,
				Arguments: tc.Arguments,
			}},
			IsFinal:   true,
			CreatedAt: time.Now(),
		}}},
		TTS:              providers.NewMockTTSProvider(providers.MockConfig{TTSFirstFrameDelayMS: 1, TTSFrameCount: 1}),
		Downlink:         downlink,
		Pacer:            audio.NewPacer(audio.PacerOptions{Sleep: audio.NoopSleep}),
		MCPBroker:        broker,
		ToolOrchestrator: toolOrchestrator,
		SceneComposer: stackchan.NewSceneComposer(stackchan.DisplayOptions{
			SceneTTLMS:      800,
			MaxCaptionChars: 32,
			EventScenes: map[string]stackchan.ScenePolicy{
				tc.Event: {
					Scene:   tc.Scene,
					Emotion: tc.Emotion,
					Caption: tc.Caption,
					Accent:  tc.Accent,
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("NewVoiceLoop() error = %v", err)
	}
	if _, err := loop.HandleHello(ctx); err != nil {
		t.Fatalf("HandleHello() error = %v", err)
	}

	runMockVoiceTurn(t, loop)
	toolOrchestrator.WaitRequests(t, 1)

	scene := waitForScreenSceneCaption(t, downlink, tc.Caption, time.Second)
	if scene.Arguments["scene"] != tc.Scene || scene.Arguments["emotion"] != tc.Emotion || scene.Arguments["accent"] != tc.Accent {
		t.Fatalf("%s scene = %#v, want %s/%s/%s", tc.Event, scene.Arguments, tc.Scene, tc.Emotion, tc.Accent)
	}
}

func waitForScreenScenes(t *testing.T, downlink *recordingDownlink, wantScenes []string, timeout time.Duration) []mcp.ToolCallParams {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		scenes := downlink.MCPToolCalls(t, mcp.ToolSetScreenScene)
		if len(scenes) < len(wantScenes) {
			time.Sleep(time.Millisecond)
			continue
		}
		matches := true
		for index, want := range wantScenes {
			if scenes[index].Arguments["scene"] != want {
				matches = false
				break
			}
		}
		if matches {
			return scenes[:len(wantScenes)]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("did not find screen scene lifecycle %v; got=%v; sequence=%v", wantScenes, screenSceneNames(downlink.MCPToolCalls(t, mcp.ToolSetScreenScene)), downlink.Sequence())
	return nil
}

func waitForScreenSceneCaption(t *testing.T, downlink *recordingDownlink, caption string, timeout time.Duration) mcp.ToolCallParams {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, scene := range downlink.MCPToolCalls(t, mcp.ToolSetScreenScene) {
			if scene.Arguments["caption"] == caption {
				return scene
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("did not find screen scene caption %q; got=%v; sequence=%v", caption, screenSceneNames(downlink.MCPToolCalls(t, mcp.ToolSetScreenScene)), downlink.Sequence())
	return mcp.ToolCallParams{}
}

func assertNoScreenSceneCaption(t *testing.T, downlink *recordingDownlink, caption string, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		for _, scene := range downlink.MCPToolCalls(t, mcp.ToolSetScreenScene) {
			if scene.Arguments["caption"] == caption {
				t.Fatalf("found unexpected screen scene caption %q; got=%v; sequence=%v", caption, screenSceneNames(downlink.MCPToolCalls(t, mcp.ToolSetScreenScene)), downlink.Sequence())
			}
		}
		time.Sleep(time.Millisecond)
	}
}

func screenSceneNames(calls []mcp.ToolCallParams) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		name, _ := call.Arguments["scene"].(string)
		names = append(names, name)
	}
	return names
}

func waitForTraceEvent(t *testing.T, traceOutput *lockedBuffer, event string, timeout time.Duration) []observability.TraceEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var events []observability.TraceEvent
	for time.Now().Before(deadline) {
		events = decodeTraceEvents(t, traceOutput.Bytes())
		for _, traceEvent := range events {
			if traceEvent.Event == event {
				return events
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("trace missing event %q; got=%v", event, traceEventNames(events))
	return nil
}

func waitForTraceEventCount(t *testing.T, traceOutput *lockedBuffer, event string, count int, timeout time.Duration) []observability.TraceEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var events []observability.TraceEvent
	for time.Now().Before(deadline) {
		events = decodeTraceEvents(t, traceOutput.Bytes())
		seen := 0
		for _, traceEvent := range events {
			if traceEvent.Event == event {
				seen++
			}
		}
		if seen >= count {
			return events
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("trace event %q count < %d; got=%v", event, count, traceEventNames(events))
	return nil
}

func findTraceEventByFields(t *testing.T, events []observability.TraceEvent, event string, fields map[string]any) observability.TraceEvent {
	t.Helper()
	for _, traceEvent := range events {
		if traceEvent.Event != event {
			continue
		}
		matches := true
		for key, want := range fields {
			if traceEvent.Fields[key] != want {
				matches = false
				break
			}
		}
		if matches {
			return traceEvent
		}
	}
	t.Fatalf("trace missing event %q with fields %+v; got=%+v", event, fields, events)
	return observability.TraceEvent{}
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := b.buffer.Bytes()
	clone := make([]byte, len(data))
	copy(clone, data)
	return clone
}

type recordingDownlink struct {
	mu     sync.Mutex
	events []downlinkEvent
	onJSON func(downlinkEvent)
}

type switchingProviderResolver struct {
	mu       sync.Mutex
	current  VoiceProviderSet
	outcomes []VoiceProviderOutcome
}

func (r *switchingProviderResolver) ResolveVoiceProviders(_ context.Context, _ string) (VoiceProviderSet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current, nil
}

func (r *switchingProviderResolver) Set(set VoiceProviderSet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = set
}

func (r *switchingProviderResolver) ObserveVoiceTurn(_ context.Context, outcome VoiceProviderOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcomes = append(r.outcomes, outcome)
}

func (r *switchingProviderResolver) Outcomes() []VoiceProviderOutcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	outcomes := make([]VoiceProviderOutcome, len(r.outcomes))
	copy(outcomes, r.outcomes)
	return outcomes
}

type staticLLMContextBuilder struct {
	context LLMContext
	err     error
}

func (b staticLLMContextBuilder) BuildLLMContext(_ context.Context, _ LLMContextRequest) (LLMContext, error) {
	if b.err != nil {
		return LLMContext{}, b.err
	}
	return b.context, nil
}

type recordingLLMContextBuilder struct {
	mu       sync.Mutex
	requests []LLMContextRequest
	result   LLMContext
	err      error
}

func (b *recordingLLMContextBuilder) BuildLLMContext(_ context.Context, request LLMContextRequest) (LLMContext, error) {
	b.mu.Lock()
	b.requests = append(b.requests, request)
	b.mu.Unlock()
	if b.err != nil {
		return LLMContext{}, b.err
	}
	return b.result, nil
}

func (b *recordingLLMContextBuilder) Requests() []LLMContextRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	requests := make([]LLMContextRequest, len(b.requests))
	copy(requests, b.requests)
	return requests
}

type recentConversationTestContextBuilder struct {
	recorder *recordingConversationRecorder
}

func (b recentConversationTestContextBuilder) BuildLLMContext(_ context.Context, request LLMContextRequest) (LLMContext, error) {
	var out strings.Builder
	recentCount := 0
	if b.recorder != nil {
		for _, turn := range b.recorder.Requests() {
			if turn.DeviceID != request.DeviceID {
				continue
			}
			if recentCount == 0 {
				out.WriteString("最近对话（从旧到新）:\n")
			}
			out.WriteString("- 用户: ")
			out.WriteString(turn.UserText)
			out.WriteByte('\n')
			out.WriteString("  助手: ")
			out.WriteString(turn.AssistantText)
			out.WriteByte('\n')
			recentCount++
		}
	}
	if recentCount > 0 {
		out.WriteString("如果当前消息是“继续”“那下一步呢”“刚才那个”“这个/它/三件事”等省略或指代，必须先根据最近对话补全语境再回答。\n")
	}
	out.WriteString("当前用户消息:\n")
	out.WriteString(request.Transcript)
	return LLMContext{Text: out.String(), RecentTurnCount: recentCount}, nil
}

type recordingLLMProvider struct {
	mu       sync.Mutex
	lastText string
	lastReq  providers.LLMRequest
}

type chunkedLLMProvider struct {
	chunks []providers.LLMChunk
}

type scriptedLLMProvider struct {
	mu       sync.Mutex
	scripts  [][]providers.LLMChunk
	requests []providers.LLMRequest
}

func (p chunkedLLMProvider) Stream(ctx context.Context, _ providers.LLMRequest) (<-chan providers.LLMChunk, error) {
	out := make(chan providers.LLMChunk, len(p.chunks))
	for _, chunk := range p.chunks {
		select {
		case <-ctx.Done():
			close(out)
			return out, nil
		case out <- chunk:
		}
	}
	close(out)
	return out, nil
}

func (p *scriptedLLMProvider) Stream(ctx context.Context, req providers.LLMRequest) (<-chan providers.LLMChunk, error) {
	p.mu.Lock()
	callIndex := len(p.requests)
	p.requests = append(p.requests, req)
	var chunks []providers.LLMChunk
	if callIndex < len(p.scripts) {
		chunks = p.scripts[callIndex]
	}
	p.mu.Unlock()

	out := make(chan providers.LLMChunk, len(chunks))
	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			close(out)
			return out, nil
		case out <- chunk:
		}
	}
	close(out)
	return out, nil
}

func (p *scriptedLLMProvider) Requests() []providers.LLMRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	requests := make([]providers.LLMRequest, len(p.requests))
	copy(requests, p.requests)
	return requests
}

func (p *recordingLLMProvider) Stream(ctx context.Context, req providers.LLMRequest) (<-chan providers.LLMChunk, error) {
	p.mu.Lock()
	p.lastText = req.Text
	p.lastReq = req
	p.mu.Unlock()

	out := make(chan providers.LLMChunk, 1)
	out <- providers.LLMChunk{Text: "好的。", IsFinal: true, CreatedAt: time.Now()}
	close(out)
	return out, nil
}

func (p *recordingLLMProvider) LastText() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastText
}

func (p *recordingLLMProvider) LastRequest() providers.LLMRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastReq
}

type qualityTTSProvider struct {
	stats audio.PCM16Stats
}

func (p qualityTTSProvider) Stream(ctx context.Context, req providers.TTSRequest) (<-chan providers.TTSFrame, error) {
	out := make(chan providers.TTSFrame, 1)
	out <- providers.TTSFrame{
		Generation:   req.Generation,
		Opus:         []byte{0xf8, 0xff, 0xfe, 0x42},
		TextSpan:     req.Text,
		Duration:     60 * time.Millisecond,
		CreatedAt:    time.Now(),
		AudioQuality: p.stats,
	}
	close(out)
	return out, nil
}

type recordingMemoryWriter struct {
	mu       sync.Mutex
	requests []MemoryWriteRequest
	result   MemoryWriteResult
	err      error
}

func (w *recordingMemoryWriter) WriteMemories(_ context.Context, request MemoryWriteRequest) (MemoryWriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.requests = append(w.requests, request)
	return w.result, w.err
}

func (w *recordingMemoryWriter) Requests() []MemoryWriteRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	requests := make([]MemoryWriteRequest, len(w.requests))
	copy(requests, w.requests)
	return requests
}

type recordingConversationRecorder struct {
	mu       sync.Mutex
	requests []ConversationTurnRecordRequest
	err      error
}

func (r *recordingConversationRecorder) RecordConversationTurn(_ context.Context, request ConversationTurnRecordRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
	return r.err
}

func (r *recordingConversationRecorder) Requests() []ConversationTurnRecordRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	requests := make([]ConversationTurnRecordRequest, len(r.requests))
	copy(requests, r.requests)
	return requests
}

type recordingAgentModeCommandHandler struct {
	mu       sync.Mutex
	requests []AgentModeCommandRequest
	result   AgentModeCommandResult
	err      error
}

func (h *recordingAgentModeCommandHandler) HandleAgentModeCommand(_ context.Context, request AgentModeCommandRequest) (AgentModeCommandResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, request)
	if h.err != nil {
		return AgentModeCommandResult{}, h.err
	}
	return h.result, nil
}

func (h *recordingAgentModeCommandHandler) Requests() []AgentModeCommandRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	requests := make([]AgentModeCommandRequest, len(h.requests))
	copy(requests, h.requests)
	return requests
}

type recordingProviderProfileCommandHandler struct {
	mu       sync.Mutex
	requests []ProviderProfileCommandRequest
	result   ProviderProfileCommandResult
	err      error
}

func (h *recordingProviderProfileCommandHandler) HandleProviderProfileCommand(_ context.Context, request ProviderProfileCommandRequest) (ProviderProfileCommandResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, request)
	if h.err != nil {
		return ProviderProfileCommandResult{}, h.err
	}
	return h.result, nil
}

func (h *recordingProviderProfileCommandHandler) Requests() []ProviderProfileCommandRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	requests := make([]ProviderProfileCommandRequest, len(h.requests))
	copy(requests, h.requests)
	return requests
}

type fixedAgentModeReader struct {
	mode string
	err  error
}

func (r fixedAgentModeReader) CurrentAgentMode(_ context.Context, _ AgentModeReadRequest) (AgentModeReadResult, error) {
	if r.err != nil {
		return AgentModeReadResult{}, r.err
	}
	return AgentModeReadResult{Mode: r.mode}, nil
}

type recordingAgentRuntimeRouter struct {
	mu       sync.Mutex
	requests []AgentRuntimeRequest
	result   AgentRuntimeResult
	err      error
}

func (r *recordingAgentRuntimeRouter) RouteAgentTurn(_ context.Context, request AgentRuntimeRequest) (AgentRuntimeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
	if r.err != nil {
		return AgentRuntimeResult{}, r.err
	}
	return r.result, nil
}

func (r *recordingAgentRuntimeRouter) Requests() []AgentRuntimeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	requests := make([]AgentRuntimeRequest, len(r.requests))
	copy(requests, r.requests)
	return requests
}

type recordingToolOrchestrator struct {
	mu       sync.Mutex
	requests []ToolCallRequest
	notify   chan struct{}
}

func newRecordingToolOrchestrator() *recordingToolOrchestrator {
	return &recordingToolOrchestrator{notify: make(chan struct{}, 8)}
}

func (o *recordingToolOrchestrator) ExecuteToolCalls(_ context.Context, request ToolCallRequest) []ToolCallOutcome {
	o.mu.Lock()
	o.requests = append(o.requests, ToolCallRequest{
		Turn:  request.Turn,
		Calls: cloneProviderToolCalls(request.Calls),
	})
	o.mu.Unlock()
	select {
	case o.notify <- struct{}{}:
	default:
	}
	outcomes := make([]ToolCallOutcome, 0, len(request.Calls))
	for index, call := range request.Calls {
		outcomes = append(outcomes, ToolCallOutcome{
			Index:         index,
			Name:          call.Name,
			ArgumentCount: len(call.Arguments),
			Result:        json.RawMessage(`{"ok":true}`),
			ResultBytes:   len(`{"ok":true}`),
		})
	}
	return outcomes
}

type failingToolOrchestrator struct {
	notify chan struct{}
}

func (o *failingToolOrchestrator) ExecuteToolCalls(_ context.Context, request ToolCallRequest) []ToolCallOutcome {
	select {
	case o.notify <- struct{}{}:
	default:
	}
	outcomes := make([]ToolCallOutcome, 0, len(request.Calls))
	for index, call := range request.Calls {
		outcomes = append(outcomes, ToolCallOutcome{
			Index:         index,
			Name:          call.Name,
			ArgumentCount: len(call.Arguments),
			Skipped:       true,
			ErrorCode:     errorCodeToolCallFailed,
		})
	}
	return outcomes
}

func (o *failingToolOrchestrator) Wait(t *testing.T) {
	t.Helper()
	select {
	case <-o.notify:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failing tool orchestrator")
	}
}

func (o *recordingToolOrchestrator) WaitRequests(t *testing.T, count int) []ToolCallRequest {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		o.mu.Lock()
		if len(o.requests) >= count {
			requests := make([]ToolCallRequest, len(o.requests))
			copy(requests, o.requests)
			o.mu.Unlock()
			return requests
		}
		o.mu.Unlock()
		select {
		case <-o.notify:
		case <-deadline:
			o.mu.Lock()
			requests := make([]ToolCallRequest, len(o.requests))
			copy(requests, o.requests)
			o.mu.Unlock()
			t.Fatalf("tool orchestrator requests len = %d, want at least %d", len(requests), count)
		}
	}
}

type fixedTranscriptASRProvider struct {
	text string
}

type scriptedASRProvider struct {
	mu    sync.Mutex
	texts []string
	index int
}

type autoFinalASRProvider struct {
	text string
}

func (p autoFinalASRProvider) Start(ctx context.Context, req providers.ASRStartRequest) (providers.ASRStream, error) {
	return &autoFinalASRStream{
		ctx: ctx,
		event: providers.ASREvent{
			Type:       providers.ASREventFinal,
			Text:       p.text,
			IsFinal:    true,
			StartedAt:  req.StartedAt,
			FinishedAt: time.Now(),
		},
		events: make(chan providers.ASREvent, 1),
	}, nil
}

type autoFinalASRStream struct {
	ctx    context.Context
	event  providers.ASREvent
	events chan providers.ASREvent
	once   sync.Once
}

func (s *autoFinalASRStream) AcceptOpus(audio.Frame) error {
	s.once.Do(func() {
		select {
		case <-s.ctx.Done():
		case s.events <- s.event:
		}
		close(s.events)
	})
	return nil
}

func (s *autoFinalASRStream) Finish() error {
	return nil
}

func (s *autoFinalASRStream) Events() <-chan providers.ASREvent {
	return s.events
}

func (s *autoFinalASRStream) Close() error {
	s.once.Do(func() {
		close(s.events)
	})
	return nil
}

func (p fixedTranscriptASRProvider) Start(ctx context.Context, req providers.ASRStartRequest) (providers.ASRStream, error) {
	return &fixedTranscriptASRStream{
		ctx: ctx,
		event: providers.ASREvent{
			Type:       providers.ASREventFinal,
			Text:       p.text,
			IsFinal:    true,
			StartedAt:  req.StartedAt,
			FinishedAt: time.Now(),
		},
		events: make(chan providers.ASREvent, 1),
	}, nil
}

func (p *scriptedASRProvider) Start(ctx context.Context, req providers.ASRStartRequest) (providers.ASRStream, error) {
	p.mu.Lock()
	index := p.index
	if index < len(p.texts)-1 {
		p.index++
	}
	text := ""
	if index < len(p.texts) {
		text = p.texts[index]
	}
	p.mu.Unlock()
	return fixedTranscriptASRProvider{text: text}.Start(ctx, req)
}

type twoStageASRProvider struct {
	mu     sync.Mutex
	calls  int
	first  providers.ASRProvider
	second providers.ASRStream
}

func (p *twoStageASRProvider) Start(ctx context.Context, req providers.ASRStartRequest) (providers.ASRStream, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		return p.first.Start(ctx, req)
	}
	return p.second, nil
}

type blockingASRStream struct {
	events chan providers.ASREvent
	closed chan struct{}
	once   sync.Once
}

func newBlockingASRStream() *blockingASRStream {
	return &blockingASRStream{
		events: make(chan providers.ASREvent),
		closed: make(chan struct{}),
	}
}

func (s *blockingASRStream) AcceptOpus(audio.Frame) error {
	return nil
}

func (s *blockingASRStream) Finish() error {
	return nil
}

func (s *blockingASRStream) Events() <-chan providers.ASREvent {
	return s.events
}

func (s *blockingASRStream) Close() error {
	s.once.Do(func() {
		close(s.events)
		close(s.closed)
	})
	return nil
}

type fixedTranscriptASRStream struct {
	ctx      context.Context
	event    providers.ASREvent
	events   chan providers.ASREvent
	finished bool
	closed   bool
	mu       sync.Mutex
}

func (s *fixedTranscriptASRStream) AcceptOpus(audio.Frame) error {
	return nil
}

func (s *fixedTranscriptASRStream) Finish() error {
	s.mu.Lock()
	if s.closed || s.finished {
		s.mu.Unlock()
		return nil
	}
	s.finished = true
	s.mu.Unlock()

	select {
	case <-s.ctx.Done():
	case s.events <- s.event:
	}
	close(s.events)
	return nil
}

func (s *fixedTranscriptASRStream) Events() <-chan providers.ASREvent {
	return s.events
}

func (s *fixedTranscriptASRStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed && !s.finished {
		close(s.events)
	}
	s.closed = true
	return nil
}

type downlinkEvent struct {
	Kind    string
	Type    string
	State   string
	Text    string
	Payload json.RawMessage
}

func (d *recordingDownlink) SendJSON(_ context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	var envelope struct {
		Type    string          `json:"type"`
		State   string          `json:"state"`
		Text    string          `json:"text"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	event := downlinkEvent{
		Kind:    "json",
		Type:    envelope.Type,
		State:   envelope.State,
		Text:    envelope.Text,
		Payload: cloneJSON(envelope.Payload),
	}
	d.mu.Lock()
	d.events = append(d.events, event)
	onJSON := d.onJSON
	d.mu.Unlock()
	if onJSON != nil {
		onJSON(event)
	}
	return nil
}

func (d *recordingDownlink) SendBinary(_ context.Context, frame []byte) error {
	if len(frame) == 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, downlinkEvent{Kind: "binary"})
	return nil
}

func (d *recordingDownlink) Sequence() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	sequence := make([]string, 0, len(d.events))
	for _, event := range d.events {
		switch {
		case event.Kind == "binary":
			sequence = append(sequence, "binary")
		case event.Type == xiaozhi.MessageTypeSTT:
			sequence = append(sequence, event.Kind+":"+event.Type+":"+event.Text)
		case event.Type == xiaozhi.MessageTypeTTS && event.Text != "":
			sequence = append(sequence, event.Kind+":"+event.Type+":"+event.State+":"+event.Text)
		case event.Type == xiaozhi.MessageTypeTTS:
			sequence = append(sequence, event.Kind+":"+event.Type+":"+event.State)
		default:
			sequence = append(sequence, event.Kind+":"+event.Type)
		}
	}
	return sequence
}

func (d *recordingDownlink) CountTTSStop() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	count := 0
	for _, event := range d.events {
		if event.Type == xiaozhi.MessageTypeTTS && event.State == "stop" {
			count++
		}
	}
	return count
}

func (d *recordingDownlink) CountBinary() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	count := 0
	for _, event := range d.events {
		if event.Kind == "binary" {
			count++
		}
	}
	return count
}

func (d *recordingDownlink) MCPMessage(t *testing.T, index int) mcp.Message {
	t.Helper()

	d.mu.Lock()
	defer d.mu.Unlock()

	seen := 0
	for _, event := range d.events {
		if event.Type != xiaozhi.MessageTypeMCP {
			continue
		}
		if seen == index {
			message, err := mcp.ParseMessage(event.Payload)
			if err != nil {
				t.Fatalf("ParseMessage(%s) error = %v", string(event.Payload), err)
			}
			return message
		}
		seen++
	}
	t.Fatalf("mcp message index %d not found", index)
	return mcp.Message{}
}

func (d *recordingDownlink) HasMCPToolCall(t *testing.T, toolName string, wantArgs map[string]float64) bool {
	t.Helper()

	d.mu.Lock()
	events := make([]downlinkEvent, len(d.events))
	copy(events, d.events)
	d.mu.Unlock()

	for _, event := range events {
		if event.Type != xiaozhi.MessageTypeMCP {
			continue
		}
		message, err := mcp.ParseMessage(event.Payload)
		if err != nil || message.Method != mcp.MethodToolsCall {
			continue
		}
		var params mcp.ToolCallParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatalf("decode tool params: %v", err)
		}
		if params.Name != toolName {
			continue
		}
		matches := true
		for key, want := range wantArgs {
			if !toolArgNumberEquals(params.Arguments, key, want) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func (d *recordingDownlink) MCPToolCalls(t *testing.T, toolName string) []mcp.ToolCallParams {
	t.Helper()

	d.mu.Lock()
	events := make([]downlinkEvent, len(d.events))
	copy(events, d.events)
	d.mu.Unlock()

	var calls []mcp.ToolCallParams
	for _, event := range events {
		if event.Type != xiaozhi.MessageTypeMCP {
			continue
		}
		message, err := mcp.ParseMessage(event.Payload)
		if err != nil || message.Method != mcp.MethodToolsCall {
			continue
		}
		var params mcp.ToolCallParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatalf("decode tool params: %v", err)
		}
		if params.Name == toolName {
			calls = append(calls, params)
		}
	}
	return calls
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	clone := make([]byte, len(raw))
	copy(clone, raw)
	return clone
}

func decodeTraceEvents(t *testing.T, data []byte) []observability.TraceEvent {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	events := make([]observability.TraceEvent, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event observability.TraceEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode trace line %q: %v", string(line), err)
		}
		events = append(events, event)
	}
	return events
}

func assertTraceContainsEvents(t *testing.T, events []observability.TraceEvent, want []string) {
	t.Helper()

	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event.Event] = true
	}
	for _, event := range want {
		if !seen[event] {
			t.Fatalf("trace missing event %q; got=%v", event, traceEventNames(events))
		}
	}
}

func assertTraceDoesNotContainField(t *testing.T, events []observability.TraceEvent, field string) {
	t.Helper()

	for _, event := range events {
		if _, ok := event.Fields[field]; ok {
			t.Fatalf("trace event %q includes forbidden field %q", event.Event, field)
		}
	}
}

func traceEventNames(events []observability.TraceEvent) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.Event)
	}
	return names
}
