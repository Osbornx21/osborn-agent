package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
)

const JSONRPCVersion = "2.0"

var (
	ErrInvalidJSONRPC = errors.New("invalid json-rpc message")
	ErrMissingMethod  = errors.New("json-rpc method is required")
)

type Message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewRequest(id uint64, method string, params any) (Message, error) {
	if method == "" {
		return Message{}, ErrMissingMethod
	}
	idRaw := json.RawMessage(fmt.Sprintf("%d", id))
	paramsRaw, err := marshalOptional(params)
	if err != nil {
		return Message{}, err
	}
	return Message{
		JSONRPC: JSONRPCVersion,
		ID:      &idRaw,
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

func NewNotification(method string, params any) (Message, error) {
	if method == "" {
		return Message{}, ErrMissingMethod
	}
	paramsRaw, err := marshalOptional(params)
	if err != nil {
		return Message{}, err
	}
	return Message{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

func NewResultResponse(id json.RawMessage, result any) (Message, error) {
	resultRaw, err := marshalOptional(result)
	if err != nil {
		return Message{}, err
	}
	idCopy := cloneRaw(id)
	return Message{
		JSONRPC: JSONRPCVersion,
		ID:      &idCopy,
		Result:  resultRaw,
	}, nil
}

func NewErrorResponse(id json.RawMessage, rpcError RPCError) Message {
	idCopy := cloneRaw(id)
	return Message{
		JSONRPC: JSONRPCVersion,
		ID:      &idCopy,
		Error:   &rpcError,
	}
}

func ParseMessage(data []byte) (Message, error) {
	var message Message
	if err := json.Unmarshal(data, &message); err != nil {
		return Message{}, fmt.Errorf("%w: %v", ErrInvalidJSONRPC, err)
	}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (m Message) Validate() error {
	if m.JSONRPC != JSONRPCVersion {
		return fmt.Errorf("%w: jsonrpc must be %q", ErrInvalidJSONRPC, JSONRPCVersion)
	}
	hasMethod := m.Method != ""
	hasResult := len(m.Result) > 0
	hasError := m.Error != nil

	switch {
	case hasMethod:
		if hasResult || hasError {
			return fmt.Errorf("%w: request cannot include result or error", ErrInvalidJSONRPC)
		}
		return nil
	case hasResult || hasError:
		if m.ID == nil {
			return fmt.Errorf("%w: response id is required", ErrInvalidJSONRPC)
		}
		if hasResult && hasError {
			return fmt.Errorf("%w: response cannot include both result and error", ErrInvalidJSONRPC)
		}
		return nil
	default:
		return fmt.Errorf("%w: message must be request, response, or notification", ErrInvalidJSONRPC)
	}
}

func (m Message) IsRequest() bool {
	return m.Method != "" && m.ID != nil
}

func (m Message) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

func (m Message) IsResponse() bool {
	return m.Method == "" && m.ID != nil && (len(m.Result) > 0 || m.Error != nil)
}

func (m Message) IDKey() string {
	if m.ID == nil {
		return ""
	}
	return string(*m.ID)
}

func (m Message) Raw() (json.RawMessage, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func RequestIDKey(id uint64) string {
	return fmt.Sprintf("%d", id)
}

func marshalOptional(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		return cloneRaw(raw), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	clone := make([]byte, len(raw))
	copy(clone, raw)
	return clone
}
