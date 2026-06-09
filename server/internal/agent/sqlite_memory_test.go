package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stackchan-gateway/internal/session"
)

func TestSQLiteMemoryStoreUpsertRetrieveAndTouch(t *testing.T) {
	store, err := NewSQLiteMemoryStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	defer store.Close()

	older := time.Now().Add(-time.Hour)
	if _, err := store.Upsert(context.Background(), Memory{
		ID:         "low",
		UserID:     "owner",
		DeviceID:   "stackchan-s3-main",
		Type:       MemoryEpisodic,
		Content:    "低优先级记忆",
		Importance: 1,
		Confidence: 0.7,
		CreatedAt:  older,
		UpdatedAt:  older,
	}); err != nil {
		t.Fatalf("Upsert(low) error = %v", err)
	}
	if _, err := store.Upsert(context.Background(), Memory{
		ID:         "name",
		UserID:     "owner",
		DeviceID:   "stackchan-s3-main",
		Type:       MemoryUserProfile,
		Content:    "用户偏好的称呼是阿豪。",
		Importance: 5,
		Confidence: 1,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Upsert(name) error = %v", err)
	}
	if _, err := store.Upsert(context.Background(), Memory{
		ID:         "other",
		UserID:     "owner",
		DeviceID:   "other-device",
		Type:       MemoryUserProfile,
		Content:    "不应出现",
		Importance: 5,
	}); err != nil {
		t.Fatalf("Upsert(other) error = %v", err)
	}

	memories, err := store.Retrieve(context.Background(), MemoryQuery{
		UserID:   "owner",
		DeviceID: "stackchan-s3-main",
		Limit:    2,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("memories len = %d, want 2: %+v", len(memories), memories)
	}
	if memories[0].ID != "name" || memories[0].Content != "用户偏好的称呼是阿豪。" {
		t.Fatalf("first memory = %+v, want high-importance name memory", memories[0])
	}
	for _, memory := range memories {
		if memory.Content == "不应出现" {
			t.Fatalf("retrieved other-device memory: %+v", memories)
		}
	}

	memories, err = store.Retrieve(context.Background(), MemoryQuery{UserID: "owner", DeviceID: "stackchan-s3-main", Limit: 1})
	if err != nil {
		t.Fatalf("Retrieve(second) error = %v", err)
	}
	if len(memories) != 1 || memories[0].LastUsedAt.IsZero() {
		t.Fatalf("last_used_at was not refreshed: %+v", memories)
	}
}

func TestSQLiteMemoryStoreRejectsInvalidMemory(t *testing.T) {
	store, err := NewSQLiteMemoryStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	defer store.Close()

	if _, err := store.Upsert(context.Background(), Memory{Type: MemoryUserProfile}); err == nil {
		t.Fatal("Upsert() error = nil, want invalid memory error")
	}
	if _, err := store.Upsert(context.Background(), Memory{Type: "unknown", Content: "x"}); err == nil {
		t.Fatal("Upsert() error = nil, want unsupported type error")
	}
}

func TestSQLiteMemoryStoreDeletesMemory(t *testing.T) {
	store, err := NewSQLiteMemoryStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	defer store.Close()

	if _, err := store.Upsert(context.Background(), Memory{
		ID:      "delete-me",
		Type:    MemoryUserProfile,
		Content: "用户喜欢可控的记忆管理。",
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	deleted, err := store.Delete(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Fatal("Delete() deleted = false, want true")
	}
	deleted, err = store.Delete(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("Delete(second) error = %v", err)
	}
	if deleted {
		t.Fatal("Delete(second) deleted = true, want false")
	}
	memories, err := store.Retrieve(context.Background(), MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("memories after delete = %+v, want empty", memories)
	}
}

func TestSQLiteMemoryStorePersistsRecentTurnsWithoutMemoryPollution(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewSQLiteMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	store.SetRecentTurnLimit(3)
	for index, text := range []string{"一", "二", "三", "四"} {
		if err := store.RecordConversationTurn(context.Background(), session.ConversationTurnRecordRequest{
			SessionID:     "sess",
			DeviceID:      "stackchan-s3-main",
			Generation:    int64(index + 1),
			UserText:      "用户" + text,
			AssistantText: "回答" + text,
			CreatedAt:     time.Now().Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatalf("RecordConversationTurn(%d) error = %v", index, err)
		}
	}
	if err := store.RecordConversationTurn(context.Background(), session.ConversationTurnRecordRequest{
		DeviceID:      "other-device",
		UserText:      "其他用户",
		AssistantText: "其他回答",
	}); err != nil {
		t.Fatalf("RecordConversationTurn(other) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewSQLiteMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("reopen NewSQLiteMemoryStore() error = %v", err)
	}
	defer reopened.Close()

	turns, err := reopened.RecentTurns(context.Background(), "stackchan-s3-main", 8)
	if err != nil {
		t.Fatalf("RecentTurns() error = %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("turns len = %d, want bounded 3: %+v", len(turns), turns)
	}
	if turns[0].UserText != "用户二" || turns[2].AssistantText != "回答四" {
		t.Fatalf("turns = %+v, want persisted oldest retained through newest", turns)
	}
	for _, turn := range turns {
		if turn.DeviceID != "stackchan-s3-main" {
			t.Fatalf("turn = %+v, want current device only", turn)
		}
	}

	memories, err := reopened.Retrieve(context.Background(), MemoryQuery{DeviceID: "stackchan-s3-main", Limit: 10})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("memories = %+v, want recent turns outside long-term memory table", memories)
	}
}

func TestSQLiteMemoryStoreBoundsRecentTurnText(t *testing.T) {
	store, err := NewSQLiteMemoryStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore() error = %v", err)
	}
	defer store.Close()

	if err := store.RecordConversationTurn(context.Background(), session.ConversationTurnRecordRequest{
		DeviceID:      "stackchan-s3-main",
		UserText:      strings.Repeat("我", maxRecentUserRunes+12),
		AssistantText: " 好的，继续。 ",
	}); err != nil {
		t.Fatalf("RecordConversationTurn() error = %v", err)
	}
	turns, err := store.RecentTurns(context.Background(), "stackchan-s3-main", 1)
	if err != nil {
		t.Fatalf("RecentTurns() error = %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns len = %d, want 1", len(turns))
	}
	if len([]rune(turns[0].UserText)) != maxRecentUserRunes || !strings.HasSuffix(turns[0].UserText, "...") {
		t.Fatalf("user text = %q, want bounded ellipsis", turns[0].UserText)
	}
	if turns[0].AssistantText != "好的，继续。" {
		t.Fatalf("assistant text = %q, want compact assistant text", turns[0].AssistantText)
	}
}
