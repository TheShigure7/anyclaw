package dispatch

import (
	"context"
	"fmt"
	"strings"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/agenthub"
	coreingress "github.com/anyclaw/anyclaw/pkg/ingress"
	"github.com/anyclaw/anyclaw/pkg/prompt"
	inboundrouting "github.com/anyclaw/anyclaw/pkg/routing/inbound"
	appRuntime "github.com/anyclaw/anyclaw/pkg/runtime"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type SessionSnapshot struct {
	ID        string
	Agent     string
	Org       string
	Project   string
	Workspace string
	History   []prompt.Message
	ReplyBack bool
}

type UserMapping struct {
	UserID        string
	UserName      string
	ReplyTarget   string
	ThreadID      string
	TransportMeta map[string]string
}

type SessionStore interface {
	EnqueueTurn(sessionID string) (SessionSnapshot, error)
	SetUserMapping(sessionID string, mapping UserMapping) (SessionSnapshot, error)
	SetPresence(sessionID string, presence string, typing bool) (SessionSnapshot, error)
	Get(sessionID string) (SessionSnapshot, bool)
	AddExchange(sessionID string, userText string, assistantText string) (SessionSnapshot, error)
}

type RuntimePool interface {
	GetOrCreate(agentName string, org string, project string, workspaceID string) (*appRuntime.App, error)
}

type EventAppender func(eventType string, sessionID string, payload map[string]any)
type ToolActivityRecorder func(session SessionSnapshot, activities []agent.ToolActivity)
type RunCoordinator func(parent context.Context, sessionID string) (context.Context, func())
type ContextDecorator func(ctx context.Context, request inboundrouting.RoutedRequest, session SessionSnapshot, targetApp *appRuntime.App) context.Context
type RunErrorHandler func(request inboundrouting.RoutedRequest, session SessionSnapshot, err error)

type Result struct {
	Session        SessionSnapshot
	Response       string
	ToolActivities []agent.ToolActivity
}

type ChannelService struct {
	Sessions             SessionStore
	RuntimePool          RuntimePool
	AppendEvent          EventAppender
	RecordToolActivities ToolActivityRecorder
	BeginRun             RunCoordinator
	DecorateContext      ContextDecorator
	HandleRunError       RunErrorHandler
}

func (s *ChannelService) Dispatch(ctx context.Context, request inboundrouting.RoutedRequest) (Result, error) {
	sessionID := strings.TrimSpace(request.Route.Session.SessionID)
	if sessionID == "" {
		return Result{}, fmt.Errorf("session id is required")
	}
	message := request.Request.Content.Text
	source := request.Route.Delivery.ChannelID

	if shouldEnqueue(request) {
		if _, err := s.Sessions.EnqueueTurn(sessionID); err == nil {
			s.appendEvent("session.queue.updated", sessionID, payloadWithRequest(request.Request, map[string]any{
				"queue_mode":   request.Route.Session.QueueMode,
				"source":       source,
				"reply_target": request.Route.Delivery.TargetRef,
			}))
		}
	}

	if _, err := s.Sessions.SetUserMapping(sessionID, UserMapping{
		UserID:        request.Request.Actor.UserID,
		UserName:      request.Request.Actor.DisplayName,
		ReplyTarget:   request.Route.Delivery.TargetRef,
		ThreadID:      request.Route.Delivery.ThreadID,
		TransportMeta: cloneStringMap(request.Route.Delivery.TransportMeta),
	}); err == nil {
		s.appendEvent("session.user_mapped", sessionID, payloadWithRequest(request.Request, map[string]any{
			"source":       source,
			"user_id":      request.Request.Actor.UserID,
			"user_name":    request.Request.Actor.DisplayName,
			"reply_target": request.Route.Delivery.TargetRef,
		}))
	}

	if _, err := s.Sessions.SetPresence(sessionID, "typing", true); err == nil {
		s.appendEvent("session.typing", sessionID, payloadWithRequest(request.Request, map[string]any{
			"typing":  true,
			"source":  source,
			"user_id": request.Request.Actor.UserID,
		}))
	}

	s.appendEvent(startedEventName(request), sessionID, payloadWithRequest(request.Request, map[string]any{
		"message": message,
		"source":  source,
	}))

	session, ok := s.Sessions.Get(sessionID)
	if !ok {
		return Result{}, fmt.Errorf("session not found: %s", sessionID)
	}
	if s.RuntimePool == nil {
		return Result{}, fmt.Errorf("runtime pool not initialized")
	}

	runCtx := ctx
	finish := func() {}
	if s.BeginRun != nil {
		runCtx, finish = s.BeginRun(ctx, sessionID)
	}
	defer finish()

	targetApp, err := s.RuntimePool.GetOrCreate(session.Agent, session.Org, session.Project, session.Workspace)
	if err != nil {
		return Result{}, err
	}
	targetApp.Agent.SetHistory(session.History)

	execCtx := tools.WithBrowserSession(runCtx, sessionID)
	execCtx = tools.WithSandboxScope(execCtx, tools.SandboxScope{SessionID: sessionID, Channel: source})
	if s.DecorateContext != nil {
		execCtx = s.DecorateContext(execCtx, request, session, targetApp)
	}

	runResult, err := targetApp.RunUserTask(execCtx, agenthub.RunRequest{
		SessionID:   sessionID,
		UserInput:   message,
		History:     targetApp.Agent.GetHistory(),
		SyncHistory: true,
	})
	if err != nil {
		s.handleRunError(request, session, err)
		return Result{}, err
	}

	updatedSession, err := s.Sessions.AddExchange(sessionID, message, runResult.Content)
	if err != nil {
		return Result{}, err
	}
	if updatedSession.ReplyBack {
		s.appendEvent("session.reply_back", sessionID, payloadWithRequest(request.Request, map[string]any{
			"enabled":      true,
			"source":       source,
			"reply_target": request.Route.Delivery.TargetRef,
		}))
	}
	if _, err := s.Sessions.SetPresence(sessionID, "idle", false); err == nil {
		s.appendEvent("session.presence", sessionID, payloadWithRequest(request.Request, map[string]any{
			"presence": "idle",
			"source":   source,
			"user_id":  request.Request.Actor.UserID,
		}))
	}

	activities := runResult.ToolActivities
	if len(activities) == 0 {
		activities = targetApp.Agent.GetLastToolActivities()
	}
	if s.RecordToolActivities != nil && len(activities) > 0 {
		s.RecordToolActivities(updatedSession, activities)
	}

	s.appendEvent("chat.completed", sessionID, payloadWithRequest(request.Request, map[string]any{
		"message":         message,
		"response_length": len(runResult.Content),
		"source":          source,
	}))

	return Result{
		Session:        updatedSession,
		Response:       runResult.Content,
		ToolActivities: activities,
	}, nil
}

func (s *ChannelService) DispatchStream(ctx context.Context, request inboundrouting.RoutedRequest, onChunk func(chunk string) error) (Result, error) {
	sessionID := strings.TrimSpace(request.Route.Session.SessionID)
	if sessionID == "" {
		return Result{}, fmt.Errorf("session id is required")
	}
	message := request.Request.Content.Text
	source := request.Route.Delivery.ChannelID

	if shouldEnqueue(request) {
		if _, err := s.Sessions.EnqueueTurn(sessionID); err == nil {
			s.appendEvent("session.queue.updated", sessionID, payloadWithRequest(request.Request, map[string]any{
				"queue_mode":   request.Route.Session.QueueMode,
				"source":       source,
				"reply_target": request.Route.Delivery.TargetRef,
				"streaming":    true,
			}))
		}
	}
	if _, err := s.Sessions.SetUserMapping(sessionID, UserMapping{
		UserID:        request.Request.Actor.UserID,
		UserName:      request.Request.Actor.DisplayName,
		ReplyTarget:   request.Route.Delivery.TargetRef,
		ThreadID:      request.Route.Delivery.ThreadID,
		TransportMeta: cloneStringMap(request.Route.Delivery.TransportMeta),
	}); err == nil {
		s.appendEvent("session.user_mapped", sessionID, payloadWithRequest(request.Request, map[string]any{
			"source":       source,
			"user_id":      request.Request.Actor.UserID,
			"user_name":    request.Request.Actor.DisplayName,
			"reply_target": request.Route.Delivery.TargetRef,
			"streaming":    true,
		}))
	}
	if _, err := s.Sessions.SetPresence(sessionID, "typing", true); err == nil {
		s.appendEvent("session.typing", sessionID, payloadWithRequest(request.Request, map[string]any{
			"typing":    true,
			"source":    source,
			"user_id":   request.Request.Actor.UserID,
			"streaming": true,
		}))
	}
	s.appendEvent(startedEventName(request), sessionID, payloadWithRequest(request.Request, map[string]any{
		"message":   message,
		"source":    source,
		"streaming": true,
	}))

	session, ok := s.Sessions.Get(sessionID)
	if !ok {
		return Result{}, fmt.Errorf("session not found: %s", sessionID)
	}
	targetApp, err := s.RuntimePool.GetOrCreate(session.Agent, session.Org, session.Project, session.Workspace)
	if err != nil {
		return Result{}, err
	}
	targetApp.Agent.SetHistory(session.History)

	runCtx := ctx
	finish := func() {}
	if s.BeginRun != nil {
		runCtx, finish = s.BeginRun(ctx, sessionID)
	}
	defer finish()

	execCtx := tools.WithBrowserSession(runCtx, sessionID)
	execCtx = tools.WithSandboxScope(execCtx, tools.SandboxScope{SessionID: sessionID, Channel: source})
	if s.DecorateContext != nil {
		execCtx = s.DecorateContext(execCtx, request, session, targetApp)
	}

	var response strings.Builder
	if err := targetApp.Agent.RunStream(execCtx, message, func(chunk string) {
		response.WriteString(chunk)
		if onChunk != nil {
			_ = onChunk(chunk)
		}
	}); err != nil {
		s.handleRunError(request, session, err)
		return Result{}, err
	}

	updatedSession, err := s.Sessions.AddExchange(sessionID, message, response.String())
	if err != nil {
		return Result{}, err
	}
	if _, err := s.Sessions.SetPresence(sessionID, "idle", false); err == nil {
		s.appendEvent("session.presence", sessionID, payloadWithRequest(request.Request, map[string]any{
			"presence":  "idle",
			"source":    source,
			"user_id":   request.Request.Actor.UserID,
			"streaming": true,
		}))
	}
	activities := targetApp.Agent.GetLastToolActivities()
	if s.RecordToolActivities != nil && len(activities) > 0 {
		s.RecordToolActivities(updatedSession, activities)
	}
	s.appendEvent("chat.completed", sessionID, payloadWithRequest(request.Request, map[string]any{
		"message":         message,
		"response_length": len(response.String()),
		"source":          source,
		"streaming":       true,
	}))
	return Result{
		Session:        updatedSession,
		Response:       response.String(),
		ToolActivities: activities,
	}, nil
}

func (s *ChannelService) appendEvent(eventType string, sessionID string, payload map[string]any) {
	if s.AppendEvent != nil {
		s.AppendEvent(eventType, sessionID, payload)
	}
}

func (s *ChannelService) handleRunError(request inboundrouting.RoutedRequest, session SessionSnapshot, err error) {
	if s.HandleRunError != nil {
		s.HandleRunError(request, session, err)
	}
}

func shouldEnqueue(request inboundrouting.RoutedRequest) bool {
	return !strings.EqualFold(strings.TrimSpace(request.Request.Content.Metadata["resume"]), "true")
}

func startedEventName(request inboundrouting.RoutedRequest) string {
	if strings.EqualFold(strings.TrimSpace(request.Request.Content.Metadata["resume"]), "true") {
		return "chat.resumed"
	}
	return "chat.started"
}

func payloadWithRequest(request coreingress.NormalizedRequest, base map[string]any) map[string]any {
	payload := make(map[string]any, len(base)+len(request.RouteContext.Metadata))
	for k, v := range base {
		payload[k] = v
	}
	for k, v := range request.RouteContext.Metadata {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			payload[k] = trimmed
		}
	}
	return payload
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
