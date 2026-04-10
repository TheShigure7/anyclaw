package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/config"
	"github.com/anyclaw/anyclaw/pkg/llm"
	"github.com/anyclaw/anyclaw/pkg/memory"
	appRuntime "github.com/anyclaw/anyclaw/pkg/runtime"
	"github.com/anyclaw/anyclaw/pkg/skills"
	"github.com/anyclaw/anyclaw/pkg/tools"
	"github.com/gorilla/websocket"
)

func TestOpenClawWSResolvesAppWorkflows(t *testing.T) {
	baseDir := t.TempDir()
	cfg := config.DefaultConfig()

	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	server := &Server{
		app: &appRuntime.App{
			Config:  cfg,
			Plugins: newWorkflowRegistryForTest(t),
			WorkDir: baseDir,
		},
		store:    store,
		sessions: NewSessionManager(store, nil),
		bus:      NewBus(),
		auth:     newAuthMiddleware(&cfg.Security),
		plugins:  newWorkflowRegistryForTest(t),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", server.wrap("/ws", server.handleOpenClawWS))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var challenge openClawWSFrame
	if err := conn.ReadJSON(&challenge); err != nil {
		t.Fatalf("ReadJSON challenge: %v", err)
	}
	challengeData, ok := challenge.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected challenge data map, got %#v", challenge.Data)
	}
	nonce, _ := challengeData["nonce"].(string)
	if nonce == "" {
		t.Fatalf("expected nonce in challenge frame: %#v", challengeData)
	}

	if err := conn.WriteJSON(openClawWSFrame{
		Type:   "req",
		ID:     "connect-1",
		Method: "connect",
		Params: map[string]any{"challenge": nonce},
	}); err != nil {
		t.Fatalf("WriteJSON connect: %v", err)
	}

	var connected openClawWSFrame
	if err := conn.ReadJSON(&connected); err != nil {
		t.Fatalf("ReadJSON connected: %v", err)
	}
	if connected.Type != "res" || !connected.OK {
		t.Fatalf("expected successful connect response, got %#v", connected)
	}

	if err := conn.WriteJSON(openClawWSFrame{
		Type:   "req",
		ID:     "workflow-resolve-1",
		Method: "app-workflows.resolve",
		Params: map[string]any{"q": "remove background from image", "limit": 2},
	}); err != nil {
		t.Fatalf("WriteJSON app-workflows.resolve: %v", err)
	}

	var resolved openClawWSFrame
	if err := conn.ReadJSON(&resolved); err != nil {
		t.Fatalf("ReadJSON app-workflows.resolve: %v", err)
	}
	if resolved.Type != "res" || !resolved.OK {
		t.Fatalf("expected successful workflow resolve response, got %#v", resolved)
	}
	payload, ok := resolved.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected workflow resolve payload map, got %#v", resolved.Data)
	}
	matches, ok := payload["matches"].([]any)
	if !ok || len(matches) == 0 {
		t.Fatalf("expected workflow matches, got %#v", payload)
	}
}

