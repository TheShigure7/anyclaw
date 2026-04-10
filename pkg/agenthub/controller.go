package agenthub

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/config"
	"github.com/anyclaw/anyclaw/pkg/memory"
	"github.com/anyclaw/anyclaw/pkg/prompt"
	"github.com/anyclaw/anyclaw/pkg/skills"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type MainControllerOptions struct {
	MainAgentName        string
	MainAgentDescription string
	MainPersonality      string
	LLM                  agent.LLMCaller
	Memory               memory.MemoryBackend
	Skills               *skills.SkillsManager
	Tools                *tools.Registry
	WorkDir              string
	WorkingDir           string
	Delegation           config.DelegationConfig
	PersistentSubagents  *PersistentSubagentRegistry
	TemporarySubagents   *TemporarySubagentManager
}

type MainController struct {
	mu                  sync.RWMutex
	mainAgentName       string
	mainMemory          memory.MemoryBackend
	mainManager         *sessionAgentManager
	delegation          config.DelegationConfig
	persistentSubagents *PersistentSubagentRegistry
	temporarySubagents  *TemporarySubagentManager
}

func NewMainController(opts MainControllerOptions) *MainController {
	mainFactory := func() *agent.Agent {
		return agent.New(agent.Config{
			Name:             strings.TrimSpace(opts.MainAgentName),
			Description:      strings.TrimSpace(opts.MainAgentDescription),
			Personality:      strings.TrimSpace(opts.MainPersonality),
			LLM:              opts.LLM,
			Memory:           opts.Memory,
			Skills:           opts.Skills,
			Tools:            opts.Tools,
			WorkDir:          opts.WorkDir,
			WorkingDir:       opts.WorkingDir,
			MaxContextTokens: 8192,
		})
	}

	ttl := time.Duration(opts.Delegation.PersistentSubagentSessionTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	return &MainController{
		mainAgentName:       strings.TrimSpace(opts.MainAgentName),
		mainMemory:          opts.Memory,
		mainManager:         newSessionAgentManager(mainFactory, ttl),
		delegation:          opts.Delegation,
		persistentSubagents: opts.PersistentSubagents,
		temporarySubagents:  opts.TemporarySubagents,
	}
}

func (c *MainController) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	input := strings.TrimSpace(req.UserInput)
	if input == "" {
		return nil, fmt.Errorf("user input is required")
	}

	c.mu.RLock()
	persistentSubagents := c.persistentSubagents
	delegation := c.delegation
	mainName := c.mainAgentName
	temporarySubagents := c.temporarySubagents
	c.mu.RUnlock()

	history := cloneHistory(req.History)
	historyProvided := req.SyncHistory
	if !historyProvided && strings.TrimSpace(req.SessionID) != "" {
		history = c.mainManager.History(req.SessionID)
		historyProvided = len(history) > 0
	}

	traces := []DelegationTrace{}
	if !req.SkipDelegation && delegation.PersistentSubagentFirst && persistentSubagents != nil {
		if subagent, reason, ok := persistentSubagents.Match(input, req.PreferredPersistentSubagent); ok {
			started := time.Now().UTC()
			subagentResult, activities, err := persistentSubagents.Run(ctx, subagent.ID, req.SessionID, history, historyProvided, input)
			completed := time.Now().UTC()
			trace := DelegationTrace{
				Kind:          "persistent_subagent",
				AgentID:       subagent.ID,
				DisplayName:   subagent.DisplayName,
				Status:        "completed",
				Reason:        reason,
				ResultSummary: summarizeText(subagentResult, 180),
				StartedAt:     started,
				CompletedAt:   completed,
				Duration:      completed.Sub(started),
			}
			if err == nil {
				publicResponse := c.finalizePersistentSubagentResult(subagent, subagentResult)
				c.syncMainConversation(req.SessionID, input, publicResponse)
				return &RunResult{
					Content:         publicResponse,
					Source:          "persistent_subagent",
					SourceID:        subagent.ID,
					ToolActivities:  activities,
					DelegationTrace: append(traces, trace),
				}, nil
			}
			trace.Status = "failed"
			trace.Error = err.Error()
			traces = append(traces, trace)
		}
	}

	if !req.SkipDelegation && delegation.AllowTemporarySubagents && temporarySubagents != nil {
		started := time.Now().UTC()
		subagentResult, activities, subagentID, err := temporarySubagents.Run(ctx, req.SessionID, history, historyProvided, input)
		completed := time.Now().UTC()
		trace := DelegationTrace{
			Kind:          "temporary_subagent",
			AgentID:       subagentID,
			DisplayName:   "Temporary Subagent Worker",
			Status:        "completed",
			Reason:        "fallback to temporary subagent",
			ResultSummary: summarizeText(subagentResult, 180),
			StartedAt:     started,
			CompletedAt:   completed,
			Duration:      completed.Sub(started),
		}
		if err == nil {
			publicResponse := c.finalizeTemporarySubagentResult(subagentResult)
			c.syncMainConversation(req.SessionID, input, publicResponse)
			return &RunResult{
				Content:         publicResponse,
				Source:          "temporary_subagent",
				SourceID:        subagentID,
				ToolActivities:  activities,
				DelegationTrace: append(traces, trace),
			}, nil
		}
		trace.Status = "failed"
		trace.Error = err.Error()
		traces = append(traces, trace)
	}

	response, activities, err := c.mainManager.Run(ctx, req.SessionID, history, historyProvided, input)
	if err != nil {
		return nil, err
	}
	return &RunResult{
		Content:         response,
		Source:          "main",
		SourceID:        mainName,
		ToolActivities:  activities,
		DelegationTrace: traces,
	}, nil
}

