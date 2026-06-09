package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stackchan-gateway/internal/session"
)

func TestDeterministicMemoryExtractorCapturesExplicitProfileAndPreferences(t *testing.T) {
	extractor := NewDeterministicMemoryExtractor()

	memories := extractor.Extract(DeterministicMemoryRequest{
		UserID:     "owner",
		DeviceID:   "stackchan-s3-main",
		SessionID:  "sess_memory",
		Transcript: "以后叫我阿豪，我喜欢低延迟语音，我不喜欢长篇大论。",
		CreatedAt:  time.Date(2026, 6, 6, 18, 0, 0, 0, time.UTC),
	})

	if len(memories) != 3 {
		t.Fatalf("memories len = %d, want 3: %+v", len(memories), memories)
	}
	contents := memoryContents(memories)
	for _, want := range []string{
		"用户偏好的称呼是阿豪。",
		"用户喜欢低延迟语音。",
		"用户不喜欢长篇大论。",
	} {
		if !strings.Contains(contents, want) {
			t.Fatalf("contents missing %q: %s", want, contents)
		}
	}
	for _, memory := range memories {
		if memory.UserID != "owner" || memory.DeviceID != "stackchan-s3-main" || memory.SessionID != "sess_memory" {
			t.Fatalf("memory scope = %+v, want request scope", memory)
		}
		if strings.Contains(memory.MetadataJSON, "阿豪") || strings.Contains(memory.MetadataJSON, "低延迟") {
			t.Fatalf("metadata leaked transcript content: %+v", memory)
		}
	}
}

func TestTranscriptMemoryWriterPersistsExtractedMemories(t *testing.T) {
	store, err := NewSQLiteMemoryStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	defer store.Close()
	writer, err := NewTranscriptMemoryWriter(TranscriptMemoryWriterOptions{
		Repository:  store,
		OwnerUserID: "owner",
	})
	if err != nil {
		t.Fatalf("NewTranscriptMemoryWriter() error = %v", err)
	}

	result, err := writer.WriteMemories(context.Background(), session.MemoryWriteRequest{
		SessionID:  "sess_memory",
		DeviceID:   "stackchan-s3-main",
		Generation: 1,
		Transcript: "叫我阿豪，我喜欢可打断的语音交互。",
		CreatedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("WriteMemories() error = %v", err)
	}
	if result.WrittenCount != 2 {
		t.Fatalf("WrittenCount = %d, want 2", result.WrittenCount)
	}

	memories, err := store.Retrieve(context.Background(), MemoryQuery{
		UserID:   "owner",
		DeviceID: "stackchan-s3-main",
		Limit:    5,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	contents := memoryContents(memories)
	if !strings.Contains(contents, "用户偏好的称呼是阿豪。") || !strings.Contains(contents, "用户喜欢可打断的语音交互。") {
		t.Fatalf("retrieved contents = %s, want extracted memories", contents)
	}
}

func memoryContents(memories []Memory) string {
	var values []string
	for _, memory := range memories {
		values = append(values, memory.Content)
	}
	return strings.Join(values, "\n")
}
