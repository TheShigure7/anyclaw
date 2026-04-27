package cliadapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInitRegistersBuiltinHandlers(t *testing.T) {
	restoreDefaults := saveDefaults()
	t.Cleanup(restoreDefaults)

	root := writeCLIRegistry(t, `{"clis":[]}`)
	if err := Init(root); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if GetRegistry() == nil {
		t.Fatal("expected default registry")
	}
	if GetExecutor() == nil {
		t.Fatal("expected default executor")
	}

	output, err := Exec(context.Background(), "echo", []string{"hello", "cli"})
	if err != nil {
		t.Fatalf("Exec echo: %v", err)
	}
	if output != "hello cli" {
		t.Fatalf("echo output = %q, want hello cli", output)
	}
}

func TestSearchAndListCategoriesAfterInit(t *testing.T) {
	restoreDefaults := saveDefaults()
	t.Cleanup(restoreDefaults)

	root := writeCLIRegistry(t, `{"clis":[
		{"name":"git","display_name":"Git","description":"Version control","category":"dev"}
	]}`)
	if err := Init(root); err != nil {
		t.Fatalf("Init: %v", err)
	}

	results, err := Search("git", "dev", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Name != "git" {
		t.Fatalf("Search results = %+v, want git", results)
	}

	categories := ListCategories()
	if categories["dev"] != 1 {
		t.Fatalf("categories = %+v, want dev count 1", categories)
	}
}

func TestInitFromEnvFindsRegistryRoot(t *testing.T) {
	restoreDefaults := saveDefaults()
	t.Cleanup(restoreDefaults)

	root := writeCLIRegistry(t, `{"clis":[]}`)
	t.Setenv("ANYCLAW_CLIADAPTER_ROOT", root)

	if err := InitFromEnv(); err != nil {
		t.Fatalf("InitFromEnv: %v", err)
	}
	if GetRegistry() == nil || GetRegistry().Root() != root {
		t.Fatalf("registry root = %v, want %q", GetRegistry(), root)
	}
}

func TestInitFromEnvWithoutRegistryIsNoop(t *testing.T) {
	restoreDefaults := saveDefaults()
	t.Cleanup(restoreDefaults)
	defaultRegistry = nil
	defaultExecutor = nil
	t.Chdir(t.TempDir())
	t.Setenv("ANYCLAW_CLIADAPTER_ROOT", "")

	if err := InitFromEnv(); err != nil {
		t.Fatalf("InitFromEnv: %v", err)
	}
	if GetRegistry() != nil || GetExecutor() != nil {
		t.Fatal("expected defaults to remain nil")
	}
}

func TestInitReturnsRegistryError(t *testing.T) {
	restoreDefaults := saveDefaults()
	t.Cleanup(restoreDefaults)

	if err := Init(t.TempDir()); err == nil {
		t.Fatal("expected missing registry error")
	}
}

func TestDiscoverRoot(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "CLI-Anything")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "registry.json"), []byte(`{"clis":[]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	found, ok := DiscoverRoot(root)
	if !ok || found != nested {
		t.Fatalf("DiscoverRoot = (%q, %v), want (%q, true)", found, ok, nested)
	}
}

func TestDiscoverRootWalksAncestorsFromNestedStart(t *testing.T) {
	root := t.TempDir()
	cliRoot := filepath.Join(root, "CLI-Anything-0.2.0")
	start := filepath.Join(root, "workspace", "project", "nested")
	if err := os.MkdirAll(cliRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll cli root: %v", err)
	}
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatalf("MkdirAll start: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cliRoot, "registry.json"), []byte(`{"clis":[]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	found, ok := DiscoverRoot(start)
	if !ok || found != cliRoot {
		t.Fatalf("DiscoverRoot nested = (%q, %v), want (%q, true)", found, ok, cliRoot)
	}
}

func TestCLIRootCandidates(t *testing.T) {
	base := filepath.Clean("/tmp/project")
	got := cliRootCandidates(base)
	want := []string{
		base,
		filepath.Join(base, "CLI-Anything-0.2.0"),
		filepath.Join(base, "CLI-Anything"),
	}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDiscoverRootMissing(t *testing.T) {
	if found, ok := DiscoverRoot(t.TempDir()); ok || found != "" {
		t.Fatalf("DiscoverRoot missing = (%q, %v), want empty false", found, ok)
	}
}

func TestUninitializedHelpersAreSafe(t *testing.T) {
	restoreDefaults := saveDefaults()
	t.Cleanup(restoreDefaults)
	defaultRegistry = nil
	defaultExecutor = nil

	if GetRegistry() != nil || GetExecutor() != nil {
		t.Fatal("expected nil defaults")
	}
	if results, err := Search("", "", 10); err != nil || results != nil {
		t.Fatalf("Search without init = (%v, %v), want nil, nil", results, err)
	}
	if output, err := Exec(context.Background(), "echo", nil); err != nil || output != "" {
		t.Fatalf("Exec without init = (%q, %v), want empty, nil", output, err)
	}
	if categories := ListCategories(); categories != nil {
		t.Fatalf("ListCategories without init = %v, want nil", categories)
	}
}

func writeCLIRegistry(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

func saveDefaults() func() {
	previousRegistry := defaultRegistry
	previousExecutor := defaultExecutor
	return func() {
		defaultRegistry = previousRegistry
		defaultExecutor = previousExecutor
	}
}
