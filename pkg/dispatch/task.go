package dispatch

import (
	"context"
	"fmt"
	"strings"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/agenthub"
	"github.com/anyclaw/anyclaw/pkg/prompt"
	appRuntime "github.com/anyclaw/anyclaw/pkg/runtime"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type TaskSessionSnapshot struct {
	ID        string
	Agent     string
	Org       string
	Project   string
	Workspace string
	History   []prompt.Message
}

type TaskSessionStore interface {
	EnqueueTurn(sessionID string) (TaskSessionSnapshot, error)
	SetPresence(sessionID string, presence string, typing bool) (TaskSessionSnapshot, error)
	Get(sessionID string) (TaskSessionSnapshot, bool)
	AddExchange(sessionID string, userText string, assistantText string) (TaskSessionSnapshot, error)
}

type TaskRuntimePool interface {
	GetOrCreate(agentName string, org string, project string, workspaceID string) (*appRuntime.App, error)
}

type TaskContextDecorator func(ctx context.Context, request TaskRunRequest, session TaskSessionSnapshot, targetApp *appRuntime.App) context.Context
type TaskRunErrorHandler func(request TaskRunRequest, session TaskSessionSnapshot, err error)

type TaskRunRequest struct {
	SessionID       string
	UserInput       string
	History         []prompt.Message
	Channel         string
	DecorateContext TaskContextDecorator
	HandleRunError  TaskRunErrorHandler
}

type TaskRunResult struct {
	Session        TaskSessionSnapshot
	Response       string
	ToolActivities []agent.ToolActivity
}

type TaskService struct {
	Sessions    TaskSessionStore
	RuntimePool TaskRuntimePool
}

func (s *TaskService) Run(ctx context.Context, request TaskRunRequest) (TaskRunResult, error) {
	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		return TaskRunResult{}, fmt.Errorf("session id is required")
	}
	if s.Sessions == nil {
		return TaskRunResult{}, fmt.Errorf("task session store not initialized")
	}
	if s.RuntimePool == nil {
		return TaskRunResult{}, fmt.Errorf("runtime pool not initialized")
	}
	channel := strings.TrimSpace(request.Channel)
	if channel == "" {
		channel = "task"
	}

	if _, err := s.Sessions.EnqueueTurn(sessionID); err == nil {
		_, _ = s.Sessions.SetPresence(sessionID, "typing", true)
	}

	session, ok := s.Sessions.Get(sessionID)
	if !ok {
		return TaskRunResult{}, fmt.Errorf("session not found: %s", sessionID)
	}

	targetApp, err := s.RuntimePool.GetOrCreate(session.Agent, session.Org, session.Project, session.Workspace)
	if err != nil {
		return TaskRunResult{Session: session}, err
	}
	history := append([]prompt.Message(nil), request.History...)
	targetApp.Agent.SetHistory(history)

	execCtx := tools.WithBrowserSession(ctx, sessionID)
	execCtx = tools.WithSandboxScope(execCtx, tools.SandboxScope{SessionID: sessionID, Channel: channel})
	if request.DecorateContext != nil {
		execCtx = request.DecorateContext(execCtx, request, session, targetApp)
	}

	runResult, err := targetApp.RunUserTask(execCtx, agenthub.RunRequest{
		SessionID:   sessionID,
		UserInput:   request.UserInput,
		History:     history,
		SyncHistory: true,
	})

	response := ""
	toolActivities := []agent.ToolActivity(nil)
	if runResult != nil {
		response = runResult.Content
		toolActivities = runResult.ToolActivities
	}
	if err != nil {
		if request.HandleRunError != nil {
			request.HandleRunError(request, session, err)
		}
		return TaskRunResult{
			Session:        session,
			Response:       response,
			ToolActivities: toolActivities,
		}, err
	}

	updatedSession, err := s.Sessions.AddExchange(sessionID, request.UserInput, response)
	if err != nil {
		if request.HandleRunError != nil {
			request.HandleRunError(request, session, err)
		}
		return TaskRunResult{
			Session:        session,
			Response:       response,
			ToolActivities: toolActivities,
		}, err
	}
	_, _ = s.Sessions.SetPresence(updatedSession.ID, "idle", false)

	return TaskRunResult{
		Session:        updatedSession,
		Response:       response,
		ToolActivities: toolActivities,
	}, nil
}
