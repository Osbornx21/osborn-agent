package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"stackchan-gateway/internal/agent"
	"stackchan-gateway/internal/agents"
	"stackchan-gateway/internal/providerrouter"
	"stackchan-gateway/internal/providers"
	"stackchan-gateway/internal/stackchan"
	servicetools "stackchan-gateway/internal/tools"
)

const (
	defaultAdminProbeTimeout = 5 * time.Second
	maxAdminProbeTimeout     = 30 * time.Second
)

type ProviderProber interface {
	Probe(ctx context.Context, request providers.ProbeRequest) (providers.ProbeResult, error)
}

type AdminRouterOptions struct {
	AdminToken          string
	Prober              ProviderProber
	ProviderProfiles    providerrouter.Controller
	AgentModes          agents.ModeController
	AgentBridges        agents.BridgeCatalogReader
	AgentRuntimeStatus  agents.RuntimeStatusReader
	Memories            agent.MemoryAdminRepository
	RecentTurns         agent.RecentTurnReader
	ServiceTools        *servicetools.Registry
	DisplayScenes       *stackchan.DisplaySceneCatalog
	DisplayCards        *stackchan.DisplayCardCatalog
	ExpressionCues      *stackchan.ExpressionCatalog
	ExpressionSequences *stackchan.ExpressionSequenceCatalog
}

func NewAdminRouter(options AdminRouterOptions) *chi.Mux {
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(adminBearerAuth(options.AdminToken))
		r.Post("/internal/v1/providers/{provider_id}/probe", providerProbeHandler(options.Prober))
		r.Get("/internal/v1/provider-profiles", providerProfilesListHandler(options.ProviderProfiles))
		r.Get("/internal/v1/agent-modes", agentModesListHandler(options.AgentModes))
		r.Get("/internal/v1/agent-bridges", agentBridgesListHandler(options.AgentBridges))
		r.Get("/internal/v1/agent-runtime-status", agentRuntimeStatusHandler(options.AgentRuntimeStatus))
		r.Get("/internal/v1/service-tools", serviceToolsCatalogHandler(options.ServiceTools))
		r.Get("/internal/v1/stackchan/display-scenes", stackChanDisplaySceneCatalogHandler(options.DisplayScenes))
		r.Get("/internal/v1/stackchan/display-cards", stackChanDisplayCardCatalogHandler(options.DisplayCards))
		r.Get("/internal/v1/stackchan/expression-cues", stackChanExpressionCueCatalogHandler(options.ExpressionCues))
		r.Get("/internal/v1/stackchan/expression-sequences", stackChanExpressionSequenceCatalogHandler(options.ExpressionSequences))
		r.Get("/internal/v1/devices/{device_id}/provider-profile", providerProfileGetHandler(options.ProviderProfiles))
		r.Put("/internal/v1/devices/{device_id}/provider-profile", providerProfilePutHandler(options.ProviderProfiles))
		r.Delete("/internal/v1/devices/{device_id}/provider-profile", providerProfileDeleteHandler(options.ProviderProfiles))
		r.Get("/internal/v1/devices/{device_id}/agent-mode", agentModeGetHandler(options.AgentModes))
		r.Put("/internal/v1/devices/{device_id}/agent-mode", agentModePutHandler(options.AgentModes))
		r.Delete("/internal/v1/devices/{device_id}/agent-mode", agentModeDeleteHandler(options.AgentModes))
		r.Get("/internal/v1/memories", memoriesListHandler(options.Memories))
		r.Get("/internal/v1/recent-turns", recentTurnsListHandler(options.RecentTurns))
		r.Post("/internal/v1/memories/compact", memoryCompactHandler(options.Memories))
		r.Put("/internal/v1/memories/{memory_id}", memoryPutHandler(options.Memories))
		r.Delete("/internal/v1/memories/{memory_id}", memoryDeleteHandler(options.Memories))
	})
	return router
}

