package ingress

import (
	"fmt"
	"strings"
	"time"
)

type ControlPlaneRawRequestOptions struct {
	RequestID        string
	SourceType       string
	EntryPoint       string
	ChannelID        string
	ConversationID   string
	Message          string
	SessionID        string
	RequestedAgentID string
	Title            string
	UserID           string
	DisplayName      string
	Roles            []string
	Authenticated    bool
	Tenant           TenantRef
	Metadata         map[string]string
}

func NewControlPlaneRawRequest(opts ControlPlaneRawRequestOptions) RawRequest {
	sourceType := firstNonEmpty(opts.SourceType, "api")
	channelID := firstNonEmpty(opts.ChannelID, sourceType)
	requestID := firstNonEmpty(opts.RequestID, fmt.Sprintf("%s-%d", channelID, time.Now().UTC().UnixNano()))

	meta := cloneStringMap(opts.Metadata)
	if meta == nil {
		meta = map[string]string{}
	}
	if trimmed := strings.TrimSpace(opts.Title); trimmed != "" {
		meta["title"] = trimmed
	}
	if trimmed := strings.TrimSpace(opts.RequestedAgentID); trimmed != "" {
		meta["agent_id"] = trimmed
	}

	return RawRequest{
		RequestID: requestID,
		SourceRef: SourceRef{
			SourceType:     sourceType,
			EntryPoint:     firstNonEmpty(opts.EntryPoint, sourceType),
			ChannelID:      channelID,
			ConversationID: firstNonEmpty(opts.ConversationID, opts.SessionID, opts.UserID, requestID),
		},
		ActorHint: ActorHint{
			UserID:        strings.TrimSpace(opts.UserID),
			SessionID:     strings.TrimSpace(opts.SessionID),
			DisplayName:   strings.TrimSpace(opts.DisplayName),
			Roles:         append([]string(nil), opts.Roles...),
			Authenticated: opts.Authenticated,
		},
		TenantHint: opts.Tenant,
		Payload: RawPayload{
			Kind:     "message",
			Text:     opts.Message,
			Metadata: meta,
		},
		ReceivedAt: time.Now().UTC(),
	}
}
