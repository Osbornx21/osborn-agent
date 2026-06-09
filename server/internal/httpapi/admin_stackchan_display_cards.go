package httpapi

import (
	"net/http"

	"stackchan-gateway/internal/stackchan"
)

func stackChanDisplayCardCatalogHandler(catalog *stackchan.DisplayCardCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if catalog == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "STACKCHAN_DISPLAY_CARD_CATALOG_NOT_CONFIGURED", "stackchan display-card catalog is not configured")
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}
