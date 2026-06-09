package feishu

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
	ListTargetsToolName = "feishu.list_targets"
	SendTextToolName    = "feishu.send_text"

	defaultMaxTextRunes = 240
)

var (
	ErrMissingTargetID  = errors.New("feishu target_id is required")
	ErrTargetNotAllowed = errors.New("feishu target is not allowlisted")
	ErrTextTooLong      = errors.New("feishu text is too long")
)

type TargetConfig struct {
	TargetID      string
	Description   string
	ReceiveIDType string
	ReceiveID     string
}

type ServiceToolOptions struct {
	Client              *Client
	Targets             []TargetConfig
	MaxTextRunes        int
	RequireConfirmation bool
}

type ListTargetsToolOptions struct {
	Targets []TargetConfig
}

type SendTextToolOptions struct {
	Client              *Client
	Targets             []TargetConfig
	MaxTextRunes        int
	RequireConfirmation bool
}

type ListTargetsPayload struct {
	Count   int                 `json:"count"`
	Targets []SafeTargetPayload `json:"targets"`
}

type SafeTargetPayload struct {
	TargetID      string `json:"target_id"`
	Description   string `json:"description,omitempty"`
	ReceiveIDType string `json:"receive_id_type"`
}

type SendTextPayload struct {
	TargetID             string `json:"target_id"`
	MessageID            string `json:"message_id,omitempty"`
	OK                   bool   `json:"ok"`
	RequiresConfirmation bool   `json:"requires_confirmation,omitempty"`
}

func RegisterServiceTools(registry *servicetools.Registry, options ServiceToolOptions) error {
	if err := RegisterListTargetsTool(registry, ListTargetsToolOptions{Targets: options.Targets}); err != nil {
		return err
	}
	return RegisterSendTextTool(registry, SendTextToolOptions{
		Client:              options.Client,
		Targets:             options.Targets,
		MaxTextRunes:        options.MaxTextRunes,
		RequireConfirmation: options.RequireConfirmation,
	})
}

func RegisterListTargetsTool(registry *servicetools.Registry, options ListTargetsToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	targets, err := safeTargets(options.Targets)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("feishu allowed_targets is required")
	}
	return registry.Register(servicetools.Definition{
		Name:        ListTargetsToolName,
		Description: "List operator-configured Feishu message targets available to this voice session.",
		Permission:  servicetools.PermissionRead,
		InputSchema: noArgumentInputSchema(),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		_ = ctx
		_ = call
		payload := ListTargetsPayload{
			Count:   len(targets),
			Targets: targets,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{Payload: raw, SafeSummary: fmt.Sprintf("targets=%d", len(targets))}, nil
	})
}

func RegisterSendTextTool(registry *servicetools.Registry, options SendTextToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	if options.Client == nil {
		return fmt.Errorf("feishu client is required")
	}
	targets, err := targetMap(options.Targets)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("feishu allowed_targets is required")
	}
	maxTextRunes := options.MaxTextRunes
	if maxTextRunes <= 0 {
		maxTextRunes = defaultMaxTextRunes
	}
	return registry.Register(servicetools.Definition{
		Name:        SendTextToolName,
		Description: "Send a short text message to one operator-configured Feishu target.",
		Permission:  servicetools.PermissionWrite,
		InputSchema: sendTextInputSchema(targetIDs(options.Targets), maxTextRunes),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		targetID := stringArgument(call.Arguments, "target_id")
		if targetID == "" {
			return servicetools.Result{}, ErrMissingTargetID
		}
		target, ok := targets[targetID]
		if !ok {
			return servicetools.Result{}, ErrTargetNotAllowed
		}
		text := stringArgument(call.Arguments, "text")
		if text == "" {
			return servicetools.Result{}, ErrMissingText
		}
		if utf8.RuneCountInString(text) > maxTextRunes {
			return servicetools.Result{}, ErrTextTooLong
		}
		if options.RequireConfirmation {
			payload := SendTextPayload{
				TargetID:             target.TargetID,
				OK:                   false,
				RequiresConfirmation: true,
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				return servicetools.Result{}, err
			}
			return servicetools.Result{Payload: raw, SafeSummary: "confirmation_required target=" + target.TargetID}, nil
		}
		response, err := options.Client.SendTextMessage(ctx, SendTextRequest{
			ReceiveIDType: target.ReceiveIDType,
			ReceiveID:     target.ReceiveID,
			Text:          sanitizeText(text),
		})
		if err != nil {
			return servicetools.Result{}, err
		}
		payload := SendTextPayload{
			TargetID:  target.TargetID,
			MessageID: response.MessageID,
			OK:        true,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{Payload: raw, SafeSummary: "target=" + target.TargetID}, nil
	})
}

func noArgumentInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func sendTextInputSchema(targetIDs []string, maxTextRunes int) map[string]any {
	enum := make([]any, 0, len(targetIDs))
	for _, targetID := range targetIDs {
		enum = append(enum, targetID)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_id": map[string]any{
				"type":        "string",
				"description": "Feishu target id from the operator-configured allowlist.",
				"enum":        enum,
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Short text to send. Mentions are disabled.",
				"minLength":   1,
				"maxLength":   maxTextRunes,
			},
		},
		"required":             []any{"target_id", "text"},
		"additionalProperties": false,
	}
}

func safeTargets(targets []TargetConfig) ([]SafeTargetPayload, error) {
	out := make([]SafeTargetPayload, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		target.TargetID = strings.TrimSpace(target.TargetID)
		target.ReceiveIDType = strings.TrimSpace(target.ReceiveIDType)
		if target.TargetID == "" || target.ReceiveIDType == "" {
			continue
		}
		if _, ok := seen[target.TargetID]; ok {
			return nil, fmt.Errorf("feishu target_id must be unique")
		}
		seen[target.TargetID] = struct{}{}
		out = append(out, SafeTargetPayload{
			TargetID:      target.TargetID,
			Description:   strings.TrimSpace(target.Description),
			ReceiveIDType: target.ReceiveIDType,
		})
	}
	return out, nil
}

func targetMap(targets []TargetConfig) (map[string]TargetConfig, error) {
	out := make(map[string]TargetConfig, len(targets))
	for _, target := range targets {
		target.TargetID = strings.TrimSpace(target.TargetID)
		target.ReceiveIDType = strings.TrimSpace(target.ReceiveIDType)
		target.ReceiveID = strings.TrimSpace(target.ReceiveID)
		if target.TargetID == "" || target.ReceiveIDType == "" || target.ReceiveID == "" {
			continue
		}
		if _, ok := out[target.TargetID]; ok {
			return nil, fmt.Errorf("feishu target_id must be unique")
		}
		out[target.TargetID] = target
	}
	return out, nil
}

func targetIDs(targets []TargetConfig) []string {
	ids := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetID := strings.TrimSpace(target.TargetID)
		if targetID == "" {
			continue
		}
		if _, ok := seen[targetID]; ok {
			continue
		}
		seen[targetID] = struct{}{}
		ids = append(ids, targetID)
	}
	return ids
}

func sanitizeText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "<", "＜")
	text = strings.ReplaceAll(text, ">", "＞")
	return text
}

func stringArgument(arguments map[string]any, key string) string {
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
