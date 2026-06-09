package httpapi

import (
	"net/http"

	"stackchan-gateway/internal/stackchan"
)

func stackChanExpressionCueCatalogHandler(catalog *stackchan.ExpressionCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if catalog == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "STACKCHAN_EXPRESSION_CUE_CATALOG_NOT_CONFIGURED", "stackchan expression-cue catalog is not configured")
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}