type providerProbeRequestBody struct {
	Modality  string `json:"modality"`
	Text      string `json:"text"`
	Voice     string `json:"voice"`
	TimeoutMS int64  `json:"timeout_ms"`
}

type providerProfileRequestBody struct {
	Profile string `json:"profile"`
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

func providerProfilesListHandler(controller providerrouter.Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "PROVIDER_PROFILE_CONTROLLER_NOT_CONFIGURED", "provider profile controller is not configured")
			return
		}
		catalog, err := controller.ListProfiles(r.Context())
		if err != nil {
			writeProviderProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}

func providerProfileGetHandler(controller providerrouter.Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "PROVIDER_PROFILE_CONTROLLER_NOT_CONFIGURED", "provider profile controller is not configured")
			return
		}
		status, err := controller.GetDeviceProfile(r.Context(), chi.URLParam(r, "device_id"))
		if err != nil {
			writeProviderProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func providerProfilePutHandler(controller providerrouter.Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "PROVIDER_PROFILE_CONTROLLER_NOT_CONFIGURED", "provider profile controller is not configured")
			return
		}
		var body providerProfileRequestBody
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid provider profile JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
			return
		}

		profile := strings.TrimSpace(body.Profile)
		if profile == "" {
			writeAPIError(w, http.StatusBadRequest, "INVALID_PROVIDER_PROFILE", "profile is required")
			return
		}
		status, err := controller.SetDeviceProfile(r.Context(), chi.URLParam(r, "device_id"), profile)
		if err != nil {
			writeProviderProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func providerProfileDeleteHandler(controller providerrouter.Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if controller == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "PROVIDER_PROFILE_CONTROLLER_NOT_CONFIGURED", "provider profile controller is not configured")
			return
		}
		status, err := controller.ClearDeviceProfile(r.Context(), chi.URLParam(r, "device_id"))
		if err != nil {
			writeProviderProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

type apiError struct {
	Code               string `json:"code"`
	Message            string `json:"message"`
	ProviderError      string `json:"provider_error,omitempty"`
	ProviderHTTPStatus int    `json:"provider_http_status,omitempty"`
	ProviderErrorCode  string `json:"provider_error_code,omitempty"`
}

func adminBearerAuth(adminToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimSpace(adminToken)
			if token == "" {
				writeAPIError(w, http.StatusServiceUnavailable, "ADMIN_AUTH_NOT_CONFIGURED", "admin auth is not configured")
				return
			}

			authorization := r.Header.Get("Authorization")
			if !strings.HasPrefix(authorization, "Bearer ") {
				writeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token")
				return
			}
			supplied := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
			if subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
				writeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid bearer token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func providerProbeHandler(prober ProviderProber) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if prober == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "PROBER_NOT_CONFIGURED", "provider prober is not configured")
			return
		}

		var body providerProbeRequestBody
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid provider probe JSON")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
			return
		}

		modality := strings.ToLower(strings.TrimSpace(body.Modality))
		if modality == "" {
			writeAPIError(w, http.StatusBadRequest, "INVALID_MODALITY", "modality is required")
			return
		}
		timeout, ok := adminProbeTimeout(body.TimeoutMS)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, "INVALID_TIMEOUT", "timeout_ms must be between 1 and 30000")
			return
		}

		result, err := prober.Probe(r.Context(), providers.ProbeRequest{
			ProviderID: chi.URLParam(r, "provider_id"),
			Modality:   modality,
			Text:       body.Text,
			Voice:      body.Voice,
			Timeout:    timeout,
		})
		if err != nil {
			writeProviderProbeError(w, err, result)
			return
		}

		result.Text = ""
		result.RawPayload = ""
		writeJSON(w, http.StatusOK, result)
	}
}

