package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"stackchan-gateway/internal/session"
)

type TranscriptMemoryWriterOptions struct {
	Repository  MemoryRepository
	OwnerUserID string
	Extractor   *DeterministicMemoryExtractor
}

type TranscriptMemoryWriter struct {
	repository  MemoryRepository
	ownerUserID string
	extractor   *DeterministicMemoryExtractor
}

func NewTranscriptMemoryWriter(options TranscriptMemoryWriterOptions) (*TranscriptMemoryWriter, error) {
	if options.Repository == nil {
		return nil, fmt.Errorf("memory repository is required")
	}
	ownerUserID := strings.TrimSpace(options.OwnerUserID)
	if ownerUserID == "" {
		ownerUserID = "owner"
	}
	extractor := options.Extractor
	if extractor == nil {
		extractor = NewDeterministicMemoryExtractor()
	}
	return &TranscriptMemoryWriter{
		repository:  options.Repository,
		ownerUserID: ownerUserID,
		extractor:   extractor,
	}, nil
}

func (w *TranscriptMemoryWriter) WriteMemories(ctx context.Context, request session.MemoryWriteRequest) (session.MemoryWriteResult, error) {
	if w == nil || w.repository == nil {
		return session.MemoryWriteResult{}, fmt.Errorf("memory writer is not configured")
	}
	memories := w.extractor.Extract(DeterministicMemoryRequest{
		UserID:     w.ownerUserID,
		DeviceID:   request.DeviceID,
		SessionID:  request.SessionID,
		Transcript: request.Transcript,
		CreatedAt:  request.CreatedAt,
	})
	written := 0
	for _, memory := range memories {
		if _, err := w.repository.Upsert(ctx, memory); err != nil {
			return session.MemoryWriteResult{WrittenCount: written}, err
		}
		written++
	}
	return session.MemoryWriteResult{WrittenCount: written}, nil
}

type DeterministicMemoryRequest struct {
	UserID     string
	DeviceID   string
	SessionID  string
	Transcript string
	CreatedAt  time.Time
}

type DeterministicMemoryExtractor struct {
	namePatterns       []*regexp.Regexp
	positivePatterns   []*regexp.Regexp
	negativePatterns   []*regexp.Regexp
	maxMemoriesPerTurn int
}

func NewDeterministicMemoryExtractor() *DeterministicMemoryExtractor {
	return &DeterministicMemoryExtractor{
		namePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?:我的名字叫|你可以叫我|以后叫我|叫我|我叫)([^，。！？,.!?\s]{1,16})`),
		},
		positivePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?:我更喜欢|我喜欢|我爱|我偏好)([^，。！？,.!?]{1,40})`),
		},
		negativePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?:我不喜欢|我讨厌)([^，。！？,.!?]{1,40})`),
		},
		maxMemoriesPerTurn: 4,
	}
}

func (e *DeterministicMemoryExtractor) Extract(request DeterministicMemoryRequest) []Memory {
	if e == nil {
		e = NewDeterministicMemoryExtractor()
	}
	transcript := strings.TrimSpace(request.Transcript)
	if transcript == "" {
		return nil
	}
	createdAt := request.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	var memories []Memory
	for _, value := range extractPatternValues(transcript, e.namePatterns) {
		name := trimMemoryValue(value)
		if !validShortMemoryValue(name, 16) {
			continue
		}
		memories = append(memories, Memory{
			ID:           stableMemoryID("user_name", request.UserID, request.DeviceID, ""),
			UserID:       request.UserID,
			DeviceID:     request.DeviceID,
			SessionID:    request.SessionID,
			Type:         MemoryUserProfile,
			Content:      "用户偏好的称呼是" + name + "。",
			Importance:   5,
			Confidence:   0.95,
			CreatedAt:    createdAt,
			UpdatedAt:    createdAt,
			MetadataJSON: `{"source":"deterministic_transcript","kind":"preferred_name"}`,
		})
	}
	for _, value := range extractPatternValues(transcript, e.positivePatterns) {
		preference := trimMemoryValue(value)
		if !validShortMemoryValue(preference, 40) {
			continue
		}
		memories = append(memories, Memory{
			ID:           stableMemoryID("user_pref_positive", request.UserID, request.DeviceID, preference),
			UserID:       request.UserID,
			DeviceID:     request.DeviceID,
			SessionID:    request.SessionID,
			Type:         MemoryUserProfile,
			Content:      "用户喜欢" + preference + "。",
			Importance:   4,
			Confidence:   0.85,
			CreatedAt:    createdAt,
			UpdatedAt:    createdAt,
			MetadataJSON: `{"source":"deterministic_transcript","kind":"positive_preference"}`,
		})
	}
	for _, value := range extractPatternValues(transcript, e.negativePatterns) {
		preference := trimMemoryValue(value)
		if !validShortMemoryValue(preference, 40) {
			continue
		}
		memories = append(memories, Memory{
			ID:           stableMemoryID("user_pref_negative", request.UserID, request.DeviceID, preference),
			UserID:       request.UserID,
			DeviceID:     request.DeviceID,
			SessionID:    request.SessionID,
			Type:         MemoryUserProfile,
			Content:      "用户不喜欢" + preference + "。",
			Importance:   4,
			Confidence:   0.85,
			CreatedAt:    createdAt,
			UpdatedAt:    createdAt,
			MetadataJSON: `{"source":"deterministic_transcript","kind":"negative_preference"}`,
		})
	}
	return limitUniqueMemories(memories, e.maxMemoriesPerTurn)
}

func extractPatternValues(text string, patterns []*regexp.Regexp) []string {
	var values []string
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if len(match) > 1 {
				values = append(values, match[1])
			}
		}
	}
	return values
}

func trimMemoryValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "「」『』“”\"'`，。！？,.!?；;：:、")
	return strings.TrimSpace(value)
}

func validShortMemoryValue(value string, maxRunes int) bool {
	runes := []rune(value)
	if len(runes) == 0 || len(runes) > maxRunes {
		return false
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return false
		}
	}
	return !strings.ContainsAny(value, "?？吗")
}

func stableMemoryID(kind string, userID string, deviceID string, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + userID + "\x00" + deviceID + "\x00" + value))
	return "mem_" + kind + "_" + hex.EncodeToString(sum[:8])
}

func limitUniqueMemories(memories []Memory, limit int) []Memory {
	if limit <= 0 {
		limit = 4
	}
	seen := make(map[string]bool, len(memories))
	out := make([]Memory, 0, len(memories))
	for _, memory := range memories {
		if seen[memory.ID] {
			continue
		}
		seen[memory.ID] = true
		out = append(out, memory)
		if len(out) >= limit {
			break
		}
	}
	return out
}
