package ingress

import "time"

// MessageActor captures the sender facts needed by the route layer.
type MessageActor struct {
	UserID      string
	DisplayName string
}

// MessageScope carries the transport coordinates for one ingress message.
type MessageScope struct {
	EntryPoint     string
	ChannelID      string
	ConversationID string
	ThreadID       string
	GroupID        string
	IsGroup        bool
	Metadata       map[string]string
}

// DeliveryHint stores the inbound delivery facts observed before delivery routing.
type DeliveryHint struct {
	ChannelID      string
	ConversationID string
	ReplyTo        string
	ThreadID       string
	Metadata       map[string]string
}

// RouteHint carries optional caller hints into the routing flow.
type RouteHint struct {
	RequestedAgentName string
	RequestedSessionID string
}

// IngressRoutingEntry is the trusted route-layer input passed from gateway to route.
type IngressRoutingEntry struct {
	MessageID  string
	Text       string
	Actor      MessageActor
	Scope      MessageScope
	Delivery   DeliveryHint
	Hint       RouteHint
	ReceivedAt time.Time
}

// MainRouteRequest is the normalized request emitted by the M1 projector.
type MainRouteRequest struct {
	MessageID    string
	Text         string
	Actor        MessageActor
	Scope        MessageScope
	DeliveryHint DeliveryHint
	Hint         RouteHint
	ReceivedAt   time.Time
}