func TestOpenClawWSSessionLifecycleSupportsOpenClawStyleMethods(t *testing.T) {
	server := newOpenClawWSSessionTestServer(t, []*llm.Response{{Content: "session reply"}})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", server.wrap("/ws", server.handleOpenClawWS))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	conn := connectOpenClawWSTestClient(t, ts.URL)
	defer conn.Close()

	spawn := wsRoundTrip(t, conn, openClawWSFrame{
		Type:   "req",
		ID:     "spawn-1",
		Method: "sessions_spawn",
		Params: map[string]any{
			"title":     "WS Session",
			"agent":     "assistant",
			"workspace": "workspace-1",
		},
	})
	if !spawn.OK {
		t.Fatalf("expected sessions_spawn success, got %#v", spawn)
	}
	spawnData, ok := spawn.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected sessions_spawn payload map, got %#v", spawn.Data)
	}
	sessionMap, ok := spawnData["session"].(map[string]any)
	if !ok {
		t.Fatalf("expected session payload, got %#v", spawnData)
	}
	sessionID, _ := sessionMap["id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session id, got %#v", sessionMap)
	}

	send := wsRoundTrip(t, conn, openClawWSFrame{
		Type:   "req",
		ID:     "send-1",
		Method: "sessions_send",
		Params: map[string]any{
			"session_id": sessionID,
			"message":    "hello from ws",
		},
	})
	if !send.OK {
		t.Fatalf("expected sessions_send success, got %#v", send)
	}
	sendData, ok := send.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected sessions_send payload map, got %#v", send.Data)
	}
	if response, _ := sendData["response"].(string); response != "session reply" {
		t.Fatalf("expected session reply, got %#v", sendData)
	}

	history := wsRoundTrip(t, conn, openClawWSFrame{
		Type:   "req",
		ID:     "history-1",
		Method: "chat.history",
		Params: map[string]any{"session_id": sessionID},
	})
	if !history.OK {
		t.Fatalf("expected chat.history success, got %#v", history)
	}
	historyData, ok := history.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected chat.history payload map, got %#v", history.Data)
	}
	items, ok := historyData["history"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 history entries, got %#v", historyData["history"])
	}

	patch := wsRoundTrip(t, conn, openClawWSFrame{
		Type:   "req",
		ID:     "patch-1",
		Method: "sessions.patch",
		Params: map[string]any{
			"session_id": sessionID,
			"title":      "Renamed Session",
		},
	})
	if !patch.OK {
		t.Fatalf("expected sessions.patch success, got %#v", patch)
	}
	patchData, ok := patch.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected sessions.patch payload map, got %#v", patch.Data)
	}
	patchedSession, ok := patchData["session"].(map[string]any)
	if !ok || patchedSession["title"] != "Renamed Session" {
		t.Fatalf("expected patched session title, got %#v", patchData)
	}

	getSession := wsRoundTrip(t, conn, openClawWSFrame{
		Type:   "req",
		ID:     "get-1",
		Method: "sessions.get",
		Params: map[string]any{"session_id": sessionID},
	})
	if !getSession.OK {
		t.Fatalf("expected sessions.get success, got %#v", getSession)
	}

	del := wsRoundTrip(t, conn, openClawWSFrame{
		Type:   "req",
		ID:     "delete-1",
		Method: "sessions.delete",
		Params: map[string]any{"session_id": sessionID},
	})
	if !del.OK {
		t.Fatalf("expected sessions.delete success, got %#v", del)
	}
	if _, ok := server.sessions.Get(sessionID); ok {
		t.Fatalf("expected session %s to be deleted", sessionID)
	}
}

func TestWSSessionMethodsRespectHierarchyAccess(t *testing.T) {
	server := newOpenClawWSSessionTestServer(t, []*llm.Response{{Content: "reply"}})
	if err := server.store.UpsertWorkspace(&Workspace{ID: "workspace-2", ProjectID: "project-1", Name: "Workspace 2", Path: t.TempDir()}); err != nil {
		t.Fatalf("UpsertWorkspace(workspace-2): %v", err)
	}
	otherSession, err := server.sessions.CreateWithOptions(SessionCreateOptions{
		Title:       "other",
		AgentName:   "assistant",
		Org:         "org-1",
		Project:     "project-1",
		Workspace:   "workspace-2",
		SessionMode: "main",
		QueueMode:   "fifo",
	})
	if err != nil {
		t.Fatalf("CreateWithOptions(other): %v", err)
	}
	visibleSession, err := server.sessions.CreateWithOptions(SessionCreateOptions{
		Title:       "mine",
		AgentName:   "assistant",
		Org:         "org-1",
		Project:     "project-1",
		Workspace:   "workspace-1",
		SessionMode: "main",
		QueueMode:   "fifo",
	})
	if err != nil {
		t.Fatalf("CreateWithOptions(visible): %v", err)
	}

	user := &AuthUser{
		Name:        "viewer",
		Permissions: []string{"sessions.read", "sessions.write", "chat.send"},
		Workspaces:  []string{"workspace-1"},
	}
	conn := &openClawWSConn{server: server, user: user}
	filtered := conn.filteredSessions(nil)
	if len(filtered) != 1 || filtered[0].ID != visibleSession.ID {
		t.Fatalf("expected only visible session, got %#v", filtered)
	}

	if _, err := server.wsSessionGet(user, map[string]any{"session_id": otherSession.ID}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden session get, got %v", err)
	}
	if _, err := server.wsSessionSend(context.Background(), user, map[string]any{"session_id": otherSession.ID, "message": "hi"}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden session send, got %v", err)
	}
	if _, err := server.wsSessionDelete(user, map[string]any{"session_id": otherSession.ID}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden session delete, got %v", err)
	}
}

