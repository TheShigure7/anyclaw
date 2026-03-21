package audit

type Event struct {
	ID               string `json:"id"`
	TaskID           string `json:"task_id"`
	Actor            string `json:"actor"`
	Action           string `json:"action"`
	Target           string `json:"target"`
	RiskLevel        string `json:"risk_level"`
	ConfirmationMode string `json:"confirmation_mode"`
	ToolName         string `json:"tool_name"`
	Result           string `json:"result"`
	Timestamp        string `json:"timestamp"`
}
