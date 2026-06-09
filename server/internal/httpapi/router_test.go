package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterMountsWebSocketPath(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/xiaozhi/v1/ws", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestRouterMountsOTAPath(t *testing.T) {
	router := NewRouter(RouterOptions{
		OTAPath: "/xiaozhi/ota/",
		OTAHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
	})

	request := httptest.NewRequest(http.MethodPost, "/xiaozhi/ota/", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
}

func TestMetricsRouterMountsMetricsPrivately(t *testing.T) {
	router := NewMetricsRouter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
}

func TestPublicRouterDoesNotMountAdminMemoryRoutes(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/memories", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminAgentModeRoutes(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPut, path: "/internal/v1/devices/stackchan-s3-main/agent-mode"},
		{method: http.MethodGet, path: "/internal/v1/agent-modes"},
	} {
		request := httptest.NewRequest(tc.method, tc.path, nil)
		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, recorder.Code, http.StatusNotFound)
		}
	}
}

func TestPublicRouterDoesNotMountAdminServiceToolCatalog(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/service-tools", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminAgentBridgeCatalog(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-bridges", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminAgentRuntimeStatus(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/agent-runtime-status", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminRecentTurns(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/recent-turns?device_id=stackchan-s3-main", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminStackChanDisplayCardCatalog(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-cards", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminStackChanExpressionCueCatalog(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-cues", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminStackChanExpressionSequenceCatalog(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/expression-sequences", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPublicRouterDoesNotMountAdminStackChanDisplaySceneCatalog(t *testing.T) {
	router := NewRouter(RouterOptions{
		WebSocketPath: "/xiaozhi/v1/ws",
		WebSocketHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/stackchan/display-scenes", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
