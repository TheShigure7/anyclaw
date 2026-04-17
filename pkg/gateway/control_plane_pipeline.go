package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/channel"
	"github.com/anyclaw/anyclaw/pkg/config"
	dispatchsvc "github.com/anyclaw/anyclaw/pkg/dispatch"
	gatewayingress "github.com/anyclaw/anyclaw/pkg/gateway/ingress"
	coreingress "github.com/anyclaw/anyclaw/pkg/ingress"
	inboundrouting "github.com/anyclaw/anyclaw/pkg/routing/inbound"
	appRuntime "github.com/anyclaw/anyclaw/pkg/runtime"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type controlPlaneMessageRequest struct {
	SourceType       string
	EntryPoint       string
	User             *AuthUser
	Message          string
	SessionID        string
	Title            string
	RequestedAgentID string
	Tenant           coreingress.TenantRef
	Resume           bool
	Metadata         map[string]string
}

func (s *Server) ensureMessagePipeline() error {
	if s == nil {
		return fmt.Errorf("server is required")
	}
	if s.sessions == nil {
		return fmt.Errorf("session manager not initialized")
	}
	if s.runtimePool == nil {
		return fmt.Errorf("runtime pool not initialized")
	}
	if s.ingressGateway == nil {
		s.ingressGateway = gatewayingress.NewService()
	}
	if s.router == nil {
		var routingCfg config.RoutingConfig
		if s.app != nil && s.app.Config != nil {
			routingCfg = s.app.Config.Channels.Routing
		}
		s.router = channel.NewRouter(routingCfg)
	}
	if s.inboundRouter == nil {
		s.inboundRouter = inboundrouting.NewService(s.router, inboundMainAgentResolver{server: s}, inboundWorkspaceResolver{server: s}, inboundSessionStoreAdapter{sessions: s.sessions})
	}
	if s.dispatcher == nil {
		s.dispatcher = &dispatchsvc.ChannelService{}
	}
	s.dispatcher.Sessions = dispatchSessionStoreAdapter{sessions: s.sessions}
	s.dispatcher.RuntimePool = s.runtimePool
	s.dispatcher.AppendEvent = s.appendEvent
	s.dispatcher.BeginRun = s.beginActiveSessionRun
	s.dispatcher.DecorateContext = s.decorateDispatchContext
	s.dispatcher.HandleRunError = s.handleDispatchRunError
	s.dispatcher.RecordToolActivities = func(session dispatchsvc.SessionSnapshot, activities []agent.ToolActivity) {
		for _, activity := range activities {
			s.appendToolActivity(session.ID, ToolActivityRecord{
				ToolName:  activity.ToolName,
				Args:      activity.Args,
				Result:    activity.Result,
				Error:     activity.Error,
				Agent:     session.Agent,
				Workspace: session.Workspace,
			})
		}
	}
	return nil
}

func (s *Server) decorateDispatchContext(ctx context.Context, request inboundrouting.RoutedRequest, session dispatchsvc.SessionSnapshot, targetApp *appRuntime.App) context.Context {
	if s == nil || targetApp == nil || targetApp.Config == nil {
		return ctx
	}
	title := strings.TrimSpace(request.Request.Content.Metadata["title"])
	source := firstNonEmpty(request.Route.Delivery.ChannelID, request.Request.RouteContext.SourceType, "api")
	sessionRef := &Session{
		ID:        session.ID,
		Title:     title,
		Workspace: session.Workspace,
	}
	ctx = agent.WithToolApprovalHook(ctx, s.sessionToolApprovalHook(sessionRef, targetApp.Config, title, request.Request.Content.Text, source))
	ctx = tools.WithToolApprovalHook(ctx, s.sessionProtocolApprovalHook(sessionRef, targetApp.Config, title, request.Request.Content.Text, source))
	return ctx
}

func (s *Server) handleDispatchRunError(request inboundrouting.RoutedRequest, session dispatchsvc.SessionSnapshot, err error) {
	if s == nil {
		return
	}
	if errors.Is(err, ErrTaskWaitingApproval) {
		s.updateSessionApprovalPresence(session.ID, "")
		return
	}
	s.updateSessionPresence(session.ID, "idle", false)
	payload := map[string]any{
		"message": request.Request.Content.Text,
		"error":   err.Error(),
		"source":  firstNonEmpty(request.Route.Delivery.ChannelID, request.Request.RouteContext.SourceType, "api"),
	}
	for k, v := range request.Request.RouteContext.Metadata {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			payload[k] = trimmed
		}
	}
	s.appendEvent("chat.failed", session.ID, payload)
}

func (s *Server) dispatchControlPlaneMessage(ctx context.Context, req controlPlaneMessageRequest) (dispatchsvc.Result, error) {
	if err := s.ensureMessagePipeline(); err != nil {
		return dispatchsvc.Result{}, err
	}

	metadata := cloneStringMap(req.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if req.Resume {
		metadata["resume"] = "true"
	}

	raw := coreingress.NewControlPlaneRawRequest(coreingress.ControlPlaneRawRequestOptions{
		SourceType:       req.SourceType,
		EntryPoint:       req.EntryPoint,
		ChannelID:        firstNonEmpty(req.SourceType, "api"),
		Message:          req.Message,
		SessionID:        req.SessionID,
		RequestedAgentID: req.RequestedAgentID,
		Title:            req.Title,
		UserID:           userName(req.User),
		DisplayName:      userName(req.User),
		Roles:            userRoles(req.User),
		Authenticated:    req.User != nil,
		Tenant:           req.Tenant,
		Metadata:         metadata,
	})
	entry, err := s.ingressGateway.NormalizeControlPlane(raw)
	if err != nil {
		return dispatchsvc.Result{}, err
	}
	routed, err := s.inboundRouter.Route(ctx, inboundrouting.RouteInput{Entry: entry})
	if err != nil {
		return dispatchsvc.Result{}, err
	}
	result, err := s.dispatcher.Dispatch(ctx, routed.Request)
	if err == nil {
		return result, nil
	}
	sessionID := strings.TrimSpace(routed.Request.Route.Session.SessionID)
	if sessionID == "" || s.sessions == nil {
		return dispatchsvc.Result{}, err
	}
	session, ok := s.sessions.Get(sessionID)
	if !ok {
		return dispatchsvc.Result{}, err
	}
	return dispatchsvc.Result{
		Session: dispatchSessionSnapshot(*session),
	}, err
}

func userName(user *AuthUser) string {
	if user == nil {
		return ""
	}
	return strings.TrimSpace(user.Name)
}

func userRoles(user *AuthUser) []string {
	if user == nil {
		return nil
	}
	if role := strings.TrimSpace(user.Role); role != "" {
		return []string{role}
	}
	return nil
}
