package gateway

import (
	dispatchsvc "github.com/anyclaw/anyclaw/pkg/dispatch"
	"github.com/anyclaw/anyclaw/pkg/prompt"
	inboundrouting "github.com/anyclaw/anyclaw/pkg/routing/inbound"
)

type inboundMainAgentResolver struct {
	server *Server
}

func (r inboundMainAgentResolver) ResolveMainAgentName() string {
	if r.server == nil || r.server.app == nil || r.server.app.Config == nil {
		return "main"
	}
	return r.server.app.Config.ResolveMainAgentName()
}

type inboundWorkspaceResolver struct {
	server *Server
}

func (r inboundWorkspaceResolver) DefaultSelection() inboundrouting.WorkspaceRef {
	if r.server == nil || r.server.app == nil {
		return inboundrouting.WorkspaceRef{}
	}
	orgID, projectID, workspaceID := defaultResourceIDs(r.server.app.WorkingDir)
	return inboundrouting.WorkspaceRef{
		OrgID:       orgID,
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
	}
}

func (r inboundWorkspaceResolver) ResolveSelection(ref inboundrouting.WorkspaceRef) (inboundrouting.WorkspaceRef, error) {
	if r.server == nil {
		return ref, nil
	}
	org, project, workspace, err := r.server.validateResourceSelection(ref.OrgID, ref.ProjectID, ref.WorkspaceID)
	if err != nil {
		return inboundrouting.WorkspaceRef{}, err
	}
	return inboundrouting.WorkspaceRef{
		OrgID:       org.ID,
		ProjectID:   project.ID,
		WorkspaceID: workspace.ID,
	}, nil
}

type inboundSessionStoreAdapter struct {
	sessions *SessionManager
}

func (a inboundSessionStoreAdapter) Get(id string) (inboundrouting.SessionSnapshot, bool) {
	session, ok := a.sessions.Get(id)
	if !ok {
		return inboundrouting.SessionSnapshot{}, false
	}
	return inboundSessionSnapshot(*session), true
}

func (a inboundSessionStoreAdapter) FindByBinding(query inboundrouting.SessionBindingQuery) (inboundrouting.SessionSnapshot, bool) {
	session, ok := a.sessions.FindByBinding(query.SourceChannel, query.ReplyTarget, query.ThreadID, query.AgentID)
	if !ok {
		return inboundrouting.SessionSnapshot{}, false
	}
	return inboundSessionSnapshot(*session), true
}

func (a inboundSessionStoreAdapter) Create(opts inboundrouting.SessionCreateOptions) (inboundrouting.SessionSnapshot, error) {
	session, err := a.sessions.CreateWithOptions(SessionCreateOptions{
		Title:         opts.Title,
		AgentName:     opts.AgentID,
		Org:           opts.WorkspaceRef.OrgID,
		Project:       opts.WorkspaceRef.ProjectID,
		Workspace:     opts.WorkspaceRef.WorkspaceID,
		SessionMode:   opts.SessionMode,
		QueueMode:     opts.QueueMode,
		ReplyBack:     opts.ReplyBack,
		SourceChannel: opts.SourceChannel,
		SourceID:      opts.SourceID,
		UserID:        opts.UserID,
		UserName:      opts.UserName,
		ReplyTarget:   opts.ReplyTarget,
		ThreadID:      opts.ThreadID,
		TransportMeta: cloneStringMap(opts.TransportMeta),
		GroupKey:      opts.GroupKey,
		IsGroup:       opts.IsGroup,
	})
	if err != nil {
		return inboundrouting.SessionSnapshot{}, err
	}
	return inboundSessionSnapshot(*session), nil
}

type dispatchSessionStoreAdapter struct {
	sessions *SessionManager
}

func (a dispatchSessionStoreAdapter) EnqueueTurn(sessionID string) (dispatchsvc.SessionSnapshot, error) {
	session, err := a.sessions.EnqueueTurn(sessionID)
	if err != nil {
		return dispatchsvc.SessionSnapshot{}, err
	}
	return dispatchSessionSnapshot(*session), nil
}

func (a dispatchSessionStoreAdapter) SetUserMapping(sessionID string, mapping dispatchsvc.UserMapping) (dispatchsvc.SessionSnapshot, error) {
	session, err := a.sessions.SetUserMapping(sessionID, mapping.UserID, mapping.UserName, mapping.ReplyTarget, mapping.ThreadID, cloneStringMap(mapping.TransportMeta))
	if err != nil {
		return dispatchsvc.SessionSnapshot{}, err
	}
	return dispatchSessionSnapshot(*session), nil
}

