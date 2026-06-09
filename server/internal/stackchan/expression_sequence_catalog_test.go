package stackchan

import (
	"encoding/json"
	"strings"
	"testing"

	"stackchan-gateway/internal/mcp"
)

func TestExpressionSequenceCatalogRedactsCueListsAndReportsDeviceAvailability(t *testing.T) {
	catalog := NewExpressionSequenceCatalog(ExpressionSequenceCatalogOptions{
		Sequences: map[string][]string{
			" Agree.Quick ": {CueAttentive, CueNod},
			"soft.pause":    {CueSettle},
		},
		Devices: []ExpressionDevice{
			{
				DeviceID: "stackchan-s3-main",
				AllowMCPTools: []string{
					mcp.ToolSetHeadAngles,
					mcp.ToolSetLEDColor,
					mcp.ToolSetScreenScene,
				},
			},
			{DeviceID: "audio-only"},
		},
	})

	if catalog.SequenceCount != 2 {
		t.Fatalf("sequence count = %d, want 2", catalog.SequenceCount)
	}
	agree := expressionSequenceCatalogEntryByID(t, catalog.Sequences, "agree.quick")
	if !agree.Configured || agree.CueCount != 2 {
		t.Fatalf("agree sequence = %+v, want configured two-cue summary", agree)
	}
	settle := expressionSequenceCatalogEntryByID(t, catalog.Sequences, "soft.pause")
	if !settle.Configured || settle.CueCount != 1 {
		t.Fatalf("settle sequence = %+v, want configured one-cue summary", settle)
	}

	main := expressionSequenceCatalogDeviceByID(t, catalog.Devices, "stackchan-s3-main")
	if !main.HeadMCPAvailable || !main.LEDMCPAvailable || !main.BodyMCPAvailable || !main.ScreenSceneMCPAvailable || !main.Available || main.SequenceCount != 2 {
		t.Fatalf("main device = %+v, want body/screen available", main)
	}
	audioOnly := expressionSequenceCatalogDeviceByID(t, catalog.Devices, "audio-only")
	if audioOnly.Available || audioOnly.BodyMCPAvailable || audioOnly.ScreenSceneMCPAvailable || audioOnly.SequenceCount != 2 {
		t.Fatalf("audio-only device = %+v, want unavailable with sequence count", audioOnly)
	}

	payload, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{`"cues"`, CueAttentive, CueNod, CueSettle, "self.robot.set_head_angles", "self.robot.set_led_color", "self.screen.set_scene"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("catalog leaked %q in %s", forbidden, body)
		}
	}
}

func expressionSequenceCatalogEntryByID(t *testing.T, entries []ExpressionSequenceCatalogEntry, sequenceID string) ExpressionSequenceCatalogEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.SequenceID == sequenceID {
			return entry
		}
	}
	t.Fatalf("missing expression sequence catalog entry %q in %+v", sequenceID, entries)
	return ExpressionSequenceCatalogEntry{}
}

func expressionSequenceCatalogDeviceByID(t *testing.T, devices []ExpressionSequenceCatalogDevice, deviceID string) ExpressionSequenceCatalogDevice {
	t.Helper()
	for _, device := range devices {
		if device.DeviceID == deviceID {
			return device
		}
	}
	t.Fatalf("missing expression sequence catalog device %q in %+v", deviceID, devices)
	return ExpressionSequenceCatalogDevice{}
}
