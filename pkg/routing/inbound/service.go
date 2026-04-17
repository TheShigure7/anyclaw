package inbound

import (
	"context"

	inboundrules "github.com/anyclaw/anyclaw/pkg/routing/inbound/rules"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type SessionSnapshot struct {
	ID            string
	AgentID       string
	OrgID         string
	ProjectID     string
	WorkspaceID   string
	SessionMode   string
	QueueMode     string
	ReplyBack     bool
	ReplyTarget   string
	ThreadID      string
	TransportMeta map[string]string
}

type SessionBindingQuery struct {
	SourceChannel string
	ReplyTarget   string
	ThreadID      string
	AgentID       string
}

type SessionCreateOptions struct {
	Title         string
	AgentID       string
	WorkspaceRef  WorkspaceRef
	SessionMode   string
	QueueMode     string
	ReplyBack     bool
	SourceChannel string
	SourceID      string
	UserID        string
	UserName      string
	ReplyTarget   string
	ThreadID      string
	TransportMeta map[string]string
	IsGroup       bool
	GroupKey      string
}

type SessionStore interface {
	Get(id string) (SessionSnapshot, bool)
	FindByBinding(query SessionBindingQuery) (SessionSnapshot, bool)
	Create(opts SessionCreateOptions) (SessionSnapshot, error)
}

type WorkspaceResolver interface {
	DefaultSelection() WorkspaceRef
	ResolveSelection(ref WorkspaceRef) (WorkspaceRef, error)
}

type MainAgentResolver interface {
	ResolveMainAgentName() string
}

type Service struct {
	Projector        IngressRouteProjector
	Agents           AgentResolver
	SessionsResolver SessionResolver
	Delivery         DeliveryResolver
}

func NewService(router *inboundrules.Router, mainAgent MainAgentResolver, workspaces WorkspaceResolver, sessions SessionStore) *Service {
	return &Service{
		Projector: IngressRouteProjector{},
		Agents: AgentResolver{
			RuleRouter: router,
			MainAgent:  mainAgent,
			Workspaces: workspaces,
			Sessions:   sessions,
		},
		SessionsResolver: SessionResolver{
			Sessions:      sessions,
			titleRenderer: cases.Title(language.English),
		},
		Delivery: DeliveryResolver{},
	}
}

func (s *Service) Route(ctx context.Context, input RouteInput) (RouteOutput, error) {
	_ = ctx

	request := s.Projector.Project(input.Entry)
	agentResolution, decision, err := s.Agents.Resolve(request)
	if err != nil {
		return RouteOutput{}, err
	}
	sessionResolution, sessionSnapshot, resolvedAgent, err := s.SessionsResolver.Resolve(request, decision, agentResolution)
	if err != nil {
		return RouteOutput{}, err
	}
	delivery := s.Delivery.Resolve(request, sessionSnapshot)

	return RouteOutput{
		Request: RoutedRequest{
			Request: input.Entry.Request,
			Route: RouteResolution{
				Agent:    resolvedAgent,
				Session:  sessionResolution,
				Delivery: delivery,
			},
		},
	}, nil
}
