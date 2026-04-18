package config

import "strings"

// RoutingConfig contains the channel routing rules consumed by M2.
type RoutingConfig struct {
	Mode  string               `json:"mode"`
	Rules []ChannelRoutingRule `json:"rules"`
}

// ChannelRoutingRule configures one channel-specific routing rule.
type ChannelRoutingRule struct {
	Channel     string `json:"channel"`
	Match       string `json:"match"`
	SessionMode string `json:"session_mode"`
	SessionID   string `json:"session_id"`
	QueueMode   string `json:"queue_mode"`
	ReplyBack   *bool  `json:"reply_back,omitempty"`
	TitlePrefix string `json:"title_prefix"`
	// Deprecated: ingress routing no longer selects agents directly.
	Agent string `json:"agent,omitempty"`
	// Deprecated: ingress routing no longer selects workspaces directly.
	Workspace string `json:"workspace,omitempty"`
	// Deprecated: ingress routing no longer selects orgs directly.
	Org string `json:"org,omitempty"`
	// Deprecated: ingress routing no longer selects projects directly.
	Project string `json:"project,omitempty"`
	// Deprecated: ingress routing no longer selects workspace refs directly.
	WorkspaceRef string `json:"workspace_ref,omitempty"`
}

var mainAgentAliasReplacer = strings.NewReplacer("-", "", "_", "", " ", "")

func normalizeAgentAlias(name string) string {
	return mainAgentAliasReplacer.Replace(strings.ToLower(strings.TrimSpace(name)))
}

// IsMainAgentAlias reports whether a requested agent name should resolve to the main agent.
func IsMainAgentAlias(name string) bool {
	switch normalizeAgentAlias(name) {
	case "main", "default", "mainagent", "defaultagent":
		return true
	default:
		return false
	}
}
