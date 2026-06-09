package agents

import (
	"context"
	"errors"
	"strings"

	"stackchan-gateway/internal/providers"
)

const (
	ReasonV21RequiresProfessionalMode = "v21_requires_professional_mode"
	OutputTargetGatewayTTS            = "gateway_tts"
)

var (
	ErrBridgeNotConfigured = errors.New("agent bridge is not configured")
)

type Mode string

const (
	ModeCasual       Mode = "casual"
	ModeRoleplay     Mode = "roleplay"
	ModeProfessional Mode = "professional"
	ModeTool         Mode = "tool"
)

type Destination string

const (
	DestinationClaude   Destination = "claude"
	DestinationHermes   Destination = "hermes"
	DestinationOpenClaw Destination = "openclaw"
	DestinationV21      Destination = "v21"
)

type Bridge interface {
	Route(ctx context.Context, request RouteRequest) (RouteResponse, error)
}

type RouterOptions struct {
	Claude   Bridge
	Hermes   Bridge
	OpenClaw Bridge
	V21      Bridge
}

type Router struct {
	claude   Bridge
	hermes   Bridge
	openClaw Bridge
	v21      Bridge
}

type RouteRequest struct {
	Mode          Mode
	Destination   Destination
	Text          string
	WorkspaceID   string
	UserID        string
	DeviceID      string
	QueryScope    string
	AgentID       string
	SessionID     string
	TurnID        string
	CollectionIDs []string
	TraceID       string
}

type RouteResponse struct {
	Blocked      bool
	Reason       string
	Text         string
	OutputTarget string
	ToolCalls    []providers.ToolCall
	V21          *V21VoiceQueryResponse
}

func NewRouter(options RouterOptions) *Router {
	return &Router{
		claude:   options.Claude,
		hermes:   options.Hermes,
		openClaw: options.OpenClaw,
		v21:      options.V21,
	}
}

func (r *Router) Route(ctx context.Context, request RouteRequest) (RouteResponse, error) {
	request = sanitizeRouteRequest(request)
	if request.Destination == DestinationV21 && request.Mode != ModeProfessional {
		return RouteResponse{
			Blocked: true,
			Reason:  ReasonV21RequiresProfessionalMode,
		}, nil
	}
	bridge := r.bridgeFor(request.Destination)
	if bridge == nil {
		return RouteResponse{}, ErrBridgeNotConfigured
	}
	response, err := bridge.Route(ctx, request)
	if err != nil {
		return RouteResponse{}, err
	}
	if response.OutputTarget == "" && response.Text != "" {
		response.OutputTarget = OutputTargetGatewayTTS
	}
	return response, nil
}

func (r *Router) bridgeFor(destination Destination) Bridge {
	if r == nil {
		return nil
	}
	switch destination {
	case DestinationClaude:
		return r.claude
	case DestinationHermes:
		return r.hermes
	case DestinationOpenClaw:
		return r.openClaw
	case DestinationV21:
		return r.v21
	default:
		return nil
	}
}

func sanitizeRouteRequest(request RouteRequest) RouteRequest {
	request.Text = strings.TrimSpace(request.Text)
	request.WorkspaceID = strings.TrimSpace(request.WorkspaceID)
	request.UserID = strings.TrimSpace(request.UserID)
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	request.QueryScope = strings.TrimSpace(request.QueryScope)
	request.AgentID = strings.TrimSpace(request.AgentID)
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.TurnID = strings.TrimSpace(request.TurnID)
	request.TraceID = strings.TrimSpace(request.TraceID)
	for index := range request.CollectionIDs {
		request.CollectionIDs[index] = strings.TrimSpace(request.CollectionIDs[index])
	}
	request.CollectionIDs = nonEmptyUniqueStrings(request.CollectionIDs)
	return request
}

func nonEmptyUniqueStrings(values []string) []string {
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

func cloneArguments(arguments map[string]any) map[string]any {
	if len(arguments) == 0 {
		return nil
	}
	out := make(map[string]any, len(arguments))
	for key, value := range arguments {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	return out
}
