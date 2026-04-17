package handoff

import "testing"

type stubPersistentMatcher struct {
	match PersistentMatch
	ok    bool
}

func (s stubPersistentMatcher) Match(input string, preferred string) (PersistentMatch, bool) {
	return s.match, s.ok
}

func TestPrepareProjectsRoutingEntryIntoRequest(t *testing.T) {
	router := NewRouter(nil)
	req := router.Prepare(HandoffRoutingEntry{
		SessionID:           "sess-1",
		UserInput:           "review this diff",
		PreferredSubagentID: "reviewer",
		SkipDelegation:      true,
		Metadata:            map[string]string{"ignored": "true"},
	})
	if req.SessionID != "sess-1" || req.UserInput != "review this diff" || req.PreferredSubagentID != "reviewer" || !req.SkipDelegation {
		t.Fatalf("unexpected request projection: %#v", req)
	}
}

func TestPlanUsesPersistentSubagentWhenPolicyAllowsIt(t *testing.T) {
	router := NewRouter(stubPersistentMatcher{
		match: PersistentMatch{AgentID: "reviewer", Reason: "matched expertise"},
		ok:    true,
	})
	plan := router.Plan(HandoffRequest{
		SessionID:           "sess-2",
		UserInput:           "review this change",
		PreferredSubagentID: "reviewer",
	}, PlanOptions{PersistentFirst: true})
	if plan.Mode != "persistent_subagent" || plan.TargetAgentID != "reviewer" || plan.Persistence != "persistent_runtime" {
		t.Fatalf("unexpected persistent handoff plan: %#v", plan)
	}
}

func TestPlanFallsBackToTemporarySubagentWhenEnabled(t *testing.T) {
	router := NewRouter(stubPersistentMatcher{})
	plan := router.Plan(HandoffRequest{
		SessionID: "sess-3",
		UserInput: "ambiguous task",
	}, PlanOptions{PersistentFirst: true, AllowTemporary: true})
	if plan.Mode != "temporary_subagent" || plan.Persistence != "temporary_runtime" {
		t.Fatalf("unexpected temporary handoff plan: %#v", plan)
	}
}

func TestPlanReturnsMainWhenDelegationIsSkipped(t *testing.T) {
	router := NewRouter(stubPersistentMatcher{
		match: PersistentMatch{AgentID: "reviewer", Reason: "matched expertise"},
		ok:    true,
	})
	plan := router.Plan(HandoffRequest{
		SessionID:      "sess-4",
		UserInput:      "handle locally",
		SkipDelegation: true,
	}, PlanOptions{PersistentFirst: true, AllowTemporary: true})
	if plan.Mode != "main" || plan.Persistence != "main_runtime" {
		t.Fatalf("unexpected main handoff plan: %#v", plan)
	}
}
