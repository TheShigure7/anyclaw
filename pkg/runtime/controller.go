package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/agenthub"
	"github.com/anyclaw/anyclaw/pkg/config"
)

func (a *App) RunUserTask(ctx context.Context, req agenthub.RunRequest) (*agenthub.RunResult, error) {
	if a != nil && a.MainController != nil {
		return a.MainController.Run(ctx, req)
	}
	if a == nil || a.Agent == nil {
		return nil, fmt.Errorf("runtime app is not initialized")
	}
	if req.SyncHistory {
		a.Agent.SetHistory(req.History)
	}
	result, err := a.Agent.Run(ctx, req.UserInput)
	return &agenthub.RunResult{
		Content:        result,
		Source:         "main",
		SourceID:       a.Config.Agent.Name,
		ToolActivities: a.Agent.GetLastToolActivities(),
	}, err
}

func (a *App) ClearUserSession(sessionID string) {
	if a != nil && a.MainController != nil {
		a.MainController.ClearSession(sessionID)
	}
}

func (a *App) ListPersistentSubagents() []agenthub.PersistentSubagentView {
	if a == nil || a.MainController == nil {
		return nil
	}
	return a.MainController.ListPersistentSubagents()
}

func (a *App) GetPersistentSubagent(id string) (agenthub.PersistentSubagentView, bool) {
	if a == nil || a.MainController == nil {
		return agenthub.PersistentSubagentView{}, false
	}
	return a.MainController.GetPersistentSubagent(id)
}

func (a *App) LastToolActivities() []agent.ToolActivity {
	if a == nil || a.Agent == nil {
		return nil
	}
	return a.Agent.GetLastToolActivities()
}

func (a *App) RefreshPersistentSubagents() error {
	if a == nil || a.Config == nil || a.LLM == nil {
		return fmt.Errorf("runtime app is not initialized")
	}

	baseProfiles := append([]config.PersistentSubagentProfile(nil), a.ConfiguredPersistentSubagents...)
	if a.Market != nil {
		if installedSubagents, err := a.Market.PersistentSubagentProfiles(); err == nil && len(installedSubagents) > 0 {
			baseProfiles = mergePersistentSubagentProfiles(baseProfiles, installedSubagents)
		}
	}
	a.Config.PersistentSubagents.Profiles = baseProfiles

	registry, err := agenthub.NewPersistentSubagentRegistry(a.Config.PersistentSubagents, agenthub.PersistentSubagentRegistryOptions{
		ConfigPath:        a.ConfigPath,
		DefaultWorkingDir: a.WorkingDir,
		LLM:               a.LLM,
		BaseSkills:        a.Skills,
		BaseTools:         a.Tools,
		SessionTTL:        time.Duration(a.Config.Delegation.PersistentSubagentSessionTTLMinutes) * time.Minute,
	})
	if err != nil {
		return err
	}
	a.PersistentSubagents = registry
	a.TemporarySubagents = agenthub.NewTemporarySubagentManager(agenthub.TemporarySubagentManagerOptions{
		LLM:             a.LLM,
		Memory:          a.Memory,
		BaseSkills:      a.Skills,
		BaseTools:       a.Tools,
		PermissionLevel: a.Config.Agent.PermissionLevel,
		WorkDir:         a.WorkDir,
		WorkingDir:      a.WorkingDir,
		TTL:             time.Duration(a.Config.Delegation.TemporarySubagentTTLMinutes) * time.Minute,
	})

	if controller, ok := a.MainController.(*agenthub.MainController); ok && controller != nil {
		controller.UpdatePersistentSubagents(registry)
		controller.UpdateDelegation(a.Config.Delegation)
		controller.UpdateTemporarySubagents(a.TemporarySubagents)
		return nil
	}

	a.MainController = agenthub.NewMainController(agenthub.MainControllerOptions{
		MainAgentName:        a.Config.Agent.Name,
		MainAgentDescription: a.Config.Agent.Description,
		MainPersonality:      agent.BuildPersonalityPrompt(resolveMainAgentPersonality(a.Config)),
		LLM:                  a.LLM,
		Memory:               a.Memory,
		Skills:               a.Skills,
		Tools:                a.Tools,
		WorkDir:              a.WorkDir,
		WorkingDir:           a.WorkingDir,
		Delegation:           a.Config.Delegation,
		PersistentSubagents:  registry,
		TemporarySubagents:   a.TemporarySubagents,
	})
	return nil
}
