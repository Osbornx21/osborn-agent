package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	servicetools "stackchan-gateway/internal/tools"
)

const (
	WebSearchToolName = "search.web"

	defaultMaxTitleRunes   = 80
	defaultMaxSnippetRunes = 240
)

var (
	ErrQueryTooLong = errors.New("search query is too long")
)

type WebSearchToolOptions struct {
	Client         *Client
	MaxResults     int
	MaxQueryRunes  int
	AllowedDomains []string
}

type WebSearchPayload struct {
	Provider    string                `json:"provider,omitempty"`
	ResultCount int                   `json:"result_count"`
	Results     []WebSearchSafeResult `json:"results"`
}

type WebSearchSafeResult struct {
	Title        string `json:"title"`
	URL          string `json:"url"`
	Snippet      string `json:"snippet,omitempty"`
	SourceDomain string `json:"source_domain,omitempty"`
	PublishedAt  string `json:"published_at,omitempty"`
}

func RegisterWebSearchTool(registry *servicetools.Registry, options WebSearchToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	if options.Client == nil {
		return fmt.Errorf("search client is required")
	}
	maxResults := options.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	maxQueryRunes := options.MaxQueryRunes
	if maxQueryRunes <= 0 {
		maxQueryRunes = defaultMaxQueryRunes
	}
	allowedDomains := nonEmptyUnique(options.AllowedDomains)
	return registry.Register(servicetools.Definition{
		Name:        WebSearchToolName,
		Description: "Search the web through the operator-configured search adapter and return bounded result summaries.",
		Permission:  servicetools.PermissionExternal,
		InputSchema: webSearchInputSchema(maxResults, maxQueryRunes),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		query := stringArgument(call.Arguments, "query")
		if query == "" {
			return servicetools.Result{}, ErrMissingQuery
		}
		if utf8.RuneCountInString(query) > maxQueryRunes {
			return servicetools.Result{}, ErrQueryTooLong
		}
		requestedMax := intArgument(call.Arguments, "max_results")
		if requestedMax <= 0 || requestedMax > maxResults {
			requestedMax = maxResults
		}
		response, err := options.Client.SearchWeb(ctx, WebSearchRequest{
			SessionID:      call.SessionID,
			DeviceID:       call.DeviceID,
			Query:          query,
			MaxResults:     requestedMax,
			AllowedDomains: allowedDomains,
		})
		if err != nil {
			return servicetools.Result{}, err
		}
		payload := webSearchPayload(response, requestedMax, allowedDomains)
		raw, err := json.Marshal(payload)
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{Payload: raw, SafeSummary: fmt.Sprintf("results=%d", payload.ResultCount)}, nil
	})
}

func webSearchInputSchema(maxResults int, maxQueryRunes int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query. Use a short, specific query.",
				"minLength":   1,
				"maxLength":   maxQueryRunes,
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of search results to return.",
				"minimum":     1,
				"maximum":     maxResults,
			},
		},
		"required":             []any{"query"},
		"additionalProperties": false,
	}
}

func webSearchPayload(response WebSearchResponse, maxResults int, allowedDomains []string) WebSearchPayload {
	allowed := domainSet(allowedDomains)
	results := make([]WebSearchSafeResult, 0, minInt(len(response.Results), maxResults))
	for _, result := range response.Results {
		if len(results) >= maxResults {
			break
		}
		safe, ok := safeSearchResult(result)
		if !ok {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[strings.ToLower(safe.SourceDomain)]; !ok {
				continue
			}
		}
		results = append(results, safe)
	}
	return WebSearchPayload{
		Provider:    limitRunes(strings.TrimSpace(response.Provider), 40),
		ResultCount: len(results),
		Results:     results,
	}
}

func safeSearchResult(result WebSearchResult) (WebSearchSafeResult, bool) {
	rawURL := strings.TrimSpace(result.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return WebSearchSafeResult{}, false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return WebSearchSafeResult{}, false
	}
	sourceDomain := strings.ToLower(strings.TrimSpace(result.SourceDomain))
	if sourceDomain == "" {
		sourceDomain = host
	}
	return WebSearchSafeResult{
		Title:        limitRunes(strings.TrimSpace(result.Title), defaultMaxTitleRunes),
		URL:          rawURL,
		Snippet:      limitRunes(strings.TrimSpace(result.Snippet), defaultMaxSnippetRunes),
		SourceDomain: sourceDomain,
		PublishedAt:  limitRunes(strings.TrimSpace(result.PublishedAt), 40),
	}, true
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

func intArgument(arguments map[string]any, key string) int {
	if arguments == nil {
		return 0
	}
	value, ok := arguments[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		if typed < 1 || typed > 1000 {
			return 0
		}
		return int(typed)
	default:
		return 0
	}
}

func domainSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
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

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
