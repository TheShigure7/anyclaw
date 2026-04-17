package inbound

import (
	"fmt"
	"testing"

	"github.com/anyclaw/anyclaw/pkg/config"
	coreingress "github.com/anyclaw/anyclaw/pkg/ingress"
	inboundrules "github.com/anyclaw/anyclaw/pkg/routing/inbound/rules"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type stubSessionStore struct {
	byID            map[string]SessionSnapshot
	bindingSnapshot SessionSnapshot
	bindingFound    bool
	lastBinding     SessionBindingQuery
	createCalls     []SessionCreateOptions
	createErr       error
}

func (s *stubSessionStore) Get(id string) (SessionSnapshot, bool) {
	snapshot, ok := s.byID[id]
	return snapshot, ok
}

func (s *stubSessionStore) FindByBinding(query SessionBindingQuery) (SessionSnapshot, bool) {
	s.lastBinding = query
	return s.bindingSnapshot, s.bindingFound
}

func (s *stubSessionStore) Create(opts SessionCreateOptions) (SessionSnapshot, error) {
	s.createCalls = append(s.createCalls, opts)
	if s.createErr != nil {
		return SessionSnapshot{}, s.createErr
	}
	return SessionSnapshot{
		ID:            "session-new",
		AgentID:       opts.AgentID,
		OrgID:         opts.WorkspaceRef.OrgID,
		ProjectID:     opts.WorkspaceRef.ProjectID,
		WorkspaceID:   opts.WorkspaceRef.WorkspaceID,
		SessionMode:   opts.SessionMode,
		QueueMode:     opts.QueueMode,
		ReplyBack:     opts.ReplyBack,
		ReplyTarget:   opts.ReplyTarget,
		ThreadID:      opts.ThreadID,
		TransportMeta: cloneStringMap(opts.TransportMeta),
	}, nil
}

type stubWorkspaceResolver struct {
	defaultSelection WorkspaceRef
	resolveErr       error
	lastSelection    WorkspaceRef
	resolveCalls     int
}

func (s *stubWorkspaceResolver) DefaultSelection() WorkspaceRef {
	return s.defaultSelection
}

func (s *stubWorkspaceResolver) ResolveSelection(ref WorkspaceRef) (WorkspaceRef, error) {
	s.resolveCalls++
	s.lastSelection = ref
	if s.resolveErr != nil {
		return WorkspaceRef{}, s.resolveErr
	}
	return ref, nil
}

type stubMainAgentResolver struct {
	name string
}

func (s stubMainAgentResolver) ResolveMainAgentName() string {
	return s.name
}

func TestIngressRouteProjectorProjectsEntry(t *testing.T) {
	entry := sampleEntry()
	entry.Hint.RequestedAgentID = "agent-1"

	request := IngressRouteProjector{}.Project(entry)
	if request.RequestID != "req-1" || request.ActorID != "user-1" || request.DisplayName != "User One" {
		t.Fatalf("unexpected projected identity fields: %#v", request)
	}
	if request.Text != "please help" || request.Scope.ChannelID != "telegram" || request.Hint.RequestedAgentID != "agent-1" {
		t.Fatalf("unexpected projected routing fields: %#v", request)
	}
}

func TestAgentResolverAppliesRuleAndWorkspaceSelection(t *testing.T) {
	workspaceResolver := &stubWorkspaceResolver{
		defaultSelection: WorkspaceRef{OrgID: "org-default", ProjectID: "proj-default", WorkspaceID: "ws-default"},
	}
	resolver := AgentResolver{
		RuleRouter: inboundrules.NewRouter(config.RoutingConfig{
			Mode: "per-chat",
			Rules: []config.ChannelRoutingRule{{
				Channel:      "telegram",
				Match:        "help",
				Agent:        "support-agent",
				Org:          "org-rule",
				Project:      "proj-rule",
				WorkspaceRef: "ws-rule",
			}},
		}),
		MainAgent:  stubMainAgentResolver{name: "main-agent"},
		Workspaces: workspaceResolver,
	}

	resolution, decision, err := resolver.Resolve(IngressRouteProjector{}.Project(sampleEntry()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Agent != "support-agent" || resolution.AgentID != "support-agent" {
		t.Fatalf("expected routed agent, got decision=%#v resolution=%#v", decision, resolution)
	}
	if resolution.WorkspaceRef != (WorkspaceRef{OrgID: "org-rule", ProjectID: "proj-rule", WorkspaceID: "ws-rule"}) {
		t.Fatalf("unexpected workspace resolution: %#v", resolution.WorkspaceRef)
	}
	if resolution.MatchedBy != "telegram:help" {
		t.Fatalf("unexpected matched rule: %#v", resolution)
	}
	if workspaceResolver.resolveCalls != 1 || workspaceResolver.lastSelection.WorkspaceID != "ws-rule" {
		t.Fatalf("expected workspace validation for routed workspace, got %#v", workspaceResolver)
	}
}

func TestAgentResolverReusesExplicitSessionBeforeWorkspaceValidation(t *testing.T) {
	sessionStore := &stubSessionStore{
		byID: map[string]SessionSnapshot{
			"sess-existing": {
				ID:          "sess-existing",
				AgentID:     "session-agent",
				OrgID:       "org-session",
				ProjectID:   "proj-session",
				WorkspaceID: "ws-session",
			},
		},
	}
	workspaceResolver := &stubWorkspaceResolver{
		defaultSelection: WorkspaceRef{OrgID: "org-default", ProjectID: "proj-default", WorkspaceID: "ws-default"},
		resolveErr:       fmt.Errorf("should not validate workspace for explicit session"),
	}
	entry := sampleEntry()
	entry.Hint.RequestedSessionID = "sess-existing"

	resolution, _, err := AgentResolver{
		RuleRouter: inboundrules.NewRouter(config.RoutingConfig{}),
		MainAgent:  stubMainAgentResolver{name: "main-agent"},
		Workspaces: workspaceResolver,
		Sessions:   sessionStore,
	}.Resolve(IngressRouteProjector{}.Project(entry))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.AgentID != "session-agent" || resolution.WorkspaceRef.WorkspaceID != "ws-session" || resolution.MatchedBy != "existing-session" {
		t.Fatalf("unexpected explicit-session resolution: %#v", resolution)
	}
	if workspaceResolver.resolveCalls != 0 {
		t.Fatalf("expected explicit session to bypass workspace validation, got %d calls", workspaceResolver.resolveCalls)
	}
}

func TestSessionResolverReusesExistingBinding(t *testing.T) {
	sessionStore := &stubSessionStore{
		bindingFound: true,
		bindingSnapshot: SessionSnapshot{
			ID:          "sess-binding",
			AgentID:     "binding-agent",
			OrgID:       "org-binding",
			ProjectID:   "proj-binding",
			WorkspaceID: "ws-binding",
			SessionMode: "shared",
			QueueMode:   "sync",
			ReplyBack:   true,
			ReplyTarget: "reply-binding",
			ThreadID:    "thread-binding",
		},
	}
	request := IngressRouteProjector{}.Project(sampleEntry())

	resolution, snapshot, agentResolution, err := SessionResolver{Sessions: sessionStore}.Resolve(
		request,
		inboundrules.RouteDecision{Key: "telegram:reply-target"},
		AgentResolution{AgentID: "main-agent", WorkspaceRef: WorkspaceRef{OrgID: "org-main", ProjectID: "proj-main", WorkspaceID: "ws-main"}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.SessionID != "sess-binding" || resolution.IsNew || resolution.QueueMode != "sync" {
		t.Fatalf("unexpected reused session resolution: %#v", resolution)
	}
	if snapshot.ID != "sess-binding" || agentResolution.AgentID != "binding-agent" || agentResolution.MatchedBy != "existing-binding" {
		t.Fatalf("unexpected session reuse details: snapshot=%#v agent=%#v", snapshot, agentResolution)
	}
	if sessionStore.lastBinding.ReplyTarget != "reply-target" || sessionStore.lastBinding.AgentID != "main-agent" {
		t.Fatalf("unexpected binding query: %#v", sessionStore.lastBinding)
	}
}

func TestSessionResolverCreatesNewSession(t *testing.T) {
	sessionStore := &stubSessionStore{}
	request := IngressRouteProjector{}.Project(sampleEntry())
	request.Original.Content.Metadata = map[string]string{"title": "Custom Title"}

	resolution, snapshot, agentResolution, err := SessionResolver{
		Sessions:      sessionStore,
		titleRenderer: cases.Title(language.English),
	}.Resolve(
		request,
		inboundrules.RouteDecision{
			Key:         "telegram:reply-target",
			SessionMode: "shared",
			QueueMode:   "sync",
			ReplyBack:   true,
			Title:       "Rule Title",
		},
		AgentResolution{AgentID: "agent-new", WorkspaceRef: WorkspaceRef{OrgID: "org-new", ProjectID: "proj-new", WorkspaceID: "ws-new"}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(sessionStore.createCalls) != 1 {
		t.Fatalf("expected one session creation, got %d", len(sessionStore.createCalls))
	}
	created := sessionStore.createCalls[0]
	if created.Title != "Custom Title" || created.AgentID != "agent-new" || created.SessionMode != "shared" || created.QueueMode != "sync" {
		t.Fatalf("unexpected session create options: %#v", created)
	}
	if resolution.SessionID != "session-new" || !resolution.IsNew || snapshot.WorkspaceID != "ws-new" || agentResolution.AgentID != "agent-new" {
		t.Fatalf("unexpected created session result: resolution=%#v snapshot=%#v agent=%#v", resolution, snapshot, agentResolution)
	}
}

func TestDeliveryResolverUsesFinalSessionBindingFacts(t *testing.T) {
	target := DeliveryResolver{}.Resolve(
		IngressRouteProjector{}.Project(sampleEntry()),
		SessionSnapshot{
			ReplyTarget:   "reply-from-session",
			ThreadID:      "thread-from-session",
			TransportMeta: map[string]string{"mode": "session", "empty": " "},
		},
	)
	if target.ChannelID != "telegram" || target.AccountID != "acct-1" {
		t.Fatalf("unexpected delivery identity: %#v", target)
	}
	if target.TargetRef != "reply-from-session" || target.ThreadID != "thread-from-session" {
		t.Fatalf("unexpected delivery destination: %#v", target)
	}
	if len(target.TransportMeta) != 1 || target.TransportMeta["mode"] != "session" {
		t.Fatalf("unexpected delivery transport meta: %#v", target.TransportMeta)
	}
}

func sampleEntry() coreingress.IngressRoutingEntry {
	return coreingress.IngressRoutingEntry{
		Request: coreingress.NormalizedRequest{
			RequestID: "req-1",
			Actor: coreingress.ActorRef{
				UserID:      "user-1",
				AccountID:   "acct-1",
				DisplayName: "User One",
			},
			TenantRef: coreingress.TenantRef{
				OrgID:       "org-request",
				ProjectID:   "proj-request",
				WorkspaceID: "ws-request",
			},
			Content: coreingress.NormalizedContent{
				Text:     "please help",
				Metadata: map[string]string{},
			},
			RouteContext: coreingress.IngressRouteContext{
				ChannelID:      "telegram",
				AccountID:      "acct-1",
				ConversationID: "conv-1",
				PeerID:         "peer-1",
				ThreadID:       "thread-1",
				IsGroup:        true,
				GroupID:        "group-1",
				Delivery: coreingress.DeliveryHint{
					ReplyTarget:   "reply-target",
					ThreadID:      "delivery-thread",
					TransportMeta: map[string]string{"mode": "request"},
				},
			},
		},
	}
}
