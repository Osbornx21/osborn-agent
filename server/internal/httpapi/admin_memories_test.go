package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stackchan-gateway/internal/agent"
)

func TestAdminMemoryRoutesManageMemories(t *testing.T) {
	repository := &staticMemoryAdminRepository{}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Memories:   repository,
	})

	putRequest := httptest.NewRequest(http.MethodPut, "/internal/v1/memories/preferred-name", strings.NewReader(`{
		"user_id":"owner",
		"device_id":"stackchan-s3-main",
		"type":"user_profile",
		"content":"用户偏好的称呼是阿豪。",
		"importance":5,
		"confidence":0.95,
		"metadata_json":"{\"source\":\"operator\"}"
	}`))
	putRequest.Header.Set("Authorization", "Bearer admin-token")
	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, putRequest)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", putRecorder.Code, putRecorder.Body.String())
	}
	if !bytes.Contains(putRecorder.Body.Bytes(), []byte(`"id":"preferred-name"`)) {
		t.Fatalf("PUT response = %s", putRecorder.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/memories?device_id=stackchan-s3-main&user_id=owner&limit=10", nil)
	getRequest.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}
	for _, want := range []string{`"count":1`, `"content":"用户偏好的称呼是阿豪。"`} {
		if !bytes.Contains(getRecorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("GET response missing %s: %s", want, getRecorder.Body.String())
		}
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/internal/v1/memories/preferred-name", nil)
	deleteRequest.Header.Set("Authorization", "Bearer admin-token")
	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if !bytes.Contains(deleteRecorder.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("DELETE response = %s", deleteRecorder.Body.String())
	}
}

func TestAdminMemoryRoutesRequireBearerToken(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Memories:   &staticMemoryAdminRepository{},
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/memories", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAdminMemoryRoutesReturnSanitizedErrors(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Memories: &staticMemoryAdminRepository{
			err: fmt.Errorf("db failed with admin-token and private transcript"),
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/memories", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"MEMORY_REPOSITORY_FAILED"`)) {
		t.Fatalf("response missing safe memory error: %s", recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("private transcript")) {
		t.Fatalf("response leaked repository error detail: %s", recorder.Body.String())
	}
}

func TestAdminMemoryPutRejectsInvalidMemory(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Memories:   &staticMemoryAdminRepository{},
	})
	request := httptest.NewRequest(http.MethodPut, "/internal/v1/memories/bad", strings.NewReader(`{
		"type":"unknown",
		"content":"secret admin-token should not leak"
	}`))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"INVALID_MEMORY_TYPE"`)) {
		t.Fatalf("response missing invalid memory type: %s", recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("secret")) || bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) {
		t.Fatalf("response leaked invalid memory detail: %s", recorder.Body.String())
	}
}

func TestAdminMemoryCompactWritesSummary(t *testing.T) {
	repository := &staticMemoryAdminRepository{
		memories: []agent.Memory{
			{ID: "name", UserID: "owner", DeviceID: "stackchan-s3-main", Type: agent.MemoryUserProfile, Content: "用户偏好的称呼是阿豪。", Importance: 5},
			{ID: "pref", UserID: "owner", DeviceID: "stackchan-s3-main", Type: agent.MemoryUserProfile, Content: "用户喜欢低延迟语音。", Importance: 4},
		},
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken: "admin-token",
		Memories:   repository,
	})
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/memories/compact", strings.NewReader(`{
		"user_id":"owner",
		"device_id":"stackchan-s3-main",
		"max_facts":2
	}`))
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{`"upserted":true`, `"source_count":2`, `"type":"relationship_state"`, "用户画像摘要"} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) {
		t.Fatalf("response leaked admin token: %s", recorder.Body.String())
	}
}

func TestAdminRecentTurnsListsCurrentDeviceOnly(t *testing.T) {
	repository := &staticMemoryAdminRepository{
		recentTurns: []agent.RecentTurn{
			{
				SessionID:     "sess-1",
				DeviceID:      "stackchan-s3-main",
				Generation:    7,
				UserText:      "刚才我问你什么？",
				AssistantText: "你问我语音链路下一步。",
				CreatedAt:     time.Date(2026, 6, 8, 23, 50, 0, 0, time.UTC),
			},
			{
				SessionID:     "other",
				DeviceID:      "other-device",
				Generation:    1,
				UserText:      "不该出现",
				AssistantText: "不该出现",
				CreatedAt:     time.Date(2026, 6, 8, 23, 51, 0, 0, time.UTC),
			},
		},
	}
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:  "admin-token",
		RecentTurns: repository,
	})
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/recent-turns?device_id=stackchan-s3-main&limit=8", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`"device_id":"stackchan-s3-main"`,
		`"count":1`,
		`"generation":7`,
		`"user_text":"刚才我问你什么？"`,
		`"assistant_text":"你问我语音链路下一步。"`,
	} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
			t.Fatalf("response missing %s: %s", want, recorder.Body.String())
		}
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("不该出现")) || bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) {
		t.Fatalf("response leaked other-device transcript or admin token: %s", recorder.Body.String())
	}
}

func TestAdminRecentTurnsRequiresDeviceIDAndSanitizesErrors(t *testing.T) {
	router := NewAdminRouter(AdminRouterOptions{
		AdminToken:  "admin-token",
		RecentTurns: &staticMemoryAdminRepository{recentErr: fmt.Errorf("db failed with admin-token and private transcript")},
	})

	missing := httptest.NewRequest(http.MethodGet, "/internal/v1/recent-turns", nil)
	missing.Header.Set("Authorization", "Bearer admin-token")
	missingRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusBadRequest {
		t.Fatalf("missing status = %d, body = %s", missingRecorder.Code, missingRecorder.Body.String())
	}
	if !bytes.Contains(missingRecorder.Body.Bytes(), []byte(`"code":"INVALID_RECENT_TURNS_DEVICE_ID"`)) {
		t.Fatalf("missing response = %s", missingRecorder.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/recent-turns?device_id=stackchan-s3-main", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("error status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"RECENT_TURNS_REPOSITORY_FAILED"`)) {
		t.Fatalf("error response = %s", recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("admin-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("private transcript")) {
		t.Fatalf("response leaked recent-turn repository detail: %s", recorder.Body.String())
	}
}

type staticMemoryAdminRepository struct {
	memories    []agent.Memory
	recentTurns []agent.RecentTurn
	err         error
	recentErr   error
}

func (r *staticMemoryAdminRepository) Retrieve(_ context.Context, query agent.MemoryQuery) ([]agent.Memory, error) {
	if r.err != nil {
		return nil, r.err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	var out []agent.Memory
	for _, memory := range r.memories {
		if query.UserID != "" && memory.UserID != query.UserID {
			continue
		}
		if query.DeviceID != "" && memory.DeviceID != query.DeviceID {
			continue
		}
		out = append(out, memory)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *staticMemoryAdminRepository) Upsert(_ context.Context, memory agent.Memory) (agent.Memory, error) {
	if r.err != nil {
		return agent.Memory{}, r.err
	}
	if err := validateAdminTestMemory(memory); err != nil {
		return agent.Memory{}, err
	}
	now := time.Date(2026, 6, 6, 19, 0, 0, 0, time.UTC)
	memory.CreatedAt = now
	memory.UpdatedAt = now
	r.memories = append(r.memories, memory)
	return memory, nil
}

func (r *staticMemoryAdminRepository) Delete(_ context.Context, id string) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	for index, memory := range r.memories {
		if memory.ID != id {
			continue
		}
		r.memories = append(r.memories[:index], r.memories[index+1:]...)
		return true, nil
	}
	return false, nil
}

func (r *staticMemoryAdminRepository) RecentTurns(_ context.Context, deviceID string, limit int) ([]agent.RecentTurn, error) {
	if r.recentErr != nil {
		return nil, r.recentErr
	}
	if limit <= 0 {
		limit = 8
	}
	var out []agent.RecentTurn
	for _, turn := range r.recentTurns {
		if turn.DeviceID != deviceID {
			continue
		}
		out = append(out, turn)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *staticMemoryAdminRepository) Close() error {
	return nil
}

func validateAdminTestMemory(memory agent.Memory) error {
	if strings.TrimSpace(memory.Content) == "" {
		return fmt.Errorf("memory content is required")
	}
	switch memory.Type {
	case agent.MemoryUserProfile, agent.MemoryRelationshipState, agent.MemoryEpisodic, agent.MemoryLorebook:
		return nil
	default:
		return fmt.Errorf("unsupported memory type: %s", memory.Type)
	}
}
