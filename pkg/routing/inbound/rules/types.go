package rules

type RouteRequest struct {
	Channel  string
	Source   string
	Text     string
	ThreadID string
	IsGroup  bool
	GroupID  string
}

type RouteDecision struct {
	Key         string `json:"key"`
	SessionMode string `json:"session_mode"`
	SessionID   string `json:"session_id,omitempty"`
	QueueMode   string `json:"queue_mode,omitempty"`
	ReplyBack   bool   `json:"reply_back,omitempty"`
	Title       string `json:"title,omitempty"`
	MatchedRule string `json:"matched_rule,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Org         string `json:"org,omitempty"`
	Project     string `json:"project,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	IsThread    bool   `json:"is_thread,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
}
