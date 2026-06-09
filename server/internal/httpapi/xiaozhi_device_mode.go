package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"stackchan-gateway/internal/agents"
)

type XiaozhiDeviceModeOptions struct {
	AgentModes    agents.ModeController
	RuntimeStatus agents.RuntimeStatusReader
	Devices       []XiaozhiDeviceModeDevice
}

type XiaozhiDeviceModeDevice struct {
	DeviceID  string
	ClientID  string
	AuthToken string
}

type xiaozhiDeviceModeIdentity struct {
	DeviceID string
	ClientID string
}

type xiaozhiDeviceModeRequestBody struct {
	Mode string `json:"mode"`
}

type xiaozhiDeviceModeResponse struct {
	DeviceID      string      `json:"device_id"`
	DefaultMode   agents.Mode `json:"default_mode"`
	RequestedMode agents.Mode `json:"requested_mode"`
	ActiveMode    agents.Mode `json:"active_mode"`
	Override      bool        `json:"override"`
	Available     bool        `json:"available"`
	Reason        string      `json:"reason"`
}

func NewXiaozhiDeviceModeHandler(options XiaozhiDeviceModeOptions) http.Handler {
	router := chi.NewRouter()
	router.Get("/status", xiaozhiDeviceModeStatusHandler(options))
	router.Post("/select", xiaozhiDeviceModeSelectHandler(options))
	return router
}

func xiaozhiDeviceModeStatusHandler(options XiaozhiDeviceModeOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if options.AgentModes == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "DEVICE_MODE_NOT_CONFIGURED", "device mode controller is not configured")
			return
		}
		identity, ok := authenticateXiaozhiDeviceModeRequest(w, r, options.Devices)
		if !ok {
			return
		}
		status, err := options.AgentModes.GetDeviceMode(r.Context(), identity.DeviceID)
		if err != nil {
			writeAgentModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, xiaozhiDeviceModeResponseFromStatus(r.Context(), options, status, status.ActiveMode))
	}
}

func xiaozhiDeviceModeSelectHandler(options XiaozhiDeviceModeOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if options.AgentModes == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "DEVICE_MODE_NOT_CONFIGURED", "device mode controller is not configured")
			return
		}
		identity, ok := authenticateXiaozhiDeviceModeRequest(w, r, options.Devices)
		if !ok {
			return
		}

		var body xiaozhiDeviceModeRequestBody
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid device mode JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
			return
		}

		requested := agents.Mode(strings.TrimSpace(body.Mode))
		if !agents.IsValidMode(requested) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_AGENT_MODE", "agent mode is invalid")
			return
		}
		current, err := options.AgentModes.GetDeviceMode(r.Context(), identity.DeviceID)
		if err != nil {
			writeAgentModeError(w, err)
			return
		}
		var status agents.ModeStatus
		if requested == current.DefaultMode {
			status, err = options.AgentModes.ClearDeviceMode(r.Context(), identity.DeviceID)
		} else {
			status, err = options.AgentModes.SetDeviceMode(r.Context(), identity.DeviceID, requested)
		}
		if err != nil {
			writeAgentModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, xiaozhiDeviceModeResponseFromStatus(r.Context(), options, status, requested))
	}
}

func authenticateXiaozhiDeviceModeRequest(w http.ResponseWriter, r *http.Request, devices []XiaozhiDeviceModeDevice) (xiaozhiDeviceModeIdentity, bool) {
	deviceID := strings.TrimSpace(r.Header.Get("Device-Id"))
	clientID := strings.TrimSpace(r.Header.Get("Client-Id"))
	if deviceID == "" || clientID == "" {
		writeAPIError(w, http.StatusForbidden, "DEVICE_AUTH_FAILED", "device auth failed")
		return xiaozhiDeviceModeIdentity{}, false
	}

	for _, device := range devices {
		if strings.TrimSpace(device.DeviceID) != deviceID {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(clientID), []byte(strings.TrimSpace(device.ClientID))) != 1 {
			writeAPIError(w, http.StatusForbidden, "DEVICE_AUTH_FAILED", "device auth failed")
			return xiaozhiDeviceModeIdentity{}, false
		}
		token := strings.TrimSpace(device.AuthToken)
		if token == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "DEVICE_AUTH_NOT_CONFIGURED", "device auth is not configured")
			return xiaozhiDeviceModeIdentity{}, false
		}
		if !xiaozhiDeviceModeAuthorizationMatchesToken(r.Header.Get("Authorization"), token) {
			writeAPIError(w, http.StatusForbidden, "DEVICE_AUTH_FAILED", "device auth failed")
			return xiaozhiDeviceModeIdentity{}, false
		}
		return xiaozhiDeviceModeIdentity{DeviceID: deviceID, ClientID: clientID}, true
	}

	writeAPIError(w, http.StatusForbidden, "DEVICE_AUTH_FAILED", "device auth failed")
	return xiaozhiDeviceModeIdentity{}, false
}

func xiaozhiDeviceModeAuthorizationMatchesToken(authorization string, token string) bool {
	authorization = strings.TrimSpace(authorization)
	token = strings.TrimSpace(token)
	if token == "" || authorization == "" {
		return false
	}
	if strings.HasPrefix(authorization, "Bearer ") {
		authorization = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	}
	return subtle.ConstantTimeCompare([]byte(authorization), []byte(token)) == 1
}

func xiaozhiDeviceModeResponseFromStatus(ctx context.Context, options XiaozhiDeviceModeOptions, status agents.ModeStatus, requested agents.Mode) xiaozhiDeviceModeResponse {
	available, reason := xiaozhiDeviceModeAvailability(ctx, options.RuntimeStatus, status)
	return xiaozhiDeviceModeResponse{
		DeviceID:      status.DeviceID,
		DefaultMode:   status.DefaultMode,
		RequestedMode: requested,
		ActiveMode:    status.ActiveMode,
		Override:      status.Override,
		Available:     available,
		Reason:        reason,
	}
}

func xiaozhiDeviceModeAvailability(ctx context.Context, runtimeStatus agents.RuntimeStatusReader, modeStatus agents.ModeStatus) (bool, string) {
	bridge := xiaozhiDeviceModeBridgeForMode(modeStatus.ActiveMode)
	if bridge == "" {
		return true, agents.RuntimeStatusReasonAvailable
	}
	if runtimeStatus == nil {
		return true, agents.RuntimeStatusReasonAvailable
	}
	catalog, err := runtimeStatus.ListRuntimeStatus(ctx)
	if err != nil {
		return false, agents.RuntimeStatusReasonBridgeDisabled
	}
	for _, device := range catalog.Devices {
		if strings.TrimSpace(device.DeviceID) != strings.TrimSpace(modeStatus.DeviceID) {
			continue
		}
		for _, bridgeStatus := range device.Bridges {
			if strings.TrimSpace(bridgeStatus.Bridge) != bridge {
				continue
			}
			reason := strings.TrimSpace(bridgeStatus.Reason)
			if reason == "" {
				reason = agents.RuntimeStatusReasonAvailable
			}
			return bridgeStatus.Available, reason
		}
	}
	return false, agents.RuntimeStatusReasonBridgeDisabled
}

func xiaozhiDeviceModeBridgeForMode(mode agents.Mode) string {
	switch mode {
	case agents.ModeRoleplay:
		return agents.BridgeHermes
	case agents.ModeTool:
		return agents.BridgeOpenClaw
	case agents.ModeProfessional:
		return agents.BridgeV21
	default:
		return ""
	}
}
