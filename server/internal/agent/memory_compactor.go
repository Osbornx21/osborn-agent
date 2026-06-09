package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	defaultCompactionSourceLimit = 50
	defaultCompactionFactLimit   = 8
)

type MemoryCompactionRequest struct {
	UserID   string
	DeviceID string
	MaxFacts int
}

type MemoryCompactionResult struct {
	Summary     Memory
	SourceCount int
	Upserted    bool
}

type MemoryCompactor struct {
	repository  agentMemoryCompactionRepository
	ownerUserID string
}

type agentMemoryCompactionRepository interface {
	MemoryStore
	Upsert(ctx context.Context, memory Memory) (Memory, error)
}

func NewMemoryCompactor(repository agentMemoryCompactionRepository, ownerUserID string) *MemoryCompactor {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		ownerUserID = "owner"
	}
	return &MemoryCompactor{
		repository:  repository,
		ownerUserID: ownerUserID,
	}
}

func (c *MemoryCompactor) Compact(ctx context.Context, request MemoryCompactionRequest) (MemoryCompactionResult, error) {
	if c == nil || c.repository == nil {
		return MemoryCompactionResult{}, fmt.Errorf("memory compactor is not configured")
	}
	userID := strings.TrimSpace(request.UserID)
	if userID == "" {
		userID = c.ownerUserID
	}
	maxFacts := request.MaxFacts
	if maxFacts <= 0 {
		maxFacts = defaultCompactionFactLimit
	}
	if maxFacts > defaultCompactionFactLimit {
		maxFacts = defaultCompactionFactLimit
	}
	memories, err := c.repository.Retrieve(ctx, MemoryQuery{
		UserID:   userID,
		DeviceID: strings.TrimSpace(request.DeviceID),
		Limit:    defaultCompactionSourceLimit,
	})
	if err != nil {
		return MemoryCompactionResult{}, err
	}
	facts := compactableFacts(memories, maxFacts)
	if len(facts) == 0 {
		return MemoryCompactionResult{SourceCount: 0}, nil
	}
	now := time.Now()
	summary := Memory{
		ID:           compactedProfileMemoryID(userID, strings.TrimSpace(request.DeviceID)),
		UserID:       userID,
		DeviceID:     strings.TrimSpace(request.DeviceID),
		Type:         MemoryRelationshipState,
		Content:      renderCompactedProfileSummary(facts),
		Importance:   5,
		Confidence:   0.9,
		CreatedAt:    now,
		UpdatedAt:    now,
		MetadataJSON: fmt.Sprintf(`{"source":"memory_compaction","kind":"profile_summary","source_count":%d}`, len(facts)),
	}
	saved, err := c.repository.Upsert(ctx, summary)
	if err != nil {
		return MemoryCompactionResult{}, err
	}
	return MemoryCompactionResult{
		Summary:     saved,
		SourceCount: len(facts),
		Upserted:    true,
	}, nil
}

func compactableFacts(memories []Memory, limit int) []string {
	seen := make(map[string]bool, len(memories))
	facts := make([]string, 0, limit)
	for _, memory := range memories {
		if memory.Type != MemoryUserProfile {
			continue
		}
		if isCompactedMemory(memory) {
			continue
		}
		fact := normalizeCompactedFact(memory.Content)
		if fact == "" || seen[fact] {
			continue
		}
		seen[fact] = true
		facts = append(facts, fact)
		if len(facts) >= limit {
			break
		}
	}
	return facts
}

func isCompactedMemory(memory Memory) bool {
	return strings.Contains(memory.MetadataJSON, `"source":"memory_compaction"`) ||
		strings.Contains(memory.Content, "用户画像摘要")
}

func normalizeCompactedFact(content string) string {
	content = strings.TrimSpace(content)
	content = strings.Trim(content, "。；; \t\r\n")
	if content == "" || strings.ContainsAny(content, "\n\r") {
		return ""
	}
	if len([]rune(content)) > 80 {
		return ""
	}
	return content
}

func renderCompactedProfileSummary(facts []string) string {
	return "用户画像摘要：" + strings.Join(facts, "；") + "。"
}

func compactedProfileMemoryID(userID string, deviceID string) string {
	sum := sha256.Sum256([]byte("profile_summary\x00" + userID + "\x00" + deviceID))
	return "mem_profile_summary_" + hex.EncodeToString(sum[:8])
}
