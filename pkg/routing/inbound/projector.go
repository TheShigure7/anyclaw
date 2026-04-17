package inbound

import (
	coreingress "github.com/anyclaw/anyclaw/pkg/ingress"
)

type IngressRouteProjector struct{}

func (p IngressRouteProjector) Project(entry coreingress.IngressRoutingEntry) MainRouteRequest {
	return MainRouteRequest{
		RequestID:   entry.Request.RequestID,
		ActorID:     entry.Request.Actor.UserID,
		DisplayName: entry.Request.Actor.DisplayName,
		Text:        entry.Request.Content.Text,
		Scope:       entry.Request.RouteContext,
		Hint:        entry.Hint,
		Original:    entry.Request,
	}
}
