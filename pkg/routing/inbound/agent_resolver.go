package inbound

import (
	"strings"

	inboundrules "github.com/anyclaw/anyclaw/pkg/routing/inbound/rules"
)

type AgentResolver struct {
	RuleRouter *inboundrules.Router
	MainAgent  MainAgentResolver
	Workspaces WorkspaceResolver
	Sessions   SessionStore
}

func (r AgentResolver) Resolve(request MainRouteRequest) (AgentResolution, inboundrules.RouteDecision, error) {
	decision := r.decide(request)
	if snapshot, ok := lookupExplicitSession(r.Sessions, request, decision); ok {
		workspaceRef := WorkspaceRef{
			OrgID:       snapshot.OrgID,
			ProjectID:   snapshot.ProjectID,
			WorkspaceID: snapshot.WorkspaceID,
		}
		agentID := firstNonEmpty(snapshot.AgentID, strings.TrimSpace(request.Hint.RequestedAgentID))
		if agentID == "" && r.MainAgent != nil {
			agentID = strings.TrimSpace(r.MainAgent.ResolveMainAgentName())
		}
		if agentID == "" {
			agentID = "main"
		}
		return AgentResolution{
			AgentID:      agentID,
			WorkspaceRef: workspaceRef,
			MatchedBy:    "existing-session",
		}, decision, nil
	}

	workspaceRef := WorkspaceRef{}
	if r.Workspaces != nil {
		workspaceRef = r.Workspaces.DefaultSelection()
	}
	if strings.TrimSpace(request.Original.TenantRef.OrgID) != "" {
		workspaceRef.OrgID = strings.TrimSpace(request.Original.TenantRef.OrgID)
	}
	if strings.TrimSpace(request.Original.TenantRef.ProjectID) != "" {
		workspaceRef.ProjectID = strings.TrimSpace(request.Original.TenantRef.ProjectID)
	}
	if strings.TrimSpace(request.Original.TenantRef.WorkspaceID) != "" {
		workspaceRef.WorkspaceID = strings.TrimSpace(request.Original.TenantRef.WorkspaceID)
	}
	if strings.TrimSpace(decision.Org) != "" {
		workspaceRef.OrgID = strings.TrimSpace(decision.Org)
	}
	if strings.TrimSpace(decision.Project) != "" {
		workspaceRef.ProjectID = strings.TrimSpace(decision.Project)
	}
	if strings.TrimSpace(decision.Workspace) != "" {
		workspaceRef.WorkspaceID = strings.TrimSpace(decision.Workspace)
	}
	if r.Workspaces != nil {
		resolved, err := r.Workspaces.ResolveSelection(workspaceRef)
		if err != nil {
			return AgentResolution{}, inboundrules.RouteDecision{}, err
		}
		workspaceRef = resolved
	}

	agentID := strings.TrimSpace(request.Hint.RequestedAgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(decision.Agent)
	}
	if agentID == "" && r.MainAgent != nil {
		agentID = strings.TrimSpace(r.MainAgent.ResolveMainAgentName())
	}
	if agentID == "" {
		agentID = "main"
	}

	matchedBy := strings.TrimSpace(decision.MatchedRule)
	if matchedBy == "" {
		matchedBy = "default"
	}

	return AgentResolution{
		AgentID:      agentID,
		WorkspaceRef: workspaceRef,
		MatchedBy:    matchedBy,
	}, decision, nil
}

func (r AgentResolver) decide(request MainRouteRequest) inboundrules.RouteDecision {
	if r.RuleRouter == nil {
		return inboundrules.RouteDecision{}
	}
	return r.RuleRouter.Decide(inboundrules.RouteRequest{
		Channel:  request.Scope.ChannelID,
		Source:   routeSource(request),
		Text:     request.Text,
		ThreadID: request.Scope.ThreadID,
		IsGroup:  request.Scope.IsGroup,
		GroupID:  request.Scope.GroupID,
	})
}
