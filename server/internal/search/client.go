package search

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
	WebSearchPath        = "/internal/v1/search/web"
	defaultMaxResults    = 3
	defaultMaxQueryRunes = 160
	maxResponseBytes     = 1 << 20
)

var (
	ErrMissingBaseURL = errors.New("search adapter base url is required")
	ErrMissingToken   = errors.New("search adapter token is required")
	ErrMissingQuery   = errors.New("search query is required")
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

type WebSearchRequest struct {
	SessionID      string   `json:"session_id,omitempty"`
	DeviceID       string   `json:"device_id,omitempty"`
	Query          string   `json:"query"`
	MaxResults     int      `json:"max_results,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
}

type WebSearchResponse struct {
	Provider string            `json:"provider,omitempty"`
	Results  []WebSearchResult `json:"results"`
}

type WebSearchResult struct {
	Title        string `json:"title"`
	URL          string `json:"url"`
	Snippet      string `json:"snippet,omitempty"`
	SourceDomain string `json:"source_domain,omitempty"`
	PublishedAt  string `json:"published_at,omitempty"`
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		return nil, ErrMissingBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("search adapter base url must be http or https")
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

func (c *Client) SearchWeb(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	if c == nil {
		return WebSearchResponse{}, ErrMissingBaseURL
	}
	request.Query = strings.TrimSpace(request.Query)
	if request.Query == "" {
		return WebSearchResponse{}, ErrMissingQuery
	}
	request.AllowedDomains = nonEmptyUnique(request.AllowedDomains)
	if request.MaxResults <= 0 {
		request.MaxResults = defaultMaxResults
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return WebSearchResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+WebSearchPath, bytes.NewReader(payload))
	if err != nil {
		return WebSearchResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return WebSearchResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return WebSearchResponse{}, fmt.Errorf("search adapter request failed: status=%d", resp.StatusCode)
	}
	var response WebSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&response); err != nil {
		return WebSearchResponse{}, fmt.Errorf("decode search adapter response: %w", err)
	}
	return response, nil
}

func nonEmptyUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
