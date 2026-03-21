package task

type Operation struct {
	Tool             string            `json:"tool"`
	Params           map[string]string `json:"params"`
	RiskLevel        string            `json:"risk_level"`
	RequiresApproval bool              `json:"requires_approval"`
	Status           string            `json:"status"`
	Result           string            `json:"result"`
}

type Task struct {
	ID            string      `json:"id"`
	AssistantID   string      `json:"assistant_id"`
	WorkspaceID   string      `json:"workspace_id"`
	Goal          string      `json:"goal"`
	PlanSteps     []string    `json:"plan_steps"`
	Operations    []Operation `json:"operations"`
	CurrentStep   int         `json:"current_step"`
	ApprovedStep  int         `json:"approved_step"`
	Priority      string      `json:"priority"`
	ApprovalState string      `json:"approval_state"`
	RetryState    string      `json:"retry_state"`
	Result        string      `json:"result"`
	Status        string      `json:"status"`
	CreatedAt     string      `json:"created_at"`
	UpdatedAt     string      `json:"updated_at"`
}
