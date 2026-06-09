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
	V21VoiceQueryPath           = "/internal/v1/knowledge/voice-query"
	V21ModeGroundedQA           = "grounded_qa"
	V21ResponseStyleShortSpoken = "short_spoken"
	V21QueryScopePublicOnly     = "public_only"
	V21DefaultMaxSpokenChars    = 180
	v21MaxResponseBytes         = 1 << 20
	v21DefaultRequireCitations  = true
	v21DefaultAllowStyleWrap    = false
)

var (
	ErrMissingV21BaseURL    = errors.New("v21 base url is required")
	ErrMissingV21Token      = errors.New("v21 token is required")
	ErrMissingV21Question   = errors.New("v21 question is required")
	ErrMissingV21Collection = errors.New("v21 collection_ids is required")
)

type V21ClientOptions struct {
	BaseURL        string
	Token          string
	MaxSpokenChars int
	Client         *http.Client
}

type V21Client struct {
	baseURL        string
	token          string
	maxSpokenChars int
	client         *http.Client
}

type V21VoiceQueryRequest struct {
	WorkspaceID      string   `json:"workspace_id,omitempty"`
	UserID           string   `json:"user_id,omitempty"`
	DeviceID         string   `json:"device_id,omitempty"`
	QueryScope       string   `json:"query_scope,omitempty"`
	AgentID          string   `json:"agent_id,omitempty"`
	SessionID        string   `json:"session_id,omitempty"`
	TurnID           string   `json:"turn_id,omitempty"`
	CollectionIDs    []string `json:"collection_ids"`
	Question         string   `json:"question"`
	Mode             string   `json:"mode,omitempty"`
	ResponseStyle    string   `json:"response_style,omitempty"`
	MaxSpokenChars   int      `json:"max_spoken_chars,omitempty"`
	RequireCitations bool     `json:"require_citations"`
	AllowStyleWrap   bool     `json:"allow_style_wrap"`
	TraceID          string   `json:"trace_id,omitempty"`
}

type V21VoiceQueryResponse struct {
	QueryRunID        string                 `json:"query_run_id"`
	AnswerType        string                 `json:"answer_type"`
	SpokenAnswer      string                 `json:"spoken_answer"`
	FullAnswer        string                 `json:"full_answer"`
	Citations         []V21Citation          `json:"citations"`
	Evidence          []V21Evidence          `json:"evidence"`
	Confidence        float64                `json:"confidence"`
	NoEvidenceReason  string                 `json:"no_evidence_reason,omitempty"`
	SafeToStyleWrap   bool                   `json:"safe_to_style_wrap"`
	Policy            V21Policy              `json:"policy"`
	ToolResults       []map[string]any       `json:"tool_results"`
	LatencyMS         map[string]int64       `json:"latency_ms,omitempty"`
	TraceID           string                 `json:"trace_id,omitempty"`
	QueryScope        string                 `json:"query_scope,omitempty"`
	SourceScopeCounts map[string]int         `json:"source_scope_counts,omitempty"`
	WorkspaceStatus   string                 `json:"workspace_status,omitempty"`
	Extra             map[string]interface{} `json:"-"`
}

type V21Citation struct {
	AnchorID       string `json:"anchor_id"`
	AssetVersionID string `json:"asset_version_id"`
	SourceUnitID   string `json:"source_unit_id"`
	SourceLabel    string `json:"source_label,omitempty"`
	Excerpt        string `json:"excerpt,omitempty"`
}

type V21Evidence struct {
	ChunkID      string  `json:"chunk_id"`
	AnchorID     string  `json:"anchor_id"`
	VersionID    string  `json:"version_id"`
	SourceUnitID string  `json:"source_unit_id"`
	SourceLabel  string  `json:"source_label,omitempty"`
	SourceScope  string  `json:"source_scope,omitempty"`
	Excerpt      string  `json:"excerpt"`
	Score        float64 `json:"score"`
}

