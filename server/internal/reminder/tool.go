package reminder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	servicetools "stackchan-gateway/internal/tools"
)

const (
	AnnounceToolName = "reminder.announce"

	defaultMaxTitleRunes   = 80
	defaultMaxMessageRunes = 240
	urgencyNormal          = "normal"
	urgencyHigh            = "high"
)

var (
	ErrMissingTitle   = errors.New("reminder title is required")
	ErrTitleTooLong   = errors.New("reminder title is too long")
	ErrMessageTooLong = errors.New("reminder message is too long")
	ErrInvalidUrgency = errors.New("reminder urgency is invalid")
)

type AnnounceToolOptions struct {
	MaxTitleRunes   int
	MaxMessageRunes int
}

type AnnouncePayload struct {
	OK           bool   `json:"ok"`
	Title        string `json:"title"`
	Urgency      string `json:"urgency"`
	MessageChars int    `json:"message_chars,omitempty"`
}

func RegisterAnnounceTool(registry *servicetools.Registry, options AnnounceToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	maxTitleRunes := options.MaxTitleRunes
	if maxTitleRunes <= 0 {
		maxTitleRunes = defaultMaxTitleRunes
	}
	maxMessageRunes := options.MaxMessageRunes
	if maxMessageRunes <= 0 {
		maxMessageRunes = defaultMaxMessageRunes
	}
	return registry.Register(servicetools.Definition{
		Name:        AnnounceToolName,
		Description: "Announce a currently due reminder to the StackChan terminal.",
		Permission:  servicetools.PermissionDeviceControl,
		InputSchema: announceInputSchema(maxTitleRunes, maxMessageRunes),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		_ = ctx
		title := stringArgument(call.Arguments, "title")
		if title == "" {
			return servicetools.Result{}, ErrMissingTitle
		}
		if utf8.RuneCountInString(title) > maxTitleRunes {
			return servicetools.Result{}, ErrTitleTooLong
		}
		message := stringArgument(call.Arguments, "message")
		if utf8.RuneCountInString(message) > maxMessageRunes {
			return servicetools.Result{}, ErrMessageTooLong
		}
		urgency := normalizeUrgency(stringArgument(call.Arguments, "urgency"))
		if urgency == "" {
			return servicetools.Result{}, ErrInvalidUrgency
		}
		payload, err := json.Marshal(AnnouncePayload{
			OK:           true,
			Title:        title,
			Urgency:      urgency,
			MessageChars: utf8.RuneCountInString(message),
		})
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{
			Payload:     payload,
			SafeSummary: "urgency=" + urgency,
		}, nil
	})
}

func announceInputSchema(maxTitleRunes, maxMessageRunes int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Short reminder title to show or speak.",
				"minLength":   1,
				"maxLength":   maxTitleRunes,
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Optional short reminder detail. It is acknowledged but not copied into the tool result.",
				"maxLength":   maxMessageRunes,
			},
			"urgency": map[string]any{
				"type":        "string",
				"description": "Attention level for this due reminder.",
				"enum":        []any{urgencyNormal, urgencyHigh},
			},
		},
		"required":             []any{"title"},
		"additionalProperties": false,
	}
}

func normalizeUrgency(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return urgencyNormal
	case urgencyNormal, urgencyHigh:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func stringArgument(arguments map[string]any, key string) string {
	if arguments == nil {
		return ""
	}
	value, ok := arguments[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