func newOpenClawWSSessionTestServer(t *testing.T, responses []*llm.Response) *Server {
	t.Helper()

	baseDir := t.TempDir()
	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.UpsertOrg(&Org{ID: "org-1", Name: "Org"}); err != nil {
		t.Fatalf("UpsertOrg: %v", err)
	}
	if err := store.UpsertProject(&Project{ID: "project-1", OrgID: "org-1", Name: "Project"}); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	workspacePath := t.TempDir()
	if err := store.UpsertWorkspace(&Workspace{ID: "workspace-1", ProjectID: "project-1", Name: "Workspace", Path: workspacePath}); err != nil {
		t.Fatalf("UpsertWorkspace: %v", err)
	}

	mem := memory.NewFileMemory(t.TempDir())
	if err := mem.Init(); err != nil {
		t.Fatalf("memory init: %v", err)
	}
	llmStub := &stubTaskLLM{responses: responses}
	ag := agent.New(agent.Config{
		Name:        "assistant",
		Description: "test assistant",
		LLM:         llmStub,
		Memory:      mem,
		Skills:      skills.NewSkillsManager(""),
		Tools:       tools.NewRegistry(),
		WorkDir:     t.TempDir(),
	})
	cfg := config.DefaultConfig()
	cfg.Agent.Name = "assistant"
	app := &appRuntime.App{
		Config:     cfg,
		Agent:      ag,
		WorkingDir: workspacePath,
		WorkDir:    baseDir,
	}
	pool := NewRuntimePool("ignored", store, 4, time.Hour)
	now := time.Now().UTC()
	pool.runtimes[runtimeKey("assistant", "org-1", "project-1", "workspace-1")] = &runtimeEntry{
		app:        app,
		createdAt:  now,
		lastUsedAt: now,
	}

	return &Server{
		app:         app,
		store:       store,
		sessions:    NewSessionManager(store, ag),
		bus:         NewBus(),
		auth:        newAuthMiddleware(&cfg.Security),
		runtimePool: pool,
	}
}

func connectOpenClawWSTestClient(t *testing.T, baseURL string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var challenge openClawWSFrame
	if err := conn.ReadJSON(&challenge); err != nil {
		t.Fatalf("ReadJSON challenge: %v", err)
	}
	challengeData, ok := challenge.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected challenge data map, got %#v", challenge.Data)
	}
	nonce, _ := challengeData["nonce"].(string)
	if nonce == "" {
		t.Fatalf("expected nonce in challenge frame: %#v", challengeData)
	}
	if err := conn.WriteJSON(openClawWSFrame{
		Type:   "req",
		ID:     "connect-1",
		Method: "connect",
		Params: map[string]any{"challenge": nonce},
	}); err != nil {
		t.Fatalf("WriteJSON connect: %v", err)
	}
	var connected openClawWSFrame
	if err := conn.ReadJSON(&connected); err != nil {
		t.Fatalf("ReadJSON connected: %v", err)
	}
	if connected.Type != "res" || !connected.OK {
		t.Fatalf("expected successful connect response, got %#v", connected)
	}
	return conn
}

func wsRoundTrip(t *testing.T, conn *websocket.Conn, frame openClawWSFrame) openClawWSFrame {
	t.Helper()
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("WriteJSON %s: %v", frame.Method, err)
	}
	var response openClawWSFrame
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("ReadJSON %s: %v", frame.Method, err)
	}
	return response
}
