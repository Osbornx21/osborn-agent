package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stackchan-gateway/internal/providerrouter"
	"stackchan-gateway/internal/providers"
)

func TestAdminProviderProbeRequiresBearerToken(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Prober:     &staticProber{},
	})

	request := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/mock/probe", bytes.NewReader([]byte(`{"modality":"llm"}`)))
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAdminProviderProbeReturnsSanitizedMetrics(t *testing.T) {
	prober := staticProber{
		result: providers.ProbeResult{
			ProviderID:       "mock",
			Modality:         providers.ProbeModalityLLM,
			OK:               true,
			FirstTokenMS:     7,
			TotalMS:          12,
			OutputTextBytes:  18,
			ProviderModelID:  "mock-llm",
			ProviderVoiceID:  "",
			ProviderError:    "",
			Text:             "",
			RawPayload:       "",
			StartedAtUnixMS:  1780000000000,
			FinishedAtUnixMS: 1780000000012,
		},
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Prober:     &prober,
	})

	request := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/mock/probe", bytes.NewReader([]byte(`{"modality":"llm","text":"do not echo this","timeout_ms":1000}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"provider_id":"mock"`)) {
		t.Fatalf("response body = %s", recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("do not echo this")) || bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) {
		t.Fatalf("response leaked prompt or admin token: %s", recorder.Body.String())
	}

	if prober.last.ProviderID != "mock" || prober.last.Modality != providers.ProbeModalityLLM {
		t.Fatalf("probe request = %+v", prober.last)
	}
	if prober.last.Timeout != time.Second {
		t.Fatalf("probe timeout = %s, want 1s", prober.last.Timeout)
	}
}

func TestAdminProviderProbeMapsUnknownProviderTo404(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Prober:     &staticProber{err: providers.ErrProviderNotFound},
	})

	request := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/missing-llm/probe", bytes.NewReader([]byte(`{"modality":"llm"}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) {
		t.Fatalf("response leaked admin token: %s", recorder.Body.String())
	}
}

func TestAdminProviderProbeMapsProviderConfigurationErrorPrecisely(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Prober: &staticProber{
			result: providers.ProbeResult{
				ProviderID:    "moonshot-llm",
				Modality:      providers.ProbeModalityLLM,
				ProviderError: "provider_config_error",
			},
			err: fmt.Errorf("%w: bad secret admin-token", providers.ErrProviderConfiguration),
		},
	})

	request := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/moonshot-llm/probe", bytes.NewReader([]byte(`{"modality":"llm"}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{`"code":"PROVIDER_CONFIG_ERROR"`, `"provider_error":"provider_config_error"`} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("bad secret")) {
		t.Fatalf("response leaked provider configuration detail: %s", recorder.Body.String())
	}
}

func TestAdminProviderProbeReturnsSafeProviderErrorMetadata(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Prober: &staticProber{
			result: providers.ProbeResult{
				ProviderID:         "stepfun-llm",
				Modality:           providers.ProbeModalityLLM,
				ProviderError:      "provider_error",
				ProviderHTTPStatus: 401,
				ProviderErrorCode:  "invalid_request:_api_key_bad",
			},
			err: errors.New("provider returned private-message with admin-token"),
		},
	})

	request := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/stepfun-llm/probe", bytes.NewReader([]byte(`{"modality":"llm"}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"code":"PROVIDER_ERROR"`,
		`"provider_error":"provider_error"`,
		`"provider_http_status":401`,
		`"provider_error_code":"invalid_request:_api_key_bad"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("private-message")) {
		t.Fatalf("response leaked provider detail: %s", recorder.Body.String())
	}
}

func TestAdminProviderProfileSwitchesDeviceOverride(t *testing.T) {
	controller := &staticProfileController{
		status: providerrouter.Status{
			DeviceID:       "stackchan-s3-main",
			DefaultProfile: "primary",
			ActiveProfile:  "fallback",
			Override:       true,
			ASRProvider:    "mock",
			LLMProvider:    "mock",
			TTSProvider:    "mock",
		},
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:       "admin-token",
		ProviderProfiles: controller,
	})

	request := httptest.NewRequest(http.MethodPut, "/internal/v1/devices/stackchan-s3-main/provider-profile", bytes.NewReader([]byte(`{"profile":"fallback"}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if controller.setDeviceID != "stackchan-s3-main" || controller.setProfile != "fallback" {
		t.Fatalf("set request = device:%q profile:%q", controller.setDeviceID, controller.setProfile)
	}
	for _, want := range []string{`"active_profile":"fallback"`, `"override":true`} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
}

