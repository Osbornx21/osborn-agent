package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"stackchan-gateway/internal/agents"
)

type agentModeRequestBody struct {
	Mode string `json:"mode"`
}

func agentModesListHandler(controller agents.ModeController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "AGENT_MODE_CONTROLLER_NOT_CONFIGURED", "agent mode controller is not configured")
			return
		}
		catalog, err := controller.ListModes(r.Context())
		if err != nil {
			writeAgentModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}

func agentModeGetHandler(controller agents.ModeController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "AGENT_MODE_CONTROLLER_NOT_CONFIGURED", "agent mode controller is not configured")
			return
		}
		status, err := controller.GetDeviceMode(r.Context(), chi.URLParam(r, "device_id"))
		if err != nil {
			writeAgentModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func agentModePutHandler(controller agents.ModeController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "AGENT_MODE_CONTROLLER_NOT_CONFIGURED", "agent mode controller is not configured")
			return
		}
		var body agentModeRequestBody
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid agent mode JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
			return
		}
		mode := agents.Mode(strings.TrimSpace(body.Mode))
		if !agents.IsValidMode(mode) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_AGENT_MODE", "agent mode is invalid")
			return
		}
		status, err := controller.SetDeviceMode(r.Context(), chi.URLParam(r, "device_id"), mode)
		if err != nil {
			writeAgentModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func agentModeDeleteHandler(controller agents.ModeController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "AGENT_MODE_CONTROLLER_NOT_CONFIGURED", "agent mode controller is not configured")
			return
		}
		status, err := controller.ClearDeviceMode(r.Context(), chi.URLParam(r, "device_id"))
		if err != nil {
			writeAgentModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func writeAgentModeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agents.ErrDeviceNotFound):
		writeAPIError(w, http.StatusNotFound, "DEVICE_NOT_FOUND", "device not found")
	case errors.Is(err, agents.ErrMissingDeviceID):
		writeAPIError(w, http.StatusBadRequest, "INVALID_DEVICE_ID", "device id is required")
	case errors.Is(err, agents.ErrInvalidMode):
		writeAPIError(w, http.StatusBadRequest, "INVALID_AGENT_MODE", "agent mode is invalid")
	default:
		writeAPIError(w, http.StatusBadGateway, "AGENT_MODE_FAILED", "agent mode operation failed")
	}
}
