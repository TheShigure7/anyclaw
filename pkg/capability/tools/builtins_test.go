package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellCommandWithShellAuto(t *testing.T) {
	cmd, err := shellCommandWithShell(context.Background(), "echo hello", "auto")
	if err != nil {
		t.Fatalf("shellCommandWithShell(auto) returned error: %v", err)
	}
	if len(cmd.Args) == 0 {
		t.Fatalf("expected command args")
	}
	if runtime.GOOS == "windows" && cmd.Args[0] != "cmd" {
		t.Fatalf("expected cmd on windows, got %q", cmd.Args[0])
	}
	if runtime.GOOS != "windows" && cmd.Args[0] != "sh" {
		t.Fatalf("expected sh on non-windows, got %q", cmd.Args[0])
	}
}

func TestShellCommandWithShellRejectsUnsupportedShell(t *testing.T) {
	if _, err := shellCommandWithShell(context.Background(), "echo hello", "fish"); err == nil {
		t.Fatal("expected unsupported shell error")
	}
}

func TestReviewCommandExecutionRequiresSandboxByDefault(t *testing.T) {
	err := reviewCommandExecution("echo hello", "", BuiltinOptions{ExecutionMode: "sandbox"})
	if err == nil {
		t.Fatal("expected sandbox-only mode to deny host execution without sandbox")
	}
}

func TestWriteFileToolWithPolicyBlocksProtectedPath(t *testing.T) {
	tempDir := t.TempDir()
	protected := filepath.Join(tempDir, "private")
	if err := os.MkdirAll(protected, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := WriteFileToolWithPolicy(context.Background(), map[string]any{
		"path":    filepath.Join(protected, "secret.txt"),
		"content": "x",
	}, tempDir, BuiltinOptions{
		PermissionLevel: "full",
		ProtectedPaths:  []string{protected},
	})
	if err == nil {
		t.Fatal("expected protected path write to be denied")
	}
}

func TestReadFileToolWithPolicyBlocksOutsideWorkingDir(t *testing.T) {
	workspace := t.TempDir()
	outsideDir := t.TempDir()
	target := filepath.Join(outsideDir, "notes.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := ReadFileToolWithPolicy(context.Background(), map[string]any{
		"path": target,
	}, workspace, BuiltinOptions{
		WorkingDir: workspace,
		Policy:     NewPolicyEngine(PolicyOptions{WorkingDir: workspace}),
	})
	if err == nil {
		t.Fatal("expected read outside working directory to be denied")
	}
}

