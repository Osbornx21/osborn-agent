package httpapi

import (
	"net/http"

	"stackchan-gateway/internal/agents"
)

func agentBridgesListHandler(catalog agents.BridgeCatalogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if catalog == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "AGENT_BRIDGE_CATALOG_NOT_CONFIGURED", "agent bridge catalog is not configured")
			return
		}
		bridges, err := catalog.ListBridges(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "AGENT_BRIDGE_CATALOG_FAILED", "agent bridge catalog operation failed")
			return
		}
		writeJSON(w, http.StatusOK, bridges)
	}
}
