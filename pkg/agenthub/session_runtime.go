package agenthub

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/prompt"
)

type runtimeFactory func() *agent.Agent

type sessionRuntime struct {
	agent      *agent.Agent
	lastActive time.Time
}

type sessionAgentManager struct {
	mu       sync.Mutex
	runtimes map[string]*sessionRuntime
	factory  runtimeFactory
	ttl      time.Duration
}

func newSessionAgentManager(factory runtimeFactory, ttl time.Duration) *sessionAgentManager {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &sessionAgentManager{
		runtimes: make(map[string]*sessionRuntime),
		factory:  factory,
		ttl:      ttl,
	}
}

func (m *sessionAgentManager) Run(ctx context.Context, sessionID string, history []prompt.Message, historyProvided bool, input string) (string, []agent.ToolActivity, error) {
	now := time.Now().UTC()
	m.mu.Lock()
	m.cleanupExpiredLocked(now)
	m.mu.Unlock()
	if strings.TrimSpace(sessionID) == "" {
		ag := m.factory()
		if historyProvided {
			ag.SetHistory(cloneHistory(history))
		}
		result, err := ag.Run(ctx, input)
		return result, ag.GetLastToolActivities(), err
	}

	m.mu.Lock()
	rt := m.ensureRuntimeLocked(sessionID)
	if historyProvided {
		rt.agent.SetHistory(cloneHistory(history))
	}
	rt.lastActive = time.Now().UTC()
	ag := rt.agent
	m.mu.Unlock()

	result, err := ag.Run(ctx, input)
	activities := ag.GetLastToolActivities()

	m.mu.Lock()
	if current, ok := m.runtimes[sessionID]; ok {
		current.lastActive = time.Now().UTC()
	}
	m.mu.Unlock()

	return result, activities, err
}

func (m *sessionAgentManager) History(sessionID string) []prompt.Message {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.runtimes[sessionID]; ok {
		return cloneHistory(rt.agent.GetHistory())
	}
	return nil
}

func (m *sessionAgentManager) RecordExchange(sessionID string, userInput string, response string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.ensureRuntimeLocked(sessionID)
	history := cloneHistory(rt.agent.GetHistory())
	history = append(history,
		prompt.Message{Role: "user", Content: userInput},
		prompt.Message{Role: "assistant", Content: response},
	)
	rt.agent.SetHistory(history)
	rt.lastActive = time.Now().UTC()
}

func (m *sessionAgentManager) Clear(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runtimes, sessionID)
}

func (m *sessionAgentManager) Stats() (int, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(time.Now().UTC())
	var lastActive time.Time
	for _, rt := range m.runtimes {
		if rt.lastActive.After(lastActive) {
			lastActive = rt.lastActive
		}
	}
	return len(m.runtimes), lastActive
}

func (m *sessionAgentManager) ensureRuntimeLocked(sessionID string) *sessionRuntime {
	if rt, ok := m.runtimes[sessionID]; ok {
		return rt
	}
	rt := &sessionRuntime{
		agent:      m.factory(),
		lastActive: time.Now().UTC(),
	}
	m.runtimes[sessionID] = rt
	return rt
}

func (m *sessionAgentManager) cleanupExpiredLocked(now time.Time) {
	for sessionID, rt := range m.runtimes {
		if now.Sub(rt.lastActive) > m.ttl {
			delete(m.runtimes, sessionID)
		}
	}
}

func cloneHistory(history []prompt.Message) []prompt.Message {
	if len(history) == 0 {
		return nil
	}
	items := make([]prompt.Message, len(history))
	copy(items, history)
	return items
}
