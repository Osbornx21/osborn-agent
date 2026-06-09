package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"stackchan-gateway/internal/protocol/xiaozhi"
)

func TestBrokerRejectsUnknownToolBeforeDeviceCall(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, 50*time.Millisecond)

	_, err := broker.CallTool(context.Background(), "self.robot.reboot", nil)

	if !HasToolErrorCode(err, ErrorCodeToolNotAllowed) {
		t.Fatalf("error = %v, want %s", err, ErrorCodeToolNotAllowed)
	}
	if downlink.Count() != 0 {
		t.Fatalf("downlink count = %d, want 0", downlink.Count())
	}
}

func TestBrokerSerializesAllowlistedToolCallAsMCP(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, time.Second)
	broker.StoreTools([]Tool{{Name: ToolSetHeadAngles}})

	done := make(chan error, 1)
	go func() {
		_, err := broker.CallTool(context.Background(), ToolSetHeadAngles, map[string]any{
			"yaw":   15,
			"pitch": 10,
			"speed": 150,
		})
		done <- err
	}()

	payload := downlink.WaitForPayload(t, 1)
	request := decodeMCPMessage(t, payload)
	if request.Method != MethodToolsCall {
		t.Fatalf("method = %q, want %q", request.Method, MethodToolsCall)
	}
	var params ToolCallParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.Name != ToolSetHeadAngles {
		t.Fatalf("tool = %q, want %q", params.Name, ToolSetHeadAngles)
	}

	sendResult(t, broker, request, json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`))
	if err := waitForToolCall(t, done); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
}

func TestBrokerToolTimeoutReturnsMCPToolTimeout(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, time.Millisecond)
	broker.StoreTools([]Tool{{Name: ToolSetHeadAngles}})

	_, err := broker.CallTool(context.Background(), ToolSetHeadAngles, map[string]any{"yaw": 5})

	if !HasToolErrorCode(err, ErrorCodeToolTimeout) {
		t.Fatalf("error = %v, want %s", err, ErrorCodeToolTimeout)
	}
	if downlink.Count() != 1 {
		t.Fatalf("downlink count = %d, want 1", downlink.Count())
	}
}

func TestBrokerPropagatesDeviceErrorSafely(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, time.Second)
	broker.StoreTools([]Tool{{Name: ToolSetHeadAngles}})

	done := make(chan error, 1)
	go func() {
		_, err := broker.CallTool(context.Background(), ToolSetHeadAngles, map[string]any{"yaw": 999})
		done <- err
	}()

	request := decodeMCPMessage(t, downlink.WaitForPayload(t, 1))
	sendError(t, broker, request, RPCError{Code: -32602, Message: "invalid yaw"})

	err := waitForToolCall(t, done)
	var toolError *ToolError
	if !asToolError(err, &toolError) {
		t.Fatalf("error = %v, want ToolError", err)
	}
	if toolError.Code != ErrorCodeDeviceError {
		t.Fatalf("code = %q, want %q", toolError.Code, ErrorCodeDeviceError)
	}
	if toolError.DeviceCode != -32602 {
		t.Fatalf("device code = %d, want -32602", toolError.DeviceCode)
	}
}

func TestBrokerPropagatesDeviceErrorWithoutCodeSafely(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, time.Second)
	broker.StoreTools([]Tool{{Name: ToolSetHeadAngles}})

	done := make(chan error, 1)
	go func() {
		_, err := broker.CallTool(context.Background(), ToolSetHeadAngles, map[string]any{"yaw": 999})
		done <- err
	}()

	request := decodeMCPMessage(t, downlink.WaitForPayload(t, 1))
	responseRaw := []byte(`{"jsonrpc":"2.0","id":` + string(*request.ID) + `,"error":{"message":"invalid yaw"}}`)
	if err := broker.HandleDevicePayload(responseRaw); err != nil {
		t.Fatalf("HandleDevicePayload(error) error = %v", err)
	}

	err := waitForToolCall(t, done)
	var toolError *ToolError
	if !asToolError(err, &toolError) {
		t.Fatalf("error = %v, want ToolError", err)
	}
	if toolError.Code != ErrorCodeDeviceError {
		t.Fatalf("code = %q, want %q", toolError.Code, ErrorCodeDeviceError)
	}
	if toolError.DeviceCode != 0 {
		t.Fatalf("device code = %d, want 0 for omitted code", toolError.DeviceCode)
	}
	if toolError.DeviceMessage != "invalid yaw" {
		t.Fatalf("device message = %q, want invalid yaw", toolError.DeviceMessage)
	}
}

func TestBrokerListToolsStoresDiscoveredTools(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, time.Second)

	done := make(chan []Tool, 1)
	errCh := make(chan error, 1)
	go func() {
		tools, err := broker.ListTools(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		done <- tools
	}()

	request := decodeMCPMessage(t, downlink.WaitForPayload(t, 1))
	if request.Method != MethodToolsList {
		t.Fatalf("method = %q, want %q", request.Method, MethodToolsList)
	}
	assertToolsListParams(t, request, "", false)
	sendResult(t, broker, request, ToolsListResult{Tools: []Tool{{Name: ToolDeviceStatus}}})

	select {
	case err := <-errCh:
		t.Fatalf("ListTools() error = %v", err)
	case tools := <-done:
		if len(tools) != 1 || tools[0].Name != ToolDeviceStatus {
			t.Fatalf("tools = %#v, want device status", tools)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ListTools")
	}

	stored := broker.Tools()
	if len(stored) != 1 || stored[0].Name != ToolDeviceStatus {
		t.Fatalf("stored tools = %#v, want device status", stored)
	}
	if !broker.AllowsTool(ToolDeviceStatus) || !broker.HasDiscoveredTool(ToolDeviceStatus) {
		t.Fatalf("broker should allow and discover %s", ToolDeviceStatus)
	}
	if broker.HasDiscoveredTool(ToolSetScreenScene) {
		t.Fatalf("broker should not report undiscovered %s", ToolSetScreenScene)
	}
}

func TestBrokerAllowedToolsFiltersDiscoveredToolsByAllowlist(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker, err := NewBroker(BrokerOptions{
		SessionID: "sess_mcp",
		Downlink:  downlink,
		Allowlist: NewAllowlist([]string{ToolSetHeadAngles}),
	})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	broker.StoreTools([]Tool{
		{Name: ToolSetHeadAngles},
		{Name: ToolTakePhoto},
	})

	allowed := broker.AllowedTools()
	if len(allowed) != 1 || allowed[0].Name != ToolSetHeadAngles {
		t.Fatalf("allowed tools = %#v, want only %s", allowed, ToolSetHeadAngles)
	}
	if broker.AllowsTool(ToolTakePhoto) {
		t.Fatalf("%s should not be allowed by explicit allowlist", ToolTakePhoto)
	}
}

func TestBrokerSendInitializeUsesMCPEnvelope(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, time.Second)

	if err := broker.SendInitialize(context.Background()); err != nil {
		t.Fatalf("SendInitialize() error = %v", err)
	}

	event := downlink.Event(0)
	if event.Type != xiaozhi.MessageTypeMCP {
		t.Fatalf("type = %q, want mcp", event.Type)
	}
	request := decodeMCPMessage(t, event.Payload)
	if request.Method != MethodInitialize {
		t.Fatalf("method = %q, want %q", request.Method, MethodInitialize)
	}
	if !request.IsRequest() {
		t.Fatal("initialize is not request")
	}
	assertNumericID(t, request)
}

func TestBrokerInitializeResponseStartsToolsListAndStoresPagedTools(t *testing.T) {
	downlink := &recordingMCPDownlink{}
	broker := newTestBroker(t, downlink, time.Second)

	if err := broker.SendInitialize(context.Background()); err != nil {
		t.Fatalf("SendInitialize() error = %v", err)
	}
	initialize := decodeMCPMessage(t, downlink.WaitForPayload(t, 1))
	sendResult(t, broker, initialize, json.RawMessage(`{}`))

	firstList := decodeMCPMessage(t, downlink.WaitForPayload(t, 2))
	if firstList.Method != MethodToolsList {
		t.Fatalf("method = %q, want %q", firstList.Method, MethodToolsList)
	}
	assertToolsListParams(t, firstList, "", false)
	sendResult(t, broker, firstList, ToolsListResult{
		Tools:      []Tool{{Name: ToolDeviceStatus}},
		NextCursor: "page_2",
	})

	secondList := decodeMCPMessage(t, downlink.WaitForPayload(t, 3))
	if secondList.Method != MethodToolsList {
		t.Fatalf("method = %q, want %q", secondList.Method, MethodToolsList)
	}
	assertToolsListParams(t, secondList, "page_2", false)
	sendResult(t, broker, secondList, ToolsListResult{
		Tools: []Tool{{Name: ToolSetHeadAngles}},
	})

	stored := broker.Tools()
	if len(stored) != 2 {
		t.Fatalf("stored tools len = %d, want 2; tools=%#v", len(stored), stored)
	}
	if !containsTool(stored, ToolDeviceStatus) || !containsTool(stored, ToolSetHeadAngles) {
		t.Fatalf("stored tools = %#v, want device status and set head angles", stored)
	}
}

func newTestBroker(t *testing.T, downlink *recordingMCPDownlink, timeout time.Duration) *Broker {
	t.Helper()

	broker, err := NewBroker(BrokerOptions{
		SessionID: "sess_mcp",
		Downlink:  downlink,
		Timeout:   timeout,
	})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	return broker
}

func decodeMCPMessage(t *testing.T, payload json.RawMessage) Message {
	t.Helper()

	message, err := ParseMessage(payload)
	if err != nil {
		t.Fatalf("ParseMessage(%s) error = %v", string(payload), err)
	}
	return message
}

func assertNumericID(t *testing.T, message Message) {
	t.Helper()

	if message.ID == nil {
		t.Fatal("id = nil, want numeric id")
	}
	var id any
	if err := json.Unmarshal(*message.ID, &id); err != nil {
		t.Fatalf("decode id: %v", err)
	}
	if _, ok := id.(float64); !ok {
		t.Fatalf("id type = %T, want JSON number", id)
	}
}

func assertToolsListParams(t *testing.T, message Message, cursor string, withUserTools bool) {
	t.Helper()

	var params ToolsListParams
	if err := json.Unmarshal(message.Params, &params); err != nil {
		t.Fatalf("decode tools/list params: %v", err)
	}
	if params.Cursor != cursor {
		t.Fatalf("cursor = %q, want %q", params.Cursor, cursor)
	}
	if params.WithUserTools != withUserTools {
		t.Fatalf("withUserTools = %v, want %v", params.WithUserTools, withUserTools)
	}
	if !jsonContains(message.Params, []byte(`"withUserTools":false`)) {
		t.Fatalf("params json = %s, want explicit withUserTools false", string(message.Params))
	}
}

func jsonContains(data json.RawMessage, want []byte) bool {
	compact := make([]byte, 0, len(data))
	for _, b := range data {
		switch b {
		case ' ', '\n', '\r', '\t':
		default:
			compact = append(compact, b)
		}
	}
	return string(compact) == string(want) ||
		len(compact) > len(want) && containsBytes(compact, want)
}

func containsBytes(data []byte, want []byte) bool {
	for i := 0; i+len(want) <= len(data); i++ {
		match := true
		for j := range want {
			if data[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsTool(tools []Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func sendResult(t *testing.T, broker *Broker, request Message, result any) {
	t.Helper()

	response, err := NewResultResponse(*request.ID, result)
	if err != nil {
		t.Fatalf("NewResultResponse() error = %v", err)
	}
	raw, err := response.Raw()
	if err != nil {
		t.Fatalf("response Raw() error = %v", err)
	}
	if err := broker.HandleDevicePayload(raw); err != nil {
		t.Fatalf("HandleDevicePayload(result) error = %v", err)
	}
}

func sendError(t *testing.T, broker *Broker, request Message, rpcError RPCError) {
	t.Helper()

	response := NewErrorResponse(*request.ID, rpcError)
	raw, err := response.Raw()
	if err != nil {
		t.Fatalf("response Raw() error = %v", err)
	}
	if err := broker.HandleDevicePayload(raw); err != nil {
		t.Fatalf("HandleDevicePayload(error) error = %v", err)
	}
}

func waitForToolCall(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool call")
		return nil
	}
}

func asToolError(err error, target **ToolError) bool {
	if err == nil {
		return false
	}
	if typed, ok := err.(*ToolError); ok {
		*target = typed
		return true
	}
	return false
}

type recordingMCPDownlink struct {
	mu     sync.Mutex
	events []mcpEvent
}

type mcpEvent struct {
	Type    string
	Payload json.RawMessage
}

func (d *recordingMCPDownlink) SendJSON(_ context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	var envelope struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, mcpEvent{
		Type:    envelope.Type,
		Payload: cloneRaw(envelope.Payload),
	})
	return nil
}

func (d *recordingMCPDownlink) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.events)
}

func (d *recordingMCPDownlink) Event(index int) mcpEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	if index >= len(d.events) {
		return mcpEvent{}
	}
	return d.events[index]
}

func (d *recordingMCPDownlink) WaitForPayload(t *testing.T, count int) json.RawMessage {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		if len(d.events) >= count {
			payload := cloneRaw(d.events[count-1].Payload)
			d.mu.Unlock()
			return payload
		}
		d.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d mcp payload(s)", count)
	return nil
}
