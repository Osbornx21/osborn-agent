package stackchan

import (
	"sort"
	"strings"

	"stackchan-gateway/internal/mcp"
)

type DisplayCardCatalogOptions struct {
	Display DisplayOptions
	Devices []DisplayCardDevice
}

type DisplayCardDevice struct {
	DeviceID      string
	AllowMCPTools []string
}

type DisplayCardCatalog struct {
	CardCount   int                        `json:"card_count"`
	Cards       []DisplayCardCatalogCard   `json:"cards"`
	DeviceCount int                        `json:"device_count"`
	Devices     []DisplayCardCatalogDevice `json:"devices"`
}

type DisplayCardCatalogCard struct {
	CardID           string  `json:"card_id"`
	Scene            string  `json:"scene"`
	Emotion          string  `json:"emotion,omitempty"`
	Accent           string  `json:"accent,omitempty"`
	AllowCaption     bool    `json:"allow_caption"`
	MaxCaptionChars  int     `json:"max_caption_chars,omitempty"`
	HasStaticCaption bool    `json:"has_static_caption"`
	MotionPreset     string  `json:"motion_preset,omitempty"`
	MotionIntensity  float64 `json:"motion_intensity,omitempty"`
}

type DisplayCardCatalogDevice struct {
	DeviceID                string `json:"device_id"`
	ScreenSceneMCPAvailable bool   `json:"screen_scene_mcp_available"`
	Available               bool   `json:"available"`
	CardCount               int    `json:"card_count"`
}

func NewDisplayCardCatalog(options DisplayCardCatalogOptions) *DisplayCardCatalog {
	display := options.Display.withDefaults()
	cards := displayCardCatalogCards(display.Cards)
	devices := displayCardCatalogDevices(options.Devices, len(cards))
	return &DisplayCardCatalog{
		CardCount:   len(cards),
		Cards:       cards,
		DeviceCount: len(devices),
		Devices:     devices,
	}
}

func displayCardCatalogCards(configured map[string]DisplayCardPolicy) []DisplayCardCatalogCard {
	if len(configured) == 0 {
		return nil
	}
	ids := make([]string, 0, len(configured))
	for id := range configured {
		id = normalizeDisplayCardID(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	cards := make([]DisplayCardCatalogCard, 0, len(ids))
	for _, id := range ids {
		policy := configured[id]
		scene := strings.TrimSpace(policy.Scene)
		if scene == "" {
			scene = SceneTool
		}
		if !IsValidScene(scene) {
			scene = SceneIdle
		}
		motion := normalizeSceneMotion(policy.Motion)
		card := DisplayCardCatalogCard{
			CardID:           id,
			Scene:            scene,
			Emotion:          normalizeEmotion(strings.TrimSpace(policy.Emotion)),
			Accent:           normalizeAccent(strings.TrimSpace(policy.Accent)),
			AllowCaption:     policy.AllowCaption,
			MaxCaptionChars:  policy.MaxCaptionChars,
			HasStaticCaption: strings.TrimSpace(policy.Caption) != "",
		}
		if motion != nil {
			card.MotionPreset = motion.Preset
			card.MotionIntensity = motion.Intensity
		}
		cards = append(cards, card)
	}
	return cards
}

func displayCardCatalogDevices(configured []DisplayCardDevice, cardCount int) []DisplayCardCatalogDevice {
	if len(configured) == 0 {
		return nil
	}
	devices := make([]DisplayCardCatalogDevice, 0, len(configured))
	for _, device := range configured {
		deviceID := strings.TrimSpace(device.DeviceID)
		if deviceID == "" {
			continue
		}
		screenSceneAvailable := displayCardDeviceAllowsScreenScene(device.AllowMCPTools)
		devices = append(devices, DisplayCardCatalogDevice{
			DeviceID:                deviceID,
			ScreenSceneMCPAvailable: screenSceneAvailable,
			Available:               cardCount > 0 && screenSceneAvailable,
			CardCount:               cardCount,
		})
	}
	return devices
}

func displayCardDeviceAllowsScreenScene(tools []string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool) == mcp.ToolSetScreenScene {
			return true
		}
	}
	return false
}
