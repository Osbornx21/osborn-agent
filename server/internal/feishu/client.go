package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	TenantAccessTokenInternalPath = "/open-apis/auth/v3/tenant_access_token/internal"
	MessageCreatePath             = "/open-apis/im/v1/messages"

	maxResponseBytes = 1 << 20
)

var (
	ErrMissingBaseURL       = errors.New("feishu base url is required")
	ErrMissingAppID         = errors.New("feishu app id is required")
	ErrMissingAppSecret     = errors.New("feishu app secret is required")
	ErrMissingReceiveID     = errors.New("feishu receive_id is required")
	ErrMissingText          = errors.New("feishu text is required")
	ErrInvalidReceiveIDType = errors.New("feishu receive_id_type is invalid")
)

type ClientOptions struct {
	BaseURL   string
	AppID     string
	AppSecret string
	Client    *http.Client
	Now       func() time.Time
}

type Client struct {
	baseURL   string
	appID     string
	appSecret string
	client    *http.Client
	now       func() time.Time

	mu             sync.Mutex
	tenantToken    string
	tokenExpiresAt time.Time
}

type SendTextRequest struct {
	ReceiveIDType string
	ReceiveID     string
	Text          string
	UUID          string
}

type SendTextResponse struct {
	MessageID string
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		return nil, ErrMissingBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("feishu base url must be http or https")
	}
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		return nil, ErrMissingAppID
	}
	appSecret := strings.TrimSpace(options.AppSecret)
	if appSecret == "" {
		return nil, ErrMissingAppSecret
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		baseURL:   baseURL,
		appID:     appID,
		appSecret: appSecret,
		client:    client,
		now:       now,
	}, nil
}

func (c *Client) SendTextMessage(ctx context.Context, request SendTextRequest) (SendTextResponse, error) {
	if c == nil {
		return SendTextResponse{}, ErrMissingBaseURL
	}
	receiveIDType := strings.TrimSpace(request.ReceiveIDType)
	if !isValidReceiveIDType(receiveIDType) {
		return SendTextResponse{}, ErrInvalidReceiveIDType
	}
	receiveID := strings.TrimSpace(request.ReceiveID)
	if receiveID == "" {
		return SendTextResponse{}, ErrMissingReceiveID
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return SendTextResponse{}, ErrMissingText
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return SendTextResponse{}, err
	}

	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return SendTextResponse{}, err
	}
	body := map[string]string{
		"receive_id": receiveID,
		"msg_type":   "text",
		"content":    string(content),
	}
	if uuid := strings.TrimSpace(request.UUID); uuid != "" {
		body["uuid"] = uuid
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return SendTextResponse{}, err
	}
	endpoint := c.baseURL + MessageCreatePath + "?receive_id_type=" + url.QueryEscape(receiveIDType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return SendTextResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return SendTextResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return SendTextResponse{}, fmt.Errorf("feishu message request failed: status=%d", resp.StatusCode)
	}
	var decoded messageResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&decoded); err != nil {
		return SendTextResponse{}, fmt.Errorf("decode feishu message response: %w", err)
	}
	if decoded.Code != 0 {
		return SendTextResponse{}, fmt.Errorf("feishu message request failed: code=%d", decoded.Code)
	}
	if strings.TrimSpace(decoded.Data.MessageID) == "" {
		return SendTextResponse{}, fmt.Errorf("feishu message response missing message_id")
	}
	return SendTextResponse{MessageID: strings.TrimSpace(decoded.Data.MessageID)}, nil
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.tenantToken != "" && c.now().Before(c.tokenExpiresAt) {
		token := c.tenantToken
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	token, expiresAt, err := c.fetchTenantAccessToken(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.tenantToken = token
	c.tokenExpiresAt = expiresAt
	c.mu.Unlock()
	return token, nil
}

func (c *Client) fetchTenantAccessToken(ctx context.Context) (string, time.Time, error) {
	payload, err := json.Marshal(map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+TenantAccessTokenInternalPath, bytes.NewReader(payload))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return "", time.Time{}, fmt.Errorf("feishu token request failed: status=%d", resp.StatusCode)
	}
	var decoded tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&decoded); err != nil {
		return "", time.Time{}, fmt.Errorf("decode feishu token response: %w", err)
	}
	if decoded.Code != 0 {
		return "", time.Time{}, fmt.Errorf("feishu token request failed: code=%d", decoded.Code)
	}
	token := strings.TrimSpace(decoded.TenantAccessToken)
	if token == "" {
		return "", time.Time{}, fmt.Errorf("feishu token response missing tenant_access_token")
	}
	expiresIn := decoded.Expire
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	if expiresIn > 600 {
		expiresIn -= 300
	}
	return token, c.now().Add(time.Duration(expiresIn) * time.Second), nil
}

type tokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
}

type messageResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		MessageID string `json:"message_id"`
	} `json:"data"`
}

func isValidReceiveIDType(value string) bool {
	switch strings.TrimSpace(value) {
	case "open_id", "user_id", "union_id", "email", "chat_id":
		return true
	default:
		return false
	}
}
