package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	servicetools "stackchan-gateway/internal/tools"
)

const (
	MemoryLookupToolName       = "memory.lookup"
	defaultMemoryLookupLimit   = 5
	maxMemoryLookupLimit       = 8
	memoryLookupToolSchemaType = "object"
)

type MemoryLookupToolOptions struct {
	Store        MemoryStore
	OwnerUserID  string
	DefaultLimit int
	MaxLimit     int
}

type memoryLookupPayload struct {
	Memories []memoryLookupItem `json:"memories"`
	Count    int                `json:"count"`
}

type memoryLookupItem struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Content    string  `json:"content"`
	Importance int     `json:"importance"`
	Confidence float64 `json:"confidence"`
}

func RegisterMemoryLookupTool(registry *servicetools.Registry, options MemoryLookupToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	definition, executor, err := NewMemoryLookupTool(options)
	if err != nil {
		return err
	}
	return registry.Register(definition, executor)
}

func NewMemoryLookupTool(options MemoryLookupToolOptions) (servicetools.Definition, servicetools.Executor, error) {
	if options.Store == nil {
		return servicetools.Definition{}, nil, fmt.Errorf("memory store is required")
	}
	ownerUserID := strings.TrimSpace(options.OwnerUserID)
	if ownerUserID == "" {
		ownerUserID = "owner"
	}
	defaultLimit := normalizeMemoryLookupLimit(options.DefaultLimit, defaultMemoryLookupLimit, maxMemoryLookupLimit)
	maxLimit := options.MaxLimit
	if maxLimit <= 0 || maxLimit > maxMemoryLookupLimit {
		maxLimit = maxMemoryLookupLimit
	}
	if defaultLimit > maxLimit {
		defaultLimit = maxLimit
	}

	definition := servicetools.Definition{
		Name:        MemoryLookupToolName,
		Description: "Look up scoped user memories for the current voice turn.",
		Permission:  servicetools.PermissionRead,
		InputSchema: map[string]any{
			"type": memoryLookupToolSchemaType,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Short lookup hint from the current user request.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     maxLimit,
					"description": "Maximum number of memories to return.",
				},
			},
			"additionalProperties": false,
		},
	}
	executor := func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		deviceID := strings.TrimSpace(call.DeviceID)
		if deviceID == "" {
			return servicetools.Result{}, fmt.Errorf("memory lookup requires device id")
		}
		limit := memoryLookupArgumentLimit(call.Arguments, defaultLimit, maxLimit)
		memories, err := options.Store.Retrieve(ctx, MemoryQuery{
			UserID:   ownerUserID,
			DeviceID: deviceID,
			Query:    stringMemoryLookupArgument(call.Arguments, "query"),
			Limit:    limit,
		})
		if err != nil {
			return servicetools.Result{}, err
		}
		payload := memoryLookupPayload{
			Memories: make([]memoryLookupItem, 0, len(memories)),
			Count:    len(memories),
		}
		for _, memory := range memories {
			payload.Memories = append(payload.Memories, memoryLookupItem{
				ID:         memory.ID,
				Type:       memory.Type,
				Content:    memory.Content,
				Importance: memory.Importance,
				Confidence: memory.Confidence,
			})
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{
			Payload:     raw,
			SafeSummary: fmt.Sprintf("memory_count=%d", payload.Count),
		}, nil
	}
	return definition, executor, nil
}

func memoryLookupArgumentLimit(arguments map[string]any, defaultLimit int, maxLimit int) int {
	return normalizeMemoryLookupLimit(intMemoryLookupArgument(arguments, "limit"), defaultLimit, maxLimit)
}

func normalizeMemoryLookupLimit(limit int, defaultLimit int, maxLimit int) int {
	if defaultLimit <= 0 {
		defaultLimit = defaultMemoryLookupLimit
	}
	if maxLimit <= 0 {
		maxLimit = maxMemoryLookupLimit
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func stringMemoryLookupArgument(arguments map[string]any, key string) string {
	if arguments == nil {
		return ""
	}
	value, ok := arguments[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func intMemoryLookupArgument(arguments map[string]any, key string) int {
	if arguments == nil {
		return 0
	}
	switch value := arguments[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return 0
}
