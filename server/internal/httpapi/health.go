package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
)

type healthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
}

type readyResponse struct {
	Ready  bool              `json:"ready"`
	Checks map[string]string `json:"checks"`
}

type ReadyStatus struct {
	Ready  bool
	Checks map[string]string
}

type ReadyCheck func(context.Context) ReadyStatus

func Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		OK:      true,
		Service: "stackchan-gateway",
	})
}

func Ready(w http.ResponseWriter, request *http.Request) {
	NewReadyHandler(nil).ServeHTTP(w, request)
}

func NewReadyHandler(check ReadyCheck) http.Handler {
	if check == nil {
		check = defaultReadyCheck
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		status := check(request.Context())
		if status.Checks == nil {
			status.Checks = map[string]string{}
		}
		httpStatus := http.StatusOK
		if !status.Ready {
			httpStatus = http.StatusServiceUnavailable
		}
		writeJSON(w, httpStatus, readyResponse{
			Ready:  status.Ready,
			Checks: status.Checks,
		})
	})
}

func defaultReadyCheck(context.Context) ReadyStatus {
	return ReadyStatus{
		Ready: true,
		Checks: map[string]string{
			"config": "ok",
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
