package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	OpenClawRespondPath           = "/internal/v1/agent/respond"
	OpenClawDefaultMaxSpokenChars = 180
)

var (
	ErrMissingOpenClawURL   = errors.New("openclaw url is required")
	ErrMissingOpenClawToken = errors.New("openclaw token is required")
	ErrMissingOpenClawText  = errors.New("openclaw text is required")
)

type OpenClawClientOptions struct {
	BaseURL            string
	Token              string
	MaxSpokenChars     int
	AllowedToolIntents []string
	MaxToolIntents     *int
	Client             *http.Client
}

type OpenClawClient struct {
	baseURL            string
	token              string
	maxSpokenChars     int
	allowedToolIntents []string
	maxToolIntents     int
	client             *http.Client
}

type OpenClawRequest struct {
	SessionID string `json:"session_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	Text      string `json:"text"`
	TraceID   string `json:"trace_id,omitempty"`
}

type OpenClawResponse struct {
	Text        string             `json:"text"`
	ToolIntents []HermesToolIntent `json:"tool_intents,omitempty"`
}

func NewOpenClawClient(options OpenClawClientOptions) *OpenClawClient {
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	maxSpokenChars := options.MaxSpokenChars
	if maxSpokenChars <= 0 {
		maxSpokenChars = OpenClawDefaultMaxSpokenChars
	}
	return &OpenClawClient{
		baseURL:            strings.TrimRight(strings.TrimSpace(options.BaseURL), "/"),
		token:              strings.TrimSpace(options.Token),
		maxSpokenChars:     maxSpokenChars,
		allowedToolIntents: NormalizeBridgeAllowedToolIntents(options.AllowedToolIntents),
		maxToolIntents:     ResolveBridgeMaxToolIntents(options.MaxToolIntents),
		client:             client,
	}
}

func (c *OpenClawClient) Route(ctx context.Context, request RouteRequest) (RouteResponse, error) {
	response, err := c.Respond(ctx, OpenClawRequest{
		SessionID: request.SessionID,
		TurnID:    request.TurnID,
		DeviceID:  request.DeviceID,
		Text:      request.Text,
		TraceID:   request.TraceID,
	})
	if err != nil {
		return RouteResponse{}, err
	}
	return RouteResponse{
		Text:         limitRunes(strings.TrimSpace(response.Text), c.maxSpokenChars),
		OutputTarget: OutputTargetGatewayTTS,
		ToolCalls:    HermesToolIntentsToGatewayToolCallsWithPolicy(response.ToolIntents, c.allowedToolIntents, c.maxToolIntents),
	}, nil
}

func (c *OpenClawClient) Respond(ctx context.Context, request OpenClawRequest) (OpenClawResponse, error) {
	if c == nil || c.baseURL == "" {
		return OpenClawResponse{}, ErrMissingOpenClawURL
	}
	if c.token == "" {
		return OpenClawResponse{}, ErrMissingOpenClawToken
	}
	request.Text = strings.TrimSpace(request.Text)
	if request.Text == "" {
		return OpenClawResponse{}, ErrMissingOpenClawText
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return OpenClawResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+OpenClawRespondPath, bytes.NewReader(payload))
	if err != nil {
		return OpenClawResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return OpenClawResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, v21MaxResponseBytes))
		return OpenClawResponse{}, fmt.Errorf("openclaw request failed: status=%d", resp.StatusCode)
	}
	var response OpenClawResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, v21MaxResponseBytes)).Decode(&response); err != nil {
		return OpenClawResponse{}, fmt.Errorf("decode openclaw response: %w", err)
	}
	return response, nil
}

func limitRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return strings.TrimSpace(text)
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}