func (c *MainController) ClearSession(sessionID string) {
	c.mainManager.Clear(sessionID)
	c.mu.RLock()
	persistentSubagents := c.persistentSubagents
	temporarySubagents := c.temporarySubagents
	c.mu.RUnlock()
	if persistentSubagents != nil {
		persistentSubagents.ClearSession(sessionID)
	}
	if temporarySubagents != nil {
		temporarySubagents.ClearSession(sessionID)
	}
}

func (c *MainController) MainAgentName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mainAgentName
}

func (c *MainController) ListPersistentSubagents() []PersistentSubagentView {
	c.mu.RLock()
	persistentSubagents := c.persistentSubagents
	c.mu.RUnlock()
	if persistentSubagents == nil {
		return nil
	}
	return persistentSubagents.List()
}

func (c *MainController) GetPersistentSubagent(id string) (PersistentSubagentView, bool) {
	c.mu.RLock()
	persistentSubagents := c.persistentSubagents
	c.mu.RUnlock()
	if persistentSubagents == nil {
		return PersistentSubagentView{}, false
	}
	return persistentSubagents.Get(id)
}

func (c *MainController) UpdatePersistentSubagents(registry *PersistentSubagentRegistry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.persistentSubagents = registry
}

func (c *MainController) UpdateDelegation(cfg config.DelegationConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delegation = cfg
}

func (c *MainController) UpdateTemporarySubagents(manager *TemporarySubagentManager) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.temporarySubagents = manager
}

func (c *MainController) syncMainConversation(sessionID string, userInput string, response string) {
	if strings.TrimSpace(sessionID) != "" {
		c.mainManager.RecordExchange(sessionID, userInput, response)
	}
	if c.mainMemory == nil {
		return
	}
	_ = c.mainMemory.Add(memory.MemoryEntry{Type: "conversation", Role: "user", Content: userInput})
	_ = c.mainMemory.Add(memory.MemoryEntry{Type: "conversation", Role: "assistant", Content: response})
}

func (c *MainController) finalizePersistentSubagentResult(subagent PersistentSubagentView, subagentResult string) string {
	result := strings.TrimSpace(subagentResult)
	name := strings.TrimSpace(subagent.DisplayName)
	if name == "" {
		name = strings.TrimSpace(subagent.ID)
	}
	switch {
	case result == "":
		return fmt.Sprintf("I asked %s to handle this, but it did not return any visible result yet.", name)
	case name == "":
		return result
	default:
		return fmt.Sprintf("I asked %s to handle this.\n\n%s", name, result)
	}
}

func (c *MainController) finalizeTemporarySubagentResult(subagentResult string) string {
	result := strings.TrimSpace(subagentResult)
	if result == "" {
		return "I used an internal temporary subagent for this task, but it did not return any visible result yet."
	}
	return result
}

func HistoryFromSessionMessages(messages []struct {
	Role    string
	Content string
}) []prompt.Message {
	if len(messages) == 0 {
		return nil
	}
	history := make([]prompt.Message, 0, len(messages))
	for _, msg := range messages {
		history = append(history, prompt.Message{Role: msg.Role, Content: msg.Content})
	}
	return history
}
