package httpapi

import (
	"net/http"

	servicetools "stackchan-gateway/internal/tools"
)

func serviceToolsCatalogHandler(registry *servicetools.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if registry == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "SERVICE_TOOL_REGISTRY_NOT_CONFIGURED", "service tool registry is not configured")
			return
		}
		writeJSON(w, http.StatusOK, registry.Catalog())
	}
}
