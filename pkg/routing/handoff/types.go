package handoff

type HandoffRoutingEntry struct {
	SessionID           string
	UserInput           string
	PreferredSubagentID string
	SkipDelegation      bool
	Metadata            map[string]string
}

type HandoffRequest struct {
	SessionID           string
	UserInput           string
	PreferredSubagentID string
	SkipDelegation      bool
}

type PlanOptions struct {
	PersistentFirst bool
	AllowTemporary  bool
}

type PersistentMatch struct {
	AgentID     string
	DisplayName string
	Reason      string
}

type HandoffPlan struct {
	Mode          string
	TargetAgentID string
	SessionID     string
	Persistence   string
	Reason        string
}

type PersistentMatcher interface {
	Match(input string, preferred string) (PersistentMatch, bool)
}

type Request = HandoffRequest
type Plan = HandoffPlan
