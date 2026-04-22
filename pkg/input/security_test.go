package input

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/anyclaw/pkg/config"
)

func TestChannelPolicyWrapAllowsGroupMessagesWhenOnlyDMAllowListIsConfigured(t *testing.T) {
	policy := DefaultChannelPolicy()
	policy.SetDMPolicy(DMPolicyAllowList)
	policy.SetGroupPolicy(GroupPolicyAllowAll)
	policy.AddAllowedUser("dm-user")

	called := false
	wrapped := policy.Wrap(func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		called = true
		return sessionID, "ok", nil
	})

	_, _, err := wrapped(context.Background(), "session-1", "hello", map[string]string{
		"channel":    "discord",
		"guild_id":   "guild-1",
		"channel_id": "channel-1",
		"user_id":    "group-user",
	})
	if err != nil {
		t.Fatalf("expected group message to pass, got %v", err)
	}
	if !called {
		t.Fatal("expected wrapped handler to be called")
	}
}

func TestChannelPolicyWrapAppliesDMPolicyToTelegramPrivateChatFallback(t *testing.T) {
	policy := DefaultChannelPolicy()
	policy.SetDMPolicy(DMPolicyAllowList)
	policy.AddAllowedUser("allowed-user")

	wrapped := policy.Wrap(func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		t.Fatal("handler should not be called for blocked DM")
		return "", "", nil
	})

	_, _, err := wrapped(context.Background(), "session-1", "hello", map[string]string{
		"channel": "telegram",
		"chat_id": "42",
		"user_id": "42",
	})
	if err == nil {
		t.Fatal("expected DM policy to block unlisted user")
	}
	if !strings.Contains(err.Error(), "blocked by DM policy") {
		t.Fatalf("expected DM policy error, got %v", err)
	}
}

