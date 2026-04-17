package ingress

import "time"

type SourceRef struct {
	SourceType     string
	EntryPoint     string
	ClientID       string
	ChannelID      string
	ConversationID string
	MessageID      string
	CallbackID     string
}

type ActorHint struct {
	UserID        string
	SessionID     string
	DisplayName   string
	Roles         []string
	Authenticated bool
}

type RequestedAction struct {
	Kind      string
	Target    string
	Name      string
	Arguments map[string]string
}

type AttachmentRef struct {
	Kind      string
	URI       string
	Name      string
	MimeType  string
	SizeBytes int64
}

type RawPayload struct {
	Kind        string
	Text        string
	Args        []string
	Flags       map[string]string
	Action      RequestedAction
	Attachments []AttachmentRef
	Metadata    map[string]string
}

type RawRequest struct {
	RequestID  string
	SourceRef  SourceRef
	ActorHint  ActorHint
	TenantHint TenantRef
	Payload    RawPayload
	ReceivedAt time.Time
}

type ActorRef struct {
	UserID        string
	AccountID     string
	DisplayName   string
	Roles         []string
	Authenticated bool
}

type TenantRef struct {
	OrgID       string
	ProjectID   string
	WorkspaceID string
}

type TraceContext struct {
	TraceID         string
	ParentRequestID string
	SourceIP        string
	UserAgent       string
	RequestPath     string
}

type NormalizedContent struct {
	Kind        string
	Text        string
	Action      RequestedAction
	Attachments []AttachmentRef
	Metadata    map[string]string
}

type GovernanceResult struct {
	Authenticated bool
	PermissionSet []string
	RateLimitKey  string
	RiskLevel     string
	DenyReason    string
}

type DeliveryHint struct {
	ReplyTarget   string
	ThreadID      string
	CallbackID    string
	TransportMeta map[string]string
}

type IngressRouteContext struct {
	SourceType     string
	ChannelID      string
	AccountID      string
	ConversationID string
	PeerID         string
	PeerKind       string
	ThreadID       string
	IsGroup        bool
	GroupID        string
	Delivery       DeliveryHint
	Metadata       map[string]string
}

type NormalizedRequest struct {
	RequestID    string
	Actor        ActorRef
	TenantRef    TenantRef
	Content      NormalizedContent
	Governance   GovernanceResult
	Trace        TraceContext
	RouteContext IngressRouteContext
	ReceivedAt   time.Time
}

type RoutingHint struct {
	RequestedAgentID   string
	RequestedSessionID string
}

type IngressRoutingEntry struct {
	Request NormalizedRequest
	Hint    RoutingHint
}