type V21Policy struct {
	RequireCitations bool `json:"require_citations"`
	AllowStyleWrap   bool `json:"allow_style_wrap"`
	FactLocked       bool `json:"fact_locked"`
}

func NewV21Client(options V21ClientOptions) *V21Client {
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	maxSpokenChars := options.MaxSpokenChars
	if maxSpokenChars <= 0 {
		maxSpokenChars = V21DefaultMaxSpokenChars
	}
	return &V21Client{
		baseURL:        strings.TrimRight(strings.TrimSpace(options.BaseURL), "/"),
		token:          strings.TrimSpace(options.Token),
		maxSpokenChars: maxSpokenChars,
		client:         client,
	}
}

func (c *V21Client) Route(ctx context.Context, request RouteRequest) (RouteResponse, error) {
	response, err := c.Query(ctx, V21VoiceQueryRequest{
		WorkspaceID:      request.WorkspaceID,
		UserID:           request.UserID,
		DeviceID:         request.DeviceID,
		QueryScope:       defaultString(request.QueryScope, V21QueryScopePublicOnly),
		AgentID:          request.AgentID,
		SessionID:        request.SessionID,
		TurnID:           request.TurnID,
		CollectionIDs:    request.CollectionIDs,
		Question:         request.Text,
		Mode:             V21ModeGroundedQA,
		ResponseStyle:    V21ResponseStyleShortSpoken,
		MaxSpokenChars:   c.maxSpokenChars,
		RequireCitations: v21DefaultRequireCitations,
		AllowStyleWrap:   v21DefaultAllowStyleWrap,
		TraceID:          request.TraceID,
	})
	if err != nil {
		return RouteResponse{}, err
	}
	return RouteResponse{
		Text:         strings.TrimSpace(response.SpokenAnswer),
		OutputTarget: OutputTargetGatewayTTS,
		V21:          &response,
	}, nil
}

func (c *V21Client) Query(ctx context.Context, request V21VoiceQueryRequest) (V21VoiceQueryResponse, error) {
	if c == nil || c.baseURL == "" {
		return V21VoiceQueryResponse{}, ErrMissingV21BaseURL
	}
	if c.token == "" {
		return V21VoiceQueryResponse{}, ErrMissingV21Token
	}
	request = sanitizeV21Request(request)
	if request.Question == "" {
		return V21VoiceQueryResponse{}, ErrMissingV21Question
	}
	if len(request.CollectionIDs) == 0 {
		return V21VoiceQueryResponse{}, ErrMissingV21Collection
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return V21VoiceQueryResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+V21VoiceQueryPath, bytes.NewReader(payload))
	if err != nil {
		return V21VoiceQueryResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return V21VoiceQueryResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, v21MaxResponseBytes))
		return V21VoiceQueryResponse{}, fmt.Errorf("v21 request failed: status=%d", resp.StatusCode)
	}

	var response V21VoiceQueryResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, v21MaxResponseBytes)).Decode(&response); err != nil {
		return V21VoiceQueryResponse{}, fmt.Errorf("decode v21 voice query response: %w", err)
	}
	return response, nil
}

func sanitizeV21Request(request V21VoiceQueryRequest) V21VoiceQueryRequest {
	request.WorkspaceID = strings.TrimSpace(request.WorkspaceID)
	request.UserID = strings.TrimSpace(request.UserID)
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	request.QueryScope = defaultString(request.QueryScope, V21QueryScopePublicOnly)
	request.AgentID = strings.TrimSpace(request.AgentID)
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.TurnID = strings.TrimSpace(request.TurnID)
	request.CollectionIDs = nonEmptyUniqueStrings(request.CollectionIDs)
	request.Question = strings.TrimSpace(request.Question)
	request.Mode = defaultString(request.Mode, V21ModeGroundedQA)
	request.ResponseStyle = defaultString(request.ResponseStyle, V21ResponseStyleShortSpoken)
	request.TraceID = strings.TrimSpace(request.TraceID)
	return request
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
