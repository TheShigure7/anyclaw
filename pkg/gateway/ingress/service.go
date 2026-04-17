package ingressgateway

import (
	"strings"

	coreingress "github.com/anyclaw/anyclaw/pkg/ingress"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Normalize(raw coreingress.RawRequest) (coreingress.IngressRoutingEntry, error) {
	meta := cloneStringMap(raw.Payload.Metadata)
	replyTarget := strings.TrimSpace(meta["reply_target"])
	threadID := strings.TrimSpace(meta["thread_id"])
	peerID := firstNonEmpty(raw.ActorHint.UserID, meta["user_id"], meta["source"], replyTarget, raw.SourceRef.ConversationID)
	peerKind := "direct"
	if meta["is_group"] == "true" || strings.TrimSpace(meta["guild_id"]) != "" {
		peerKind = "group"
	}
	channelID := firstNonEmpty(raw.SourceRef.ChannelID, raw.SourceRef.SourceType, "channel")

	request := coreingress.NormalizedRequest{
		RequestID: raw.RequestID,
		Actor: coreingress.ActorRef{
			UserID:        strings.TrimSpace(raw.ActorHint.UserID),
			AccountID:     strings.TrimSpace(meta["account_id"]),
			DisplayName:   strings.TrimSpace(raw.ActorHint.DisplayName),
			Authenticated: raw.ActorHint.Authenticated,
			Roles:         append([]string(nil), raw.ActorHint.Roles...),
		},
		Content: coreingress.NormalizedContent{
			Kind:        raw.Payload.Kind,
			Text:        raw.Payload.Text,
			Action:      raw.Payload.Action,
			Attachments: append([]coreingress.AttachmentRef(nil), raw.Payload.Attachments...),
			Metadata:    cloneStringMap(raw.Payload.Metadata),
		},
		TenantRef: raw.TenantHint,
		Governance: coreingress.GovernanceResult{
			Authenticated: raw.ActorHint.Authenticated,
			RiskLevel:     "low",
		},
		Trace: coreingress.TraceContext{
			TraceID:     raw.RequestID,
			RequestPath: strings.TrimSpace(raw.SourceRef.EntryPoint),
		},
		RouteContext: coreingress.IngressRouteContext{
			SourceType:     firstNonEmpty(raw.SourceRef.SourceType, "channel"),
			ChannelID:      channelID,
			AccountID:      strings.TrimSpace(meta["account_id"]),
			ConversationID: firstNonEmpty(raw.SourceRef.ConversationID, replyTarget, meta["chat_id"], meta["source"], meta["channel_id"]),
			PeerID:         peerID,
			PeerKind:       peerKind,
			ThreadID:       threadID,
			IsGroup:        peerKind == "group",
			GroupID:        strings.TrimSpace(meta["guild_id"]),
			Delivery: coreingress.DeliveryHint{
				ReplyTarget:   replyTarget,
				ThreadID:      threadID,
				CallbackID:    strings.TrimSpace(raw.SourceRef.CallbackID),
				TransportMeta: transportMeta(meta),
			},
			Metadata: cloneStringMap(meta),
		},
		ReceivedAt: raw.ReceivedAt,
	}

	return coreingress.IngressRoutingEntry{
		Request: request,
		Hint: coreingress.RoutingHint{
			RequestedAgentID:   strings.TrimSpace(meta["agent_id"]),
			RequestedSessionID: strings.TrimSpace(raw.ActorHint.SessionID),
		},
	}, nil
}

func (s *Service) NormalizeChannel(raw coreingress.RawRequest) (coreingress.IngressRoutingEntry, error) {
	return s.Normalize(raw)
}

func (s *Service) NormalizeControlPlane(raw coreingress.RawRequest) (coreingress.IngressRoutingEntry, error) {
	return s.Normalize(raw)
}

func transportMeta(meta map[string]string) map[string]string {
	items := map[string]string{}
	for _, key := range []string{"channel_id", "chat_id", "guild_id", "attachment_count", "message_type", "audio_url", "audio_mime", "caption"} {
		if value := strings.TrimSpace(meta[key]); value != "" {
			items[key] = value
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
