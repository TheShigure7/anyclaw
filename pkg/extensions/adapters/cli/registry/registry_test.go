package cliadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryLoadsSearchesAndReturnsCopies(t *testing.T) {
	root := writeRegistry(t, []Entry{
		{Name: "git", DisplayName: "Git", Description: "Version control", Category: "dev", EntryPoint: "git"},
		{Name: "docker", DisplayName: "Docker", Description: "Container runtime", Category: "ops", EntryPoint: "docker"},
	})

	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if got := registry.EntriesCount(); got != 2 {
		t.Fatalf("EntriesCount = %d, want 2", got)
	}
	if got := registry.Categories()["dev"]; got != 1 {
		t.Fatalf("dev category count = %d, want 1", got)
	}

	entry, ok := registry.Get("GIT")
	if !ok {
		t.Fatal("expected case-insensitive get")
	}
	entry.Name = "mutated"
	entry.Installed = true

	again, ok := registry.Get("git")
	if !ok {
		t.Fatal("expected git entry")
	}
	if again.Name != "git" || again.Installed {
		t.Fatalf("registry entry was mutated through returned copy: %+v", again)
	}

	list := registry.List()
	if len(list) != 2 || list[0].Name != "docker" || list[1].Name != "git" {
		t.Fatalf("List order = %+v, want docker then git", list)
	}

	search := registry.Search("", "", 1)
	if len(search) != 1 || search[0].Name != "docker" {
		t.Fatalf("Search limit result = %+v, want docker", search)
	}

	found, ok := registry.Find("Docker")
	if !ok || found.Name != "docker" {
		t.Fatalf("Find by display name = (%+v, %v), want docker", found, ok)
	}

	registry.MarkInstalled("git", "/usr/bin/git")
	installed, _ := registry.Get("git")
	if !installed.Installed || installed.ExecutablePath != "/usr/bin/git" {
		t.Fatalf("installed git = %+v", installed)
	}
}

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	root := writeRegistry(t, []Entry{
		{Name: "git", DisplayName: "Git", Category: "dev"},
		{Name: "GIT", DisplayName: "Git Duplicate", Category: "dev"},
	})

	if _, err := NewRegistry(root); err == nil {
		t.Fatal("expected duplicate registry entry error")
	}
}

func TestBuiltinAdaptersReturnCopies(t *testing.T) {
	resetBuiltinAdapters(t)
	RegisterBuiltinAdapter("echo", "Echo", "utility", func(args []string) (string, error) {
		return "", nil
	})

	adapter, ok := GetBuiltinAdapter("ECHO")
	if !ok {
		t.Fatal("expected builtin adapter")
	}
	adapter.Name = "mutated"

	again, ok := GetBuiltinAdapter("echo")
	if !ok {
		t.Fatal("expected builtin adapter")
	}
	if again.Name != "echo" {
		t.Fatalf("builtin adapter mutated through returned copy: %+v", again)
	}

	list := ListBuiltinAdapters()
	if len(list) != 1 || list[0].Name != "echo" {
		t.Fatalf("builtin adapter list = %+v", list)
	}
}

func writeRegistry(t *testing.T, entries []Entry) string {
	t.Helper()
	root := t.TempDir()
	data, err := json.Marshal(map[string]any{
		"meta": map[string]any{
			"repo":        "test",
			"description": "test registry",
			"updated":     "2026-04-27",
		},
		"clis": entries,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "registry.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

func resetBuiltinAdapters(t *testing.T) {
	t.Helper()
	builtinAdaptersMu.Lock()
	previous := builtinAdapters
	builtinAdapters = map[string]*builtinAdapter{}
	builtinAdaptersMu.Unlock()

	t.Cleanup(func() {
		builtinAdaptersMu.Lock()
		builtinAdapters = previous
		builtinAdaptersMu.Unlock()
	})
}
