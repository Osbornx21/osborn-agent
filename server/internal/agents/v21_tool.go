package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	servicetools "stackchan-gateway/internal/tools"
)

const V21VoiceQueryToolName = "v21.voice_query"

var (
	ErrMissingModeStore      = errors.New("agent mode store is required")
	ErrMissingAgentRouter    = errors.New("agent router is required")
	ErrMissingQuestion       = errors.New("v21 question is required")
	ErrCollectionNotAllowed  = errors.New("v21 collection is not allowlisted")
	ErrMissingCollectionList = errors.New("v21 allowed collection ids are required")
)

type V21VoiceQueryToolOptions struct {
	Router               *Router
	Modes                ModeReader
	AllowedCollectionIDs []string
}

type V21VoiceQueryToolPayload struct {
	Blocked       bool     `json:"blocked,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	QueryRunID    string   `json:"query_run_id,omitempty"`
	AnswerType    string   `json:"answer_type,omitempty"`
	SpokenAnswer  string   `json:"spoken_answer,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	CitationCount int      `json:"citation_count,omitempty"`
	EvidenceCount int      `json:"evidence_count,omitempty"`
	SourceLabels  []string `json:"source_labels,omitempty"`
}

func RegisterV21VoiceQueryTool(registry *servicetools.Registry, options V21VoiceQueryToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	if options.Router == nil {
		return ErrMissingAgentRouter
	}
	if options.Modes == nil {
		return ErrMissingModeStore
	}
	collections := nonEmptyUniqueStrings(options.AllowedCollectionIDs)
	if len(collections) == 0 {
		return ErrMissingCollectionList
	}
	allowedCollections := stringSet(collections)
	return registry.Register(servicetools.Definition{
		Name:        V21VoiceQueryToolName,
		Description: "Query the professional V21 knowledge bridge for a short cited spoken answer. Only works in professional mode.",
		Permission:  servicetools.PermissionExternal,
		InputSchema: v21VoiceQueryInputSchema(collections),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		question := stringToolArgument(call.Arguments, "question")
		if question == "" {
			return servicetools.Result{}, ErrMissingQuestion
		}
		collectionID := stringToolArgument(call.Arguments, "collection_id")
		if collectionID == "" && len(collections) == 1 {
			collectionID = collections[0]
		}
		if _, ok := allowedCollections[collectionID]; !ok {
			return servicetools.Result{}, ErrCollectionNotAllowed
		}
		status, err := options.Modes.GetDeviceMode(ctx, call.DeviceID)
		if err != nil {
			return servicetools.Result{}, err
		}
		response, err := options.Router.Route(ctx, RouteRequest{
			Mode:          status.ActiveMode,
			Destination:   DestinationV21,
			Text:          question,
			DeviceID:      call.DeviceID,
			SessionID:     call.SessionID,
			TurnID:        fmt.Sprintf("%d", call.Generation),
			CollectionIDs: []string{collectionID},
		})
		if err != nil {
			return servicetools.Result{}, err
		}
		payload := v21ToolPayloadFromRoute(response)
		raw, err := json.Marshal(payload)
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{Payload: raw, SafeSummary: v21ToolSafeSummary(payload)}, nil
	})
}

func v21VoiceQueryInputSchema(collections []string) map[string]any {
	enum := make([]any, 0, len(collections))
	for _, collection := range collections {
		enum = append(enum, collection)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "Short professional knowledge question to ask V21.",
				"minLength":   1,
				"maxLength":   512,
			},
			"collection_id": map[string]any{
				"type":        "string",
				"description": "V21 collection id from the allowlist.",
				"enum":        enum,
			},
		},
		"required":             []any{"question", "collection_id"},
		"additionalProperties": false,
	}
}

func v21ToolPayloadFromRoute(response RouteResponse) V21VoiceQueryToolPayload {
	if response.Blocked {
		return V21VoiceQueryToolPayload{
			Blocked: true,
			Reason:  strings.TrimSpace(response.Reason),
		}
	}
	payload := V21VoiceQueryToolPayload{
		SpokenAnswer: strings.TrimSpace(response.Text),
	}
	if response.V21 != nil {
		payload.QueryRunID = strings.TrimSpace(response.V21.QueryRunID)
		payload.AnswerType = strings.TrimSpace(response.V21.AnswerType)
		payload.Confidence = response.V21.Confidence
		payload.CitationCount = len(response.V21.Citations)
		payload.EvidenceCount = len(response.V21.Evidence)
		payload.SourceLabels = sourceLabels(response.V21.Citations, response.V21.Evidence)
		if payload.SpokenAnswer == "" {
			payload.SpokenAnswer = strings.TrimSpace(response.V21.SpokenAnswer)
		}
	}
	return payload
}

func sourceLabels(citations []V21Citation, evidence []V21Evidence) []string {
	labels := make([]string, 0, len(citations)+len(evidence))
	for _, citation := range citations {
		labels = append(labels, citation.SourceLabel)
	}
	for _, item := range evidence {
		labels = append(labels, item.SourceLabel)
	}
	return nonEmptyUniqueStrings(labels)
}

func v21ToolSafeSummary(payload V21VoiceQueryToolPayload) string {
	if payload.Blocked {
		return "blocked=" + strings.TrimSpace(payload.Reason)
	}
	return fmt.Sprintf("answer_type=%s citations=%d evidence=%d", payload.AnswerType, payload.CitationCount, payload.EvidenceCount)
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func stringToolArgument(arguments map[string]any, key string) string {
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
