package channel

import (
	inboundrules "github.com/anyclaw/anyclaw/pkg/routing/inbound/rules"
)

type RouteRequest = inboundrules.RouteRequest
type RouteDecision = inboundrules.RouteDecision
type Router = inboundrules.Router

var NewRouter = inboundrules.NewRouter
