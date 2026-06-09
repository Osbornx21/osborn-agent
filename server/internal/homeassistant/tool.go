package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	servicetools "stackchan-gateway/internal/tools"
)

const (
	GetStateToolName   = "homeassistant.get_state"
	CallActionToolName = "homeassistant.call_action"
)

var (
	ErrEntityNotAllowed = errors.New("home assistant entity is not allowlisted")
	ErrMissingActionID  = errors.New("home assistant action_id is required")
	ErrActionNotAllowed = errors.New("home assistant action is not allowlisted")
	ErrSlotNotAllowed   = errors.New("home assistant action slot is not allowlisted")
	ErrInvalidSlotValue = errors.New("home assistant action slot value is invalid")
)

type GetStateToolOptions struct {
	Client          *Client
	AllowedEntities []string
}

type ActionConfig struct {
	ActionID    string
	Description string
	Domain      string
	Service     string
	EntityIDs   []string
	Data        map[string]any
	Slots       []ActionSlotConfig
}

type ActionSlotConfig struct {
	Name        string
	Description string
	Type        string
	Enum        []string
	Min         *float64
	Max         *float64
	MaxChars    int
}

type CallActionToolOptions struct {
	Client  *Client
	Actions []ActionConfig
}

type GetStatePayload struct {
	EntityID          string `json:"entity_id"`
	State             string `json:"state"`
	FriendlyName      string `json:"friendly_name,omitempty"`
	UnitOfMeasurement string `json:"unit_of_measurement,omitempty"`
	DeviceClass       string `json:"device_class,omitempty"`
	LastChanged       string `json:"last_changed,omitempty"`
}

type CallActionPayload struct {
	ActionID  string   `json:"action_id"`
	Domain    string   `json:"domain"`
	Service   string   `json:"service"`
	EntityIDs []string `json:"entity_ids"`
	OK        bool     `json:"ok"`
}

func RegisterGetStateTool(registry *servicetools.Registry, options GetStateToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	if options.Client == nil {
		return fmt.Errorf("home assistant client is required")
	}
	allowed := allowedEntitySet(options.AllowedEntities)
	if len(allowed) == 0 {
		return fmt.Errorf("home assistant allowed_entities is required")
	}
	return registry.Register(servicetools.Definition{
		Name:        GetStateToolName,
		Description: "Read the state of an allowlisted Home Assistant entity.",
		Permission:  servicetools.PermissionExternal,
		InputSchema: getStateInputSchema(options.AllowedEntities),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		entityID := stringArgument(call.Arguments, "entity_id")
		if entityID == "" {
			return servicetools.Result{}, ErrMissingEntity
		}
		if _, ok := allowed[entityID]; !ok {
			return servicetools.Result{}, ErrEntityNotAllowed
		}
		state, err := options.Client.GetState(ctx, entityID)
		if err != nil {
			return servicetools.Result{}, err
		}
		payload, err := json.Marshal(safeStatePayload(state))
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{Payload: payload}, nil
	})
}

func RegisterCallActionTool(registry *servicetools.Registry, options CallActionToolOptions) error {
	if registry == nil {
		return fmt.Errorf("service tool registry is required")
	}
	if options.Client == nil {
		return fmt.Errorf("home assistant client is required")
	}
	actions, err := actionMap(options.Actions)
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		return fmt.Errorf("home assistant allowed_actions is required")
	}
	return registry.Register(servicetools.Definition{
		Name:        CallActionToolName,
		Description: "Run one operator-configured Home Assistant action by action_id.",
		Permission:  servicetools.PermissionWrite,
		InputSchema: callActionInputSchema(options.Actions),
	}, func(ctx context.Context, call servicetools.Call) (servicetools.Result, error) {
		actionID := stringArgument(call.Arguments, "action_id")
		if actionID == "" {
			return servicetools.Result{}, ErrMissingActionID
		}
		action, ok := actions[actionID]
		if !ok {
			return servicetools.Result{}, ErrActionNotAllowed
		}
		data, err := actionDataForCall(action, call.Arguments)
		if err != nil {
			return servicetools.Result{}, err
		}
		if err := options.Client.CallService(ctx, action.Domain, action.Service, action.EntityIDs, data); err != nil {
			return servicetools.Result{}, err
		}
		payload, err := json.Marshal(CallActionPayload{
			ActionID:  action.ActionID,
			Domain:    action.Domain,
			Service:   action.Service,
			EntityIDs: nonEmptyUnique(action.EntityIDs),
			OK:        true,
		})
		if err != nil {
			return servicetools.Result{}, err
		}
		return servicetools.Result{Payload: payload, SafeSummary: action.ActionID}, nil
	})
}

