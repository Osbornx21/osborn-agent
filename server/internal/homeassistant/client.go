package homeassistant

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
)

const (
	DefaultTimeoutMS = 1200
)

var (
	ErrMissingBaseURL = errors.New("home assistant base url is required")
	ErrMissingToken   = errors.New("home assistant token is required")
	ErrMissingEntity  = errors.New("home assistant entity_id is required")
	ErrMissingService = errors.New("home assistant domain and service are required")
)

type ClientOptions struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

type Client struct {
	baseURL string
	token   string
	client  *http.Client
}

type State struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastChanged string         `json:"last_changed"`
	LastUpdated string         `json:"last_updated"`
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		return nil, ErrMissingBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("home assistant base url must be http or https")
	}
	token := strings.TrimSpace(options.Token)
	if token == "" {
		return nil, ErrMissingToken
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		client:  client,
	}, nil
}

func (c *Client) GetState(ctx context.Context, entityID string) (State, error) {
	if c == nil {
		return State{}, ErrMissingBaseURL
	}
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return State{}, ErrMissingEntity
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/states/"+url.PathEscape(entityID), nil)
	if err != nil {
		return State{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return State{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return State{}, fmt.Errorf("home assistant request failed: status=%d", resp.StatusCode)
	}

	var state State
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32*1024)).Decode(&state); err != nil {
		return State{}, fmt.Errorf("decode home assistant state: %w", err)
	}
	return state, nil
}

func (c *Client) CallService(ctx context.Context, domain string, service string, entityIDs []string, data map[string]any) error {
	if c == nil {
		return ErrMissingBaseURL
	}
	domain = strings.TrimSpace(domain)
	service = strings.TrimSpace(service)
	if !isSafeServiceSegment(domain) || !isSafeServiceSegment(service) {
		return ErrMissingService
	}
	entities := nonEmptyUnique(entityIDs)
	if len(entities) == 0 {
		return ErrMissingEntity
	}

	body := cloneServiceData(data)
	body["entity_id"] = entities
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	endpoint := c.baseURL + "/api/services/" + url.PathEscape(domain) + "/" + url.PathEscape(service)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("home assistant request failed: status=%d", resp.StatusCode)
	}
	return nil
}

func cloneServiceData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data)+1)
	for key, value := range data {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func isSafeServiceSegment(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}
