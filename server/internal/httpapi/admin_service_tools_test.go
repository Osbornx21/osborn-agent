package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	servicetools "stackchan-gateway/internal/tools"
)

func TestAdminServiceToolCatalogReturnsSafeReadOnlyCatalog(t *testing.T) {
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{"memory.lookup"},
		AllowedPermissions: []string{servicetools.PermissionRead},
	})
	if err := registry.Register(servicetools.Definition{
		Name:        "memory.lookup",
		Description: "Look up scoped memories.",
		Permission:  servicetools.PermissionRead,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":        map[string]any{"type": "string"},
				"token_secret": map[string]any{"type": "string"},
			},
		},
	}, func(context.Context, servicetools.Call) (servicetools.Result, error) {
		t.Fatal("catalog must not execute service tools")
		return servicetools.Result{}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:   "admin-token",
		ServiceTools: registry,
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/service-tools", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"count":1`,
		`"name":"memory.lookup"`,
		`"description":"Look up scoped memories."`,
		`"permission":"read"`,
		`"allowed":true`,
		`"permission_granted":true`,
		`"schema_properties":["query"]`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "token_secret", "Bearer", "metadata_json", "result"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminServiceToolCatalogRequiresConfiguredRegistry(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/service-tools", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"SERVICE_TOOL_REGISTRY_NOT_CONFIGURED"`)) {
		t.Fatalf("response = %s, want safe registry-not-configured error", recorder.Body.String())
	}
}
