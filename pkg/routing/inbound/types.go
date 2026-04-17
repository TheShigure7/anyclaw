package inbound

import (
	"context"

	coreingress "github.com/anyclaw/anyclaw/pkg/ingress"
)

type WorkspaceRef struct {
	OrgID       string
	ProjectID   string
	WorkspaceID string
}

type MainRouteRequest struct {
	RequestID   string
	ActorID     string
	DisplayName string
	Text        string
	Scope       coreingress.IngressRouteContext
	Hint        coreingress.RoutingHint
	Original    coreingress.NormalizedRequest
}

type AgentResolution struct {
	AgentID      string
	WorkspaceRef WorkspaceRef
	MatchedBy    string
}

type SessionResolution struct {
	SessionKey  string
	SessionID   string
	SessionMode string
	QueueMode   string
	ReplyBack   bool
	IsNew       bool
}

type DeliveryTarget struct {
	ChannelID     string
	AccountID     string
	TargetRef     string
	ThreadID      string
	TransportMeta map[string]string
}

type RouteResolution struct {
	Agent    AgentResolution
	Session  SessionResolution
	Delivery DeliveryTarget
}

type RoutedRequest struct {
	Request coreingress.NormalizedRequest
	Route   RouteResolution
}

type RouteInput struct {
	Entry coreingress.IngressRoutingEntry
}

type RouteOutput struct {
	Request RoutedRequest
}

type Router interface {
	Route(ctx context.Context, input RouteInput) (RouteOutput, error)
}