func adminProbeTimeout(timeoutMS int64) (time.Duration, bool) {
	if timeoutMS == 0 {
		return defaultAdminProbeTimeout, true
	}
	if timeoutMS < 0 {
		return 0, false
	}
	if timeoutMS > int64(maxAdminProbeTimeout/time.Millisecond) {
		return 0, false
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > maxAdminProbeTimeout {
		return 0, false
	}
	return timeout, true
}

func writeProviderProbeError(w http.ResponseWriter, err error, result providers.ProbeResult) {
	switch {
	case errors.Is(err, providers.ErrProviderNotFound):
		writeAPIError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not found")
	case errors.Is(err, providers.ErrUnsupportedProbeModality):
		writeAPIError(w, http.StatusBadRequest, "UNSUPPORTED_MODALITY", "unsupported provider probe modality")
	case errors.Is(err, providers.ErrProviderConfiguration):
		writeProviderAPIError(w, http.StatusBadGateway, "PROVIDER_CONFIG_ERROR", "provider configuration error", result)
	case errors.Is(err, context.DeadlineExceeded):
		writeAPIError(w, http.StatusGatewayTimeout, "PROVIDER_PROBE_TIMEOUT", "provider probe timed out")
	case errors.Is(err, context.Canceled):
		writeAPIError(w, http.StatusRequestTimeout, "PROVIDER_PROBE_CANCELED", "provider probe was canceled")
	case result.ProviderError == "provider_error":
		writeProviderAPIError(w, http.StatusBadGateway, "PROVIDER_ERROR", "provider returned an error", result)
	default:
		writeProviderAPIError(w, http.StatusBadGateway, "PROVIDER_PROBE_FAILED", "provider probe failed", result)
	}
}

func writeProviderProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, providerrouter.ErrDeviceNotFound):
		writeAPIError(w, http.StatusNotFound, "DEVICE_NOT_FOUND", "device not found")
	case errors.Is(err, providerrouter.ErrProfileNotFound):
		writeAPIError(w, http.StatusNotFound, "PROVIDER_PROFILE_NOT_FOUND", "provider profile not found")
	case errors.Is(err, providers.ErrProviderNotFound):
		writeAPIError(w, http.StatusBadGateway, "PROVIDER_NOT_FOUND", "provider in profile not found")
	case errors.Is(err, providers.ErrProviderConfiguration):
		writeAPIError(w, http.StatusBadGateway, "PROVIDER_CONFIG_ERROR", "provider profile configuration error")
	default:
		writeAPIError(w, http.StatusBadGateway, "PROVIDER_PROFILE_FAILED", "provider profile operation failed")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, apiErrorResponse{
		Error: apiError{
			Code:    code,
			Message: message,
		},
	})
}

func writeProviderAPIError(w http.ResponseWriter, status int, code string, message string, result providers.ProbeResult) {
	errorBody := apiError{
		Code:    code,
		Message: message,
	}
	if value := sanitizeAPIErrorToken(result.ProviderError); value != "" {
		errorBody.ProviderError = value
	}
	if result.ProviderHTTPStatus >= 100 && result.ProviderHTTPStatus <= 599 {
		errorBody.ProviderHTTPStatus = result.ProviderHTTPStatus
	}
	if value := sanitizeAPIErrorToken(result.ProviderErrorCode); value != "" {
		errorBody.ProviderErrorCode = value
	}
	writeJSON(w, status, apiErrorResponse{Error: errorBody})
}

func sanitizeAPIErrorToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const maxLen = 128
	var builder strings.Builder
	lastWasUnderscore := false
	for _, char := range value {
		if builder.Len() >= maxLen {
			break
		}
		if isSafeAPIErrorTokenChar(char) {
			builder.WriteRune(char)
			lastWasUnderscore = false
			continue
		}
		if !lastWasUnderscore {
			builder.WriteByte('_')
			lastWasUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func isSafeAPIErrorTokenChar(char rune) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9') ||
		char == '_' ||
		char == '-' ||
		char == ':' ||
		char == '.'
}
