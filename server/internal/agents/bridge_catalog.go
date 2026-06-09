package agents

import (
	"context"
	"sort"
	"strings"
)

const (
	BridgeHermes   = "hermes"
	BridgeOpenClaw = "openclaw"
	BridgeV21      = "v21"

	BridgeInvocationRuntimeRoute = "runtime_route"
	BridgeInvocationServiceTool  = "service_tool"
)

type BridgeStatus struct {
	Bridge                         string   `json:"bridge"`
	Enabled                        bool     `json:"enabled"`
	RequiredMode                   Mode     `json:"required_mode,omitempty"`
	Invocation                     string   `json:"invocation"`
	ServiceTool                    string   `json:"service_tool,omitempty"`
	RuntimeRoute                   bool     `json:"runtime_route"`
	ToolIntents                    bool     `json:"tool_intents"`
	AllowedToolIntents             []string `json:"allowed_tool_intents,omitempty"`
	MaxToolIntents                 int      `json:"max_tool_intents"`
	MaxRuntimeRoutesPerMinute      int      `json:"max_runtime_routes_per_minute"`
	MaxRuntimeInputChars           int      `json:"max_runtime_input_chars"`
	MaxRuntimeErrorsBeforeCooldown int      `json:"max_runtime_errors_before_cooldown"`
	RuntimeErrorCooldownMS         int      `json:"runtime_error_cooldown_ms"`
	FallbackOnError                bool     `json:"fallback_on_error"`
	FallbackOnEmpty                bool     `json:"fallback_on_empty_response"`
	BoundedSpokenOutput            bool     `json:"bounded_spoken_output"`
}

type BridgeCatalog struct {
	Count   int            `json:"count"`
	Bridges []BridgeStatus `json:"bridges"`
}

type BridgeCatalogReader interface {
	ListBridges(ctx context.Context) (BridgeCatalog, error)
}

type BridgeCatalogStore struct {
	bridges []BridgeStatus
}

func NewBridgeCatalogStore(bridges []BridgeStatus) *BridgeCatalogStore {
	clone := make([]BridgeStatus, 0, len(bridges))
	for _, bridge := range bridges {
		bridge.Bridge = strings.ToLower(strings.TrimSpace(bridge.Bridge))
		bridge.RequiredMode = normalizeMode(bridge.RequiredMode)
		bridge.Invocation = strings.ToLower(strings.TrimSpace(bridge.Invocation))
		bridge.ServiceTool = strings.TrimSpace(bridge.ServiceTool)
		bridge.AllowedToolIntents = NormalizeBridgeAllowedToolIntents(bridge.AllowedToolIntents)
		if bridge.Bridge == "" {
			continue
		}
		clone = append(clone, bridge)
	}
	sort.SliceStable(clone, func(i, j int) bool {
		return clone[i].Bridge < clone[j].Bridge
	})
	return &BridgeCatalogStore{bridges: clone}
}

func (s *BridgeCatalogStore) ListBridges(_ context.Context) (BridgeCatalog, error) {
	if s == nil {
		return BridgeCatalog{}, nil
	}
	bridges := make([]BridgeStatus, len(s.bridges))
	copy(bridges, s.bridges)
	return BridgeCatalog{
		Count:   len(bridges),
		Bridges: bridges,
	}, nil
}
