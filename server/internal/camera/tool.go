package camera

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
	RequestCaptureToolName = "camera.request_capture"

	defaultMaxReasonRunes = 120
)

var (
	ErrMissingReason = errors.New("camera capture reason is required")
	ErrReasonTooLong = errors.New("camera capture reason is too long")
)

type RequestCaptureToolOptions struct {
	MaxReasonRunes int
}

type RequestCapturePayload struct {
	OK                   bool   `json:"ok"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	Capability           string `json:"capability"`
	ReasonChars          int    `json:"reason_chars"`
}

func RegisterRequestCaptureTool(registry *servicetools.Registry, options RequestCaptureToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	maxReasonRunes := options.MaxReasonRunes
	if maxReasonRunes <= 0 {
		maxReasonRunes = defaultMaxReasonRunes
	}
	return registry.Register(servicetools.Definition{
		Name:        RequestCaptureToolName,
		Description: "Request explicit user confirmation before using the StackChan camera. This tool does not take a photo.",
		Permission:  servicetools.PermissionDeviceControl,
		InputSchema: requestCaptureInputSchema(maxReasonRunes),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		_ = ctx
		reason := stringArgument(call.Arguments, "reason")
		if reason == "" {
			return servicetools.Result{}, ErrMissingReason
		}
		reasonChars := utf8.RuneCountInString(reason)
		if reasonChars > maxReasonRunes {
			return servicetools.Result{}, ErrReasonTooLong
		}
		payload, err := json.Marshal(RequestCapturePayload{
			OK:                   false,
			RequiresConfirmation: true,
			Capability:           "camera.capture",
			ReasonChars:          reasonChars,
		})
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{
			Payload:     payload,
			SafeSummary: fmt.Sprintf("confirmation_required reason_chars=%d", reasonChars),
		}, nil
	})
}

func requestCaptureInputSchema(maxReasonRunes int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reason": map[string]any{
				"type":        "string",
				"description": "Short reason for requesting camera use. The camera is not used until the user confirms.",
				"minLength":   1,
				"maxLength":   maxReasonRunes,
			},
		},
		"required":             []any{"reason"},
		"additionalProperties": false,
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
