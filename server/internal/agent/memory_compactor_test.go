package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryCompactorWritesProfileSummary(t *testing.T) {
	store, err := NewSQLiteMemoryStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	defer store.Close()

	for _, memory := range []Memory{
		{ID: "name", UserID: "owner", DeviceID: "stackchan-s3-main", Type: MemoryUserProfile, Content: "用户偏好的称呼是阿豪。", Importance: 5},
		{ID: "positive", UserID: "owner", DeviceID: "stackchan-s3-main", Type: MemoryUserProfile, Content: "用户喜欢低延迟语音。", Importance: 4},
		{ID: "negative", UserID: "owner", DeviceID: "stackchan-s3-main", Type: MemoryUserProfile, Content: "用户不喜欢长篇大论。", Importance: 4},
		{ID: "other-device", UserID: "owner", DeviceID: "other-device", Type: MemoryUserProfile, Content: "不应进入摘要。", Importance: 5},
	} {
		if _, err := store.Upsert(context.Background(), memory); err != nil {
			t.Fatalf("Upsert(%s) error = %v", memory.ID, err)
		}
	}

	compactor := NewMemoryCompactor(store, "owner")
	result, err := compactor.Compact(context.Background(), MemoryCompactionRequest{
		DeviceID: "stackchan-s3-main",
		MaxFacts: 3,
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !result.Upserted || result.SourceCount != 3 {
		t.Fatalf("result = %+v, want upserted summary from 3 facts", result)
	}
	if result.Summary.Type != MemoryRelationshipState || result.Summary.Importance != 5 {
		t.Fatalf("summary = %+v, want relationship_state importance 5", result.Summary)
	}
	for _, want := range []string{"用户画像摘要：", "用户偏好的称呼是阿豪", "用户喜欢低延迟语音", "用户不喜欢长篇大论"} {
		if !strings.Contains(result.Summary.Content, want) {
			t.Fatalf("summary content missing %q: %s", want, result.Summary.Content)
		}
	}
	if strings.Contains(result.Summary.Content, "不应进入摘要") {
		t.Fatalf("summary included other-device memory: %s", result.Summary.Content)
	}
	if strings.Contains(result.Summary.MetadataJSON, "阿豪") || strings.Contains(result.Summary.MetadataJSON, "低延迟") {
		t.Fatalf("summary metadata leaked content: %s", result.Summary.MetadataJSON)
	}

	afterFirst, err := store.Retrieve(context.Background(), MemoryQuery{
		UserID:   "owner",
		DeviceID: "stackchan-s3-main",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Retrieve(after first compact) error = %v", err)
	}
	assertMemoryIDs(t, afterFirst, []string{"name", "positive", "negative", result.Summary.ID})

	second, err := compactor.Compact(context.Background(), MemoryCompactionRequest{DeviceID: "stackchan-s3-main"})
	if err != nil {
		t.Fatalf("Compact(second) error = %v", err)
	}
	if second.Summary.ID != result.Summary.ID {
		t.Fatalf("summary id changed: first=%s second=%s", result.Summary.ID, second.Summary.ID)
	}
	afterSecond, err := store.Retrieve(context.Background(), MemoryQuery{
		UserID:   "owner",
		DeviceID: "stackchan-s3-main",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Retrieve(after second compact) error = %v", err)
	}
	if len(afterSecond) != len(afterFirst) {
		t.Fatalf("second compact changed memory count: before=%d after=%d", len(afterFirst), len(afterSecond))
	}
}

func TestMemoryCompactorNoopsWithoutProfileFacts(t *testing.T) {
	store, err := NewSQLiteMemoryStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	defer store.Close()

	compactor := NewMemoryCompactor(store, "owner")
	result, err := compactor.Compact(context.Background(), MemoryCompactionRequest{DeviceID: "stackchan-s3-main"})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if result.Upserted || result.SourceCount != 0 {
		t.Fatalf("result = %+v, want no-op", result)
	}
}

func assertMemoryIDs(t *testing.T, memories []Memory, want []string) {
	t.Helper()
	seen := make(map[string]bool, len(memories))
	for _, memory := range memories {
		seen[memory.ID] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("memory id %q missing from %+v", id, memories)
		}
	}
}
