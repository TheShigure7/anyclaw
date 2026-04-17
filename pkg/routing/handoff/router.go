package handoff

type Router struct {
	Persistent PersistentMatcher
}

func NewRouter(persistent PersistentMatcher) *Router {
	return &Router{Persistent: persistent}
}

func (r *Router) Prepare(entry HandoffRoutingEntry) HandoffRequest {
	return HandoffRequest{
		SessionID:           entry.SessionID,
		UserInput:           entry.UserInput,
		PreferredSubagentID: entry.PreferredSubagentID,
		SkipDelegation:      entry.SkipDelegation,
	}
}

func (r *Router) Plan(req HandoffRequest, options PlanOptions) HandoffPlan {
	if req.SkipDelegation {
		return HandoffPlan{
			Mode:        "main",
			SessionID:   req.SessionID,
			Persistence: "main_runtime",
			Reason:      "delegation skipped",
		}
	}
	if options.PersistentFirst && r != nil && r.Persistent != nil {
		if match, ok := r.Persistent.Match(req.UserInput, req.PreferredSubagentID); ok {
			return HandoffPlan{
				Mode:          "persistent_subagent",
				TargetAgentID: match.AgentID,
				SessionID:     req.SessionID,
				Persistence:   "persistent_runtime",
				Reason:        match.Reason,
			}
		}
	}
	if options.AllowTemporary {
		return HandoffPlan{
			Mode:        "temporary_subagent",
			SessionID:   req.SessionID,
			Persistence: "temporary_runtime",
			Reason:      "fallback to temporary subagent",
		}
	}
	return HandoffPlan{
		Mode:        "main",
		SessionID:   req.SessionID,
		Persistence: "main_runtime",
		Reason:      "main agent",
	}
}