func TestAdminProviderProfileGetAndClear(t *testing.T) {
	controller := &staticProfileController{
		status: providerrouter.Status{
			DeviceID:       "stackchan-s3-main",
			DefaultProfile: "primary",
			ActiveProfile:  "primary",
			Override:       false,
		},
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:       "admin-token",
		ProviderProfiles: controller,
	})

	getRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/devices/stackchan-s3-main/provider-profile", nil)
	getRequest.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}
	if controller.getDeviceID != "stackchan-s3-main" {
		t.Fatalf("get device = %q, want stackchan-s3-main", controller.getDeviceID)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/internal/v1/devices/stackchan-s3-main/provider-profile", nil)
	deleteRequest.Header.Set("Authorization", "Bearer admin-token")
	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if controller.clearDeviceID != "stackchan-s3-main" {
		t.Fatalf("clear device = %q, want stackchan-s3-main", controller.clearDeviceID)
	}
}

func TestAdminProviderProfileCatalogReturnsSafeProfilesAndDevices(t *testing.T) {
	controller := &staticProfileController{
		catalog: providerrouter.ProfileCatalog{
			DefaultProfile:       "primary",
			AutoFallbackEnabled:  true,
			AutoFallbackProfiles: []string{"fallback"},
			Profiles: []providerrouter.ProfileInfo{
				{Profile: "fallback", AutoFallback: true, VoiceRuntime: true, SharedDefaultVoiceIO: true, LLMOnlyFallback: true, ASRProvider: "mock", LLMProvider: "mock", TTSProvider: "mock"},
				{Profile: "llm-only", VoiceRuntime: false, LLMProvider: "mock"},
				{Profile: "primary", Default: true, VoiceRuntime: true, ASRProvider: "mock", LLMProvider: "mock", TTSProvider: "mock"},
			},
			Devices: []providerrouter.Status{
				{DeviceID: "stackchan-s3-main", DefaultProfile: "primary", ActiveProfile: "fallback", Override: true, OverrideSource: "manual"},
			},
		},
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:       "admin-token",
		ProviderProfiles: controller,
	})

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/provider-profiles", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !controller.listCalled {
		t.Fatal("ListProfiles was not called")
	}
	for _, want := range []string{
		`"default_profile":"primary"`,
		`"auto_fallback_enabled":true`,
		`"profile":"fallback"`,
		`"shared_default_voice_io":true`,
		`"llm_only_fallback":true`,
		`"voice_runtime":false`,
		`"active_profile":"fallback"`,
		`"override_source":"manual"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"admin-token", "SECRET_ENV", "api_key"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAdminProviderProfileErrorsAreSanitized(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		ProviderProfiles: &staticProfileController{
			err: fmt.Errorf("%w: secret admin-token", providers.ErrProviderConfiguration),
		},
	})

	request := httptest.NewRequest(http.MethodPut, "/internal/v1/devices/stackchan-s3-main/provider-profile", bytes.NewReader([]byte(`{"profile":"fallback"}`)))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"PROVIDER_CONFIG_ERROR"`)) {
		t.Fatalf("response missing provider config error: %s", recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("secret")) || bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) {
		t.Fatalf("response leaked error detail: %s", recorder.Body.String())
	}
}

type staticProber struct {
	result providers.ProbeResult
	err    error
	last   providers.ProbeRequest
}

type staticProfileController struct {
	status        providerrouter.Status
	catalog       providerrouter.ProfileCatalog
	err           error
	listCalled    bool
	getDeviceID   string
	setDeviceID   string
	setProfile    string
	clearDeviceID string
}

func (c *staticProfileController) ListProfiles(ctx context.Context) (providerrouter.ProfileCatalog, error) {
	_ = ctx
	c.listCalled = true
	if c.err != nil {
		return providerrouter.ProfileCatalog{}, c.err
	}
	return c.catalog, nil
}

func (c *staticProfileController) GetDeviceProfile(ctx context.Context, deviceID string) (providerrouter.Status, error) {
	_ = ctx
	c.getDeviceID = deviceID
	if c.err != nil {
		return providerrouter.Status{}, c.err
	}
	return c.status, nil
}

func (c *staticProfileController) SetDeviceProfile(ctx context.Context, deviceID string, profile string) (providerrouter.Status, error) {
	_ = ctx
	c.setDeviceID = deviceID
	c.setProfile = profile
	if c.err != nil {
		return providerrouter.Status{}, c.err
	}
	return c.status, nil
}

func (c *staticProfileController) ClearDeviceProfile(ctx context.Context, deviceID string) (providerrouter.Status, error) {
	_ = ctx
	c.clearDeviceID = deviceID
	if c.err != nil {
		return providerrouter.Status{}, c.err
	}
	return c.status, nil
}

func (p *staticProber) Probe(ctx context.Context, request providers.ProbeRequest) (providers.ProbeResult, error) {
	_ = ctx
	p.last = request
	result := p.result
	if result.ProviderID == "" {
		result.ProviderID = request.ProviderID
		result.Modality = request.Modality
		result.OK = true
	}
	if p.err != nil {
		return result, p.err
	}
	return result, nil
}
