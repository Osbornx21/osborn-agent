package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"stackchan-gateway/internal/observability"
	"stackchan-gateway/internal/protocol/xiaozhi"
)

const (
	ErrorCodeToolNotAllowed  = "MCP_TOOL_NOT_ALLOWED"
	ErrorCodeToolUnavailable = "MCP_TOOL_UNAVAILABLE"
	ErrorCodeToolTimeout     = "MCP_TOOL_TIMEOUT"
	ErrorCodeDeviceError     = "MCP_DEVICE_ERROR"
	ErrorCodeSendFailed      = "MCP_SEND_FAILED"
	ErrorCodeBadResponse     = "MCP_BAD_RESPONSE"
)

var ErrBrokerNotConfigured = errors.New("mcp broker not configured")

type Downlink interface {
	SendJSON(ctx context.Context, msg any) error
}

type BrokerOptions struct {
	SessionID string
	Downlink  Downlink
	Allowlist Allowlist
	Timeout   time.Duration
	Metrics   *observability.Metrics
}

type Broker struct {
	sessionID string
	downlink  Downlink
	allowlist Allowlist
	timeout   time.Duration
	metrics   *observability.Metrics

	nextID     atomic.Uint64
	mu         sync.Mutex
	pending    map[string]pendingRequest
	tools      map[string]Tool
	toolsKnown bool
}

type pendingRequest struct {
	method string
	ch     chan Message
}