func getStateInputSchema(allowedEntities []string) map[string]any {
	entities := nonEmptyUnique(allowedEntities)
	enum := make([]any, 0, len(entities))
	for _, entity := range entities {
		enum = append(enum, entity)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"entity_id": map[string]any{
				"type":        "string",
				"description": "Home Assistant entity id from the allowlist.",
				"enum":        enum,
			},
		},
		"required":             []any{"entity_id"},
		"additionalProperties": false,
	}
}

func callActionInputSchema(actions []ActionConfig) map[string]any {
	actionIDs := nonEmptyUniqueActionIDs(actions)
	enum := make([]any, 0, len(actionIDs))
	for _, actionID := range actionIDs {
		enum = append(enum, actionID)
	}
	properties := map[string]any{
		"action_id": map[string]any{
			"type":        "string",
			"description": actionIDDescription(actions),
			"enum":        enum,
		},
	}
	for _, slot := range uniqueActionSlots(actions) {
		properties[slot.Name] = actionSlotInputSchema(slot)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             []any{"action_id"},
		"additionalProperties": false,
	}
}

func safeStatePayload(state State) GetStatePayload {
	attributes := state.Attributes
	return GetStatePayload{
		EntityID:          strings.TrimSpace(state.EntityID),
		State:             strings.TrimSpace(state.State),
		FriendlyName:      stringAttribute(attributes, "friendly_name"),
		UnitOfMeasurement: stringAttribute(attributes, "unit_of_measurement"),
		DeviceClass:       stringAttribute(attributes, "device_class"),
		LastChanged:       strings.TrimSpace(state.LastChanged),
	}
}

func actionMap(actions []ActionConfig) (map[string]ActionConfig, error) {
	out := make(map[string]ActionConfig, len(actions))
	for _, action := range actions {
		action.ActionID = strings.TrimSpace(action.ActionID)
		action.Domain = strings.TrimSpace(action.Domain)
		action.Service = strings.TrimSpace(action.Service)
		action.EntityIDs = nonEmptyUnique(action.EntityIDs)
		action.Data = cloneActionData(action.Data)
		action.Slots = normalizeActionSlots(action.Slots)
		if action.ActionID == "" {
			continue
		}
		if _, ok := out[action.ActionID]; ok {
			return nil, fmt.Errorf("home assistant action_id must be unique")
		}
		out[action.ActionID] = action
	}
	return out, nil
}

func nonEmptyUniqueActionIDs(actions []ActionConfig) []string {
	ids := make([]string, 0, len(actions))
	for _, action := range actions {
		ids = append(ids, action.ActionID)
	}
	return nonEmptyUnique(ids)
}

func uniqueActionSlots(actions []ActionConfig) []ActionSlotConfig {
	seen := make(map[string]struct{})
	out := make([]ActionSlotConfig, 0)
	for _, action := range actions {
		for _, slot := range normalizeActionSlots(action.Slots) {
			if slot.Name == "" {
				continue
			}
			if _, ok := seen[slot.Name]; ok {
				continue
			}
			seen[slot.Name] = struct{}{}
			out = append(out, slot)
		}
	}
	return out
}

func actionSlotInputSchema(slot ActionSlotConfig) map[string]any {
	schema := map[string]any{
		"type": slot.Type,
	}
	if slot.Description != "" {
		schema["description"] = slot.Description
	}
	if len(slot.Enum) > 0 {
		enum := make([]any, 0, len(slot.Enum))
		for _, value := range slot.Enum {
			enum = append(enum, value)
		}
		schema["enum"] = enum
	}
	if slot.MaxChars > 0 {
		schema["maxLength"] = slot.MaxChars
	}
	if slot.Min != nil {
		schema["minimum"] = *slot.Min
	}
	if slot.Max != nil {
		schema["maximum"] = *slot.Max
	}
	return schema
}

