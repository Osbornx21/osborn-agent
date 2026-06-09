package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRuntimeStatusListsPerDeviceBridgeAvailability(t *testing.T) {
	modes := NewModeStore(ModeCasual, []string{"stackchan-s3-main", "desk-secondary"})
	if _, err := modes.SetDeviceMode(context.Background(), "stackchan-s3-main", ModeProfessional); err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}
	bridges := NewBridgeCatalogStore([]BridgeStatus{
		{
			Bridge:                         BridgeOpenClaw,
			Enabled:                        true,
			RequiredMode:                   ModeTool,
			Invocation:                     BridgeInvocationRuntimeRoute,
			RuntimeRoute:                   true,
			ToolIntents:                    true,
			AllowedToolIntents:             []string{"memory.lookup", "stackchan.express"},
			MaxToolIntents:                 1,
			MaxRuntimeRoutesPerMinute:      12,
			MaxRuntimeInputChars:           360,
			MaxRuntimeErrorsBeforeCooldown: 2,
			RuntimeErrorCooldownMS:         30000,
			FallbackOnError:                true,
			FallbackOnEmpty:                true,
			BoundedSpokenOutput:            true,
		},
		{
			Bridge:              BridgeV21,
			Enabled:             true,
			RequiredMode:        ModeProfessional,
			Invocation:          BridgeInvocationServiceTool,
			ServiceTool:         V21VoiceQueryToolName,
			BoundedSpokenOutput: true,
		},
		{
			Bridge:              BridgeHermes,
			Enabled:             false,
			RequiredMode:        ModeRoleplay,
			Invocation:          BridgeInvocationRuntimeRoute,
			RuntimeRoute:        false,
			ToolIntents:         true,
			BoundedSpokenOutput: true,
		},
	})
	status := NewRuntimeStatusStore(modes, bridges)

	catalog, err := status.ListRuntimeStatus(context.Background())

	if err != nil {
		t.Fatalf("ListRuntimeStatus() error = %v", err)
	}
	if catalog.Count != 2 || catalog.BridgeCount != 3 || len(catalog.Devices) != 2 {
		t.Fatalf("catalog = %+v, want two devices and three bridges", catalog)
	}
	secondary := catalog.Devices[0]
	if secondary.DeviceID != "desk-secondary" || secondary.ActiveMode != ModeCasual || secondary.Override {
		t.Fatalf("first device = %+v, want sorted casual secondary", secondary)
	}
	if bridgeStatus(secondary.Bridges, BridgeHermes).Reason != RuntimeStatusReasonBridgeDisabled {
		t.Fatalf("secondary hermes = %+v, want disabled reason", bridgeStatus(secondary.Bridges, BridgeHermes))
	}
	openClaw := bridgeStatus(secondary.Bridges, BridgeOpenClaw)
	if openClaw.Available || openClaw.Reason != RuntimeStatusReasonModeMismatch || openClaw.RequiredMode != ModeTool || !openClaw.RuntimeRoute {
		t.Fatalf("secondary openclaw = %+v, want mode mismatch runtime route", openClaw)
	}
	if len(openClaw.AllowedToolIntents) != 2 || openClaw.AllowedToolIntents[0] != "memory.lookup" || openClaw.AllowedToolIntents[1] != "stackchan.express" {
		t.Fatalf("secondary openclaw allowed tool intents = %+v, want configured safe allowlist", openClaw.AllowedToolIntents)
	}
	if openClaw.MaxToolIntents != 1 {
		t.Fatalf("secondary openclaw max tool intents = %d, want configured cap", openClaw.MaxToolIntents)
	}
	if openClaw.MaxRuntimeRoutesPerMinute != 12 {
		t.Fatalf("secondary openclaw max runtime routes/min = %d, want configured cap", openClaw.MaxRuntimeRoutesPerMinute)
	}
	if openClaw.MaxRuntimeInputChars != 360 {
		t.Fatalf("secondary openclaw max runtime input chars = %d, want configured cap", openClaw.MaxRuntimeInputChars)
	}
	if openClaw.MaxRuntimeErrorsBeforeCooldown != 2 {
		t.Fatalf("secondary openclaw max runtime errors before cooldown = %d, want configured threshold", openClaw.MaxRuntimeErrorsBeforeCooldown)
	}
	if openClaw.RuntimeErrorCooldownMS != 30000 {
		t.Fatalf("secondary openclaw runtime error cooldown ms = %d, want configured cooldown", openClaw.RuntimeErrorCooldownMS)
	}
	if !openClaw.FallbackOnError || !openClaw.FallbackOnEmpty {
		t.Fatalf("secondary openclaw fallback policy = error:%v empty:%v, want runtime fail-open policies", openClaw.FallbackOnError, openClaw.FallbackOnEmpty)
	}
	main := catalog.Devices[1]
	if main.DeviceID != "stackchan-s3-main" || main.ActiveMode != ModeProfessional || !main.Override {
		t.Fatalf("second device = %+v, want professional override", main)
	}
	v21 := bridgeStatus(main.Bridges, BridgeV21)
	if !v21.Available || v21.Reason != RuntimeStatusReasonAvailable || v21.Invocation != BridgeInvocationServiceTool || v21.ServiceTool != V21VoiceQueryToolName {
		t.Fatalf("main v21 = %+v, want available professional service tool", v21)
	}

	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	for _, forbidden := range []string{"V21_ADAPTER_URL", "V21_ADAPTER_TOKEN", "HERMES_AGENT_URL", "OPENCLAW_WS_URL", "secret", "token", "http://", "https://", "prompt", "persona"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("runtime status leaked %q: %s", forbidden, string(encoded))
		}
	}
}

