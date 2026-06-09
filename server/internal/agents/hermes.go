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

	"stackchan-gateway/internal/providers"
)

const (
	HermesRespondPath           = "/internal/v1/agent/respond"
	HermesDefaultMaxSpokenChars = 180
)

var (
	ErrMissingHermesURL   = errors.New("hermes url is required")
	ErrMissingHermesToken = errors.New("hermes token is required")
	ErrMissingHermesText  = errors.New("hermes text is required")
)

type HermesToolIntent struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}

type HermesClientOptions struct {
	BaseURL            string
	Token              string
	MaxSpokenChars     int
	AllowedToolIntents []string
	MaxToolIntents     *int
	Client             *http.Client
}

type HermesClient struct {
	baseURL            string
	token              string
	maxSpokenChars     int
	allowedToolIntents []string
	maxToolIntents     int
	client             *http.Client
}

type HermesRequest struct {
	SessionID string `json:"session_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	Text      string `json:"text"`
	TraceID   string `json:"trace_id,omitempty"`
}

type HermesResponse struct {
	Text        string             `json:"text"`
	ToolIntents []HermesToolIntent `json:"tool_intents,omitempty"`
}

func NewHermesClient(options HermesClientOptions) *HermesClient {
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	maxSpokenChars := options.MaxSpokenChars
	if maxSpokenChars <= 0 {
		maxSpokenChars = HermesDefaultMaxSpokenChars
	}
	return &HermesClient{
		baseURL:            strings.TrimRight(strings.TrimSpace(options.BaseURL), "/"),
		token:              strings.TrimSpace(options.Token),
		maxSpokenChars:     maxSpokenChars,
		allowedToolIntents: NormalizeBridgeAllowedToolIntents(options.AllowedToolIntents),
		maxToolIntents:     ResolveBridgeMaxToolIntents(options.MaxToolIntents),
		client:             client,
	}
}

func (c *HermesClient) Route(ctx context.Context, request RouteRequest) (RouteResponse, error) {
	response, err := c.Respond(ctx, HermesRequest{
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

func (c *HermesClient) Respond(ctx context.Context, request HermesRequest) (HermesResponse, error) {
	if c == nil || c.baseURL == "" {
		return HermesResponse{}, ErrMissingHermesURL
	}
	if c.token == "" {
		return HermesResponse{}, ErrMissingHermesToken
	}
	request.Text = strings.TrimSpace(request.Text)
	if request.Text == "" {
		return HermesResponse{}, ErrMissingHermesText
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return HermesResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+HermesRespondPath, bytes.NewReader(payload))
	if err != nil {
		return HermesResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return HermesResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, v21MaxResponseBytes))
		return HermesResponse{}, fmt.Errorf("hermes request failed: status=%d", resp.StatusCode)
	}
	var response HermesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, v21MaxResponseBytes)).Decode(&response); err != nil {
		return HermesResponse{}, fmt.Errorf("decode hermes response: %w", err)
	}
	return response, nil
}

func HermesToolIntentsToGatewayToolCalls(intents []HermesToolIntent) []providers.ToolCall {
	return HermesToolIntentsToGatewayToolCallsWithAllowedTools(intents, nil)
}

func HermesToolIntentsToGatewayToolCallsWithAllowedTools(intents []HermesToolIntent, allowedToolIntents []string) []providers.ToolCall {
	return HermesToolIntentsToGatewayToolCallsWithPolicy(intents, allowedToolIntents, MaxBridgeToolIntentsPerTurn)
}

func HermesToolIntentsToGatewayToolCallsWithPolicy(intents []HermesToolIntent, allowedToolIntents []string, maxToolIntents int) []providers.ToolCall {
	calls := make([]providers.ToolCall, 0, len(intents))
	for index, intent := range intents {
		calls = appendBridgeToolCallWithPolicy(calls, "hermes", index, intent.Tool, intent.Args, allowedToolIntents, maxToolIntents)
	}
	return calls
}