func TestChannelPolicyFromConfigAllowsExplicitFalseOverrides(t *testing.T) {
	var cfg config.ChannelSecurityConfig
	if err := json.Unmarshal([]byte(`{"mention_gate":false,"default_deny_dm":false}`), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	policy := ChannelPolicyFromConfig(cfg)

	if policy.MentionGateEnabled() {
		t.Fatal("expected mention gate to respect explicit false override")
	}
	if policy.DefaultDenyDM() {
		t.Fatal("expected default_deny_dm to respect explicit false override")
	}
}

func TestChannelPolicyFromConfigAppliesExplicitSettings(t *testing.T) {
	var cfg config.ChannelSecurityConfig
	if err := json.Unmarshal([]byte(`{
  "dm_policy":"pairing",
  "group_policy":"allow-list",
  "allow_from":[" alice ","bob"],
  "pairing_enabled":true,
  "pairing_ttl_hours":12,
  "mention_gate":true,
  "risk_acknowledged":true,
  "default_deny_dm":true
}`), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	policy := ChannelPolicyFromConfig(cfg)
	if policy.DMPolicy() != DMPolicyPairing {
		t.Fatalf("expected pairing DM policy, got %q", policy.DMPolicy())
	}
	if policy.GroupPolicy() != GroupPolicyAllowList {
		t.Fatalf("expected allow-list group policy, got %q", policy.GroupPolicy())
	}
	if !policy.PairingEnabled() {
		t.Fatal("expected pairing to be enabled from config")
	}
	if policy.PairingTTL() != 12*time.Hour {
		t.Fatalf("expected 12h pairing ttl, got %v", policy.PairingTTL())
	}
	if !policy.MentionGateEnabled() {
		t.Fatal("expected mention gate to be enabled from config")
	}
	if !policy.RiskAcknowledged() {
		t.Fatal("expected risk acknowledgement to be enabled from config")
	}
	if !policy.DefaultDenyDM() {
		t.Fatal("expected default deny dm to be enabled from config")
	}
	if !policy.AllowGroup("alice", "group-1", false) || !policy.AllowGroup("bob", "group-1", false) {
		t.Fatal("expected allow_from users to populate allow list from config")
	}
}

func TestChannelPolicyWrapBlocksUnpairedDMWhenPairingRequired(t *testing.T) {
	policy := DefaultChannelPolicy()
	policy.SetDMPolicy(DMPolicyPairing)
	policy.SetPairingEnabled(true)

	wrapped := policy.Wrap(func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		t.Fatal("handler should not be called for unpaired DM")
		return "", "", nil
	})

	_, _, err := wrapped(context.Background(), "session-1", "hello", map[string]string{
		"channel": "telegram",
		"chat_id": "42",
		"user_id": "42",
	})
	if err == nil {
		t.Fatal("expected unpaired DM to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked by DM policy") {
		t.Fatalf("expected DM policy error, got %v", err)
	}
}

func TestChannelPolicyWrapAllowsPairedDMWhenPairingRequired(t *testing.T) {
	policy := DefaultChannelPolicy()
	policy.SetDMPolicy(DMPolicyPairing)
	policy.SetPairingEnabled(true)

	meta := map[string]string{
		"channel": "telegram",
		"chat_id": "42",
		"user_id": "42",
	}
	policy.PairDM("42", meta)

	called := false
	wrapped := policy.Wrap(func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		called = true
		return sessionID, "ok", nil
	})

	_, _, err := wrapped(context.Background(), "session-1", "hello", meta)
	if err != nil {
		t.Fatalf("expected paired DM to pass, got %v", err)
	}
	if !called {
		t.Fatal("expected paired DM to reach handler")
	}
}

func TestChannelPolicyValidateReportsMisconfigurations(t *testing.T) {
	t.Run("invalid policies and ttl", func(t *testing.T) {
		policy := DefaultChannelPolicy()
		policy.SetDMPolicy(DMPolicy("broken"))
		policy.SetGroupPolicy(GroupPolicy("broken"))
		policy.SetPairingTTL(0)

		issues := strings.Join(policy.Validate(), "\n")
		if !strings.Contains(issues, `invalid dm_policy: "broken"`) {
			t.Fatalf("expected invalid dm policy issue, got %q", issues)
		}
		if !strings.Contains(issues, `invalid group_policy: "broken"`) {
			t.Fatalf("expected invalid group policy issue, got %q", issues)
		}
		if !strings.Contains(issues, "pairing_ttl must be positive") {
			t.Fatalf("expected pairing ttl issue, got %q", issues)
		}
	})

	t.Run("allow-list without users", func(t *testing.T) {
		policy := DefaultChannelPolicy()
		policy.SetDMPolicy(DMPolicyAllowList)
		issues := strings.Join(policy.Validate(), "\n")
		if !strings.Contains(issues, "allow_from is empty") {
			t.Fatalf("expected allow_from issue, got %q", issues)
		}
	})

	t.Run("pairing policy without pairing enabled", func(t *testing.T) {
		policy := DefaultChannelPolicy()
		policy.SetDMPolicy(DMPolicyPairing)
		issues := strings.Join(policy.Validate(), "\n")
		if !strings.Contains(issues, "pairing is disabled") {
			t.Fatalf("expected pairing disabled issue, got %q", issues)
		}
	})

	t.Run("allow-all without risk acknowledgement", func(t *testing.T) {
		policy := DefaultChannelPolicy()
		policy.SetDMPolicy(DMPolicyAllowAll)
		issues := strings.Join(policy.Validate(), "\n")
		if !strings.Contains(issues, "allow-all without risk acknowledgement") {
			t.Fatalf("expected risk acknowledgement issue, got %q", issues)
		}
	})
}

func TestChannelPolicyAccessorsAndAllowRules(t *testing.T) {
	policy := DefaultChannelPolicy()
	if !policy.IsUserAllowed("anyone") {
		t.Fatal("expected empty allow list to allow any user")
	}

	policy.AddAllowedUser(" alice ")
	if !policy.IsUserAllowed("alice") {
		t.Fatal("expected trimmed user to be allow-listed")
	}
	if policy.IsUserAllowed("bob") {
		t.Fatal("expected unrelated user to be blocked when allow list is populated")
	}
	if users := policy.AllowedUsers(); len(users) != 1 || users[0] != "alice" {
		t.Fatalf("unexpected allowed users: %v", users)
	}

	policy.RemoveAllowedUser("alice")
	if users := policy.AllowedUsers(); len(users) != 0 {
		t.Fatalf("expected allowed users to be empty after removal, got %v", users)
	}

	policy.SetDMPolicy(DMPolicyDenyAll)
	if policy.DMPolicy() != DMPolicyDenyAll || policy.AllowDM("alice") {
		t.Fatal("expected deny-all DM policy to block DM")
	}

	policy.SetDMPolicy(DMPolicyAllowAll)
	if !policy.AllowDM("alice") {
		t.Fatal("expected allow-all DM policy to allow DM")
	}

	policy.SetDMPolicy(DMPolicyAllowList)
	policy.AddAllowedUser("alice")
	if !policy.AllowDM("alice") || policy.AllowDM("bob") {
		t.Fatal("expected allow-list DM policy to only allow listed users")
	}

	pairMeta := map[string]string{
		"channel": "telegram",
		"chat_id": "42",
		"user_id": "42",
	}
	policy.SetDMPolicy(DMPolicyPairing)
	policy.SetPairingEnabled(true)
	policy.PairDM("42", pairMeta)
	if !policy.allowDM("42", pairMeta) {
		t.Fatal("expected pairing DM policy to allow paired user")
	}

	policy.mu.Lock()
	policy.dmPolicy = DMPolicy("custom")
	policy.defaultDenyDM = false
	policy.mu.Unlock()
	if !policy.AllowDM("someone") {
		t.Fatal("expected unknown DM policy to fall back to allow when default deny is disabled")
	}

	policy.SetGroupPolicy(GroupPolicyDenyAll)
	if policy.GroupPolicy() != GroupPolicyDenyAll || policy.AllowGroup("alice", "g1", true) {
		t.Fatal("expected deny-all group policy to block group message")
	}

	policy.SetGroupPolicy(GroupPolicyAllowAll)
	if !policy.AllowGroup("alice", "g1", false) {
		t.Fatal("expected allow-all group policy to allow group message")
	}

	policy.SetGroupPolicy(GroupPolicyAllowList)
	if !policy.AllowGroup("alice", "g1", false) || policy.AllowGroup("bob", "g1", false) {
		t.Fatal("expected allow-list group policy to only allow listed users")
	}

	policy.SetGroupPolicy(GroupPolicyMention)
	if !policy.AllowGroup("alice", "g1", true) || policy.AllowGroup("alice", "g1", false) {
		t.Fatal("expected mention-only group policy to require mention")
	}

	policy.SetMentionGate(false)
	if policy.MentionGateEnabled() {
		t.Fatal("expected mention gate to be disabled")
	}

	policy.SetPairingEnabled(true)
	if !policy.PairingEnabled() {
		t.Fatal("expected pairing to be enabled")
	}

	policy.SetPairingTTL(2 * time.Hour)
	if policy.PairingTTL() != 2*time.Hour {
		t.Fatalf("unexpected pairing ttl: %v", policy.PairingTTL())
	}

	if policy.RiskAcknowledged() {
		t.Fatal("expected risk acknowledgement to start false")
	}
	policy.AcknowledgeRisk()
	if !policy.RiskAcknowledged() {
		t.Fatal("expected risk acknowledgement to be true after acknowledgement")
	}
}

func TestChannelPolicyPairingLifecycle(t *testing.T) {
	policy := DefaultChannelPolicy()
	policy.SetPairingEnabled(true)
	policy.SetPairingTTL(time.Hour)

	meta := map[string]string{
		"channel": "telegram",
		"chat_id": "42",
		"user_id": "42",
	}
	policy.PairDM("42", meta)
	if !policy.IsDMPaired("42", meta) {
		t.Fatal("expected paired DM to be active")
	}
	policy.UnpairDM("42", meta)
	if policy.IsDMPaired("42", meta) {
		t.Fatal("expected unpaired DM to be removed")
	}

	policy.SetPairingTTL(-time.Minute)
	policy.PairDM("42", meta)
	if policy.IsDMPaired("42", meta) {
		t.Fatal("expected expired DM pairing to be treated as inactive")
	}

	policy.PairDM("", meta)
	policy.UnpairDM("", meta)

	if key := directPairingKey("", meta); key != "" {
		t.Fatalf("expected empty user ID to produce empty pairing key, got %q", key)
	}
}

func TestChannelPolicyWrapStreamAppliesPolicies(t *testing.T) {
	allowed := DefaultChannelPolicy()
	allowed.SetDMPolicy(DMPolicyAllowAll)

	called := false
	wrapped := allowed.WrapStream(func(ctx context.Context, sessionID string, message string, meta map[string]string, onChunk func(chunk string) error) (string, error) {
		called = true
		return sessionID, onChunk("ok")
	})

	var chunks []string
	sessionID, err := wrapped(context.Background(), "session-1", "hello", map[string]string{
		"channel": "telegram",
		"chat_id": "42",
		"user_id": "42",
	}, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("expected allow-all DM policy to pass stream handler, got %v", err)
	}
	if !called || sessionID != "session-1" || len(chunks) != 1 || chunks[0] != "ok" {
		t.Fatalf("unexpected stream result: called=%v session=%q chunks=%v", called, sessionID, chunks)
	}

	blocked := DefaultChannelPolicy()
	blocked.SetGroupPolicy(GroupPolicyMention)
	wrappedBlocked := blocked.WrapStream(func(ctx context.Context, sessionID string, message string, meta map[string]string, onChunk func(chunk string) error) (string, error) {
		t.Fatal("blocked group message should not reach handler")
		return "", nil
	})
	_, err = wrappedBlocked(context.Background(), "session-2", "hello group", map[string]string{
		"channel":    "slack",
		"channel_id": "C123456",
		"user_id":    "user-1",
	}, func(chunk string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "blocked by group policy") {
		t.Fatalf("expected blocked group policy error, got %v", err)
	}
}

func TestInferChannelPolicyContext(t *testing.T) {
	tests := []struct {
		name      string
		meta      map[string]string
		wantType  string
		wantGroup bool
		wantID    string
	}{
		{
			name:      "discord guild",
			meta:      map[string]string{"channel": "discord", "guild_id": "guild-1"},
			wantType:  "guild",
			wantGroup: true,
			wantID:    "guild-1",
		},
		{
			name:      "slack dm",
			meta:      map[string]string{"channel": "slack", "channel_id": "D123"},
			wantType:  "dm",
			wantGroup: false,
			wantID:    "",
		},
		{
			name:      "slack group",
			meta:      map[string]string{"channel": "slack", "channel_id": "C123"},
			wantType:  "group",
			wantGroup: true,
			wantID:    "C123",
		},
		{
			name:      "telegram supergroup",
			meta:      map[string]string{"channel": "telegram", "chat_type": "supergroup", "chat_id": "-1001"},
			wantType:  "supergroup",
			wantGroup: true,
			wantID:    "-1001",
		},
		{
			name:      "telegram private fallback",
			meta:      map[string]string{"channel": "telegram", "chat_id": "42", "user_id": "42"},
			wantType:  "private",
			wantGroup: false,
			wantID:    "",
		},
		{
			name:      "signal thread",
			meta:      map[string]string{"channel": "signal", "thread_id": "thread-1"},
			wantType:  "group",
			wantGroup: true,
			wantID:    "thread-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotGroup, gotID := inferChannelPolicyContext(tt.meta)
			if gotType != tt.wantType || gotGroup != tt.wantGroup || gotID != tt.wantID {
				t.Fatalf("unexpected context: got (%q,%v,%q), want (%q,%v,%q)", gotType, gotGroup, gotID, tt.wantType, tt.wantGroup, tt.wantID)
			}
		})
	}
}

func TestInferChannelPolicyContextFallbacks(t *testing.T) {
	channelType, isGroup, groupID := inferChannelPolicyContext(map[string]string{
		"channel_type": "group",
		"is_group":     "true",
		"channel_id":   "C999",
	})
	if channelType != "group" || !isGroup || groupID != "C999" {
		t.Fatalf("unexpected explicit group context: (%q,%v,%q)", channelType, isGroup, groupID)
	}

	channelType, isGroup, groupID = inferChannelPolicyContext(map[string]string{
		"channel": "discord",
	})
	if channelType != "private" || isGroup || groupID != "" {
		t.Fatalf("unexpected discord private fallback: (%q,%v,%q)", channelType, isGroup, groupID)
	}
}

func TestMentionIDsForPolicyAndDirectPairingKey(t *testing.T) {
	ids := mentionIDsForPolicy(map[string]string{
		"bot_user_id":     "BOT123",
		"bot_mention_ids": "BOT123, ALT456 ,",
	})
	if len(ids) != 3 || ids[0] != "BOT123" || ids[1] != "BOT123" || ids[2] != "ALT456" {
		t.Fatalf("unexpected mention IDs: %v", ids)
	}

	if key := directPairingKey("user-1", map[string]string{
		"channel":   "telegram",
		"device_id": "device-1",
	}); key != "telegram:user-1:device-1" {
		t.Fatalf("unexpected pairing key from device ID: %q", key)
	}

	if key := directPairingKey("user-1", map[string]string{
		"channel": "telegram",
		"chat_id": "42",
	}); key != "telegram:user-1:42" {
		t.Fatalf("unexpected pairing key from chat ID: %q", key)
	}

	if key := directPairingKey("user-1", map[string]string{
		"channel":    "slack",
		"channel_id": "C123",
	}); key != "slack:user-1:C123" {
		t.Fatalf("unexpected pairing key from channel ID: %q", key)
	}

	if key := directPairingKey("user-1", map[string]string{
		"thread_id": "thread-1",
	}); key != "direct:user-1:thread-1" {
		t.Fatalf("unexpected pairing key from thread ID: %q", key)
	}

	if key := directPairingKey("user-1", map[string]string{}); key != "direct:user-1:user-1" {
		t.Fatalf("unexpected fallback pairing key: %q", key)
	}
}

func TestAuditChannelPolicy(t *testing.T) {
	permissive := DefaultChannelPolicy()
	permissive.SetDMPolicy(DMPolicyAllowAll)
	permissive.SetGroupPolicy(GroupPolicyAllowAll)
	permissive.SetMentionGate(false)
	permissive.mu.Lock()
	permissive.defaultDenyDM = false
	permissive.mu.Unlock()

	result := AuditChannelPolicy(permissive)
	if result.Passed {
		t.Fatal("expected permissive policy to fail audit")
	}
	if result.Score >= result.MaxScore {
		t.Fatalf("expected permissive policy to lose score, got %d/%d", result.Score, result.MaxScore)
	}

	issueText := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		issueText = append(issueText, issue.ID)
	}
	joined := strings.Join(issueText, ",")
	for _, want := range []string{
		"dm-allow-all-no-ack",
		"dm-policy-permissive",
		"mention-gate-disabled",
		"group-policy-permissive",
		"no-default-deny",
		"risk-not-acknowledged",
		"no-allowlist",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected audit issue %q, got %v", want, issueText)
		}
	}

	hardened := DefaultChannelPolicy()
	hardened.SetDMPolicy(DMPolicyDenyAll)
	hardened.SetGroupPolicy(GroupPolicyMention)
	hardened.SetMentionGate(true)
	hardened.AcknowledgeRisk()

	result = AuditChannelPolicy(hardened)
	if !result.Passed {
		t.Fatalf("expected hardened policy to pass audit, got %+v", result)
	}
	if result.Score != result.MaxScore {
		t.Fatalf("expected full audit score, got %d/%d", result.Score, result.MaxScore)
	}
}
