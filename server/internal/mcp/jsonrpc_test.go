package mcp

import (
	"encoding/json"
	"testing"
)

func TestParseJSONRPCRequestWithID(t *testing.T) {
	message, err := ParseMessage([]byte(`{"jsonrpc":"2.0","id":"req_1","method":"tools/list"}`))
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if !message.IsRequest() {
		t.Fatal("message is not request")
	}
	if message.Method != MethodToolsList {
		t.Fatalf("method = %q, want %q", message.Method, MethodToolsList)
	}
	if message.IDKey() != `"req_1"` {
		t.Fatalf("id key = %q, want quoted req_1", message.IDKey())
	}
}

func TestParseJSONRPCResultResponse(t *testing.T) {
	message, err := ParseMessage([]byte(`{"jsonrpc":"2.0","id":"req_1","result":{"tools":[]}}`))
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if !message.IsResponse() {
		t.Fatal("message is not response")
	}
	if message.Error != nil {
		t.Fatalf("error = %#v, want nil", message.Error)
	}
}

func TestParseJSONRPCErrorResponse(t *testing.T) {
	message, err := ParseMessage([]byte(`{"jsonrpc":"2.0","id":"req_1","error":{"code":-32601,"message":"no method"}}`))
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if !message.IsResponse() {
		t.Fatal("message is not response")
	}
	if message.Error == nil || message.Error.Code != -32601 {
		t.Fatalf("error = %#v, want code -32601", message.Error)
	}
}

func TestParseJSONRPCNotificationWithoutID(t *testing.T) {
	message, err := ParseMessage([]byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`))
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if !message.IsNotification() {
		t.Fatal("message is not notification")
	}
}

func TestParseJSONRPCRejectsMixedResultAndError(t *testing.T) {
	_, err := ParseMessage([]byte(`{"jsonrpc":"2.0","id":"req_1","result":{},"error":{"code":1,"message":"bad"}}`))
	if err == nil {
		t.Fatal("error = nil, want invalid json-rpc")
	}
}

func TestNewRequestMarshalsParams(t *testing.T) {
	message, err := NewRequest(1, MethodToolsCall, ToolCallParams{
		Name:      ToolSetHeadAngles,
		Arguments: map[string]any{"yaw": 15},
	})
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	data, err := message.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}

	var decoded struct {
		Params ToolCallParams `json:"params"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if decoded.Params.Name != ToolSetHeadAngles {
		t.Fatalf("tool = %q, want %q", decoded.Params.Name, ToolSetHeadAngles)
	}
	if decoded.Params.Arguments["yaw"].(float64) != 15 {
		t.Fatalf("yaw = %v, want 15", decoded.Params.Arguments["yaw"])
	}
}

func TestNewRequestUsesNumericIDForXiaozhiFirmware(t *testing.T) {
	message, err := NewRequest(7, MethodInitialize, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	data, err := message.Raw()
	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}

	var decoded struct {
		ID any `json:"id"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if _, ok := decoded.ID.(float64); !ok {
		t.Fatalf("id type = %T, want JSON number; json=%s", decoded.ID, string(data))
	}
}
