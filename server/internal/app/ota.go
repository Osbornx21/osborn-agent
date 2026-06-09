package app

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"stackchan-gateway/internal/agents"
	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/httpapi"
)

const gatewayTimezoneOffsetMinutes = 480

func newGatewayXiaozhiOTAHandler(cfg *gatewayconfig.Config, lookup gatewayconfig.LookupEnv) (http.Handler, error) {
	if cfg == nil || strings.TrimSpace(cfg.Server.OTAPath) == "" {
		return nil, nil
	}
	if lookup == nil {
		lookup = gatewayconfig.OSLookupEnv
	}
	if len(cfg.Devices) == 0 {
		return nil, fmt.Errorf("xiaozhi ota: no device configured")
	}

	websocketURL, err := resolveGatewayPublicWebsocketURL(cfg, lookup)
	if err != nil {
		return nil, err
	}
	devices := make([]httpapi.XiaozhiOTADevice, 0, len(cfg.Devices))
	for _, device := range cfg.Devices {
		token, _ := lookup(strings.TrimSpace(device.AuthTokenEnv))
		devices = append(devices, httpapi.XiaozhiOTADevice{
			DeviceID:       device.DeviceID,
			ClientID:       device.ClientID,
			WebSocketToken: token,
		})
	}

	return httpapi.NewXiaozhiOTAHandler(httpapi.XiaozhiOTAOptions{
		WebSocketURL:          websocketURL,
		WebSocketVersion:      cfg.Server.WebsocketVersion,
		TimezoneOffsetMinutes: gatewayTimezoneOffsetMinutes,
		Now:                   time.Now,
		Devices:               devices,
	}), nil
}

func newGatewayDeviceModeHandler(cfg *gatewayconfig.Config, lookup gatewayconfig.LookupEnv, modes agents.ModeController, runtimeStatus agents.RuntimeStatusReader) http.Handler {
	if cfg == nil || modes == nil || len(cfg.Devices) == 0 {
		return nil
	}
	if lookup == nil {
		lookup = gatewayconfig.OSLookupEnv
	}
	devices := make([]httpapi.XiaozhiDeviceModeDevice, 0, len(cfg.Devices))
	for _, device := range cfg.Devices {
		token, _ := lookup(strings.TrimSpace(device.AuthTokenEnv))
		devices = append(devices, httpapi.XiaozhiDeviceModeDevice{
			DeviceID:  device.DeviceID,
			ClientID:  device.ClientID,
			AuthToken: token,
		})
	}
	return httpapi.NewXiaozhiDeviceModeHandler(httpapi.XiaozhiDeviceModeOptions{
		AgentModes:    modes,
		RuntimeStatus: runtimeStatus,
		Devices:       devices,
	})
}

func resolveGatewayPublicWebsocketURL(cfg *gatewayconfig.Config, lookup gatewayconfig.LookupEnv) (string, error) {
	if envName := strings.TrimSpace(cfg.Server.WebsocketPublicURLEnv); envName != "" {
		if value, ok := lookup(envName); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}

	base := strings.TrimSpace(cfg.Server.PublicBaseURL)
	if base == "" {
		return "", nil
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("xiaozhi ota: parse server.public_base_url: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("xiaozhi ota: server.public_base_url must use http, https, ws or wss")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("xiaozhi ota: server.public_base_url must include host")
	}
	parsed.Path = path.Join(parsed.Path, cfg.Server.WebsocketPath)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
