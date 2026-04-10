package agenthub

import (
	"context"
	"strings"
	"testing"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/config"
	"github.com/anyclaw/anyclaw/pkg/llm"
	"github.com/anyclaw/anyclaw/pkg/memory"
	"github.com/anyclaw/anyclaw/pkg/skills"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type stubLLM struct{}

func (s *stubLLM) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.Response, error) {
	for _, msg := range messages {
		if strings.Contains(msg.Content, "Persistent subagent specialized in code review") {
			return &llm.Response{Content: "resident response"}, nil
		}
		if strings.Contains(msg.Content, "internal temporary subagent") {
			return &llm.Response{Content: "transient response"}, nil
		}
	}
	return &llm.Response{Content: "main response"}, nil
}

func (s *stubLLM) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition, onChunk func(string)) error {
	resp, err := s.Chat(ctx, messages, tools)
	if err != nil {
		return err
	}
	if onChunk != nil {
		onChunk(resp.Content)
	}
	return nil
}

func (s *stubLLM) Name() string { return "stub" }

func newFileMemoryBackend(t *testing.T) memory.MemoryBackend {
	t.Helper()
	mem, err := memory.NewMemoryBackend(memory.Config{
		Backend: memory.BackendFile,
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewMemoryBackend: %v", err)
	}
	if err := mem.Init(); err != nil {
		t.Fatalf("Init memory: %v", err)
	}
	t.Cleanup(func() {
		_ = mem.Close()
	})
	return mem
}

func TestMainControllerUsesPersistentSubagentWhenMatched(t *testing.T) {
	llmClient := &stubLLM{}
	mainMemory := newFileMemoryBackend(t)
	registry, err := NewPersistentSubagentRegistry(config.PersistentSubagentsConfig{
		Enabled: true,
		Profiles: []config.PersistentSubagentProfile{{
			ID:              "code-reviewer",
			DisplayName:     "Code Reviewer",
			SystemPrompt:    "Persistent subagent specialized in code review",
			Domain:          "code review",
			Expertise:       []string{"review", "regression"},
			PermissionLevel: "limited",
		}},
	}, PersistentSubagentRegistryOptions{
		ConfigPath:        "anyclaw.json",
		DefaultWorkingDir: t.TempDir(),
		LLM:               llmClient,
		BaseSkills:        skills.NewSkillsManager(""),
		BaseTools:         tools.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("NewPersistentSubagentRegistry: %v", err)
	}
	t.Cleanup(func() {
		registry.Close()
	})

	controller := NewMainController(MainControllerOptions{
		MainAgentName:        "Main",
		MainAgentDescription: "Main agent",
		MainPersonality:      "Main assistant",
		LLM:                  llmClient,
		Memory:               mainMemory,
		Skills:               skills.NewSkillsManager(""),
		Tools:                tools.NewRegistry(),
		Delegation: config.DelegationConfig{
			PersistentSubagentFirst: true,
		},
		PersistentSubagents: registry,
	})

	result, err := controller.Run(context.Background(), RunRequest{
		SessionID: "sess-1",
		UserInput: "please review this change for regression risk",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Source != "persistent_subagent" {
		t.Fatalf("expected persistent_subagent source, got %q", result.Source)
	}
	if result.Content != "I asked Code Reviewer to handle this.\n\nresident response" {
		t.Fatalf("expected persistent subagent response, got %q", result.Content)
	}
	if len(result.DelegationTrace) != 1 || result.DelegationTrace[0].AgentID != "code-reviewer" || result.DelegationTrace[0].Kind != "persistent_subagent" {
		t.Fatalf("expected persistent subagent delegation trace, got %#v", result.DelegationTrace)
	}
}

func TestMainControllerClearsSessionState(t *testing.T) {
	controller := NewMainController(MainControllerOptions{
		MainAgentName:        "Main",
		MainAgentDescription: "Main agent",
		MainPersonality:      "Main assistant",
		LLM:                  &stubLLM{},
		Memory:               newFileMemoryBackend(t),
		Skills:               skills.NewSkillsManager(""),
		Tools:                tools.NewRegistry(),
		Delegation:           config.DelegationConfig{},
	})

	if _, err := controller.Run(context.Background(), RunRequest{
		SessionID: "sess-2",
		UserInput: "hello",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	controller.ClearSession("sess-2")

	result, err := controller.Run(context.Background(), RunRequest{
		SessionID: "sess-2",
		UserInput: "hello again",
	})
	if err != nil {
		t.Fatalf("Run after clear: %v", err)
	}
	if result.Content != "main response" {
		t.Fatalf("expected main response after clear, got %q", result.Content)
	}
}

func TestMainControllerFallsBackToTemporarySubagentWhenNoPersistentSubagentMatches(t *testing.T) {
	llmClient := &stubLLM{}
	controller := NewMainController(MainControllerOptions{
		MainAgentName:        "Main",
		MainAgentDescription: "Main agent",
		MainPersonality:      "Main assistant",
		LLM:                  llmClient,
		Memory:               newFileMemoryBackend(t),
		Skills:               skills.NewSkillsManager(""),
		Tools:                tools.NewRegistry(),
		Delegation: config.DelegationConfig{
			PersistentSubagentFirst: true,
			AllowTemporarySubagents: true,
		},
		TemporarySubagents: NewTemporarySubagentManager(TemporarySubagentManagerOptions{
			LLM:             llmClient,
			Memory:          newFileMemoryBackend(t),
			BaseSkills:      skills.NewSkillsManager(""),
			BaseTools:       tools.NewRegistry(),
			PermissionLevel: "limited",
		}),
	})

	result, err := controller.Run(context.Background(), RunRequest{
		SessionID: "sess-3",
		UserInput: "please analyze this ambiguous task",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Source != "temporary_subagent" {
		t.Fatalf("expected temporary_subagent source, got %q", result.Source)
	}
	if result.Content != "transient response" {
		t.Fatalf("expected temporary subagent response, got %q", result.Content)
	}
	if len(result.DelegationTrace) != 1 || result.DelegationTrace[0].Kind != "temporary_subagent" {
		t.Fatalf("expected temporary subagent delegation trace, got %#v", result.DelegationTrace)
	}
}

var _ agent.LLMCaller = (*stubLLM)(nil)
