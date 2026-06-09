package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicetools "stackchan-gateway/internal/tools"
)

func TestRegisterGetStateToolReadsAllowlistedEntityWithSafePayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/states/light.desk" {
			t.Fatalf("path = %s, want /api/states/light.desk", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ha-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"entity_id":"light.desk",
			"state":"on",
			"attributes":{
				"friendly_name":"Desk Light",
				"unit_of_measurement":"%",
				"device_class":"light",
				"access_token":"must-not-leak"
			},
			"last_changed":"2026-06-06T10:00:00+00:00",
			"last_updated":"2026-06-06T10:00:01+00:00"
		}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		Token:   "ha-secret",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{GetStateToolName},
		AllowedPermissions: []string{servicetools.PermissionExternal},
	})
	if err := RegisterGetStateTool(registry, GetStateToolOptions{
		Client:          client,
		AllowedEntities: []string{"light.desk"},
	}); err != nil {
		t.Fatalf("RegisterGetStateTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      GetStateToolName,
		Arguments: map[string]any{"entity_id": "light.desk"},
	})

	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	var payload GetStatePayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.EntityID != "light.desk" || payload.State != "on" || payload.FriendlyName != "Desk Light" || payload.LastChanged == "" {
		t.Fatalf("payload = %+v, want safe entity state", payload)
	}
	if strings.Contains(string(result.Payload), "must-not-leak") || strings.Contains(string(result.Payload), "last_updated") {
		t.Fatalf("payload leaked raw HA attributes: %s", string(result.Payload))
	}
}

func TestRegisterGetStateToolRejectsDisallowedEntityBeforeHTTP(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		Token:   "ha-secret",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{GetStateToolName},
		AllowedPermissions: []string{servicetools.PermissionExternal},
	})
	if err := RegisterGetStateTool(registry, GetStateToolOptions{
		Client:          client,
		AllowedEntities: []string{"light.desk"},
	}); err != nil {
		t.Fatalf("RegisterGetStateTool() error = %v", err)
	}

	_, err = registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      GetStateToolName,
		Arguments: map[string]any{"entity_id": "switch.secret"},
	})

	if code := servicetools.ErrorCode(err); code != servicetools.ErrorCodeToolFailed {
		t.Fatalf("error code = %q, want %q", code, servicetools.ErrorCodeToolFailed)
	}
	if called {
		t.Fatal("disallowed entity reached Home Assistant HTTP server")
	}
}

func TestRegisterCallActionToolExecutesConfiguredActionOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/services/light/turn_on" {
			t.Fatalf("path = %s, want /api/services/light/turn_on", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ha-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		entityIDs, ok := body["entity_id"].([]any)
		if !ok || len(entityIDs) != 1 || entityIDs[0] != "light.desk" {
			t.Fatalf("entity_id = %#v, want configured light.desk list", body["entity_id"])
		}
		if body["domain"] != nil || body["service"] != nil || body["target"] != nil {
			t.Fatalf("body included caller override fields: %#v", body)
		}
		if body["brightness_pct"] != float64(70) {
			t.Fatalf("brightness_pct = %#v, want configured 70", body["brightness_pct"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"changed_states":[]}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		Token:   "ha-secret",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{CallActionToolName},
		AllowedPermissions: []string{servicetools.PermissionWrite},
	})
	if err := RegisterCallActionTool(registry, CallActionToolOptions{
		Client: client,
		Actions: []ActionConfig{
			{
				ActionID:    "desk_light_on",
				Description: "Turn on the desk light",
				Domain:      "light",
				Service:     "turn_on",
				EntityIDs:   []string{"light.desk"},
				Data:        map[string]any{"brightness_pct": 70},
			},
		},
	}); err != nil {
		t.Fatalf("RegisterCallActionTool() error = %v", err)
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: CallActionToolName,
		Arguments: map[string]any{
			"action_id": "desk_light_on",
			"domain":    "switch",
			"service":   "turn_off",
			"entity_id": "switch.secret",
			"target":    map[string]any{"entity_id": "switch.secret"},
		},
	})

	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	var payload CallActionPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !payload.OK || payload.ActionID != "desk_light_on" || payload.Domain != "light" || payload.Service != "turn_on" || len(payload.EntityIDs) != 1 || payload.EntityIDs[0] != "light.desk" {
		t.Fatalf("payload = %+v, want safe configured action result", payload)
	}
	if strings.Contains(string(result.Payload), "switch.secret") {
		t.Fatalf("payload leaked caller override: %s", string(result.Payload))
	}
}

