package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type RouterOptions struct {
	WebSocketPath     string
	WebSocketHandler  http.Handler
	OTAPath           string
	OTAHandler        http.Handler
	DeviceModeHandler http.Handler
	ReadyCheck        ReadyCheck
}

func NewRouter(options ...RouterOptions) *chi.Mux {
	opts := RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
	}
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.WebSocketPath == "" {
		opts.WebSocketPath = "/xiaozhi/v1/ws"
	}

	router := chi.NewRouter()
	router.Get("/healthz", Health)
	router.Get("/readyz", NewReadyHandler(opts.ReadyCheck).ServeHTTP)
	if opts.WebSocketHandler != nil {
		router.Handle(opts.WebSocketPath, opts.WebSocketHandler)
	}
	if opts.OTAHandler != nil && opts.OTAPath != "" {
		router.Handle(opts.OTAPath, opts.OTAHandler)
	}
	if opts.DeviceModeHandler != nil {
		router.Mount("/xiaozhi/device-mode", opts.DeviceModeHandler)
	}
	return router
}

func NewMetricsRouter(metricsHandler http.Handler) *chi.Mux {
	router := chi.NewRouter()
	if metricsHandler != nil {
		router.Handle("/metrics", metricsHandler)
	}
	return router
}