func TestReviewCommandExecutionBlocksProtectedPathReference(t *testing.T) {
	tempDir := t.TempDir()
	protected := filepath.Join(tempDir, "Documents")
	if err := os.MkdirAll(protected, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := reviewCommandExecution("type "+filepath.Join(protected, "secret.txt"), "", BuiltinOptions{
		ExecutionMode: "host-reviewed",
		ProtectedPaths: []string{
			protected,
		},
	})
	if err == nil {
		t.Fatal("expected command referencing protected path to be denied")
	}
}

func TestReviewCommandExecutionAllowsExplicitlyAllowedProtectedPathReference(t *testing.T) {
	tempDir := t.TempDir()
	protected := filepath.Join(tempDir, "Desktop")
	if err := os.MkdirAll(protected, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := reviewCommandExecution("mkdir "+filepath.Join(protected, "hello"), "", BuiltinOptions{
		ExecutionMode:  "host-reviewed",
		ProtectedPaths: []string{protected},
		AllowedWritePaths: []string{
			protected,
		},
	})
	if err != nil {
		t.Fatalf("expected explicitly allowed protected path reference to pass review, got %v", err)
	}
}

func TestRunCommandToolWithPolicyBlocksOutsideWorkingDirCwd(t *testing.T) {
	workspace := t.TempDir()
	outsideDir := t.TempDir()

	_, err := RunCommandToolWithPolicy(context.Background(), map[string]any{
		"command": "echo hello",
		"cwd":     outsideDir,
	}, BuiltinOptions{
		WorkingDir:      workspace,
		ExecutionMode:   "host-reviewed",
		PermissionLevel: "limited",
		Policy:          NewPolicyEngine(PolicyOptions{WorkingDir: workspace, PermissionLevel: "limited"}),
	})
	if err == nil {
		t.Fatal("expected command cwd outside working directory to be denied")
	}
}

func TestEnsureDesktopAllowedRequiresHostReviewed(t *testing.T) {
	err := ensureDesktopAllowed("desktop_click", BuiltinOptions{ExecutionMode: "sandbox", PermissionLevel: "limited"}, false)
	if err == nil {
		t.Fatal("expected desktop tool to require host-reviewed mode")
	}
}

func TestMemoryToolsSearchAndGetDailyFiles(t *testing.T) {
	workspace := t.TempDir()
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "2026-03-29.md"), []byte("# Daily Memory 2026-03-29\n\nRelease checklist completed."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	registry := NewRegistry()
	RegisterBuiltins(registry, BuiltinOptions{WorkingDir: workspace})

	searchResult, err := registry.Call(context.Background(), "memory_search", map[string]any{"query": "checklist"})
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	if !strings.Contains(searchResult, "2026-03-29") {
		t.Fatalf("expected search result to mention date, got %q", searchResult)
	}

	getResult, err := registry.Call(context.Background(), "memory_get", map[string]any{"date": "2026-03-29"})
	if err != nil {
		t.Fatalf("memory_get: %v", err)
	}
	if !strings.Contains(getResult, "Release checklist completed.") {
		t.Fatalf("expected memory_get output, got %q", getResult)
	}
}

func TestWebSearchToolWithPolicyRejectsDisallowedEgress(t *testing.T) {
	policy := NewPolicyEngine(PolicyOptions{
		WorkingDir:           t.TempDir(),
		AllowedEgressDomains: []string{"example.com"},
	})

	_, err := WebSearchToolWithPolicy(context.Background(), map[string]any{
		"query": "golang",
	}, BuiltinOptions{Policy: policy})
	if err == nil {
		t.Fatal("expected web search to be denied when search provider host is not allowlisted")
	}
}

func TestFetchURLToolWithPolicyRejectsDisallowedEgress(t *testing.T) {
	policy := NewPolicyEngine(PolicyOptions{
		WorkingDir:           t.TempDir(),
		AllowedEgressDomains: []string{"example.com"},
	})

	_, err := FetchURLToolWithPolicy(context.Background(), map[string]any{
		"url": "https://openai.com",
	}, BuiltinOptions{Policy: policy})
	if err == nil {
		t.Fatal("expected fetch_url to be denied when target host is not allowlisted")
	}
}

func TestRegisterToolDoesNotCacheByDefault(t *testing.T) {
	registry := NewRegistry()
	callCount := 0
	registry.RegisterTool("side_effect", "demo", map[string]any{}, func(_ context.Context, _ map[string]any) (string, error) {
		callCount++
		return strings.Repeat("x", callCount), nil
	})

	first, err := registry.Call(context.Background(), "side_effect", map[string]any{"id": 1})
	if err != nil {
		t.Fatalf("first Call: %v", err)
	}
	second, err := registry.Call(context.Background(), "side_effect", map[string]any{"id": 1})
	if err != nil {
		t.Fatalf("second Call: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected handler to run twice without default caching, got %d", callCount)
	}
	if first == second {
		t.Fatalf("expected different results from repeated side-effecting calls, got %q and %q", first, second)
	}
}

func TestQMDInsertGeneratesUniqueIDs(t *testing.T) {
	client := &stubQMDClient{}
	input := map[string]any{
		"data": map[string]any{
			"name": "demo",
		},
	}

	if _, err := qmdInsert(context.Background(), client, "tasks", input); err != nil {
		t.Fatalf("first qmdInsert: %v", err)
	}
	if _, err := qmdInsert(context.Background(), client, "tasks", input); err != nil {
		t.Fatalf("second qmdInsert: %v", err)
	}

	if len(client.insertedIDs) != 2 {
		t.Fatalf("expected two inserted IDs, got %d", len(client.insertedIDs))
	}
	if client.insertedIDs[0] == client.insertedIDs[1] {
		t.Fatalf("expected generated QMD IDs to differ, got %q", client.insertedIDs[0])
	}
}

type stubQMDClient struct {
	insertedIDs []string
}

func (s *stubQMDClient) CreateTable(context.Context, string, []string) error {
	return nil
}

func (s *stubQMDClient) Insert(_ context.Context, _ string, record map[string]any) error {
	if id, _ := record["id"].(string); id != "" {
		s.insertedIDs = append(s.insertedIDs, id)
	}
	return nil
}

func (s *stubQMDClient) Get(context.Context, string, string) (map[string]any, error) {
	return nil, nil
}

func (s *stubQMDClient) Update(context.Context, string, map[string]any) error {
	return nil
}

func (s *stubQMDClient) Delete(context.Context, string, string) error {
	return nil
}

func (s *stubQMDClient) List(context.Context, string, int) ([]map[string]any, error) {
	return nil, nil
}

func (s *stubQMDClient) Query(context.Context, string, string, any, int) ([]map[string]any, error) {
	return nil, nil
}

func (s *stubQMDClient) ListTables(context.Context) ([]TableStat, error) {
	return nil, nil
}

func (s *stubQMDClient) Count(context.Context, string) (int, error) {
	return 0, nil
}
