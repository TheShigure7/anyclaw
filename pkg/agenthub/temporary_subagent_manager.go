package agenthub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/memory"
	"github.com/anyclaw/anyclaw/pkg/orchestrator"
	"github.com/anyclaw/anyclaw/pkg/prompt"
	"github.com/anyclaw/anyclaw/pkg/skills"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type TemporarySubagentManagerOptions struct {
	LLM             agent.LLMCaller
	Memory          memory.MemoryBackend
	BaseSkills      *skills.SkillsManager
	BaseTools       *tools.Registry
	PermissionLevel string
	WorkDir         string
	WorkingDir      string
	TTL             time.Duration
}

type TemporarySubagentManager struct {
	manager   *sessionAgentManager
	lifecycle *orchestrator.AgentLifecycle
}

func NewTemporarySubagentManager(opts TemporarySubagentManagerOptions) *TemporarySubagentManager {
	if opts.LLM == nil {
		return nil
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	filteredTools := filteredPersistentSubagentTools(opts.BaseTools, firstNonEmpty(strings.TrimSpace(opts.PermissionLevel), "limited"))
	filteredSkills := filteredPersistentSubagentSkills(opts.BaseSkills, nil)
	if filteredSkills != nil {
		filteredSkills.RegisterTools(filteredTools, skills.ExecutionOptions{AllowExec: true, ExecTimeoutSeconds: 30})
	}

	factory := func() *agent.Agent {
		return agent.New(agent.Config{
			Name:             "Temporary Subagent Worker",
			Description:      "Internal temporary subagent delegated by the main agent",
			Personality:      buildTemporarySubagentPrompt(),
			LLM:              opts.LLM,
			Memory:           opts.Memory,
			Skills:           filteredSkills,
			Tools:            filteredTools,
			WorkDir:          opts.WorkDir,
			WorkingDir:       opts.WorkingDir,
			MaxContextTokens: 8192,
		})
	}

	return &TemporarySubagentManager{
		manager:   newSessionAgentManager(factory, ttl),
		lifecycle: orchestrator.NewAgentLifecycle(2, 128),
	}
}

func (m *TemporarySubagentManager) Run(ctx context.Context, sessionID string, history []prompt.Message, historyProvided bool, input string) (string, []agent.ToolActivity, string, error) {
	if m == nil || m.manager == nil {
		return "", nil, "", fmt.Errorf("temporary subagent manager is not available")
	}

	subagentID := fmt.Sprintf("temporary-subagent-%d", time.Now().UnixNano())
	if managed, err := m.lifecycle.Spawn(subagentID, "", map[string]string{
		"session_id": sessionID,
		"mode":       "temporary_subagent",
	}); err == nil && managed != nil {
		subagentID = managed.ID
		_ = m.lifecycle.Start(subagentID)
	}

	result, activities, err := m.manager.Run(ctx, sessionID, history, historyProvided, input)
	if err != nil {
		_ = m.lifecycle.Fail(subagentID, err.Error())
		return "", activities, subagentID, err
	}
	_ = m.lifecycle.Complete(subagentID, result)
	return result, activities, subagentID, nil
}

func (m *TemporarySubagentManager) ClearSession(sessionID string) {
	if m == nil || m.manager == nil {
		return
	}
	m.manager.Clear(sessionID)
}

func buildTemporarySubagentPrompt() string {
	return strings.Join([]string{
		"You are an internal temporary subagent delegated by the main AnyClaw agent.",
		"You are not a public-facing assistant.",
		"Focus on completing the delegated task efficiently using the available tools and skills.",
		"Return the concrete result needed by the main agent, including blockers or evidence when relevant.",
	}, "\n\n")
}
