package inbound

type DeliveryResolver struct{}

func (r DeliveryResolver) Resolve(request MainRouteRequest, session SessionSnapshot) DeliveryTarget {
	targetRef := firstNonEmpty(session.ReplyTarget, request.Scope.Delivery.ReplyTarget, request.Scope.ConversationID, request.Scope.PeerID)
	threadID := firstNonEmpty(session.ThreadID, request.Scope.ThreadID, request.Scope.Delivery.ThreadID)
	transportMeta := cloneStringMap(session.TransportMeta)
	if len(transportMeta) == 0 {
		transportMeta = cloneStringMap(request.Scope.Delivery.TransportMeta)
	}
	return DeliveryTarget{
		ChannelID:     request.Scope.ChannelID,
		AccountID:     request.Scope.AccountID,
		TargetRef:     targetRef,
		ThreadID:      threadID,
		TransportMeta: transportMeta,
	}
}