func TestRegisterCallActionToolAppliesConfiguredSlots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services/light/turn_on" {
			t.Fatalf("path = %s, want /api/services/light/turn_on", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["brightness_pct"] != float64(42) {
			t.Fatalf("brightness_pct = %#v, want dynamic slot 42", body["brightness_pct"])
		}
		if body["transition"] != float64(1.5) {
			t.Fatalf("transition = %#v, want dynamic slot 1.5", body["transition"])
		}
		entityIDs, ok := body["entity_id"].([]any)
		if !ok || len(entityIDs) != 1 || entityIDs[0] != "light.desk" {
			t.Fatalf("entity_id = %#v, want configured light.desk list", body["entity_id"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"changed_states":[]}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		Token:   "ha-secret",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	minBrightness := float64(1)
	maxBrightness := float64(100)
	minTransition := float64(0)
	maxTransition := float64(5)
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{CallActionToolName},
		AllowedPermissions: []string{servicetools.PermissionWrite},
	})
	if err := RegisterCallActionTool(registry, CallActionToolOptions{
		Client: client,
		Actions: []ActionConfig{
			{
				ActionID:  "desk_light_on",
				Domain:    "light",
				Service:   "turn_on",
				EntityIDs: []string{"light.desk"},
				Data:      map[string]any{"brightness_pct": 70},
				Slots: []ActionSlotConfig{
					{Name: "brightness_pct", Description: "Brightness percent.", Type: "integer", Min: &minBrightness, Max: &maxBrightness},
					{Name: "transition", Description: "Transition seconds.", Type: "number", Min: &minTransition, Max: &maxTransition},
				},
			},
		},
	}); err != nil {
		t.Fatalf("RegisterCallActionTool() error = %v", err)
	}

	definitions := registry.Definitions()
	if len(definitions) != 1 {
		t.Fatalf("definitions len = %d, want 1", len(definitions))
	}
	properties, ok := definitions[0].InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v", definitions[0].InputSchema["properties"])
	}
	brightness, ok := properties["brightness_pct"].(map[string]any)
	if !ok || brightness["type"] != "integer" || brightness["minimum"] != float64(1) || brightness["maximum"] != float64(100) {
		t.Fatalf("brightness slot schema = %#v", properties["brightness_pct"])
	}

	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: CallActionToolName,
		Arguments: map[string]any{
			"action_id":        "desk_light_on",
			"brightness_pct":   float64(42),
			"transition":       float64(1.5),
			"entity_id":        "switch.secret",
			"ignored_reserved": "nope",
		},
	})
	if err == nil {
		t.Fatalf("ExecuteTool() result = %+v, want unknown non-slot argument rejected before HTTP", result)
	}

	result, err = registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: CallActionToolName,
		Arguments: map[string]any{
			"action_id":      "desk_light_on",
			"brightness_pct": float64(42),
			"transition":     float64(1.5),
			"entity_id":      "switch.secret",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.SafeSummary != "desk_light_on" {
		t.Fatalf("SafeSummary = %q, want desk_light_on", result.SafeSummary)
	}
}

