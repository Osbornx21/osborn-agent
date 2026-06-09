package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchClientPostsBoundedRequestWithBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != WebSearchPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, WebSearchPath)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer search-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		var body WebSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.SessionID != "session-1" || body.DeviceID != "stackchan-s3-main" || body.Query != "M5Stack StackChan" || body.MaxResults != 2 {
			t.Fatalf("request body = %+v", body)
		}
		if len(body.AllowedDomains) != 1 || body.AllowedDomains[0] != "docs.m5stack.com" {
			t.Fatalf("allowed domains = %v", body.AllowedDomains)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider":"adapter","results":[{"title":"StackChan","url":"https://docs.m5stack.com/zh_CN/StackChan/","snippet":"docs","source_domain":"docs.m5stack.com"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL + "/", Token: "search-secret"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	response, err := client.SearchWeb(context.Background(), WebSearchRequest{
		SessionID:      "session-1",
		DeviceID:       "stackchan-s3-main",
		Query:          " M5Stack StackChan ",
		MaxResults:     2,
		AllowedDomains: []string{"docs.m5stack.com", "docs.m5stack.com"},
	})
	if err != nil {
		t.Fatalf("SearchWeb() error = %v", err)
	}
	if response.Provider != "adapter" || len(response.Results) != 1 || response.Results[0].Title != "StackChan" {
		t.Fatalf("response = %+v", response)
	}
}

func TestSearchClientErrorsDoNotLeakTokenOrBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`token-shaped-body-that-must-not-leak`))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL, Token: "search-secret-that-must-not-leak"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.SearchWeb(context.Background(), WebSearchRequest{Query: "hello"})
	if err == nil {
		t.Fatal("SearchWeb() error = nil, want status error")
	}
	for _, forbidden := range []string{"search-secret-that-must-not-leak", "token-shaped-body-that-must-not-leak"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error leaked %q: %v", forbidden, err)
		}
	}
}

func TestNewSearchClientRequiresURLAndToken(t *testing.T) {
	if _, err := NewClient(ClientOptions{Token: "secret"}); !errors.Is(err, ErrMissingBaseURL) {
		t.Fatalf("missing URL error = %v, want ErrMissingBaseURL", err)
	}
	if _, err := NewClient(ClientOptions{BaseURL: "https://search.example.internal"}); !errors.Is(err, ErrMissingToken) {
		t.Fatalf("missing token error = %v, want ErrMissingToken", err)
	}
	if _, err := NewClient(ClientOptions{BaseURL: "file:///tmp/search", Token: "secret"}); err == nil {
		t.Fatal("NewClient() accepted non-http URL")
	}
}