func (a dispatchSessionStoreAdapter) SetPresence(sessionID string, presence string, typing bool) (dispatchsvc.SessionSnapshot, error) {
	session, err := a.sessions.SetPresence(sessionID, presence, typing)
	if err != nil {
		return dispatchsvc.SessionSnapshot{}, err
	}
	return dispatchSessionSnapshot(*session), nil
}

func (a dispatchSessionStoreAdapter) Get(sessionID string) (dispatchsvc.SessionSnapshot, bool) {
	session, ok := a.sessions.Get(sessionID)
	if !ok {
		return dispatchsvc.SessionSnapshot{}, false
	}
	return dispatchSessionSnapshot(*session), true
}

func (a dispatchSessionStoreAdapter) AddExchange(sessionID string, userText string, assistantText string) (dispatchsvc.SessionSnapshot, error) {
	session, err := a.sessions.AddExchange(sessionID, userText, assistantText)
	if err != nil {
		return dispatchsvc.SessionSnapshot{}, err
	}
	return dispatchSessionSnapshot(*session), nil
}

type taskDispatchSessionStoreAdapter struct {
	sessions taskSessionStore
}

func (a taskDispatchSessionStoreAdapter) EnqueueTurn(sessionID string) (dispatchsvc.TaskSessionSnapshot, error) {
	session, err := a.sessions.EnqueueTurn(sessionID)
	if err != nil {
		return dispatchsvc.TaskSessionSnapshot{}, err
	}
	return dispatchTaskSessionSnapshot(*session), nil
}

func (a taskDispatchSessionStoreAdapter) SetPresence(sessionID string, presence string, typing bool) (dispatchsvc.TaskSessionSnapshot, error) {
	session, err := a.sessions.SetPresence(sessionID, presence, typing)
	if err != nil {
		return dispatchsvc.TaskSessionSnapshot{}, err
	}
	return dispatchTaskSessionSnapshot(*session), nil
}

func (a taskDispatchSessionStoreAdapter) Get(sessionID string) (dispatchsvc.TaskSessionSnapshot, bool) {
	session, ok := a.sessions.Get(sessionID)
	if !ok {
		return dispatchsvc.TaskSessionSnapshot{}, false
	}
	return dispatchTaskSessionSnapshot(*session), true
}

func (a taskDispatchSessionStoreAdapter) AddExchange(sessionID string, userText string, assistantText string) (dispatchsvc.TaskSessionSnapshot, error) {
	session, err := a.sessions.AddExchange(sessionID, userText, assistantText)
	if err != nil {
		return dispatchsvc.TaskSessionSnapshot{}, err
	}
	return dispatchTaskSessionSnapshot(*session), nil
}

func inboundSessionSnapshot(session Session) inboundrouting.SessionSnapshot {
	return inboundrouting.SessionSnapshot{
		ID:            session.ID,
		AgentID:       session.Agent,
		OrgID:         session.Org,
		ProjectID:     session.Project,
		WorkspaceID:   session.Workspace,
		SessionMode:   session.SessionMode,
		QueueMode:     session.QueueMode,
		ReplyBack:     session.ReplyBack,
		ReplyTarget:   session.ReplyTarget,
		ThreadID:      session.ThreadID,
		TransportMeta: cloneStringMap(session.TransportMeta),
	}
}

func dispatchSessionSnapshot(session Session) dispatchsvc.SessionSnapshot {
	return dispatchsvc.SessionSnapshot{
		ID:        session.ID,
		Agent:     session.Agent,
		Org:       session.Org,
		Project:   session.Project,
		Workspace: session.Workspace,
		History:   append([]prompt.Message(nil), session.History...),
		ReplyBack: session.ReplyBack,
	}
}

func dispatchTaskSessionSnapshot(session Session) dispatchsvc.TaskSessionSnapshot {
	return dispatchsvc.TaskSessionSnapshot{
		ID:        session.ID,
		Agent:     session.Agent,
		Org:       session.Org,
		Project:   session.Project,
		Workspace: session.Workspace,
		History:   append([]prompt.Message(nil), session.History...),
	}
}
