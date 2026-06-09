package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistryExecutesAllowedToolWithClonedArguments(t *testing.T) {
	registry := NewRegistry(RegistryOptions{
		AllowedPermissions: []string{PermissionRead},
	})
	var received Call
	if err := registry.Register(Definition{
		Name:        "memory.lookup",
		Description: "Look up safe memory metadata.",
		Permission:  PermissionRead,
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, call Call) (Result, error) {
		received = call
		call.Arguments["query"] = "mutated"
		return Result{Payload: json.RawMessage(`{"ok":true}`), SafeSummary: "ok"}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	arguments := map[string]any{"query": "阿豪"}

	result, err := registry.ExecuteTool(context.Background(), Call{
		SessionID:  "sess",
		DeviceID:   "stackchan-s3-main",
		Generation: 7,
		Name:       " memory.lookup ",
		Arguments:  arguments,
	})

	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if string(result.Payload) != `{"ok":true}` {
		t.Fatalf("payload = %s, want ok json", string(result.Payload))
	}
	if received.Name != "memory.lookup" || received.DeviceID != "stackchan-s3-main" || received.Generation != 7 {
		t.Fatalf("received call = %+v, want normalized identity", received)
	}
	if arguments["query"] != "阿豪" {
		t.Fatalf("executor mutated caller arguments: %+v", arguments)
	}
}

func TestRegistryRejectsUnknownDisallowedAndPermissionDeniedTools(t *testing.T) {
	registry := NewRegistry(RegistryOptions{
		AllowedTools:       []string{"calendar.create"},
		AllowedPermissions: []string{PermissionRead},
	})
	if err := registry.Register(Definition{
		Name:       "calendar.create",
		Permission: PermissionWrite,
	}, func(context.Context, Call) (Result, error) {
		t.Fatal("executor should not run")
		return Result{}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(Definition{
		Name:       "memory.lookup",
		Permission: PermissionRead,
	}, func(context.Context, Call) (Result, error) {
		t.Fatal("executor should not run")
		return Result{}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := registry.ExecuteTool(context.Background(), Call{Name: "missing.tool"})
	if code := ErrorCode(err); code != ErrorCodeToolNotFound {
		t.Fatalf("missing error code = %q, want %q", code, ErrorCodeToolNotFound)
	}
	_, err = registry.ExecuteTool(context.Background(), Call{Name: "memory.lookup"})
	if code := ErrorCode(err); code != ErrorCodeToolNotAllowed {
		t.Fatalf("disallowed error code = %q, want %q", code, ErrorCodeToolNotAllowed)
	}
	_, err = registry.ExecuteTool(context.Background(), Call{Name: "calendar.create"})
	if code := ErrorCode(err); code != ErrorCodePermissionDenied {
		t.Fatalf("permission error code = %q, want %q", code, ErrorCodePermissionDenied)
	}
}

func TestRegistryTimeoutReturnsSafeError(t *testing.T) {
	registry := NewRegistry(RegistryOptions{
		AllowedPermissions: []string{PermissionExternal},
		DefaultTimeout:     time.Millisecond,
	})
	if err := registry.Register(Definition{
		Name:       "search.web",
		Permission: PermissionExternal,
	}, func(ctx context.Context, _ Call) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := registry.ExecuteTool(context.Background(), Call{Name: "search.web"})

	if code := ErrorCode(err); code != ErrorCodeToolTimeout {
		t.Fatalf("timeout error code = %q, want %q", code, ErrorCodeToolTimeout)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("raw context error leaked through: %v", err)
	}
}

func TestRegistryCatalogReturnsSafeSortedToolMetadata(t *testing.T) {
	registry := NewRegistry(RegistryOptions{
		AllowedTools:       []string{"memory.lookup"},
		AllowedPermissions: []string{PermissionRead},
	})
	if err := registry.Register(Definition{
		Name:        "search.web",
		Description: "Search the operator-owned web index.",
		Permission:  PermissionExternal,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":         map[string]any{"type": "string", "description": "Bearer private-token"},
				"max_results":   map[string]any{"type": "integer"},
				"token_secret":  map[string]any{"type": "string"},
				"metadata_json": map[string]any{"type": "string"},
			},
		},
	}, func(context.Context, Call) (Result, error) {
		t.Fatal("catalog must not execute service tools")
		return Result{}, nil
	}); err != nil {
		t.Fatalf("Register(search.web) error = %v", err)
	}
	if err := registry.Register(Definition{
		Name:        "memory.lookup",
		Description: "Look up scoped memories.",
		Permission:  PermissionRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			},
		},
	}, func(context.Context, Call) (Result, error) {
		t.Fatal("catalog must not execute service tools")
		return Result{}, nil
	}); err != nil {
		t.Fatalf("Register(memory.lookup) error = %v", err)
	}

	catalog := registry.Catalog()

	if catalog.Count != 2 || len(catalog.Tools) != 2 {
		t.Fatalf("catalog = %+v, want two tools", catalog)
	}
	first := catalog.Tools[0]
	if first.Name != "memory.lookup" || first.Permission != PermissionRead || !first.Allowed || !first.PermissionGranted {
		t.Fatalf("first catalog entry = %+v, want allowed read memory.lookup", first)
	}
	if len(first.SchemaProperties) != 2 || first.SchemaProperties[0] != "limit" || first.SchemaProperties[1] != "query" {
		t.Fatalf("memory schema properties = %v, want sorted safe property names", first.SchemaProperties)
	}
	second := catalog.Tools[1]
	if second.Name != "search.web" || second.Permission != PermissionExternal || second.Allowed || second.PermissionGranted {
		t.Fatalf("second catalog entry = %+v, want disallowed external search.web", second)
	}
	if len(second.SchemaProperties) != 2 || second.SchemaProperties[0] != "max_results" || second.SchemaProperties[1] != "query" {
		t.Fatalf("search schema properties = %v, want sensitive properties filtered", second.SchemaProperties)
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	for _, forbidden := range []string{"private-token", "token_secret", "metadata_json", "executor"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("catalog leaked %q: %s", forbidden, string(encoded))
		}
	}
}
