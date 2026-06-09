package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBridgeCatalogListsSafeBridgeStatuses(t *testing.T) {
	store := NewBridgeCatalogStore([]BridgeStatus{
		{
			Bridge:                         BridgeOpenClaw,
			Enabled:                        true,
			RequiredMode:                   ModeTool,
			Invocation:                     BridgeInvocationRuntimeRoute,
			RuntimeRoute:                   true,
			ToolIntents:                    true,
			AllowedToolIntents:             []string{" search.web ", "memory.lookup"},
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
			RuntimeRoute:        false,
			ToolIntents:         false,
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

	catalog, err := store.ListBridges(context.Background())

	if err != nil {
		t.Fatalf("ListBridges() error = %v", err)
	}
	if catalog.Count != 3 || len(catalog.Bridges) != 3 {
		t.Fatalf("catalog = %+v, want three bridge entries", catalog)
	}
	if catalog.Bridges[0].Bridge != BridgeHermes || catalog.Bridges[1].Bridge != BridgeOpenClaw || catalog.Bridges[2].Bridge != BridgeV21 {
		t.Fatalf("bridge order = %+v, want stable sorted ids", catalog.Bridges)
	}
	v21 := catalog.Bridges[2]
	if !v21.Enabled || v21.RequiredMode != ModeProfessional || v21.Invocation != BridgeInvocationServiceTool || v21.ServiceTool != V21VoiceQueryToolName || v21.RuntimeRoute {
		t.Fatalf("v21 status = %+v, want professional service-tool bridge", v21)
	}
	openClaw := catalog.Bridges[1]
	if len(openClaw.AllowedToolIntents) != 2 || openClaw.AllowedToolIntents[0] != "memory.lookup" || openClaw.AllowedToolIntents[1] != "search.web" {
		t.Fatalf("openclaw allowed tool intents = %+v, want normalized sorted allowlist", openClaw.AllowedToolIntents)
	}
	if openClaw.MaxToolIntents != 1 {
		t.Fatalf("openclaw max tool intents = %d, want configured cap", openClaw.MaxToolIntents)
	}
	if openClaw.MaxRuntimeRoutesPerMinute != 12 {
		t.Fatalf("openclaw max runtime routes/min = %d, want configured cap", openClaw.MaxRuntimeRoutesPerMinute)
	}
	if openClaw.MaxRuntimeInputChars != 360 {
		t.Fatalf("openclaw max runtime input chars = %d, want configured cap", openClaw.MaxRuntimeInputChars)
	}
	if openClaw.MaxRuntimeErrorsBeforeCooldown != 2 {
		t.Fatalf("openclaw max runtime errors before cooldown = %d, want configured threshold", openClaw.MaxRuntimeErrorsBeforeCooldown)
	}
	if openClaw.RuntimeErrorCooldownMS != 30000 {
		t.Fatalf("openclaw runtime error cooldown ms = %d, want configured cooldown", openClaw.RuntimeErrorCooldownMS)
	}
	if !openClaw.FallbackOnError || !openClaw.FallbackOnEmpty {
		t.Fatalf("openclaw fallback policy = error:%v empty:%v, want runtime fail-open policies", openClaw.FallbackOnError, openClaw.FallbackOnEmpty)
	}
	if v21.FallbackOnError || v21.FallbackOnEmpty {
		t.Fatalf("v21 fallback policy = error:%v empty:%v, want service-tool bridge without runtime fallback policy", v21.FallbackOnError, v21.FallbackOnEmpty)
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	for _, forbidden := range []string{"V21_ADAPTER_URL", "V21_ADAPTER_TOKEN", "HERMES_AGENT_URL", "OPENCLAW_WS_URL", "secret", "token", "http://", "https://"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("bridge catalog leaked %q: %s", forbidden, string(encoded))
		}
	}
}
