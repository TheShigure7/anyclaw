package agenthub

import (
	"context"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/prompt"
)

type RunRequest struct {
	SessionID                    string
	UserInput                    string
	History                      []prompt.Message
	SyncHistory                  bool
	PreferredPersistentSubagent  string
	SkipDelegation               bool
}

type RunResult struct {
	Content         string
	Source          string
	SourceID        string
	ToolActivities  []agent.ToolActivity
	DelegationTrace []DelegationTrace
}

type DelegationTrace struct {
	Kind          string        `json:"kind"`
	AgentID       string        `json:"agent_id"`
	DisplayName   string        `json:"display_name"`
	Status        string        `json:"status"`
	Reason        string        `json:"reason,omitempty"`
	ResultSummary string        `json:"result_summary,omitempty"`
	Error         string        `json:"error,omitempty"`
	StartedAt     time.Time     `json:"started_at"`
	CompletedAt   time.Time     `json:"completed_at"`
	Duration      time.Duration `json:"duration"`
}

type PersistentSubagentView struct {
	ID                string    `json:"id"`
	DisplayName       string    `json:"display_name"`
	Description       string    `json:"description,omitempty"`
	AvatarPreset      string    `json:"avatar_preset,omitempty"`
	AvatarDataURL     string    `json:"avatar_data_url,omitempty"`
	Domain            string    `json:"domain,omitempty"`
	Expertise         []string  `json:"expertise,omitempty"`
	Status            string    `json:"status"`
	ManagedByMain     bool      `json:"managed_by_main"`
	Visibility        string    `json:"visibility,omitempty"`
	PermissionLevel   string    `json:"permission_level,omitempty"`
	ModelMode         string    `json:"model_mode,omitempty"`
	WorkingDir        string    `json:"working_dir,omitempty"`
	RequiredSkills    []string  `json:"required_skills,omitempty"`
	RequiredCLIs      []string  `json:"required_clis,omitempty"`
	SessionCount      int       `json:"session_count"`
	LastActiveAt      time.Time `json:"last_active_at,omitempty"`
	RecentTaskSummary string    `json:"recent_task_summary,omitempty"`
}

type Controller interface {
	Run(ctx context.Context, req RunRequest) (*RunResult, error)
	ClearSession(sessionID string)
	MainAgentName() string
	ListPersistentSubagents() []PersistentSubagentView
	GetPersistentSubagent(id string) (PersistentSubagentView, bool)
}