func TestRegisterCallActionToolRejectsInvalidSlotBeforeHTTP(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		Token:   "ha-secret",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	minBrightness := float64(1)
	maxBrightness := float64(100)
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{CallActionToolName},
		AllowedPermissions: []string{servicetools.PermissionWrite},
	})
	if err := RegisterCallActionTool(registry, CallActionToolOptions{
		Client: client,
		Actions: []ActionConfig{
			{
				ActionID:  "desk_light_on",
				Domain:    "light",
				Service:   "turn_on",
				EntityIDs: []string{"light.desk"},
				Slots: []ActionSlotConfig{
					{Name: "brightness_pct", Type: "integer", Min: &minBrightness, Max: &maxBrightness},
				},
			},
			{
				ActionID:  "desk_temp",
				Domain:    "climate",
				Service:   "set_temperature",
				EntityIDs: []string{"climate.desk"},
				Slots: []ActionSlotConfig{
					{Name: "temperature", Type: "number", Min: &minBrightness, Max: &maxBrightness},
				},
			},
		},
	}); err != nil {
		t.Fatalf("RegisterCallActionTool() error = %v", err)
	}

	_, err = registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      CallActionToolName,
		Arguments: map[string]any{"action_id": "desk_light_on", "brightness_pct": float64(101)},
	})
	if code := servicetools.ErrorCode(err); code != servicetools.ErrorCodeToolFailed {
		t.Fatalf("out-of-range error code = %q, want %q", code, servicetools.ErrorCodeToolFailed)
	}
	_, err = registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      CallActionToolName,
		Arguments: map[string]any{"action_id": "desk_light_on", "temperature": float64(23)},
	})
	if code := servicetools.ErrorCode(err); code != servicetools.ErrorCodeToolFailed {
		t.Fatalf("wrong-action slot error code = %q, want %q", code, servicetools.ErrorCodeToolFailed)
	}
	if called {
		t.Fatal("invalid slot reached Home Assistant HTTP server")
	}
}

func TestRegisterCallActionToolRejectsUnknownActionBeforeHTTP(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		Token:   "ha-secret",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{CallActionToolName},
		AllowedPermissions: []string{servicetools.PermissionWrite},
	})
	if err := RegisterCallActionTool(registry, CallActionToolOptions{
		Client: client,
		Actions: []ActionConfig{
			{
				ActionID:  "desk_light_on",
				Domain:    "light",
				Service:   "turn_on",
				EntityIDs: []string{"light.desk"},
			},
		},
	}); err != nil {
		t.Fatalf("RegisterCallActionTool() error = %v", err)
	}

	_, err = registry.ExecuteTool(context.Background(), servicetools.Call{
		Name:      CallActionToolName,
		Arguments: map[string]any{"action_id": "secret_action"},
	})

	if code := servicetools.ErrorCode(err); code != servicetools.ErrorCodeToolFailed {
		t.Fatalf("error code = %q, want %q", code, servicetools.ErrorCodeToolFailed)
	}
	if called {
		t.Fatal("unknown action reached Home Assistant HTTP server")
	}
}

func TestClientErrorsDoNotLeakToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad token ha-secret"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BaseURL: server.URL,
		Token:   "ha-secret",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetState(context.Background(), "light.desk")

	if err == nil {
		t.Fatal("GetState() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "ha-secret") || strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error leaked provider payload or token: %v", err)
	}

	err = client.CallService(context.Background(), "light", "turn_on", []string{"light.desk"}, nil)

	if err == nil {
		t.Fatal("CallService() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "ha-secret") || strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error leaked provider payload or token: %v", err)
	}
}

func TestNewClientRequiresURLAndToken(t *testing.T) {
	if _, err := NewClient(ClientOptions{Token: "ha-secret"}); !errors.Is(err, ErrMissingBaseURL) {
		t.Fatalf("missing url error = %v, want ErrMissingBaseURL", err)
	}
	if _, err := NewClient(ClientOptions{BaseURL: "https://ha.example.internal"}); !errors.Is(err, ErrMissingToken) {
		t.Fatalf("missing token error = %v, want ErrMissingToken", err)
	}
}
