package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agenthub"
	"github.com/anyclaw/anyclaw/pkg/orchestrator"
	"github.com/anyclaw/anyclaw/pkg/prompt"
)

type mainChatManager struct {
	mu            sync.RWMutex
	sessions      map[string]*Session
	controller    agenthub.Controller
	mainAgentName string
	idCount       int
}

func NewMainChatManager(controller agenthub.Controller, mainAgentName string) ChatManager {
	return &mainChatManager{
		sessions:      make(map[string]*Session),
		controller:    controller,
		mainAgentName: strings.TrimSpace(mainAgentName),
	}
}

func (m *mainChatManager) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if m.controller == nil {
		return nil, fmt.Errorf("main controller not available")
	}
	if strings.TrimSpace(req.Message) == "" {
		return nil, fmt.Errorf("message is required")
	}
	if name := strings.TrimSpace(req.AgentName); name != "" &&
		!strings.EqualFold(name, m.mainAgentName) &&
		!strings.EqualFold(name, "main") &&
		!strings.EqualFold(name, "main-agent") {
		return nil, fmt.Errorf("agent not found: %s", req.AgentName)
	}

	m.mu.Lock()
	var session *Session
	var ok bool
	if req.SessionID != "" {
		session, ok = m.sessions[req.SessionID]
		if !ok {
			m.mu.Unlock()
			return nil, fmt.Errorf("session not found: %s", req.SessionID)
		}
	} else {
		m.idCount++
		sessionID := fmt.Sprintf("chat_%d_%d", time.Now().UnixNano(), m.idCount)
		session = &Session{
			ID:        sessionID,
			AgentName: m.mainAgentName,
			Title:     shortenMessage(req.Message),
			Messages:  make([]Message, 0),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		m.sessions[sessionID] = session
	}
	history := sessionPromptHistory(session.Messages)
	m.mu.Unlock()

	result, err := m.controller.Run(ctx, agenthub.RunRequest{
		SessionID:   session.ID,
		UserInput:   req.Message,
		History:     history,
		SyncHistory: true,
	})
	if err != nil {
		return nil, fmt.Errorf("agent error: %w", err)
	}

	m.mu.Lock()
	userMsg := Message{Role: "user", Content: req.Message, Timestamp: time.Now()}
	assistantMsg := Message{Role: "assistant", Content: result.Content, AgentName: m.mainAgentName, Timestamp: time.Now()}
	session.Messages = append(session.Messages, userMsg, assistantMsg)
	session.UpdatedAt = time.Now()
	historyCopy := make([]Message, len(session.Messages))
	copy(historyCopy, session.Messages)
	m.mu.Unlock()

	return &ChatResponse{
		SessionID: session.ID,
		AgentName: m.mainAgentName,
		Message:   assistantMsg,
		History:   historyCopy,
	}, nil
}

func (m *mainChatManager) GetSession(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	s := *session
	return &s, nil
}

func (m *mainChatManager) ListSessions() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, *s)
	}
	return list
}

func (m *mainChatManager) GetSessionHistory(sessionID string) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	history := make([]Message, len(session.Messages))
	copy(history, session.Messages)
	return history, nil
}

func (m *mainChatManager) DeleteSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	delete(m.sessions, sessionID)
	if m.controller != nil {
		m.controller.ClearSession(sessionID)
	}
	return nil
}

func (m *mainChatManager) ListAgents() []orchestrator.AgentInfo {
	return nil
}

func sessionPromptHistory(messages []Message) []prompt.Message {
	if len(messages) == 0 {
		return nil
	}
	history := make([]prompt.Message, 0, len(messages))
	for _, msg := range messages {
		history = append(history, prompt.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return history
}