func TestRuntimeStatusOverlaysRuntimePolicyBlocks(t *testing.T) {
	modes := NewModeStore(ModeTool, []string{"stackchan-s3-main"})
	bridges := NewBridgeCatalogStore([]BridgeStatus{
		{
			Bridge:       BridgeOpenClaw,
			Enabled:      true,
			RequiredMode: ModeTool,
			Invocation:   BridgeInvocationRuntimeRoute,
			RuntimeRoute: true,
		},
		{
			Bridge:       BridgeHermes,
			Enabled:      true,
			RequiredMode: ModeRoleplay,
			Invocation:   BridgeInvocationRuntimeRoute,
			RuntimeRoute: true,
		},
	})
	policies := staticRuntimePolicyStatusReader{
		"stackchan-s3-main\x00openclaw": {
			Available: false,
			Reason:    RuntimeStatusReasonRuntimeRateLimited,
		},
	}
	status := NewRuntimeStatusStoreWithPolicies(modes, bridges, policies)

	catalog, err := status.ListRuntimeStatus(context.Background())

	if err != nil {
		t.Fatalf("ListRuntimeStatus() error = %v", err)
	}
	openClaw := bridgeStatus(catalog.Devices[0].Bridges, BridgeOpenClaw)
	if openClaw.Available || openClaw.Reason != RuntimeStatusReasonRuntimeRateLimited {
		t.Fatalf("openclaw = %+v, want runtime policy block to override static availability", openClaw)
	}
	hermes := bridgeStatus(catalog.Devices[0].Bridges, BridgeHermes)
	if hermes.Reason != RuntimeStatusReasonModeMismatch {
		t.Fatalf("hermes = %+v, want static mode mismatch to win before policy checks", hermes)
	}
}

func TestRuntimeStatusRequiresModeAndBridgeSources(t *testing.T) {
	modes := NewModeStore(ModeCasual, []string{"stackchan-s3-main"})
	bridges := NewBridgeCatalogStore([]BridgeStatus{{Bridge: BridgeHermes}})

	if _, err := NewRuntimeStatusStore(nil, bridges).ListRuntimeStatus(context.Background()); !errors.Is(err, ErrRuntimeModesNotConfigured) {
		t.Fatalf("nil modes error = %v, want ErrRuntimeModesNotConfigured", err)
	}
	if _, err := NewRuntimeStatusStore(modes, nil).ListRuntimeStatus(context.Background()); !errors.Is(err, ErrRuntimeBridgesNotConfigured) {
		t.Fatalf("nil bridges error = %v, want ErrRuntimeBridgesNotConfigured", err)
	}
}

type staticRuntimePolicyStatusReader map[string]RuntimePolicyStatus

func (r staticRuntimePolicyStatusReader) RuntimePolicyStatus(_ context.Context, deviceID string, bridge string) RuntimePolicyStatus {
	if status, ok := r[strings.TrimSpace(deviceID)+"\x00"+strings.TrimSpace(bridge)]; ok {
		return status
	}
	return RuntimePolicyStatus{
		Available: true,
		Reason:    RuntimeStatusReasonAvailable,
	}
}

func bridgeStatus(statuses []RuntimeBridgeStatus, bridge string) RuntimeBridgeStatus {
	for _, status := range statuses {
		if status.Bridge == bridge {
			return status
		}
	}
	return RuntimeBridgeStatus{}
}
