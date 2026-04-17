package inbound

import (
	"fmt"
	"strings"

	inboundrules "github.com/anyclaw/anyclaw/pkg/routing/inbound/rules"
	"golang.org/x/text/cases"
)

type SessionResolver struct {
	Sessions      SessionStore
	titleRenderer cases.Caser
}

func (r SessionResolver) Resolve(request MainRouteRequest, decision inboundrules.RouteDecision, agentResolution AgentResolution) (SessionResolution, SessionSnapshot, AgentResolution, error) {
	if r.Sessions == nil {
		return SessionResolution{}, SessionSnapshot{}, AgentResolution{}, fmt.Errorf("session store is required")
	}

	if snapshot, ok := lookupExplicitSession(r.Sessions, request, decision); ok {
		resolvedAgent := agentResolutionFromSession(agentResolution, snapshot, "existing-session")
		return resolutionFromSession(snapshot, decision.Key, false), snapshot, resolvedAgent, nil
	}

	replyTarget := firstNonEmpty(request.Scope.Delivery.ReplyTarget, request.Scope.ConversationID, request.Scope.PeerID)
	if snapshot, ok := r.Sessions.FindByBinding(SessionBindingQuery{
		SourceChannel: request.Scope.ChannelID,
		ReplyTarget:   replyTarget,
		ThreadID:      request.Scope.ThreadID,
		AgentID:       agentResolution.AgentID,
	}); ok {
		resolvedAgent := agentResolutionFromSession(agentResolution, snapshot, "existing-binding")
		return resolutionFromSession(snapshot, firstNonEmpty(decision.Key, derivedSessionKey(request)), false), snapshot, resolvedAgent, nil
	}

	title := strings.TrimSpace(request.Original.Content.Metadata["title"])
	if title == "" {
		title = strings.TrimSpace(decision.Title)
	}
	if title == "" {
		title = r.titleRenderer.String(firstNonEmpty(request.Scope.ChannelID, "channel")) + " session"
	}
	snapshot, err := r.Sessions.Create(SessionCreateOptions{
		Title:         title,
		AgentID:       agentResolution.AgentID,
		WorkspaceRef:  agentResolution.WorkspaceRef,
		SessionMode:   normalizeSessionMode(decision.SessionMode, "channel-dm"),
		QueueMode:     normalizeQueueMode(decision.QueueMode),
		ReplyBack:     decision.ReplyBack,
		SourceChannel: request.Scope.ChannelID,
		SourceID:      firstNonEmpty(request.Scope.PeerID, replyTarget),
		UserID:        request.ActorID,
		UserName:      request.DisplayName,
		ReplyTarget:   replyTarget,
		ThreadID:      request.Scope.ThreadID,
		TransportMeta: cloneStringMap(request.Scope.Delivery.TransportMeta),
		IsGroup:       request.Scope.IsGroup,
		GroupKey:      request.Scope.GroupID,
	})
	if err != nil {
		return SessionResolution{}, SessionSnapshot{}, AgentResolution{}, err
	}
	return resolutionFromSession(snapshot, firstNonEmpty(decision.Key, derivedSessionKey(request)), true), snapshot, agentResolution, nil
}

func agentResolutionFromSession(base AgentResolution, snapshot SessionSnapshot, matchedBy string) AgentResolution {
	resolved := base
	if strings.TrimSpace(snapshot.AgentID) != "" {
		resolved.AgentID = strings.TrimSpace(snapshot.AgentID)
	}
	if strings.TrimSpace(snapshot.OrgID) != "" {
		resolved.WorkspaceRef.OrgID = strings.TrimSpace(snapshot.OrgID)
	}
	if strings.TrimSpace(snapshot.ProjectID) != "" {
		resolved.WorkspaceRef.ProjectID = strings.TrimSpace(snapshot.ProjectID)
	}
	if strings.TrimSpace(snapshot.WorkspaceID) != "" {
		resolved.WorkspaceRef.WorkspaceID = strings.TrimSpace(snapshot.WorkspaceID)
	}
	if strings.TrimSpace(matchedBy) != "" {
		resolved.MatchedBy = matchedBy
	}
	return resolved
}

func resolutionFromSession(snapshot SessionSnapshot, sessionKey string, isNew bool) SessionResolution {
	return SessionResolution{
		SessionKey:  firstNonEmpty(sessionKey, snapshot.ID),
		SessionID:   snapshot.ID,
		SessionMode: normalizeSessionMode(snapshot.SessionMode, "main"),
		QueueMode:   normalizeQueueMode(snapshot.QueueMode),
		ReplyBack:   snapshot.ReplyBack,
		IsNew:       isNew,
	}
}

func derivedSessionKey(request MainRouteRequest) string {
	base := firstNonEmpty(request.Scope.ChannelID, "channel") + ":" + firstNonEmpty(request.Scope.Delivery.ReplyTarget, request.Scope.ConversationID, request.Scope.PeerID, request.ActorID)
	if threadID := strings.TrimSpace(request.Scope.ThreadID); threadID != "" {
		return base + ":thread:" + threadID
	}
	return base
}

func normalizeSessionMode(value string, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	switch value {
	case "group", "group-shared", "channel-group":
		return fallback
	default:
		return value
	}
}

func normalizeQueueMode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "fifo"
	}
	return value
}

func lookupExplicitSession(sessions SessionStore, request MainRouteRequest, decision inboundrules.RouteDecision) (SessionSnapshot, bool) {
	if sessions == nil {
		return SessionSnapshot{}, false
	}
	if sessionID := strings.TrimSpace(request.Hint.RequestedSessionID); sessionID != "" {
		if snapshot, ok := sessions.Get(sessionID); ok {
			return snapshot, true
		}
	}
	if sessionID := strings.TrimSpace(decision.SessionID); sessionID != "" {
		if snapshot, ok := sessions.Get(sessionID); ok {
			return snapshot, true
		}
	}
	return SessionSnapshot{}, false
}
