package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"stackchan-gateway/internal/session"
)

func TestInMemoryRecentTurnStoreBoundsAndFiltersByDevice(t *testing.T) {
	store := NewInMemoryRecentTurnStore(3)
	for index, text := range []string{"一", "二", "三", "四"} {
		if err := store.AppendRecentTurn(context.Background(), RecentTurn{
			SessionID:     "sess",
			DeviceID:      "stackchan-s3-main",
			Generation:    int64(index + 1),
			UserText:      "用户" + text,
			AssistantText: "回答" + text,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("AppendRecentTurn(%d) error = %v", index, err)
		}
	}
	if err := store.AppendRecentTurn(context.Background(), RecentTurn{
		DeviceID:      "other-device",
		UserText:      "用户其他",
		AssistantText: "回答其他",
	}); err != nil {
		t.Fatalf("AppendRecentTurn(other) error = %v", err)
	}

	turns, err := store.RecentTurns(context.Background(), "stackchan-s3-main", 8)
	if err != nil {
		t.Fatalf("RecentTurns() error = %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("turns len = %d, want bounded 3: %+v", len(turns), turns)
	}
	if turns[0].UserText != "用户二" || turns[2].AssistantText != "回答四" {
		t.Fatalf("turns = %+v, want oldest retained through newest", turns)
	}
	for _, turn := range turns {
		if turn.DeviceID != "stackchan-s3-main" {
			t.Fatalf("turn = %+v, want only current device", turn)
		}
	}
}

func TestInMemoryRecentTurnStoreRecordsVoiceLoopRequests(t *testing.T) {
	store := NewInMemoryRecentTurnStore(4)
	err := store.RecordConversationTurn(context.Background(), session.ConversationTurnRecordRequest{
		SessionID:     "sess",
		DeviceID:      "stackchan-s3-main",
		Generation:    7,
		UserText:      strings.Repeat("我", maxRecentUserRunes+8),
		AssistantText: " 好的，继续。 ",
	})
	if err != nil {
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
		t.Fatalf("assistant text = %q, want trimmed compact text", turns[0].AssistantText)
	}
}
