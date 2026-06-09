package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type XiaozhiOTAOptions struct {
	WebSocketURL          string
	WebSocketVersion      int
	TimezoneOffsetMinutes int
	Now                   func() time.Time
	Devices               []XiaozhiOTADevice
}

type XiaozhiOTADevice struct {
	DeviceID       string
	ClientID       string
	WebSocketToken string
}

type xiaozhiOTAResponse struct {
	WebSocket  xiaozhiOTAWebSocket  `json:"websocket"`
	ServerTime xiaozhiOTAServerTime `json:"server_time"`
}

type xiaozhiOTAWebSocket struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Version int    `json:"version"`
}

type xiaozhiOTAServerTime struct {
	Timestamp      int64 `json:"timestamp"`
	TimezoneOffset int   `json:"timezone_offset"`
}

func NewXiaozhiOTAHandler(options XiaozhiOTAOptions) http.Handler {
	now := options.Now
	if now == nil {
		now = time.Now
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
			return
		}
		device, ok := matchXiaozhiOTADevice(options.Devices, r.Header.Get("Device-Id"), r.Header.Get("Client-Id"))
		if !ok {
			http.Error(w, "OTA_DEVICE_NOT_CONFIGURED", http.StatusForbidden)
			return
		}
		if strings.TrimSpace(options.WebSocketURL) == "" || strings.TrimSpace(device.WebSocketToken) == "" || options.WebSocketVersion <= 0 {
			http.Error(w, "OTA_CONFIG_UNAVAILABLE", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		response := xiaozhiOTAResponse{
			WebSocket: xiaozhiOTAWebSocket{
				URL:     strings.TrimSpace(options.WebSocketURL),
				Token:   strings.TrimSpace(device.WebSocketToken),
				Version: options.WebSocketVersion,
			},
			ServerTime: xiaozhiOTAServerTime{
				Timestamp:      now().UnixMilli(),
				TimezoneOffset: options.TimezoneOffsetMinutes,
			},
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "OTA_ENCODE_FAILED", http.StatusInternalServerError)
		}
	})
}

func matchXiaozhiOTADevice(devices []XiaozhiOTADevice, deviceID string, clientID string) (XiaozhiOTADevice, bool) {
	deviceID = strings.TrimSpace(deviceID)
	clientID = strings.TrimSpace(clientID)
	if deviceID == "" || clientID == "" {
		return XiaozhiOTADevice{}, false
	}
	for _, device := range devices {
		if strings.TrimSpace(device.DeviceID) == deviceID && strings.TrimSpace(device.ClientID) == clientID {
			return device, true
		}
	}
	return XiaozhiOTADevice{}, false
}
