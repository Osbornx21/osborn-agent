package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	"stackchan-gateway/internal/session"
)

const (
	defaultRecentTurnStoreLimit = 32
	maxRecentUserRunes          = 160
	maxRecentAssistantRunes     = 240
)

type RecentTurn struct {
	SessionID     string
	DeviceID      string
	Generation    int64
	UserText      string
	AssistantText string
	CreatedAt     time.Time
}

type RecentTurnReader interface {
	RecentTurns(ctx context.Context, deviceID string, limit int) ([]RecentTurn, error)
}

type InMemoryRecentTurnStore struct {
	mu           sync.RWMutex
	maxPerDevice int
	turns        map[string][]RecentTurn
}

func NewInMemoryRecentTurnStore(maxPerDevice int) *InMemoryRecentTurnStore {
	if maxPerDevice <= 0 {
		maxPerDevice = defaultRecentTurnStoreLimit
	}
	return &InMemoryRecentTurnStore{
		maxPerDevice: maxPerDevice,
		turns:        make(map[string][]RecentTurn),
	}
}

func (s *InMemoryRecentTurnStore) RecordConversationTurn(ctx context.Context, request session.ConversationTurnRecordRequest) error {
	if s == nil {
		return nil
	}
	return s.AppendRecentTurn(ctx, RecentTurn{
		SessionID:     request.SessionID,
		DeviceID:      request.DeviceID,
		Generation:    request.Generation,
		UserText:      request.UserText,
		AssistantText: request.AssistantText,
		CreatedAt:     request.CreatedAt,
	})
}

func (s *InMemoryRecentTurnStore) AppendRecentTurn(ctx context.Context, turn RecentTurn) error {
	if s == nil {
		return nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	deviceID := strings.TrimSpace(turn.DeviceID)
	userText := compactRecentText(turn.UserText, maxRecentUserRunes)
	assistantText := compactRecentText(turn.AssistantText, maxRecentAssistantRunes)
	if deviceID == "" || userText == "" || assistantText == "" {
		return nil
	}
	createdAt := turn.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	normalized := RecentTurn{
		SessionID:     strings.TrimSpace(turn.SessionID),
		DeviceID:      deviceID,
		Generation:    turn.Generation,
		UserText:      userText,
		AssistantText: assistantText,
		CreatedAt:     createdAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	turns := append(s.turns[deviceID], normalized)
	if len(turns) > s.maxPerDevice {
		turns = append([]RecentTurn(nil), turns[len(turns)-s.maxPerDevice:]...)
	}
	s.turns[deviceID] = turns
	return nil
}

func (s *InMemoryRecentTurnStore) RecentTurns(ctx context.Context, deviceID string, limit int) ([]RecentTurn, error) {
	if s == nil || limit <= 0 {
		return nil, nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	turns := s.turns[deviceID]
	if len(turns) == 0 {
		return nil, nil
	}
	start := 0
	if len(turns) > limit {
		start = len(turns) - limit
	}
	out := make([]RecentTurn, len(turns[start:]))
	copy(out, turns[start:])
	return out, nil
}

func compactRecentText(text string, maxRunes int) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if text == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