type ToolError struct {
	Code          string
	Message       string
	DeviceCode    int
	DeviceMessage string
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func HasToolErrorCode(err error, code string) bool {
	var toolError *ToolError
	return errors.As(err, &toolError) && toolError.Code == code
}

func NewBroker(options BrokerOptions) (*Broker, error) {
	if options.SessionID == "" || options.Downlink == nil {
		return nil, ErrBrokerNotConfigured
	}
	allowlist := options.Allowlist
	if len(allowlist.allowed) == 0 {
		allowlist = NewDefaultAllowlist()
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	return &Broker{
		sessionID: options.SessionID,
		downlink:  options.Downlink,
		allowlist: allowlist,
		timeout:   timeout,
		metrics:   options.Metrics,
		pending:   make(map[string]pendingRequest),
		tools:     make(map[string]Tool),
	}, nil
}

func (b *Broker) SendInitialize(ctx context.Context) error {
	_, err := b.sendTracked(ctx, MethodInitialize, DefaultInitializeParams(), nil)
	return err
}

func (b *Broker) ListTools(ctx context.Context) ([]Tool, error) {
	var allTools []Tool
	cursor := ""
	for {
		response, err := b.roundTrip(ctx, MethodToolsList, ToolsListParams{Cursor: cursor, WithUserTools: false})
		if err != nil {
			return nil, err
		}
		if response.Error != nil {
			return nil, deviceError(response.Error)
		}

		result, err := decodeToolsListResult(response.Result)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, result.Tools...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	b.storeTools(allTools)
	return allTools, nil
}

func (b *Broker) CallTool(ctx context.Context, name string, arguments map[string]any) (json.RawMessage, error) {
	allowed := b.allowlist.Allows(name)
	if b.metrics != nil {
		b.metrics.IncMCPToolCall(name, allowed)
	}
	if !allowed {
		return nil, &ToolError{Code: ErrorCodeToolNotAllowed, Message: "tool is not allowlisted"}
	}
	if !b.toolAvailable(name) {
		return nil, &ToolError{Code: ErrorCodeToolUnavailable, Message: "tool was not discovered for this session"}
	}

	startedAt := time.Now()
	response, err := b.roundTrip(ctx, MethodToolsCall, ToolCallParams{Name: name, Arguments: arguments})
	if b.metrics != nil {
		b.metrics.ObserveMCPToolLatency(name, time.Since(startedAt))
	}
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, deviceError(response.Error)
	}
	return cloneRaw(response.Result), nil
}

func (b *Broker) HandleDevicePayload(payload json.RawMessage) error {
	message, err := ParseMessage(payload)
	if err != nil {
		return err
	}
	if !message.IsResponse() {
		return nil
	}

	key := message.IDKey()
	b.mu.Lock()
	pending := b.pending[key]
	delete(b.pending, key)
	b.mu.Unlock()
	if pending.method == "" {
		return nil
	}
	if pending.ch != nil {
		select {
		case pending.ch <- message:
		default:
		}
		return nil
	}

	return b.handleAsyncResponse(pending.method, message)
}

func (b *Broker) StoreTools(tools []Tool) {
	b.storeTools(tools)
}

func (b *Broker) Tools() []Tool {
	b.mu.Lock()
	defer b.mu.Unlock()

	tools := make([]Tool, 0, len(b.tools))
	for _, tool := range b.tools {
		tools = append(tools, tool)
	}
	return tools
}

func (b *Broker) AllowedTools() []Tool {
	b.mu.Lock()
	defer b.mu.Unlock()

	tools := make([]Tool, 0, len(b.tools))
	for _, tool := range b.tools {
		if b.allowlist.Allows(tool.Name) {
			tools = append(tools, tool)
		}
	}
	return tools
}

func (b *Broker) AllowsTool(name string) bool {
	return b.allowlist.Allows(name)
}

func (b *Broker) HasDiscoveredTool(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.toolsKnown {
		return false
	}
	_, ok := b.tools[name]
	return ok
}

func (b *Broker) roundTrip(ctx context.Context, method string, params any) (Message, error) {
	ch := make(chan Message, 1)
	key, err := b.sendTracked(ctx, method, params, ch)
	if err != nil {
		return Message{}, &ToolError{Code: ErrorCodeSendFailed, Message: err.Error()}
	}
	defer b.removePending(key)

	timer := time.NewTimer(b.timeout)
	defer timer.Stop()

	select {
	case response := <-ch:
		return response, nil
	case <-ctx.Done():
		return Message{}, &ToolError{Code: ErrorCodeToolTimeout, Message: ctx.Err().Error()}
	case <-timer.C:
		return Message{}, &ToolError{Code: ErrorCodeToolTimeout, Message: "device tool call timed out"}
	}
}

func (b *Broker) sendTracked(ctx context.Context, method string, params any, ch chan Message) (string, error) {
	id := b.nextRequestID()
	message, err := NewRequest(id, method, params)
	if err != nil {
		return "", err
	}
	key := RequestIDKey(id)

	b.mu.Lock()
	b.pending[key] = pendingRequest{method: method, ch: ch}
	b.mu.Unlock()

	if err := b.send(ctx, message); err != nil {
		b.removePending(key)
		return "", err
	}
	return key, nil
}

func (b *Broker) send(ctx context.Context, message Message) error {
	payload, err := message.Raw()
	if err != nil {
		return err
	}
	return b.downlink.SendJSON(ctx, xiaozhi.NewServerMCP(b.sessionID, payload))
}

func (b *Broker) nextRequestID() uint64 {
	return b.nextID.Add(1)
}

func (b *Broker) removePending(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, key)
}

func (b *Broker) storeTools(tools []Tool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.tools = make(map[string]Tool, len(tools))
	for _, tool := range tools {
		if tool.Name == "" {
			continue
		}
		b.tools[tool.Name] = tool
	}
	b.toolsKnown = true
}

func (b *Broker) appendTools(tools []Tool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, tool := range tools {
		if tool.Name == "" {
			continue
		}
		b.tools[tool.Name] = tool
	}
	b.toolsKnown = true
}

func (b *Broker) toolAvailable(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.toolsKnown {
		return true
	}
	_, ok := b.tools[name]
	return ok
}

func deviceError(rpcError *RPCError) error {
	if rpcError == nil {
		return nil
	}
	return &ToolError{
		Code:          ErrorCodeDeviceError,
		Message:       "device returned MCP error",
		DeviceCode:    rpcError.Code,
		DeviceMessage: rpcError.Message,
	}
}

func (b *Broker) handleAsyncResponse(method string, message Message) error {
	if message.Error != nil {
		return nil
	}

	switch method {
	case MethodInitialize:
		return b.sendToolsListPage(context.Background(), "")
	case MethodToolsList:
		result, err := decodeToolsListResult(message.Result)
		if err != nil {
			return err
		}
		b.appendTools(result.Tools)
		if result.NextCursor != "" {
			return b.sendToolsListPage(context.Background(), result.NextCursor)
		}
	}
	return nil
}

func (b *Broker) sendToolsListPage(ctx context.Context, cursor string) error {
	_, err := b.sendTracked(ctx, MethodToolsList, ToolsListParams{Cursor: cursor, WithUserTools: false}, nil)
	return err
}

func decodeToolsListResult(raw json.RawMessage) (ToolsListResult, error) {
	var result ToolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return ToolsListResult{}, &ToolError{Code: ErrorCodeBadResponse, Message: "decode tools/list result failed"}
	}
	return result, nil
}
