package httpapi

import (
	"errors"
	"net/http"

	"stackchan-gateway/internal/agents"
)

func agentRuntimeStatusHandler(status agents.RuntimeStatusReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if status == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "AGENT_RUNTIME_STATUS_NOT_CONFIGURED", "agent runtime status is not configured")
			return
		}
		catalog, err := status.ListRuntimeStatus(r.Context())
		if err != nil {
			writeAgentRuntimeStatusError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}

func writeAgentRuntimeStatusError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agents.ErrRuntimeModesNotConfigured), errors.Is(err, agents.ErrRuntimeBridgesNotConfigured):
		writeAPIError(w, http.StatusServiceUnavailable, "AGENT_RUNTIME_STATUS_NOT_CONFIGURED", "agent runtime status is not configured")
	default:
		writeAPIError(w, http.StatusBadGateway, "AGENT_RUNTIME_STATUS_FAILED", "agent runtime status operation failed")
	}
}