func actionDataForCall(action ActionConfig, arguments map[string]any) (map[string]any, error) {
	data := cloneActionData(action.Data)
	slots := actionSlotMap(action.Slots)
	for key := range arguments {
		key = strings.TrimSpace(key)
		if key == "" || key == "action_id" || isReservedActionOverrideKey(key) {
			continue
		}
		if _, ok := slots[key]; !ok {
			return nil, ErrSlotNotAllowed
		}
	}
	for name, slot := range slots {
		value, ok := arguments[name]
		if !ok {
			continue
		}
		normalized, err := normalizeSlotValue(slot, value)
		if err != nil {
			return nil, err
		}
		data[name] = normalized
	}
	return data, nil
}

func actionSlotMap(slots []ActionSlotConfig) map[string]ActionSlotConfig {
	out := make(map[string]ActionSlotConfig, len(slots))
	for _, slot := range normalizeActionSlots(slots) {
		if slot.Name != "" {
			out[slot.Name] = slot
		}
	}
	return out
}

func normalizeActionSlots(slots []ActionSlotConfig) []ActionSlotConfig {
	out := make([]ActionSlotConfig, 0, len(slots))
	for _, slot := range slots {
		slot.Name = strings.TrimSpace(slot.Name)
		slot.Description = strings.TrimSpace(slot.Description)
		slot.Type = strings.ToLower(strings.TrimSpace(slot.Type))
		slot.Enum = nonEmptyUnique(slot.Enum)
		if slot.Name != "" {
			out = append(out, slot)
		}
	}
	return out
}

func normalizeSlotValue(slot ActionSlotConfig, value any) (any, error) {
	switch slot.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return nil, ErrInvalidSlotValue
		}
		text = strings.TrimSpace(text)
		if len(slot.Enum) > 0 {
			for _, allowed := range slot.Enum {
				if text == allowed {
					return text, nil
				}
			}
			return nil, ErrInvalidSlotValue
		}
		if slot.MaxChars <= 0 || len([]rune(text)) > slot.MaxChars {
			return nil, ErrInvalidSlotValue
		}
		return text, nil
	case "number":
		number, ok := numericValue(value)
		if !ok || !slotNumberInRange(number, slot) {
			return nil, ErrInvalidSlotValue
		}
		return number, nil
	case "integer":
		number, ok := numericValue(value)
		if !ok || !isWholeNumber(number) || !slotNumberInRange(number, slot) {
			return nil, ErrInvalidSlotValue
		}
		return int64(number), nil
	case "boolean":
		boolValue, ok := value.(bool)
		if !ok {
			return nil, ErrInvalidSlotValue
		}
		return boolValue, nil
	default:
		return nil, ErrInvalidSlotValue
	}
}

func numericValue(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case uint:
		number = float64(typed)
	case uint64:
		number = float64(typed)
	case uint32:
		number = float64(typed)
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func slotNumberInRange(number float64, slot ActionSlotConfig) bool {
	if slot.Min != nil && number < *slot.Min {
		return false
	}
	if slot.Max != nil && number > *slot.Max {
		return false
	}
	return true
}

func isWholeNumber(number float64) bool {
	return number == math.Trunc(number)
}

func isReservedActionOverrideKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "domain", "service", "entity_id", "entity_ids", "target", "data":
		return true
	default:
		return false
	}
}

func actionIDDescription(actions []ActionConfig) string {
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		actionID := strings.TrimSpace(action.ActionID)
		if actionID == "" {
			continue
		}
		description := strings.TrimSpace(action.Description)
		if description == "" {
			parts = append(parts, actionID)
			continue
		}
		parts = append(parts, actionID+" ("+description+")")
	}
	if len(parts) == 0 {
		return "Operator-configured Home Assistant action id."
	}
	return "Operator-configured Home Assistant action id. Available actions: " + strings.Join(parts, "; ")
}

func cloneActionData(data map[string]any) map[string]any {
	if len(data) == 0 {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func allowedEntitySet(entities []string) map[string]struct{} {
	set := make(map[string]struct{}, len(entities))
	for _, entity := range entities {
		entity = strings.TrimSpace(entity)
		if entity != "" {
			set[entity] = struct{}{}
		}
	}
	return set
}

func nonEmptyUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringArgument(arguments map[string]any, key string) string {
	value, ok := arguments[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func stringAttribute(attributes map[string]any, key string) string {
	value, ok := attributes[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
