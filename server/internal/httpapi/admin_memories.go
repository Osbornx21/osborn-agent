package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"stackchan-gateway/internal/agent"
)

const (
	defaultAdminMemoryLimit = 20
	maxAdminMemoryLimit     = 50
	defaultAdminRecentLimit = 8
	maxAdminRecentLimit     = 20
)

type memoryUpsertRequestBody struct {
	UserID       string  `json:"user_id"`
	DeviceID     string  `json:"device_id"`
	SessionID    string  `json:"session_id"`
	Type         string  `json:"type"`
	Content      string  `json:"content"`
	Importance   int     `json:"importance"`
	Confidence   float64 `json:"confidence"`
	MetadataJSON string  `json:"metadata_json"`
}

type memoryCompactRequestBody struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
	MaxFacts int    `json:"max_facts"`
}

type memoryListResponse struct {
	Memories []memoryResponse `json:"memories"`
	Count    int              `json:"count"`
}

type memoryResponse struct {
	ID           string  `json:"id"`
	UserID       string  `json:"user_id,omitempty"`
	DeviceID     string  `json:"device_id,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	Type         string  `json:"type"`
	Content      string  `json:"content"`
	Importance   int     `json:"importance"`
	Confidence   float64 `json:"confidence"`
	CreatedAt    string  `json:"created_at,omitempty"`
	UpdatedAt    string  `json:"updated_at,omitempty"`
	LastUsedAt   string  `json:"last_used_at,omitempty"`
	MetadataJSON string  `json:"metadata_json,omitempty"`
}

type memoryDeleteResponse struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

type memoryCompactResponse struct {
	Summary     *memoryResponse `json:"summary,omitempty"`
	SourceCount int             `json:"source_count"`
	Upserted    bool            `json:"upserted"`
}

type recentTurnsListResponse struct {
	DeviceID    string               `json:"device_id"`
	RecentTurns []recentTurnResponse `json:"recent_turns"`
	Count       int                  `json:"count"`
}

type recentTurnResponse struct {
	SessionID     string `json:"session_id,omitempty"`
	DeviceID      string `json:"device_id,omitempty"`
	Generation    int64  `json:"generation,omitempty"`
	UserText      string `json:"user_text"`
	AssistantText string `json:"assistant_text"`
	CreatedAt     string `json:"created_at,omitempty"`
}

func memoriesListHandler(repository agent.MemoryAdminRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repository == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "MEMORY_REPOSITORY_NOT_CONFIGURED", "memory repository is not configured")
			return
		}
		limit, ok := parseAdminMemoryLimit(r.URL.Query().Get("limit"))
		if !ok {
			writeAPIError(w, http.StatusBadRequest, "INVALID_MEMORY_LIMIT", "limit must be between 1 and 50")
			return
		}
		memories, err := repository.Retrieve(r.Context(), agent.MemoryQuery{
			UserID:   strings.TrimSpace(r.URL.Query().Get("user_id")),
			DeviceID: strings.TrimSpace(r.URL.Query().Get("device_id")),
			Limit:    limit,
		})
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "MEMORY_REPOSITORY_FAILED", "memory repository operation failed")
			return
		}
		response := memoryListResponse{
			Memories: memoryResponses(memories),
			Count:    len(memories),
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func recentTurnsListHandler(reader agent.RecentTurnReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "RECENT_TURNS_REPOSITORY_NOT_CONFIGURED", "recent turns repository is not configured")
			return
		}
		deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
		if deviceID == "" {
			writeAPIError(w, http.StatusBadRequest, "INVALID_RECENT_TURNS_DEVICE_ID", "device_id is required")
			return
		}
		limit, ok := parseAdminRecentTurnsLimit(r.URL.Query().Get("limit"))
		if !ok {
			writeAPIError(w, http.StatusBadRequest, "INVALID_RECENT_TURNS_LIMIT", "limit must be between 1 and 20")
			return
		}
		turns, err := reader.RecentTurns(r.Context(), deviceID, limit)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "RECENT_TURNS_REPOSITORY_FAILED", "recent turns repository operation failed")
			return
		}
		response := recentTurnsListResponse{
			DeviceID:    deviceID,
			RecentTurns: recentTurnResponses(turns),
			Count:       len(turns),
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func memoryPutHandler(repository agent.MemoryAdminRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repository == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "MEMORY_REPOSITORY_NOT_CONFIGURED", "memory repository is not configured")
			return
		}
		memoryID := strings.TrimSpace(chi.URLParam(r, "memory_id"))
		if memoryID == "" {
			writeAPIError(w, http.StatusBadRequest, "INVALID_MEMORY_ID", "memory id is required")
			return
		}
		var body memoryUpsertRequestBody
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid memory JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
			return
		}
		memory := agent.Memory{
			ID:           memoryID,
			UserID:       body.UserID,
			DeviceID:     body.DeviceID,
			SessionID:    body.SessionID,
			Type:         body.Type,
			Content:      body.Content,
			Importance:   body.Importance,
			Confidence:   body.Confidence,
			MetadataJSON: body.MetadataJSON,
		}
		saved, err := repository.Upsert(r.Context(), memory)
		if err != nil {
			writeMemoryValidationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, memoryResponseFromMemory(saved))
	}
}

func memoryCompactHandler(repository agent.MemoryAdminRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repository == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "MEMORY_REPOSITORY_NOT_CONFIGURED", "memory repository is not configured")
			return
		}
		var body memoryCompactRequestBody
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid memory compaction JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
			return
		}
		if body.MaxFacts < 0 || body.MaxFacts > 8 {
			writeAPIError(w, http.StatusBadRequest, "INVALID_MEMORY_COMPACTION_LIMIT", "max_facts must be between 0 and 8")
			return
		}
		compactor := agent.NewMemoryCompactor(repository, "owner")
		result, err := compactor.Compact(r.Context(), agent.MemoryCompactionRequest{
			UserID:   body.UserID,
			DeviceID: body.DeviceID,
			MaxFacts: body.MaxFacts,
		})
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "MEMORY_COMPACTION_FAILED", "memory compaction failed")
			return
		}
		response := memoryCompactResponse{
			SourceCount: result.SourceCount,
			Upserted:    result.Upserted,
		}
		if result.Upserted {
			summary := memoryResponseFromMemory(result.Summary)
			response.Summary = &summary
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func memoryDeleteHandler(repository agent.MemoryAdminRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repository == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "MEMORY_REPOSITORY_NOT_CONFIGURED", "memory repository is not configured")
			return
		}
		memoryID := strings.TrimSpace(chi.URLParam(r, "memory_id"))
		if memoryID == "" {
			writeAPIError(w, http.StatusBadRequest, "INVALID_MEMORY_ID", "memory id is required")
			return
		}
		deleted, err := repository.Delete(r.Context(), memoryID)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "MEMORY_REPOSITORY_FAILED", "memory repository operation failed")
			return
		}
		if !deleted {
			writeAPIError(w, http.StatusNotFound, "MEMORY_NOT_FOUND", "memory not found")
			return
		}
		writeJSON(w, http.StatusOK, memoryDeleteResponse{Deleted: true, ID: memoryID})
	}
}

func parseAdminMemoryLimit(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultAdminMemoryLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > maxAdminMemoryLimit {
		return 0, false
	}
	return limit, true
}

func parseAdminRecentTurnsLimit(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultAdminRecentLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > maxAdminRecentLimit {
		return 0, false
	}
	return limit, true
}

func writeMemoryValidationError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, "content is required"):
		writeAPIError(w, http.StatusBadRequest, "INVALID_MEMORY_CONTENT", "memory content is required")
	case strings.Contains(message, "unsupported memory type"):
		writeAPIError(w, http.StatusBadRequest, "INVALID_MEMORY_TYPE", "memory type is unsupported")
	case strings.Contains(message, "memory id is required"):
		writeAPIError(w, http.StatusBadRequest, "INVALID_MEMORY_ID", "memory id is required")
	default:
		writeAPIError(w, http.StatusBadGateway, "MEMORY_REPOSITORY_FAILED", "memory repository operation failed")
	}
}

func recentTurnResponses(turns []agent.RecentTurn) []recentTurnResponse {
	responses := make([]recentTurnResponse, len(turns))
	for i, turn := range turns {
		responses[i] = recentTurnResponse{
			SessionID:     turn.SessionID,
			DeviceID:      turn.DeviceID,
			Generation:    turn.Generation,
			UserText:      turn.UserText,
			AssistantText: turn.AssistantText,
			CreatedAt:     formatAdminMemoryTime(turn.CreatedAt),
		}
	}
	return responses
}

func memoryResponses(memories []agent.Memory) []memoryResponse {
	responses := make([]memoryResponse, len(memories))
	for i, memory := range memories {
		responses[i] = memoryResponseFromMemory(memory)
	}
	return responses
}

func memoryResponseFromMemory(memory agent.Memory) memoryResponse {
	return memoryResponse{
		ID:           memory.ID,
		UserID:       memory.UserID,
		DeviceID:     memory.DeviceID,
		SessionID:    memory.SessionID,
		Type:         memory.Type,
		Content:      memory.Content,
		Importance:   memory.Importance,
		Confidence:   memory.Confidence,
		CreatedAt:    formatAdminMemoryTime(memory.CreatedAt),
		UpdatedAt:    formatAdminMemoryTime(memory.UpdatedAt),
		LastUsedAt:   formatAdminMemoryTime(memory.LastUsedAt),
		MetadataJSON: memory.MetadataJSON,
	}
}

func formatAdminMemoryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
