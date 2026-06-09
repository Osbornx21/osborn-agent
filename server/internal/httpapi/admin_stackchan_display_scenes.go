package httpapi

import (
	"net/http"

	"stackchan-gateway/internal/stackchan"
)

func stackChanDisplaySceneCatalogHandler(catalog *stackchan.DisplaySceneCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if catalog == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "STACKCHAN_DISPLAY_SCENE_CATALOG_NOT_CONFIGURED", "stackchan display-scene catalog is not configured")
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}
