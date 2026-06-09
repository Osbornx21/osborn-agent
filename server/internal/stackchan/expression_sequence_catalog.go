package stackchan

import (
	"sort"
	"strings"

	"stackchan-gateway/internal/mcp"
)

type ExpressionSequenceCatalogOptions struct {
	Sequences map[string][]string
	Devices   []ExpressionDevice
}

type ExpressionSequenceCatalog struct {
	SequenceCount int                               `json:"sequence_count"`
	Sequences     []ExpressionSequenceCatalogEntry  `json:"sequences"`
	DeviceCount   int                               `json:"device_count"`
	Devices       []ExpressionSequenceCatalogDevice `json:"devices"`
}

type ExpressionSequenceCatalogEntry struct {
	SequenceID string `json:"sequence_id"`
	Configured bool   `json:"configured"`
	CueCount   int    `json:"cue_count"`
}

type ExpressionSequenceCatalogDevice struct {
	DeviceID                string `json:"device_id"`
	HeadMCPAvailable        bool   `json:"head_mcp_available"`
	LEDMCPAvailable         bool   `json:"led_mcp_available"`
	BodyMCPAvailable        bool   `json:"body_mcp_available"`
	ScreenSceneMCPAvailable bool   `json:"screen_scene_mcp_available"`
	Available               bool   `json:"available"`
	SequenceCount           int    `json:"sequence_count"`
}

func NewExpressionSequenceCatalog(options ExpressionSequenceCatalogOptions) *ExpressionSequenceCatalog {
	sequences := expressionSequenceCatalogEntries(options.Sequences)
	devices := expressionSequenceCatalogDevices(options.Devices, len(sequences))
	return &ExpressionSequenceCatalog{
		SequenceCount: len(sequences),
		Sequences:     sequences,
		DeviceCount:   len(devices),
		Devices:       devices,
	}
}

func expressionSequenceCatalogEntries(configured map[string][]string) []ExpressionSequenceCatalogEntry {
	if len(configured) == 0 {
		return nil
	}
	ids := make([]string, 0, len(configured))
	normalized := make(map[string][]string, len(configured))
	for id, cues := range configured {
		id = normalizeExpressionSequenceID(id)
		if id == "" || !expressionSequenceCueListIsValid(cues) {
			continue
		}
		if _, exists := normalized[id]; !exists {
			ids = append(ids, id)
		}
		normalized[id] = cues
	}
	sort.Strings(ids)

	entries := make([]ExpressionSequenceCatalogEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, ExpressionSequenceCatalogEntry{
			SequenceID: id,
			Configured: true,
			CueCount:   len(normalized[id]),
		})
	}
	return entries
}

func expressionSequenceCatalogDevices(configured []ExpressionDevice, sequenceCount int) []ExpressionSequenceCatalogDevice {
	if len(configured) == 0 {
		return nil
	}
	devices := make([]ExpressionSequenceCatalogDevice, 0, len(configured))
	for _, device := range configured {
		deviceID := strings.TrimSpace(device.DeviceID)
		if deviceID == "" {
			continue
		}
		headAvailable := expressionSequenceDeviceAllowsTool(device.AllowMCPTools, mcp.ToolSetHeadAngles)
		ledAvailable := expressionSequenceDeviceAllowsTool(device.AllowMCPTools, mcp.ToolSetLEDColor)
		screenSceneAvailable := expressionSequenceDeviceAllowsTool(device.AllowMCPTools, mcp.ToolSetScreenScene)
		bodyAvailable := headAvailable || ledAvailable
		devices = append(devices, ExpressionSequenceCatalogDevice{
			DeviceID:                deviceID,
			HeadMCPAvailable:        headAvailable,
			LEDMCPAvailable:         ledAvailable,
			BodyMCPAvailable:        bodyAvailable,
			ScreenSceneMCPAvailable: screenSceneAvailable,
			Available:               sequenceCount > 0 && (bodyAvailable || screenSceneAvailable),
			SequenceCount:           sequenceCount,
		})
	}
	return devices
}

func expressionSequenceCueListIsValid(cues []string) bool {
	if len(cues) == 0 || len(cues) > MaxExpressionSequenceCues {
		return false
	}
	for _, cue := range cues {
		if !IsExpressionCue(cue) {
			return false
		}
	}
	return true
}

func expressionSequenceDeviceAllowsTool(tools []string, target string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool) == target {
			return true
		}
	}
	return false
}
