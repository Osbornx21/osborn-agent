package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	PermissionRead          = "read"
	PermissionWrite         = "write"
	PermissionExternal      = "external"
	PermissionDeviceControl = "device_control"

	ErrorCodeToolNotFound          = "SERVICE_TOOL_NOT_FOUND"
	ErrorCodeToolNotAllowed        = "SERVICE_TOOL_NOT_ALLOWED"
	ErrorCodePermissionDenied      = "SERVICE_TOOL_PERMISSION_DENIED"
	ErrorCodeToolTimeout           = "SERVICE_TOOL_TIMEOUT"
	ErrorCodeToolFailed            = "SERVICE_TOOL_FAILED"
	ErrorCodeInvalidToolDefinition = "SERVICE_TOOL_INVALID_DEFINITION"

	defaultTimeout = 1200 * time.Millisecond
)

var (
	ErrInvalidDefinition = errors.New("service tool definition is invalid")
)

type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	Permission  string         `json:"permission"`
}

type Catalog struct {
	Count int            `json:"count"`
	Tools []CatalogEntry `json:"tools"`
}

type CatalogEntry struct {
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	Permission        string   `json:"permission"`
	Allowed           bool     `json:"allowed"`
	PermissionGranted bool     `json:"permission_granted"`
	SchemaProperties  []string `json:"schema_properties,omitempty"`
}

type Call struct {
	SessionID  string
	DeviceID   string
	Generation int64
	Name       string
	Arguments  map[string]any
	CreatedAt  time.Time
}

type Result struct {
	Payload     json.RawMessage
	SafeSummary string
}

type Executor func(ctx context.Context, call Call) (Result, error)

type RegistryOptions struct {
	AllowedTools       []string
	AllowedPermissions []string
	DefaultTimeout     time.Duration
	Now                func() time.Time
}

type Registry struct {
	mu                 sync.RWMutex
	entries            map[string]entry
	allowedTools       map[string]struct{}
	allowedPermissions map[string]struct{}
	defaultTimeout     time.Duration
	now                func() time.Time
}

type entry struct {
	definition Definition
	executor   Executor
}

type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func ErrorCode(err error) string {
	var toolError *ToolError
	if errors.As(err, &toolError) {
		return toolError.Code
	}
	return ""
}

func NewRegistry(options RegistryOptions) *Registry {
	timeout := options.DefaultTimeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Registry{
		entries:            make(map[string]entry),
		allowedTools:       stringSet(options.AllowedTools),
		allowedPermissions: stringSet(options.AllowedPermissions),
		defaultTimeout:     timeout,
		now:                now,
	}
}

func (r *Registry) Register(definition Definition, executor Executor) error {
	if r == nil {
		return &ToolError{Code: ErrorCodeInvalidToolDefinition, Message: "service tool registry is not configured"}
	}
	definition.Name = normalizeName(definition.Name)
	definition.Permission = normalizeName(definition.Permission)
	if definition.Name == "" || definition.Permission == "" || executor == nil {
		return fmt.Errorf("%w: name, permission and executor are required", ErrInvalidDefinition)
	}
	definition.InputSchema = cloneMap(definition.InputSchema)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[definition.Name] = entry{
		definition: definition,
		executor:   executor,
	}
	return nil
}

func (r *Registry) HasTool(name string) bool {
	if r == nil {
		return false
	}
	name = normalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.entries[name]
	return ok
}

func (r *Registry) Definitions() []Definition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]Definition, 0, len(r.entries))
	for _, entry := range r.entries {
		definition := entry.definition
		definition.InputSchema = cloneMap(definition.InputSchema)
		definitions = append(definitions, definition)
	}
	return definitions
}

func (r *Registry) Catalog() Catalog {
	if r == nil {
		return Catalog{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]CatalogEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		definition := entry.definition
		tools = append(tools, CatalogEntry{
			Name:              definition.Name,
			Description:       definition.Description,
			Permission:        definition.Permission,
			Allowed:           r.toolAllowed(definition.Name),
			PermissionGranted: r.permissionAllowed(definition.Permission),
			SchemaProperties:  safeSchemaPropertyNames(definition.InputSchema),
		})
	}
	sort.SliceStable(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return Catalog{
		Count: len(tools),
		Tools: tools,
	}
}

func (r *Registry) ExecuteTool(ctx context.Context, call Call) (Result, error) {
	if r == nil {
		return Result{}, &ToolError{Code: ErrorCodeToolNotFound, Message: "service tool registry is not configured"}
	}
	call.Name = normalizeName(call.Name)
	if call.CreatedAt.IsZero() {
		call.CreatedAt = r.now()
	}

	entry, ok := r.lookup(call.Name)
	if !ok {
		return Result{}, &ToolError{Code: ErrorCodeToolNotFound, Message: "service tool is not registered"}
	}
	if !r.toolAllowed(call.Name) {
		return Result{}, &ToolError{Code: ErrorCodeToolNotAllowed, Message: "service tool is not allowlisted"}
	}
	if !r.permissionAllowed(entry.definition.Permission) {
		return Result{}, &ToolError{Code: ErrorCodePermissionDenied, Message: "service tool permission is not granted"}
	}

	call.Arguments = cloneMap(call.Arguments)
	toolCtx, cancel := context.WithTimeout(ctx, r.defaultTimeout)
	defer cancel()

	result, err := entry.executor(toolCtx, call)
	if err != nil {
		if errors.Is(toolCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, &ToolError{Code: ErrorCodeToolTimeout, Message: "service tool timed out"}
		}
		if errors.Is(toolCtx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			return Result{}, &ToolError{Code: ErrorCodeToolTimeout, Message: "service tool was canceled"}
		}
		return Result{}, &ToolError{Code: ErrorCodeToolFailed, Message: "service tool failed"}
	}
	result.Payload = cloneRaw(result.Payload)
	return result, nil
}

func (r *Registry) lookup(name string) (entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[name]
	return entry, ok
}

func (r *Registry) toolAllowed(name string) bool {
	if len(r.allowedTools) == 0 {
		return true
	}
	_, ok := r.allowedTools[name]
	return ok
}

func (r *Registry) permissionAllowed(permission string) bool {
	if len(r.allowedPermissions) == 0 {
		return false
	}
	_, ok := r.allowedPermissions[permission]
	return ok
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeName(value)
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	return set
}

func normalizeName(value string) string {
	return strings.TrimSpace(value)
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	clone := make([]byte, len(raw))
	copy(clone, raw)
	return clone
}

func safeSchemaPropertyNames(schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		name = strings.TrimSpace(name)
		if name == "" || isSensitiveCatalogProperty(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isSensitiveCatalogProperty(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, fragment := range []string{
		"token",
		"secret",
		"password",
		"credential",
		"authorization",
		"api_key",
		"apikey",
		"metadata_json",
	} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}
