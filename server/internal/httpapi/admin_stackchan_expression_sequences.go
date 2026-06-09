package httpapi

import (
	"net/http"

	"stackchan-gateway/internal/stackchan"
)

func stackChanExpressionSequenceCatalogHandler(catalog *stackchan.ExpressionSequenceCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if catalog == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "STACKCHAN_EXPRESSION_SEQUENCE_CATALOG_NOT_CONFIGURED", "stackchan expression-sequence catalog is not configured")
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}
