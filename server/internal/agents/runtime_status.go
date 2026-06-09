package agents

import (
	"context"
	"errors"
	"sort"
	"strings"
)

const (
	RuntimeStatusReasonAvailable                = "available"
	RuntimeStatusReasonBridgeDisabled           = "bridge_disabled"
	RuntimeStatusReasonModeMismatch             = "mode_mismatch"
	RuntimeStatusReasonInvocationNotConfigured  = "invocation_not_configured"
	RuntimeStatusReasonRuntimeRouteDisabled     = "runtime_route_disabled"
	RuntimeStatusReasonServiceToolNotConfigured = "service_tool_not_configured"
	RuntimeStatusReasonRuntimeRateLimited       = "runtime_rate_limited"
	RuntimeStatusReasonRuntimeInputTooLong      = "runtime_input_too_long"
	RuntimeStatusReasonRuntimeErrorCooldown     = "runtime_error_cooldown"
)

var (
	ErrRuntimeModesNotConfigured   = errors.New("agent runtime mode source is not configured")
	ErrRuntimeBridgesNotConfigured = errors.New("agent runtime bridge source is not configured")
)

type RuntimeBridgeStatus struct {
	Bridge                         string   `json:"bridge"`
	Available                      bool     `json:"available"`
	Reason                         string   `json:"reason"`
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

type RuntimeDeviceStatus struct {
	DeviceID    string                `json:"device_id"`
	DefaultMode Mode                  `json:"default_mode"`
	ActiveMode  Mode                  `json:"active_mode"`
	Override    bool                  `json:"override"`
	Bridges     []RuntimeBridgeStatus `json:"bridges"`
}

type RuntimeStatusCatalog struct {
	Count       int                   `json:"count"`
	BridgeCount int                   `json:"bridge_count"`
	Devices     []RuntimeDeviceStatus `json:"devices"`
}

type RuntimeStatusReader interface {
	ListRuntimeStatus(ctx context.Context) (RuntimeStatusCatalog, error)
}

type RuntimePolicyStatus struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type RuntimePolicyStatusReader interface {
	RuntimePolicyStatus(ctx context.Context, deviceID string, bridge string) RuntimePolicyStatus
}

type RuntimeStatusStore struct {
	modes    ModeController
	bridges  BridgeCatalogReader
	policies RuntimePolicyStatusReader
}

func NewRuntimeStatusStore(modes ModeController, bridges BridgeCatalogReader) *RuntimeStatusStore {
	return NewRuntimeStatusStoreWithPolicies(modes, bridges, nil)
}

func NewRuntimeStatusStoreWithPolicies(modes ModeController, bridges BridgeCatalogReader, policies RuntimePolicyStatusReader) *RuntimeStatusStore {
	return &RuntimeStatusStore{
		modes:    modes,
		bridges:  bridges,
		policies: policies,
	}
}

func (s *RuntimeStatusStore) ListRuntimeStatus(ctx context.Context) (RuntimeStatusCatalog, error) {
	if s == nil || s.modes == nil {
		return RuntimeStatusCatalog{}, ErrRuntimeModesNotConfigured
	}
	if s.bridges == nil {
		return RuntimeStatusCatalog{}, ErrRuntimeBridgesNotConfigured
	}
	modeCatalog, err := s.modes.ListModes(ctx)
	if err != nil {
		return RuntimeStatusCatalog{}, err
	}
	bridgeCatalog, err := s.bridges.ListBridges(ctx)
	if err != nil {
		return RuntimeStatusCatalog{}, err
	}

	bridges := make([]BridgeStatus, len(bridgeCatalog.Bridges))
	copy(bridges, bridgeCatalog.Bridges)
	sort.SliceStable(bridges, func(i, j int) bool {
		return bridges[i].Bridge < bridges[j].Bridge
	})

	devices := make([]ModeStatus, len(modeCatalog.Devices))
	copy(devices, modeCatalog.Devices)
	sort.SliceStable(devices, func(i, j int) bool {
		return devices[i].DeviceID < devices[j].DeviceID
	})

	statuses := make([]RuntimeDeviceStatus, 0, len(devices))
	for _, device := range devices {
		statuses = append(statuses, RuntimeDeviceStatus{
			DeviceID:    strings.TrimSpace(device.DeviceID),
			DefaultMode: normalizeMode(device.DefaultMode),
			ActiveMode:  normalizeMode(device.ActiveMode),
			Override:    device.Override,
			Bridges:     runtimeBridgeStatusesForDevice(ctx, device.DeviceID, device.ActiveMode, bridges, s.policies),
		})
	}

	return RuntimeStatusCatalog{
		Count:       len(statuses),
		BridgeCount: len(bridges),
		Devices:     statuses,
	}, nil
}

func runtimeBridgeStatusesForMode(activeMode Mode, bridges []BridgeStatus) []RuntimeBridgeStatus {
	return runtimeBridgeStatusesForDevice(context.Background(), "", activeMode, bridges, nil)
}

func runtimeBridgeStatusesForDevice(ctx context.Context, deviceID string, activeMode Mode, bridges []BridgeStatus, policies RuntimePolicyStatusReader) []RuntimeBridgeStatus {
	statuses := make([]RuntimeBridgeStatus, 0, len(bridges))
	activeMode = normalizeMode(activeMode)
	for _, bridge := range bridges {
		bridge.Bridge = strings.ToLower(strings.TrimSpace(bridge.Bridge))
		bridge.RequiredMode = normalizeMode(bridge.RequiredMode)
		bridge.Invocation = strings.ToLower(strings.TrimSpace(bridge.Invocation))
		bridge.ServiceTool = strings.TrimSpace(bridge.ServiceTool)
		available, reason := runtimeBridgeAvailability(activeMode, bridge)
		if available && policies != nil && bridge.Invocation == BridgeInvocationRuntimeRoute {
			policy := policies.RuntimePolicyStatus(ctx, deviceID, bridge.Bridge)
			if !policy.Available && strings.TrimSpace(policy.Reason) != "" {
				available = false
				reason = strings.TrimSpace(policy.Reason)
			}
		}
		statuses = append(statuses, RuntimeBridgeStatus{
			Bridge:                         bridge.Bridge,
			Available:                      available,
			Reason:                         reason,
			RequiredMode:                   bridge.RequiredMode,
			Invocation:                     bridge.Invocation,
			ServiceTool:                    bridge.ServiceTool,
			RuntimeRoute:                   bridge.RuntimeRoute,
			ToolIntents:                    bridge.ToolIntents,
			AllowedToolIntents:             NormalizeBridgeAllowedToolIntents(bridge.AllowedToolIntents),
			MaxToolIntents:                 bridge.MaxToolIntents,
			MaxRuntimeRoutesPerMinute:      bridge.MaxRuntimeRoutesPerMinute,
			MaxRuntimeInputChars:           bridge.MaxRuntimeInputChars,
			MaxRuntimeErrorsBeforeCooldown: bridge.MaxRuntimeErrorsBeforeCooldown,
			RuntimeErrorCooldownMS:         bridge.RuntimeErrorCooldownMS,
			FallbackOnError:                bridge.FallbackOnError,
			FallbackOnEmpty:                bridge.FallbackOnEmpty,
			BoundedSpokenOutput:            bridge.BoundedSpokenOutput,
		})
	}
	return statuses
}

func runtimeBridgeAvailability(activeMode Mode, bridge BridgeStatus) (bool, string) {
	if !bridge.Enabled {
		return false, RuntimeStatusReasonBridgeDisabled
	}
	if bridge.Invocation == "" {
		return false, RuntimeStatusReasonInvocationNotConfigured
	}
	if bridge.Invocation == BridgeInvocationRuntimeRoute && !bridge.RuntimeRoute {
		return false, RuntimeStatusReasonRuntimeRouteDisabled
	}
	if bridge.Invocation == BridgeInvocationServiceTool && strings.TrimSpace(bridge.ServiceTool) == "" {
		return false, RuntimeStatusReasonServiceToolNotConfigured
	}
	if bridge.RequiredMode != "" && normalizeMode(activeMode) != bridge.RequiredMode {
		return false, RuntimeStatusReasonModeMismatch
	}
	return true, RuntimeStatusReasonAvailable
}
