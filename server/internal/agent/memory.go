package agent

import (
	"context"
	"sort"
	"sync"
	"time"
)

const (
	MemoryUserProfile       = "user_profile"
	MemoryRelationshipState = "relationship_state"
	MemoryEpisodic          = "episodic_memory"
	MemoryLorebook          = "lorebook"
)

type Memory struct {
	ID           string
	UserID       string
	DeviceID     string
	SessionID    string
	Type         string
	Content      string
	Importance   int
	Confidence   float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastUsedAt   time.Time
	MetadataJSON string
}

type MemoryQuery struct {
	UserID    string
	DeviceID  string
	SessionID string
	Query     string
	Limit     int
}

type MemoryStore interface {
	Retrieve(ctx context.Context, query MemoryQuery) ([]Memory, error)
}

type MemoryRepository interface {
	MemoryStore
	Upsert(ctx context.Context, memory Memory) (Memory, error)
	Close() error
}

type MemoryAdminRepository interface {
	MemoryRepository
	Delete(ctx context.Context, id string) (bool, error)
}

type StaticMemoryStore struct {
	mu       sync.RWMutex
	memories []Memory
}

func NewStaticMemoryStore(memories []Memory) *StaticMemoryStore {
	store := &StaticMemoryStore{}
	store.Replace(memories)
	return store
}

func (s *StaticMemoryStore) Replace(memories []Memory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = cloneMemories(memories)
}

func (s *StaticMemoryStore) Retrieve(_ context.Context, query MemoryQuery) ([]Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	matches := make([]Memory, 0, len(s.memories))
	for _, memory := range s.memories {
		if memory.DeviceID != "" && query.DeviceID != "" && memory.DeviceID != query.DeviceID {
			continue
		}
		if memory.UserID != "" && query.UserID != "" && memory.UserID != query.UserID {
			continue
		}
		matches = append(matches, memory)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Importance != matches[j].Importance {
			return matches[i].Importance > matches[j].Importance
		}
		return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return cloneMemories(matches), nil
}

func cloneMemories(memories []Memory) []Memory {
	clone := make([]Memory, len(memories))
	copy(clone, memories)
	return clone
}
